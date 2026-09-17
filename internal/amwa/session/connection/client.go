// Package connection is the IS-05 Connection API CLIENT — the half a
// Controller needs to actually route signal.
//
// The provider half (internal/amwa/provider/connection*.go) has served
// this API for a while; nothing consumed it. A Controller that can only
// LIST resources is a browser, not a control surface, so this is what
// turns `dhs consumer nmos` into a controller.
//
// Layer 2 (session): HTTP mechanics only. The IS-05 payload shapes and
// their validation live in codec/is05, and the decision of what to
// route lives in consumer/. This package just moves bytes.
package connection

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

	"dhs/internal/amwa/codec/is05"
)

// Client speaks IS-05 to one Device's Connection API.
//
// Base is the href IS-04 advertised in the Device's `controls` array
// under urn:x-nmos:control:sr-ctrl/vX.Y — NOT a host guess. Discovering
// it from IS-04 is the spec's mechanism and the only way that works
// when a Device serves its Connection API on a different port from its
// Node API, which real ones do.
type Client struct {
	HTTP   *http.Client
	Base   string // e.g. "http://10.6.255.102:3000/x-nmos/connection/v1.1"
	APIVer string
}

// NewClient builds a Client from an IS-05 control href.
//
// The href already carries the /x-nmos/connection/<ver> prefix, because
// that is how IS-04 advertises it. We parse the version back out of it
// rather than asking the caller, so the two can never disagree.
func NewClient(controlHref string) (*Client, error) {
	base := strings.TrimRight(controlHref, "/")
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("nmos/connection: parse %q: %w", controlHref, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("nmos/connection: %q must be an absolute URL", controlHref)
	}
	ver := ""
	if segs := strings.Split(strings.Trim(u.Path, "/"), "/"); len(segs) > 0 {
		last := segs[len(segs)-1]
		if strings.HasPrefix(last, "v") {
			ver = last
		}
	}
	if ver == "" {
		return nil, fmt.Errorf("nmos/connection: %q does not end in an api_ver "+
			"(expected .../x-nmos/connection/v1.1)", controlHref)
	}
	return &Client{
		HTTP:   &http.Client{Timeout: 30 * time.Second},
		Base:   base,
		APIVer: ver,
	}, nil
}

// StagedReceiver reads a Receiver's staged state.
func (c *Client) StagedReceiver(ctx context.Context, id string) (*is05.StagedReceiver, error) {
	raw, err := c.get(ctx, "single/receivers/"+id+"/staged")
	if err != nil {
		return nil, err
	}
	return is05.DecodeStagedReceiver(raw)
}

// ActiveReceiver reads a Receiver's active state — what it is actually
// doing right now, as opposed to what has been staged for it.
func (c *Client) ActiveReceiver(ctx context.Context, id string) (*is05.ActiveReceiver, error) {
	raw, err := c.get(ctx, "single/receivers/"+id+"/active")
	if err != nil {
		return nil, err
	}
	return is05.DecodeStagedReceiver(raw)
}

// ActiveSender reads a Sender's active state.
func (c *Client) ActiveSender(ctx context.Context, id string) (*is05.ActiveSender, error) {
	raw, err := c.get(ctx, "single/senders/"+id+"/active")
	if err != nil {
		return nil, err
	}
	return is05.DecodeStagedSender(raw)
}

// TransportFile fetches a Sender's SDP.
//
// Returned as text/plain per IS-05, and fed verbatim into the
// Receiver's staged transport_file. We never parse and re-emit it: an
// SDP round-trip through our own encoder would change bytes the sender
// meant literally, and the receiver is the one entitled to interpret
// them.
func (c *Client) TransportFile(ctx context.Context, senderID string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.Base+"/single/senders/"+senderID+"/transportfile", nil)
	if err != nil {
		return "", err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("nmos/connection: transportfile %s: %w", senderID, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("nmos/connection: transportfile %s: HTTP %d: %s",
			senderID, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return string(body), nil
}

// PatchReceiver stages a change on a Receiver and returns the state the
// Device reports back.
//
// IS-05 §"staged" is explicit that a PATCH is a MERGE, not a replace:
// fields the caller omits keep their current value. So this takes an
// already-built partial body rather than a full StagedReceiver — a
// round-trip through the full struct would send zero values for every
// field the caller never mentioned and silently clear them.
func (c *Client) PatchReceiver(ctx context.Context, id string, patch map[string]any) (*is05.StagedReceiver, error) {
	raw, err := c.patch(ctx, "single/receivers/"+id+"/staged", patch)
	if err != nil {
		return nil, err
	}
	return is05.DecodeStagedReceiver(raw)
}

// PatchSender stages a change on a Sender. Same merge semantics as
// [Client.PatchReceiver].
func (c *Client) PatchSender(ctx context.Context, id string, patch map[string]any) (*is05.StagedSender, error) {
	raw, err := c.patch(ctx, "single/senders/"+id+"/staged", patch)
	if err != nil {
		return nil, err
	}
	return is05.DecodeStagedSender(raw)
}

// Bulk reports whether the Device advertises the IS-05 bulk endpoint.
// Handy before offering a salvo-style multi-route.
func (c *Client) Bulk(ctx context.Context) bool {
	_, err := c.get(ctx, "bulk/")
	return err == nil
}

func (c *Client) get(ctx context.Context, rest string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+"/"+rest, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return c.do(req, rest)
}

func (c *Client) patch(ctx context.Context, rest string, body any) ([]byte, error) {
	enc, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("nmos/connection: encode %s: %w", rest, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.Base+"/"+rest, bytes.NewReader(enc))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return c.do(req, rest)
}

// do sends the request and turns a non-2xx into an error carrying the
// Device's own message. IS-05 devices explain refusals in the body
// ("transport_params[0].destination_ip is not routable"), and dropping
// that in favour of a bare status code is what makes a routing failure
// impossible to debug from the CLI.
func (c *Client) do(req *http.Request, what string) ([]byte, error) {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nmos/connection: %s: %w", what, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("nmos/connection: %s: HTTP %d: %s",
			what, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}
