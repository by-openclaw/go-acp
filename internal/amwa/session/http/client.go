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

// GetJSONPage is GetJSON plus the one response header a Query API
// client cannot live without: the RFC 8288 Link header. IS-04 §6.2
// paginates collections and points at the next page via
// `Link: <url>; rel="next"` — a client that ignores it sees exactly
// one page and silently reports a 2-sender registry when the paging
// limit is 2 (the AMWA IS-04-04 suite sets precisely that trap).
//
// Returns the rel="next" target, or "" when the server sent none.
func (c *Client) GetJSONPage(ctx context.Context, url string, dst any) (string, error) {
	if dst == nil {
		return "", fmt.Errorf("nmos/http: GetJSONPage: dst must not be nil")
	}
	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("nmos/http: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("nmos/http: GET %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != stdhttp.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, c.MaxBody))
		return "", fmt.Errorf("nmos/http: GET %s: HTTP %d: %s",
			url, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	max := c.MaxBody
	if max <= 0 {
		max = DefaultMaxBody
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return "", fmt.Errorf("nmos/http: read body: %w", err)
	}
	if int64(len(body)) > max {
		return "", fmt.Errorf("nmos/http: response body exceeds %d bytes", max)
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return "", fmt.Errorf("nmos/http: decode body: %w", err)
	}
	return linkRel(resp.Header.Values("Link"), "next"), nil
}

// GetJSONPageLinks is GetJSONPage returning BOTH pagination cursors.
//
// IS-04 §6.1.6 orientation (pinned by the AMWA suite): collections
// are newest-first and the Link cursors move through TIME —
// rel="next" points at NEWER data, rel="prev" at OLDER. A client
// walking a whole collection therefore starts at the head and follows
// PREV; following next from the head asks for the future and legally
// returns nothing. Both targets are surfaced so the caller can pick
// its direction; "" when the server sent none.
func (c *Client) GetJSONPageLinks(ctx context.Context, url string, dst any) (next, prev string, err error) {
	// Delegate the fetch to GetJSONPage's body handling by re-doing the
	// header extraction here would mean two requests — so this is the
	// primary implementation and GetJSONPage keeps its shape above.
	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodGet, url, nil)
	if err != nil {
		return "", "", fmt.Errorf("nmos/http: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("nmos/http: GET %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != stdhttp.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, c.MaxBody))
		return "", "", fmt.Errorf("nmos/http: GET %s: HTTP %d: %s",
			url, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	max := c.MaxBody
	if max <= 0 {
		max = DefaultMaxBody
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return "", "", fmt.Errorf("nmos/http: read body: %w", err)
	}
	if int64(len(body)) > max {
		return "", "", fmt.Errorf("nmos/http: response body exceeds %d bytes", max)
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return "", "", fmt.Errorf("nmos/http: decode %s: %w", url, err)
	}
	links := resp.Header.Values("Link")
	return linkRel(links, "next"), linkRel(links, "prev"), nil
}

// linkRel extracts the target of the first Link entry carrying
// rel="<rel>". Handles both repeated Link headers and the
// comma-joined single-header form.
func linkRel(headers []string, rel string) string {
	want := `rel="` + rel + `"`
	for _, h := range headers {
		for _, part := range strings.Split(h, ",") {
			seg := strings.Split(part, ";")
			if len(seg) < 2 {
				continue
			}
			target := strings.Trim(strings.TrimSpace(seg[0]), "<>")
			for _, attr := range seg[1:] {
				if strings.ReplaceAll(strings.TrimSpace(attr), "'", `"`) == want {
					return target
				}
			}
		}
	}
	return ""
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
