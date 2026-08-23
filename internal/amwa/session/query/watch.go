package query

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"dhs/internal/amwa/codec/is04"
	amwahttp "dhs/internal/amwa/session/http"
)

// Subscription is the resource a Registry returns from POST /subscriptions.
// WSHref is the WebSocket endpoint to dial for the change stream.
type Subscription struct {
	ID            string         `json:"id"`
	WSHref        string         `json:"ws_href"`
	ResourcePath  string         `json:"resource_path"`
	Params        map[string]any `json:"params"`
	Persist       bool           `json:"persist"`
	MaxUpdateRate int            `json:"max_update_rate_ms"`
	Secure        bool           `json:"secure,omitempty"`
}

// SubscribeRequest asks a Registry to open a subscription.
//
// ResourcePath is the collection to watch, with leading and trailing slashes
// as the spec writes them: "/nodes/", "/senders/", "/receivers/", …
type SubscribeRequest struct {
	ResourcePath  string
	Params        map[string]string
	Persist       bool
	MaxUpdateRate int // milliseconds; 0 lets the Registry choose
}

// ErrNoWSHref is returned when a Registry answers a subscription without the
// endpoint to dial — the response is then useless to a Controller, so it is a
// failure rather than something to work around.
var ErrNoWSHref = errors.New("query: subscription response has no ws_href")

// Subscribe opens (or re-uses) a Query API subscription.
//
// A Registry may answer 200 with an existing subscription instead of 201 when
// the same request has been made before; both are success, per IS-04.
func (c *Client) Subscribe(ctx context.Context, req SubscribeRequest) (*Subscription, error) {
	// IS-04's subscription schema enumerates resource_path WITHOUT a trailing
	// slash ("/nodes", "/senders", ...). Accept either spelling from callers
	// and always put the spec form on the wire — a Registry that matches on
	// the exact string silently returns nothing for "/nodes/".
	path := strings.TrimSpace(req.ResourcePath)
	if path == "" {
		return nil, errors.New("query: resource_path is required")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if len(path) > 1 {
		path = strings.TrimSuffix(path, "/")
	}

	body := map[string]any{
		"max_update_rate_ms": req.MaxUpdateRate,
		"resource_path":      path,
		"params":             map[string]string{},
		"persist":            req.Persist,
	}
	if len(req.Params) > 0 {
		body["params"] = req.Params
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("query: encode subscription: %w", err)
	}

	url := c.urlFor("subscriptions")
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("query: build subscription request: %w", err)
	}
	hreq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("query: POST %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query: POST %s: unexpected status %d", url, resp.StatusCode)
	}

	var sub Subscription
	if err := json.NewDecoder(resp.Body).Decode(&sub); err != nil {
		return nil, fmt.Errorf("query: decode subscription: %w", err)
	}
	if sub.WSHref == "" {
		return nil, ErrNoWSHref
	}
	return &sub, nil
}

// GrainFunc is called once per received grain. Returning an error stops the
// watch and Watch returns that error.
type GrainFunc func(*is04.Grain) error

// WatchOptions tunes a Watch call.
type WatchOptions struct {
	// ReadTimeout bounds a single frame read. Zero means wait indefinitely,
	// which is what a low-traffic plant needs — an idle subscription is
	// normal, not a fault.
	ReadTimeout time.Duration
}

// Watch dials a subscription's WebSocket and streams grains to fn until the
// context is cancelled, the peer closes, or fn returns an error.
//
// The first frame a Registry sends after connect is a SYNC burst carrying the
// current state of the collection; subsequent frames are changes. Callers that
// only want changes can skip rows whose Pre and Post are equal.
func Watch(ctx context.Context, wsHref string, fn GrainFunc, opts WatchOptions) error {
	if wsHref == "" {
		return ErrNoWSHref
	}
	if fn == nil {
		return errors.New("query: Watch needs a GrainFunc")
	}

	ws, err := amwahttp.DialWebSocket(ctx, wsHref, nil)
	if err != nil {
		return fmt.Errorf("query: dial %s: %w", wsHref, err)
	}
	defer func() { _ = ws.Close() }()

	// Close the socket when the context ends so a blocked ReadText returns.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = ws.Close()
		case <-done:
		}
	}()

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if opts.ReadTimeout > 0 {
			if err := ws.SetReadDeadline(time.Now().Add(opts.ReadTimeout)); err != nil {
				return fmt.Errorf("query: set read deadline: %w", err)
			}
		}
		frame, err := ws.ReadText()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("query: read frame: %w", err)
		}
		g, err := is04.DecodeGrain(frame)
		if err != nil {
			// A non-grain frame is a peer deviation, not a reason to drop the
			// stream: keep watching and let the caller see the rest.
			if errors.Is(err, is04.ErrNotGrain) {
				continue
			}
			return err
		}
		if err := fn(g); err != nil {
			return err
		}
	}
}
