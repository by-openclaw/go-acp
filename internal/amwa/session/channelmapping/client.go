// Package channelmapping is the IS-08 Audio Channel Mapping API CLIENT —
// the half a Controller needs to re-route audio channels inside a
// Device.
//
// The provider half (internal/amwa/provider/channelmapping.go) serves
// this API; nothing consumed it. Same layering as session/connection:
// HTTP mechanics only. The IS-08 payload shapes and their validation
// live in codec/is08, and the decision of what to map lives in
// consumer/. This package just moves bytes.
package channelmapping

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"dhs/internal/amwa/codec/is08"
)

// Client speaks IS-08 to one Device's Channel Mapping API.
//
// Base is the href IS-04 advertised in the Device's `controls` array
// under urn:x-nmos:control:cm-ctrl/vX.Y — the spec's discovery
// mechanism, never a host guess.
type Client struct {
	HTTP   *http.Client
	Base   string // e.g. "http://10.6.255.103:3000/x-nmos/channelmapping/v1.0"
	APIVer string
}

// NewClient builds a Client from an IS-08 control href. The href
// carries the /x-nmos/channelmapping/<ver> prefix, and the version is
// read back out of it so the two can never disagree.
func NewClient(controlHref string) (*Client, error) {
	base := strings.TrimRight(controlHref, "/")
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("nmos/channelmapping: parse %q: %w", controlHref, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("nmos/channelmapping: %q must be an absolute URL", controlHref)
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	ver := segs[len(segs)-1]
	if !strings.HasPrefix(ver, "v") {
		return nil, fmt.Errorf("nmos/channelmapping: %q does not end in an api_ver "+
			"(expected .../x-nmos/channelmapping/v1.0)", controlHref)
	}
	return &Client{
		HTTP:   &http.Client{Timeout: 30 * time.Second},
		Base:   base,
		APIVer: ver,
	}, nil
}

// IO reads the Device's inputs and outputs in one view: what can be
// routed, to where, and under which constraints (caps).
func (c *Client) IO(ctx context.Context) (is08.IO, error) {
	raw, err := c.send(ctx, http.MethodGet, "io", nil)
	if err != nil {
		return is08.IO{}, err
	}
	return is08.DecodeIO(raw)
}

// Active reads the map the Device is applying right now.
func (c *Client) Active(ctx context.Context) (is08.MapActive, error) {
	raw, err := c.send(ctx, http.MethodGet, "map/active", nil)
	if err != nil {
		return is08.MapActive{}, err
	}
	return is08.DecodeMapActive(raw)
}

// Activations reads the scheduled activations still waiting for their
// instant, keyed by activation id.
func (c *Client) Activations(ctx context.Context) (map[string]is08.MapActivationResponse, error) {
	raw, err := c.send(ctx, http.MethodGet, "map/activations", nil)
	if err != nil {
		return nil, err
	}
	return decodeActivations(raw)
}

// Activate posts a re-map. The Device answers with the activation it
// created, keyed by its id: applied at once for an immediate request
// (scheduled false), or queued for its instant (scheduled true).
func (c *Client) Activate(ctx context.Context, req is08.MapActivationRequest) (id string, resp is08.MapActivationResponse, scheduled bool, err error) {
	body, err := is08.EncodeMapActivationRequest(req)
	if err != nil {
		return "", is08.MapActivationResponse{}, false, err
	}
	raw, code, err := c.do(ctx, http.MethodPost, "map/activations", body)
	if err != nil {
		return "", is08.MapActivationResponse{}, false, err
	}
	created, err := decodeActivations(raw)
	if err != nil {
		return "", is08.MapActivationResponse{}, false, err
	}
	if len(created) != 1 {
		return "", is08.MapActivationResponse{}, false,
			fmt.Errorf("nmos/channelmapping: the Device answered %d activation(s) to one request", len(created))
	}
	for id, resp = range created {
	}
	return id, resp, code == http.StatusAccepted, nil
}

// Cancel withdraws a scheduled activation before its instant.
func (c *Client) Cancel(ctx context.Context, id string) error {
	_, err := c.send(ctx, http.MethodDelete, "map/activations/"+id, nil)
	return err
}

func decodeActivations(raw []byte) (map[string]is08.MapActivationResponse, error) {
	var out map[string]is08.MapActivationResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("nmos/channelmapping: decode activations: %w", err)
	}
	for id, a := range out {
		if err := is08.ValidateMapActivationResponse(a); err != nil {
			return nil, fmt.Errorf("nmos/channelmapping: activation %s: %w", id, err)
		}
	}
	return out, nil
}

func (c *Client) send(ctx context.Context, method, rest string, body []byte) ([]byte, error) {
	raw, _, err := c.do(ctx, method, rest, body)
	return raw, err
}

// do sends the request and turns a non-2xx into an error carrying the
// Device's own message: IS-08 devices explain a refused map in the body
// ("input in1 is not routable to output out2"), and a bare status code
// makes that impossible to act on from the CLI.
func (c *Client) do(ctx context.Context, method, rest string, body []byte) ([]byte, int, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+"/"+rest, rd)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("nmos/channelmapping: %s: %w", rest, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode, &StatusError{What: rest, Code: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
	}
	return raw, resp.StatusCode, nil
}

// StatusError is a non-2xx answer from the Device, kept typed so a
// caller can tell a refused map from a missing endpoint with errors.As.
type StatusError struct {
	What string
	Code int
	Body string
}

func (e *StatusError) Error() string {
	msg := e.Body
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	if msg == "" {
		return fmt.Sprintf("nmos/channelmapping: %s: HTTP %d", e.What, e.Code)
	}
	return fmt.Sprintf("nmos/channelmapping: %s: HTTP %d: %s", e.What, e.Code, msg)
}
