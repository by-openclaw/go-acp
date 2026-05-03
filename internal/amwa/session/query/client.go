package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"dhs/internal/amwa/codec/is04"
	httpsession "dhs/internal/amwa/session/http"
)

// Client is the IS-04 Query API client. Hold one per Registry: it
// caches the base URL + the negotiated wire-version codec.
//
// Constructor takes a Codec via DI so tests can substitute a fake.
// Production callers usually obtain one from the Controller after
// `is04.SelectHighest(peer.APIVersions)`.
type Client struct {
	HTTP   *httpsession.Client
	Base   string     // e.g. "http://10.6.239.113:8235"
	Codec  is04.Codec // negotiated wire version — drives URL APIVer + payload shape
}

// NewClient constructs a Client. base is the Registry origin
// (`http(s)://host:port`, no trailing slash, no `/x-nmos/...`); codec
// must already be selected via [is04.SelectHighest] or [is04.Get].
//
// Returns an error when base is empty / unparseable / already carries
// a path; codec must be non-nil. Both checks fail fast — invalid input
// here is a programming error.
func NewClient(base string, codec is04.Codec) (*Client, error) {
	if codec == nil {
		return nil, fmt.Errorf("nmos/query: codec must not be nil")
	}
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil {
		return nil, fmt.Errorf("nmos/query: parse base %q: %w", base, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("nmos/query: base %q must be an absolute URL", base)
	}
	if u.Path != "" && u.Path != "/" {
		return nil, fmt.Errorf("nmos/query: base %q must not include a path", base)
	}
	return &Client{
		HTTP:  httpsession.NewClient(),
		Base:  u.Scheme + "://" + u.Host,
		Codec: codec,
	}, nil
}

// urlFor returns the full URL for a Query API resource path —
// `/x-nmos/query/<api_ver>/<rest>`.
func (c *Client) urlFor(rest string) string {
	rest = strings.TrimPrefix(rest, "/")
	return c.Base + "/x-nmos/query/" + c.Codec.APIVer() + "/" + rest
}

// Index returns the top-level Query API index — the JSON array of
// sub-resource paths a Registry exposes. Non-empty array confirms the
// Registry's right face is reachable.
func (c *Client) Index(ctx context.Context) ([]string, error) {
	var out []string
	if err := c.HTTP.GetJSON(ctx, c.urlFor("/"), &out); err != nil {
		return nil, fmt.Errorf("nmos/query: index: %w", err)
	}
	return out, nil
}

// ListNodes fetches every Node currently registered. The result is
// already decoded against the negotiated minor's schema. Caller may
// pass a non-empty filter map to apply RQL-lite query parameters
// (label / description / id / version equality).
func (c *Client) ListNodes(ctx context.Context, filter map[string]string) ([]is04.Node, error) {
	raw, err := c.fetchListRaw(ctx, "nodes", filter)
	if err != nil {
		return nil, err
	}
	return decodeList(raw, "node", c.Codec.DecodeNode)
}

// ListDevices fetches every Device.
func (c *Client) ListDevices(ctx context.Context, filter map[string]string) ([]is04.Device, error) {
	raw, err := c.fetchListRaw(ctx, "devices", filter)
	if err != nil {
		return nil, err
	}
	return decodeList(raw, "device", c.Codec.DecodeDevice)
}

// ListSources fetches every Source.
func (c *Client) ListSources(ctx context.Context, filter map[string]string) ([]is04.Source, error) {
	raw, err := c.fetchListRaw(ctx, "sources", filter)
	if err != nil {
		return nil, err
	}
	return decodeList(raw, "source", c.Codec.DecodeSource)
}

// ListFlows fetches every Flow.
func (c *Client) ListFlows(ctx context.Context, filter map[string]string) ([]is04.Flow, error) {
	raw, err := c.fetchListRaw(ctx, "flows", filter)
	if err != nil {
		return nil, err
	}
	return decodeList(raw, "flow", c.Codec.DecodeFlow)
}

// ListSenders fetches every Sender.
func (c *Client) ListSenders(ctx context.Context, filter map[string]string) ([]is04.Sender, error) {
	raw, err := c.fetchListRaw(ctx, "senders", filter)
	if err != nil {
		return nil, err
	}
	return decodeList(raw, "sender", c.Codec.DecodeSender)
}

// ListReceivers fetches every Receiver.
func (c *Client) ListReceivers(ctx context.Context, filter map[string]string) ([]is04.Receiver, error) {
	raw, err := c.fetchListRaw(ctx, "receivers", filter)
	if err != nil {
		return nil, err
	}
	return decodeList(raw, "receiver", c.Codec.DecodeReceiver)
}

// fetchListRaw issues GET /x-nmos/query/<api_ver>/<plural>?<filter>
// and returns the raw JSON array. Per-resource decode happens via the
// negotiated Codec so version-specific field gating applies.
func (c *Client) fetchListRaw(ctx context.Context, plural string, filter map[string]string) ([]json.RawMessage, error) {
	u := c.urlFor(plural)
	if len(filter) > 0 {
		q := url.Values{}
		for k, v := range filter {
			q.Set(k, v)
		}
		u += "?" + q.Encode()
	}
	var out []json.RawMessage
	if err := c.HTTP.GetJSON(ctx, u, &out); err != nil {
		return nil, fmt.Errorf("nmos/query: list %s: %w", plural, err)
	}
	return out, nil
}

// decodeList runs each raw element through the per-resource codec
// decoder. Returns a partial result on the first decode error so the
// caller can surface "X of Y resources decoded; the rest were
// non-spec".
func decodeList[T any](raw []json.RawMessage, kind string, decode func([]byte) (T, error)) ([]T, error) {
	out := make([]T, 0, len(raw))
	for i, rb := range raw {
		v, err := decode([]byte(rb))
		if err != nil {
			return out, fmt.Errorf("nmos/query: decode %s[%d]: %w", kind, i, err)
		}
		out = append(out, v)
	}
	return out, nil
}
