package http

import (
	"bytes"
	"context"
	"fmt"
	"io"
	stdhttp "net/http"
	"time"

	"dhs/internal/transport"
)

// NewTLSClient returns a Client whose TLS posture is opts. It is the one
// place an HTTP client's *http.Transport is assembled, so a connector
// names a posture and never builds a transport of its own. A zero
// timeout means DefaultTimeout.
func NewTLSClient(opts transport.TLSOptions, timeout time.Duration) (*Client, error) {
	cfg, err := opts.Client()
	if err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Client{
		HTTP: &stdhttp.Client{
			Timeout:   timeout,
			Transport: &stdhttp.Transport{TLSClientConfig: cfg},
		},
		MaxBody: DefaultMaxBody,
	}, nil
}

// Exchange issues one request and returns the status and the body as
// they came — for an API whose answers the typed helpers cannot read:
// any method, a body on a non-2xx answer that the caller must decode,
// headers of its own.
//
// headers are written under the exact names given, not canonicalised:
// a peer that matches header names case-sensitively is a peer all the
// same, and net/http would send "Reqid" for "reqid". body may be nil; a
// request without one still declares Content-Length 0.
//
// err is non-nil only on transport failure or an over-long body, NOT on
// a non-2xx status — a server that answers 404 has answered. The
// exchange is counted and captured like every other one.
func (c *Client) Exchange(ctx context.Context, method, url string, headers map[string]string, body []byte) (int, []byte, error) {
	var rd io.Reader = stdhttp.NoBody
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := stdhttp.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return 0, nil, fmt.Errorf("http: build %s: %w", method, err)
	}
	if err := c.applyAuth(ctx, req); err != nil {
		return 0, nil, err
	}
	for name, value := range headers {
		req.Header[name] = []string{value}
	}

	resp, err := c.do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("http: %s %s: %w", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	max := c.MaxBody
	if max <= 0 {
		max = DefaultMaxBody
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("http: read %s body: %w", method, err)
	}
	if int64(len(raw)) > max {
		return resp.StatusCode, nil, fmt.Errorf("http: %s response exceeds %d bytes", method, max)
	}
	return resp.StatusCode, raw, nil
}
