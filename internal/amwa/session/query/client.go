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
	HTTP  *httpsession.Client
	Base  string     // e.g. "http://10.6.239.113:8235"
	Codec is04.Codec // negotiated wire version — drives URL APIVer + payload shape

	// Face selects which IS-04 HTTP surface to talk to. Zero value is
	// [FaceQuery], so existing callers are unaffected.
	Face Face
}

// Face is the IS-04 HTTP surface a Client addresses. The two carry the
// same six collections under different prefixes, which is why one
// client serves both.
//
// The distinction is not cosmetic: a Registry is a catalogue of many
// Nodes, a Node is one device describing itself. Only the Node face
// can reach a device that is not registered anywhere — which is the
// normal state of a device on a network segment that cannot route back
// to the Registry, and the state a real EVS Neuron is in for us today.
type Face string

const (
	// FaceQuery addresses a Registry — /x-nmos/query/{ver}/. The
	// zero value, so NewClient keeps its original meaning.
	FaceQuery Face = "query"
	// FaceNode addresses one Node's own API — /x-nmos/node/{ver}/.
	FaceNode Face = "node"
)

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
	face := c.Face
	if face == "" {
		face = FaceQuery
	}
	return c.Base + "/x-nmos/" + string(face) + "/" + c.Codec.APIVer() + "/" + rest
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
	if c.Face == FaceNode {
		// A Node has no /nodes collection — it describes exactly one
		// Node, itself, at /self. Returning it as a one-element list
		// keeps every caller's shape identical across both faces.
		var raw json.RawMessage
		if err := c.HTTP.GetJSON(ctx, c.urlFor("self"), &raw); err != nil {
			return nil, fmt.Errorf("nmos/query: self: %w", err)
		}
		n, err := c.Codec.DecodeNode(raw)
		if err != nil {
			return nil, fmt.Errorf("nmos/query: self: %w", err)
		}
		return []is04.Node{n}, nil
	}
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

// ListRaw fetches one collection as raw JSON documents, following
// IS-04 §6.2 pagination. For callers that must keep the WIRE bytes —
// a registry mirror caches and re-forwards documents verbatim, and a
// typed decode + re-marshal would silently normalise them.
func (c *Client) ListRaw(ctx context.Context, plural string, filter map[string]string) ([]json.RawMessage, error) {
	return c.fetchListRaw(ctx, plural, filter)
}

// fetchListRaw issues GET /x-nmos/query/<api_ver>/<plural>?<filter>
// and returns the raw JSON array, following IS-04 §6.2 pagination.
//
// A Registry may serve a collection in pages of any size it likes —
// the AMWA IS-04-04 suite deliberately drops the paging limit to 2 —
// and points at the next page via `Link: rel="next"`. Stopping after
// the first response would silently truncate the catalogue, which for
// a controller means routing decisions taken on a fraction of the
// plant. Pages are followed until the server stops offering a next
// link or a page comes back empty.
func (c *Client) fetchListRaw(ctx context.Context, plural string, filter map[string]string) ([]json.RawMessage, error) {
	u := c.urlFor(plural)
	if len(filter) > 0 {
		q := url.Values{}
		for k, v := range filter {
			q.Set(k, v)
		}
		u += "?" + q.Encode()
	}
	// Whole-collection walk, BOTH directions. IS-04 §6.1.6 pins the
	// cursor semantics (rel="next" = newer data, rel="prev" = older)
	// but NOT which window an unanchored GET returns: our registry
	// serves the newest page (the rest lies behind prev), while the
	// AMWA tool's own mock registry serves the OLDEST page with both
	// links present (the rest lies ahead of next). IS-04-04 test_03
	// drops the mock's page size to 2 exactly to catch controllers
	// that guess one direction — and this walker's earlier
	// prev-when-offered guess walked into the void there and
	// truncated the catalogue at one page (first #954 fleet run). So:
	// take the anchor page, exhaust the prev chain, then exhaust the
	// next chain from the same anchor, deduplicating by resource id —
	// whichever direction is the empty one costs one fetch and
	// nothing else. Every chain ends at an empty page, a missing
	// link, or a URL already visited.
	var out []json.RawMessage
	seenURL := map[string]bool{u: true}
	seenID := map[string]bool{}
	appendPage := func(page []json.RawMessage) {
		for _, r := range page {
			if id := rawResourceID(r); id != "" {
				if seenID[id] {
					continue
				}
				seenID[id] = true
			}
			out = append(out, r)
		}
	}
	var first []json.RawMessage
	firstNext, firstPrev, err := c.HTTP.GetJSONPageLinks(ctx, u, &first)
	if err != nil {
		return nil, fmt.Errorf("nmos/query: list %s: %w", plural, err)
	}
	appendPage(first)
	if len(first) == 0 {
		return out, nil
	}
	chains := []struct {
		start   string
		usePrev bool
	}{
		{start: firstPrev, usePrev: true},
		{start: firstNext, usePrev: false},
	}
	for _, chain := range chains {
		u := chain.start
		// 10k pages caps a runaway server that links to itself
		// forever; a real catalogue at limit=2 never gets near it.
		for i := 0; u != "" && i < 10000; i++ {
			if seenURL[u] {
				break // servers that Link back to an earlier page
			}
			seenURL[u] = true
			var page []json.RawMessage
			next, prev, err := c.HTTP.GetJSONPageLinks(ctx, u, &page)
			if err != nil {
				return nil, fmt.Errorf("nmos/query: list %s: %w", plural, err)
			}
			appendPage(page)
			if len(page) == 0 {
				break
			}
			if chain.usePrev {
				u = prev
			} else {
				u = next
			}
		}
	}
	return out, nil
}

// rawResourceID pulls the IS-04 id out of one raw listing element —
// the dedupe key for the two-direction page walk (windows should
// never overlap, but a server bug must not double resources).
func rawResourceID(r json.RawMessage) string {
	var v struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(r, &v)
	return v.ID
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

// NewNodeClient constructs a Client addressing one Node's own API
// rather than a Registry's Query API.
//
// This is how a Controller reaches a device directly — the IS-04
// peer-to-peer path (Mode C/D in internal/amwa/CLAUDE.md), and the
// only path to a device no Registry can see.
func NewNodeClient(base string, codec is04.Codec) (*Client, error) {
	c, err := NewClient(base, codec)
	if err != nil {
		return nil, err
	}
	c.Face = FaceNode
	return c, nil
}
