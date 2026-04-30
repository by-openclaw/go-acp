package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	stdhttp "net/http"
	"strings"
)

// PostJSON issues a POST with a JSON body and decodes the response
// into dst. status is the actual response status code (caller decides
// whether 200/201 is acceptable); err is non-nil only on transport /
// decode failure, NOT on a non-2xx response.
//
// dst may be nil — useful when the caller only cares about status.
// When non-nil it must be a pointer; the body is decoded with
// DisallowUnknownFields so spec deviations surface loudly.
func (c *Client) PostJSON(ctx context.Context, url string, src, dst any) (int, error) {
	body, err := json.Marshal(src)
	if err != nil {
		return 0, fmt.Errorf("nmos/http: marshal body: %w", err)
	}
	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("nmos/http: build POST: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, fmt.Errorf("nmos/http: POST %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	max := c.MaxBody
	if max <= 0 {
		max = DefaultMaxBody
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return resp.StatusCode, fmt.Errorf("nmos/http: read POST body: %w", err)
	}
	if int64(len(raw)) > max {
		return resp.StatusCode, fmt.Errorf("nmos/http: POST response exceeds %d bytes", max)
	}

	if dst == nil {
		return resp.StatusCode, nil
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return resp.StatusCode, nil
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return resp.StatusCode, fmt.Errorf("nmos/http: decode POST response: %w", err)
	}
	return resp.StatusCode, nil
}
