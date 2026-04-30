package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	stdhttp "net/http"
	"strings"
	"time"
)

// DefaultMaxBody caps a single response body. NMOS payloads are
// generally small; the IS-04 Query API can return larger node
// catalogues but those land later. 1 MiB is comfortable for IS-09.
const DefaultMaxBody = 1 << 20

// DefaultTimeout caps a single HTTP exchange.
const DefaultTimeout = 5 * time.Second

// Client is a thin typed-GET wrapper around net/http.Client.
type Client struct {
	HTTP    *stdhttp.Client
	MaxBody int64
}

// NewClient returns a Client with sensible defaults (per-call timeout
// applied via context, body cap = DefaultMaxBody). Callers can mutate
// the returned struct before first use.
func NewClient() *Client {
	return &Client{
		HTTP: &stdhttp.Client{
			Timeout: DefaultTimeout,
		},
		MaxBody: DefaultMaxBody,
	}
}

// GetJSON issues a GET against url, validates Content-Type is
// application/json, reads up to MaxBody bytes, then decodes into dst
// (a non-nil pointer). dst is decoded with DisallowUnknownFields so
// peers that emit non-spec keys are flagged rather than absorbed.
func (c *Client) GetJSON(ctx context.Context, url string, dst any) error {
	if dst == nil {
		return fmt.Errorf("nmos/http: GetJSON: dst must not be nil")
	}
	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("nmos/http: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("nmos/http: GET %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != stdhttp.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, c.MaxBody))
		return fmt.Errorf("nmos/http: GET %s: HTTP %d: %s",
			url, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	ct := resp.Header.Get("Content-Type")
	if !isJSONContentType(ct) {
		return fmt.Errorf("nmos/http: GET %s: unexpected Content-Type %q (want application/json)", url, ct)
	}

	max := c.MaxBody
	if max <= 0 {
		max = DefaultMaxBody
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return fmt.Errorf("nmos/http: read body: %w", err)
	}
	if int64(len(body)) > max {
		return fmt.Errorf("nmos/http: response body exceeds %d bytes", max)
	}

	d := json.NewDecoder(strings.NewReader(string(body)))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return fmt.Errorf("nmos/http: decode body: %w", err)
	}
	if d.More() {
		return fmt.Errorf("nmos/http: trailing JSON content in body")
	}
	return nil
}

// isJSONContentType matches `application/json` and the `; charset=...`
// suffix variant. Case-insensitive per RFC 7231 §3.1.1.5.
func isJSONContentType(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if ct == "application/json" {
		return true
	}
	return strings.HasPrefix(ct, "application/json;")
}
