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
// Defaults for a 24/7 subscription. A quiet plant is normal, so liveness
// cannot be inferred from grain traffic; it is established by our own pings,
// and only then is a read deadline safe.
const (
	DefaultKeepAlive   = 30 * time.Second
	DefaultReadTimeout = 90 * time.Second
)

type WatchOptions struct {
	// ReadTimeout bounds how long the peer may be silent before the read
	// fails. It is a LIVENESS bound, not a traffic bound: KeepAlive pings
	// and their pongs re-arm it, so an idle-but-healthy subscription never
	// trips it. Zero takes DefaultReadTimeout; negative disables it.
	//
	// It used to default to "wait indefinitely" on the grounds that an idle
	// subscription is normal. It is — but "indefinitely" also covers a
	// half-open socket, and then a 24/7 watch parks forever: connected to
	// nothing, reporting nothing, never failing.
	ReadTimeout time.Duration

	// KeepAlive is the client-side ping cadence that proves the peer is
	// still there on a quiet subscription. Zero takes DefaultKeepAlive;
	// negative disables pinging.
	KeepAlive time.Duration
}

// resolve expands the zero/negative sentinels into concrete durations.
func (o WatchOptions) resolve() (readTimeout, keepAlive time.Duration) {
	switch {
	case o.ReadTimeout < 0:
		readTimeout = 0
	case o.ReadTimeout == 0:
		readTimeout = DefaultReadTimeout
	default:
		readTimeout = o.ReadTimeout
	}
	switch {
	case o.KeepAlive < 0:
		keepAlive = 0
	case o.KeepAlive == 0:
		keepAlive = DefaultKeepAlive
	default:
		keepAlive = o.KeepAlive
	}
	return readTimeout, keepAlive
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

	readTimeout, keepAlive := opts.resolve()
	// The deadline lives in the socket and is re-armed by the WS layer on
	// every inbound frame, so a pong counts as liveness just like a grain.
	ws.SetIdleTimeout(readTimeout)

	// Client-side keep-alive. A Registry with nothing to report sends
	// nothing, so without our own pings there is no way to tell a quiet
	// subscription from a dead one.
	if keepAlive > 0 {
		ticker := time.NewTicker(keepAlive)
		defer ticker.Stop()
		go func() {
			for {
				select {
				case <-done:
					return
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := ws.SendPing(nil); err != nil {
						// The socket is gone; closing makes the blocked
						// ReadText return instead of waiting out the deadline.
						_ = ws.Close()
						return
					}
				}
			}
		}()
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
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
