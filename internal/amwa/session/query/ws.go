package query

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	httpsession "dhs/internal/amwa/session/http"
)

// subscriptionRequest is the body POSTed to /subscriptions (IS-04 §5).
type subscriptionRequest struct {
	MaxUpdateRate int            `json:"max_update_rate_ms"`
	Persist       bool           `json:"persist"`
	Secure        bool           `json:"secure"`
	ResourcePath  string         `json:"resource_path"`
	Params        map[string]any `json:"params"`
}

// subscriptionResponse mirrors the full IS-04 subscription resource.
// Every field, not just the two we read: the shared PostJSON decodes
// with DisallowUnknownFields, so a partial struct rejects a
// spec-correct reply.
type subscriptionResponse struct {
	ID            string `json:"id"`
	WSHref        string `json:"ws_href"`
	MaxUpdateRate int    `json:"max_update_rate_ms"`
	Persist       bool   `json:"persist"`
	Secure        bool   `json:"secure"`
	ResourcePath  string `json:"resource_path"`
	Params        any    `json:"params"`
	Authorization bool   `json:"authorization"`
}

// wsGrain is the client-side slice of the IS-04 §5.2 grain envelope.
// The server side has its own full struct in internal/amwa/registry;
// the two do not share code because codec-layer rules forbid the
// session layer importing a plugin.
type wsGrain struct {
	Grain struct {
		Type  string `json:"type"`
		Topic string `json:"topic"`
		Data  []struct {
			Path string          `json:"path"`
			Pre  json.RawMessage `json:"pre"`
			Post json.RawMessage `json:"post"`
		} `json:"data"`
	} `json:"grain"`
}

// ListViaSubscription enumerates one collection through a Query API
// WebSocket subscription instead of the paged REST collection.
//
// This is not an optimisation, it is the ONLY enumeration mechanism
// IS-04 v1.0 fully specifies for dynamic state: REST pagination
// (Link / X-Paging-*) arrived in v1.1, so a v1.0 Query API with more
// resources than its page size has no REST way to show them all. On
// subscription the server MUST send SYNC grains carrying the current
// state of every matching resource — that snapshot is the listing.
//
// plural is "senders", "receivers", etc. The subscription is
// non-persistent, so closing the socket is the cleanup.
func (c *Client) ListViaSubscription(ctx context.Context, plural string) ([]json.RawMessage, error) {
	subURL := c.urlFor("subscriptions")
	req := subscriptionRequest{
		MaxUpdateRate: 100,
		Persist:       false,
		Secure:        false,
		ResourcePath:  "/" + plural,
		Params:        map[string]any{},
	}
	var sub subscriptionResponse
	status, err := c.HTTP.PostJSON(ctx, subURL, req, &sub)
	if err != nil {
		return nil, fmt.Errorf("nmos/query: subscribe %s: %w", plural, err)
	}
	// 201 = created, 200 = an equivalent subscription already existed.
	// Both carry the resource.
	if status != 200 && status != 201 {
		return nil, fmt.Errorf("nmos/query: subscribe %s: HTTP %d", plural, status)
	}
	if sub.WSHref == "" {
		return nil, fmt.Errorf("nmos/query: subscribe %s: response carries no ws_href", plural)
	}

	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ws, err := httpsession.DialWebSocket(dctx, sub.WSHref, nil)
	if err != nil {
		return nil, fmt.Errorf("nmos/query: dial %s: %w", sub.WSHref, err)
	}
	defer func() { _ = ws.Close() }()

	// The SYNC snapshot arrives as one or more grains immediately after
	// the upgrade. There is no in-band "end of snapshot" marker, so we
	// read until the stream goes quiet: 800ms of silence after at least
	// one grain, or 5s total for a registry with nothing to say.
	byID := map[string]json.RawMessage{}
	deadline := time.Now().Add(5 * time.Second)
	got := false
	for time.Now().Before(deadline) {
		wait := 800 * time.Millisecond
		if !got {
			wait = time.Until(deadline)
		}
		_ = ws.SetReadDeadline(time.Now().Add(wait))
		txt, err := ws.ReadText()
		if err != nil {
			break // quiet period, close, or timeout — snapshot complete
		}
		var g wsGrain
		if err := json.Unmarshal(txt, &g); err != nil {
			continue // a frame we don't understand is not fatal
		}
		topic := strings.Trim(g.Grain.Topic, "/")
		if topic != plural {
			continue
		}
		for _, row := range g.Grain.Data {
			if len(row.Post) > 0 && string(row.Post) != "null" {
				byID[row.Path] = row.Post
				got = true
			}
		}
		if got {
			deadline = time.Now().Add(800 * time.Millisecond)
		}
	}

	out := make([]json.RawMessage, 0, len(byID))
	for _, v := range byID {
		out = append(out, v)
	}
	return out, nil
}
