// Package streamcompat is the IS-11 Stream Compatibility Management API
// CLIENT — the half a Controller needs to see why a Sender and a
// Receiver do not agree, and to constrain a Sender until they do.
//
// The provider half (internal/amwa/provider/streamcompat.go) serves
// this API; nothing consumed it. Same layering as session/connection:
// HTTP mechanics only. The IS-11 payload shapes and their validation
// live in codec/is11.
package streamcompat

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

	"dhs/internal/amwa/codec/is11"
)

// Client speaks IS-11 to one Device's Stream Compatibility API.
//
// Base is the href IS-04 advertised in the Device's `controls` array
// under urn:x-nmos:control:stream-compat/vX.Y.
type Client struct {
	HTTP   *http.Client
	Base   string // e.g. "http://10.6.255.102:3000/x-nmos/streamcompatibility/v1.0"
	APIVer string
}

// NewClient builds a Client from an IS-11 control href; the version is
// read back out of the href so the two can never disagree.
func NewClient(controlHref string) (*Client, error) {
	base := strings.TrimRight(controlHref, "/")
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("nmos/streamcompat: parse %q: %w", controlHref, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("nmos/streamcompat: %q must be an absolute URL", controlHref)
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	ver := segs[len(segs)-1]
	if !strings.HasPrefix(ver, "v") {
		return nil, fmt.Errorf("nmos/streamcompat: %q does not end in an api_ver "+
			"(expected .../x-nmos/streamcompatibility/v1.0)", controlHref)
	}
	return &Client{
		HTTP:   &http.Client{Timeout: 30 * time.Second},
		Base:   base,
		APIVer: ver,
	}, nil
}

// Resource kinds the API lists.
const (
	KindSenders   = "senders"
	KindReceivers = "receivers"
	KindInputs    = "inputs"
	KindOutputs   = "outputs"
)

// List returns the ids of one kind of resource, or the ids one resource
// refers to (a Sender's inputs, a Receiver's outputs). IS-11 lists are
// arrays of "<id>/" path segments; the trailing slash is dropped.
func (c *Client) List(ctx context.Context, path string) ([]string, error) {
	raw, err := c.do(ctx, http.MethodGet, strings.Trim(path, "/")+"/", "", nil)
	if err != nil {
		return nil, err
	}
	var segs []string
	if err := json.Unmarshal(raw, &segs); err != nil {
		return nil, fmt.Errorf("nmos/streamcompat: decode %s: %w", path, err)
	}
	for i, s := range segs {
		segs[i] = strings.TrimSuffix(s, "/")
	}
	return segs, nil
}

// Status reads the state of a Sender or a Receiver: whether the stream
// it emits honours its constraints, or the stream it takes is one it
// can decode. kind is KindSenders or KindReceivers.
func (c *Client) Status(ctx context.Context, kind, id string) (is11.Status, error) {
	raw, err := c.do(ctx, http.MethodGet, kind+"/"+id+"/status/", "", nil)
	if err != nil {
		return is11.Status{}, err
	}
	var st is11.Status
	if err := json.Unmarshal(raw, &st); err != nil {
		return is11.Status{}, fmt.Errorf("nmos/streamcompat: decode %s status: %w", id, err)
	}
	if err := is11.ValidateStatus(strings.TrimSuffix(kind, "s"), st); err != nil {
		return is11.Status{}, err
	}
	return st, nil
}

// Input reads an Input's properties.
func (c *Client) Input(ctx context.Context, id string) (is11.Input, error) {
	raw, err := c.do(ctx, http.MethodGet, "inputs/"+id+"/properties/", "", nil)
	if err != nil {
		return is11.Input{}, err
	}
	return is11.DecodeInput(raw)
}

// Output reads an Output's properties.
func (c *Client) Output(ctx context.Context, id string) (is11.Output, error) {
	raw, err := c.do(ctx, http.MethodGet, "outputs/"+id+"/properties/", "", nil)
	if err != nil {
		return is11.Output{}, err
	}
	return is11.DecodeOutput(raw)
}

// ActiveConstraints reads the constraint sets a Sender is held to.
func (c *Client) ActiveConstraints(ctx context.Context, senderID string) (is11.ActiveConstraints, error) {
	raw, err := c.do(ctx, http.MethodGet, "senders/"+senderID+"/constraints/active/", "", nil)
	if err != nil {
		return is11.ActiveConstraints{}, err
	}
	return is11.DecodeActiveConstraints(raw)
}

// SupportedConstraints reads the parameter-constraint URNs a Sender
// understands: what a controller may constrain it on.
func (c *Client) SupportedConstraints(ctx context.Context, senderID string) (is11.SupportedConstraints, error) {
	raw, err := c.do(ctx, http.MethodGet, "senders/"+senderID+"/constraints/supported/", "", nil)
	if err != nil {
		return is11.SupportedConstraints{}, err
	}
	var sc is11.SupportedConstraints
	if err := json.Unmarshal(raw, &sc); err != nil {
		return is11.SupportedConstraints{}, fmt.Errorf("nmos/streamcompat: decode supported constraints: %w", err)
	}
	return sc, nil
}

// PutActiveConstraints holds a Sender to the given constraint sets and
// returns what the Device says it now applies.
func (c *Client) PutActiveConstraints(ctx context.Context, senderID string, ac is11.ActiveConstraints) (is11.ActiveConstraints, error) {
	body, err := is11.EncodeActiveConstraints(ac)
	if err != nil {
		return is11.ActiveConstraints{}, err
	}
	raw, err := c.do(ctx, http.MethodPut, "senders/"+senderID+"/constraints/active/", "application/json", body)
	if err != nil {
		return is11.ActiveConstraints{}, err
	}
	return is11.DecodeActiveConstraints(raw)
}

// DeleteActiveConstraints releases a Sender from its constraints.
func (c *Client) DeleteActiveConstraints(ctx context.Context, senderID string) error {
	_, err := c.do(ctx, http.MethodDelete, "senders/"+senderID+"/constraints/active/", "", nil)
	return err
}

// EDID kinds an Input serves; an Output serves one, named "".
const (
	EDIDBase      = "base"
	EDIDEffective = "effective"
)

// EDID reads an EDID as the bytes the Device serves. For an Input,
// which is EDIDBase (what a controller put there) or EDIDEffective
// (what the upstream unit is shown); for an Output it is "".
func (c *Client) EDID(ctx context.Context, kind, id, which string) ([]byte, error) {
	path := kind + "/" + id + "/edid/"
	if which != "" {
		path += which + "/"
	}
	return c.do(ctx, http.MethodGet, path, "", nil)
}

// PutBaseEDID replaces an Input's Base EDID.
func (c *Client) PutBaseEDID(ctx context.Context, inputID string, edid []byte) error {
	_, err := c.do(ctx, http.MethodPut, "inputs/"+inputID+"/edid/base/", "application/octet-stream", edid)
	return err
}

// DeleteBaseEDID restores an Input's default EDID.
func (c *Client) DeleteBaseEDID(ctx context.Context, inputID string) error {
	_, err := c.do(ctx, http.MethodDelete, "inputs/"+inputID+"/edid/base/", "", nil)
	return err
}

// do sends the request and turns a non-2xx into an error carrying the
// Device's own message: an IS-11 Device explains a refused constraint
// set in the body, and a bare status code is nothing to act on.
func (c *Client) do(ctx context.Context, method, rest, contentType string, body []byte) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+"/"+rest, rd)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nmos/streamcompat: %s: %w", rest, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &StatusError{What: rest, Code: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
	}
	return raw, nil
}

// StatusError is a non-2xx answer from the Device, kept typed so a
// caller can tell a refusal from a missing endpoint with errors.As.
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
		return fmt.Sprintf("nmos/streamcompat: %s: HTTP %d", e.What, e.Code)
	}
	return fmt.Sprintf("nmos/streamcompat: %s: HTTP %d: %s", e.What, e.Code, msg)
}
