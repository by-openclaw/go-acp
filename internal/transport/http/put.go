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

// PutJSON issues a PUT with a JSON body and decodes the response into
// dst. It is the write half of a read-modify-write REST client: the
// caller GETs a resource, changes one field, and PUTs the whole document
// back — the shape every emSFP / Fusion resource expects (a partial body
// is rejected with 400, key not found).
//
// status is the actual response status code; the caller decides which
// 2xx it accepts. err is non-nil only on transport / decode failure, NOT
// on a non-2xx response — a device that answers 400 has answered.
//
// dst may be nil when only the status matters. When non-nil it must be
// a pointer; the body is decoded with DisallowUnknownFields so a device
// that grew a field is noticed, not silently dropped.
//
// Unlike PostJSON this goes through do(), so a PUT is counted in the
// connector metrics (tx bytes + round-trip, rx bytes) exactly like a GET:
// a consumer that only ever writes still reports.
func (c *Client) PutJSON(ctx context.Context, url string, src, dst any) (int, error) {
	return c.writeJSON(ctx, stdhttp.MethodPut, url, src, dst)
}

// PatchJSON issues a PATCH with a JSON body: the partial write CCM
// 0v1 §11.2 makes mandatory, carrying only the fields named so two
// clients never overwrite each other's. Same contract as PutJSON.
func (c *Client) PatchJSON(ctx context.Context, url string, src, dst any) (int, error) {
	return c.writeJSON(ctx, stdhttp.MethodPatch, url, src, dst)
}

func (c *Client) writeJSON(ctx context.Context, method, url string, src, dst any) (int, error) {
	body, err := json.Marshal(src)
	if err != nil {
		return 0, fmt.Errorf("http: marshal body: %w", err)
	}
	req, err := stdhttp.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("http: build %s: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if err := c.applyAuth(ctx, req); err != nil {
		return 0, err
	}

	resp, err := c.do(req)
	if err != nil {
		return 0, fmt.Errorf("http: %s %s: %w", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	max := c.MaxBody
	if max <= 0 {
		max = DefaultMaxBody
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return resp.StatusCode, fmt.Errorf("http: read %s body: %w", method, err)
	}
	if int64(len(raw)) > max {
		return resp.StatusCode, fmt.Errorf("http: %s response exceeds %d bytes", method, max)
	}

	if dst == nil || len(bytes.TrimSpace(raw)) == 0 {
		return resp.StatusCode, nil
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return resp.StatusCode, fmt.Errorf("http: decode %s response: %w", method, err)
	}
	return resp.StatusCode, nil
}
