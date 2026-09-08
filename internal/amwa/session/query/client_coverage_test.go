package query

import (
	"context"
	"encoding/json"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04"
	v13 "dhs/internal/amwa/codec/is04/v13"
)

// ----------------------------------------------------------------------
// Fixtures — every wire payload the list tests serve is produced by the
// is04 v1.3 encoder, so a schema change breaks the fixture rather than
// letting hand-written JSON drift away from what the client decodes.
// ----------------------------------------------------------------------

const fixtureID = "3b8be755-08ff-452b-b217-c9151eb21193"

func mustEncode(t *testing.T, kind string, b []byte, err error) []byte {
	t.Helper()
	if err != nil {
		t.Fatalf("encode %s fixture: %v", kind, err)
	}
	return b
}

func encNode(t *testing.T) []byte {
	c := v13.New()
	chassis := "00-11-22-33-44-55"
	n := is04.Node{
		ResourceCore: is04.ResourceCore{
			ID: fixtureID, Version: "1700000000:0",
			Label: "lab-node", Description: "fixture", Tags: map[string][]string{},
		},
		Hostname: "dhs-lab.local",
		Href:     "http://dhs-lab.local:8080/",
		Caps:     map[string]any{},
		API: is04.NodeAPI{
			Versions:  []string{"v1.3"},
			Endpoints: []is04.NodeEndpoint{{Host: "dhs-lab.local", Port: 8080, Protocol: "http"}},
		},
		Services: []is04.NodeService{}, Clocks: []is04.NodeClock{},
		Interfaces: []is04.NodeIface{{Name: "eth0", ChassisID: &chassis, PortID: "00-11-22-33-44-66"}},
	}
	b, err := c.EncodeNode(n)
	return mustEncode(t, "node", b, err)
}

func encDevice(t *testing.T) []byte {
	c := v13.New()
	d := is04.Device{
		ResourceCore: is04.ResourceCore{
			ID: fixtureID, Version: "0:0", Label: "lab-dev", Description: "fixture", Tags: map[string][]string{},
		},
		Type: "urn:x-nmos:device:generic", NodeID: fixtureID,
		Senders: []string{}, Receivers: []string{}, Controls: []is04.DeviceControl{},
	}
	b, err := c.EncodeDevice(d)
	return mustEncode(t, "device", b, err)
}

func encSource(t *testing.T) []byte {
	c := v13.New()
	clk := "clk0"
	s := is04.Source{
		ResourceCore: is04.ResourceCore{
			ID: fixtureID, Version: "0:0", Label: "src-1", Description: "fixture", Tags: map[string][]string{},
		},
		Caps: map[string]any{}, DeviceID: fixtureID, Parents: []string{}, ClockName: &clk, Format: is04.FormatVideo,
	}
	b, err := c.EncodeSource(s)
	return mustEncode(t, "source", b, err)
}

func encFlow(t *testing.T) []byte {
	c := v13.New()
	f := is04.Flow{
		ResourceCore: is04.ResourceCore{
			ID: fixtureID, Version: "0:0", Label: "flow-1", Description: "fixture", Tags: map[string][]string{},
		},
		SourceID: fixtureID, DeviceID: fixtureID, Parents: []string{},
		Format: is04.FormatVideo, MediaType: "video/raw",
		FrameWidth: 1920, FrameHeight: 1080, Interlace: "progressive", ColorSpace: "BT709",
		Components: []is04.FlowVideoComponent{
			{Name: "Y", Width: 1920, Height: 1080, BitDepth: 10},
			{Name: "Cb", Width: 960, Height: 1080, BitDepth: 10},
			{Name: "Cr", Width: 960, Height: 1080, BitDepth: 10},
		},
	}
	b, err := c.EncodeFlow(f)
	return mustEncode(t, "flow", b, err)
}

func encSender(t *testing.T) []byte {
	c := v13.New()
	href := "http://10.6.239.113:8080/x-nmos/node/v1.3/senders/abc/manifest"
	flow := fixtureID
	s := is04.Sender{
		ResourceCore: is04.ResourceCore{
			ID: fixtureID, Version: "0:0", Label: "snd-1", Description: "fixture", Tags: map[string][]string{},
		},
		FlowID: &flow, Transport: is04.TransportRTPMcast, DeviceID: fixtureID,
		ManifestHref: &href, InterfaceBindings: []string{"eth0"}, Subscription: is04.SenderSubscription{Active: false},
	}
	b, err := c.EncodeSender(s)
	return mustEncode(t, "sender", b, err)
}

func encReceiver(t *testing.T) []byte {
	c := v13.New()
	r := is04.Receiver{
		ResourceCore: is04.ResourceCore{
			ID: fixtureID, Version: "0:0", Label: "rcv-1", Description: "fixture", Tags: map[string][]string{},
		},
		DeviceID: fixtureID, Transport: is04.TransportRTPMcast, InterfaceBindings: []string{"eth0"},
		Format: is04.FormatVideo, Caps: is04.ReceiverCaps{MediaTypes: []string{"video/raw"}},
		Subscription: is04.ReceiverSubscription{Active: false},
	}
	b, err := c.EncodeReceiver(r)
	return mustEncode(t, "receiver", b, err)
}

// listServer serves each collection's single-element listing keyed by
// the plural in the URL path — one page, no Link header.
func listServer(t *testing.T, bodies map[string][]byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		for plural, body := range bodies {
			if strings.HasSuffix(r.URL.Path, "/"+plural) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("[" + string(body) + "]"))
				return
			}
		}
		w.WriteHeader(stdhttp.StatusNotFound)
	}))
}

// ----------------------------------------------------------------------
// NewClient: the url.Parse failure arm — a control character makes the
// base unparseable, which must be reported (not panicked over).
// ----------------------------------------------------------------------

func TestNewClientRejectsUnparseableBase(t *testing.T) {
	_, err := NewClient("http://h\x7f", v13.New())
	if err == nil || !strings.Contains(err.Error(), "parse base") {
		t.Fatalf("err = %v, want a parse-base error for a control char in the URL", err)
	}
}

// ----------------------------------------------------------------------
// Index: a non-2xx from the Registry must surface as an index error, not
// an empty slice a caller could mistake for a reachable-but-empty face.
// ----------------------------------------------------------------------

func TestIndexReportsHTTPError(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusInternalServerError)
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL, v13.New())
	if _, err := c.Index(context.Background()); err == nil || !strings.Contains(err.Error(), "index") {
		t.Fatalf("err = %v, want an index error on HTTP 500", err)
	}
}

// ----------------------------------------------------------------------
// The six list collections: each must return its single element on the
// happy path AND surface the transport error on a 500. The error arm of
// every List* wrapper is a distinct return that 100% coverage requires,
// so both directions are exercised per collection.
// ----------------------------------------------------------------------

func TestListCollections(t *testing.T) {
	bodies := map[string][]byte{
		"nodes":     encNode(t),
		"devices":   encDevice(t),
		"sources":   encSource(t),
		"flows":     encFlow(t),
		"senders":   encSender(t),
		"receivers": encReceiver(t),
	}
	good := listServer(t, bodies)
	defer good.Close()
	bad := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusInternalServerError)
	}))
	defer bad.Close()

	cases := []struct {
		name string
		call func(c *Client) (int, error)
	}{
		{"nodes", func(c *Client) (int, error) { v, e := c.ListNodes(context.Background(), nil); return len(v), e }},
		{"devices", func(c *Client) (int, error) { v, e := c.ListDevices(context.Background(), nil); return len(v), e }},
		{"sources", func(c *Client) (int, error) { v, e := c.ListSources(context.Background(), nil); return len(v), e }},
		{"flows", func(c *Client) (int, error) { v, e := c.ListFlows(context.Background(), nil); return len(v), e }},
		{"senders", func(c *Client) (int, error) { v, e := c.ListSenders(context.Background(), nil); return len(v), e }},
		{"receivers", func(c *Client) (int, error) { v, e := c.ListReceivers(context.Background(), nil); return len(v), e }},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/ok", func(t *testing.T) {
			c, _ := NewClient(good.URL, v13.New())
			n, err := tc.call(c)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if n != 1 {
				t.Fatalf("%s: got %d resources, want 1", tc.name, n)
			}
		})
		t.Run(tc.name+"/http-error", func(t *testing.T) {
			c, _ := NewClient(bad.URL, v13.New())
			if _, err := tc.call(c); err == nil {
				t.Fatalf("%s: HTTP 500 must be an error", tc.name)
			}
		})
	}
}

// TestListRawReturnsWireBytes: ListRaw hands back the undecoded JSON
// documents — the path a registry mirror relies on to re-forward bytes
// verbatim.
func TestListRawReturnsWireBytes(t *testing.T) {
	good := listServer(t, map[string][]byte{"senders": encSender(t)})
	defer good.Close()

	c, _ := NewClient(good.URL, v13.New())
	raw, err := c.ListRaw(context.Background(), "senders", nil)
	if err != nil {
		t.Fatalf("ListRaw: %v", err)
	}
	if len(raw) != 1 {
		t.Fatalf("got %d raw docs, want 1", len(raw))
	}
	if id := rawResourceID(raw[0]); id != fixtureID {
		t.Fatalf("raw id = %q, want %q", id, fixtureID)
	}
}

// ----------------------------------------------------------------------
// Node face (/self): a Node has no /nodes collection, so ListNodes must
// fetch /self and present it as a one-element list. Both failure arms —
// the HTTP fetch and the decode — must be distinguishable errors.
// ----------------------------------------------------------------------

func TestNewNodeClientSetsFace(t *testing.T) {
	c, err := NewNodeClient("http://10.0.0.9:8080", v13.New())
	if err != nil {
		t.Fatalf("NewNodeClient: %v", err)
	}
	if c.Face != FaceNode {
		t.Fatalf("Face = %q, want %q", c.Face, FaceNode)
	}
}

func TestNewNodeClientRejectsBadBase(t *testing.T) {
	if _, err := NewNodeClient("not-a-url", v13.New()); err == nil {
		t.Fatal("a bare host must be rejected, same as NewClient")
	}
}

func TestListNodesNodeFaceSelf(t *testing.T) {
	self := encNode(t)
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.URL.Path != "/x-nmos/node/v1.3/self" {
			t.Errorf("node face must GET /self, got %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(self)
	}))
	defer srv.Close()

	c, _ := NewNodeClient(srv.URL, v13.New())
	nodes, err := c.ListNodes(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListNodes(node face): %v", err)
	}
	if len(nodes) != 1 || nodes[0].ID != fixtureID {
		t.Fatalf("self must be returned as a one-element list, got %+v", nodes)
	}
}

func TestListNodesNodeFaceHTTPError(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c, _ := NewNodeClient(srv.URL, v13.New())
	if _, err := c.ListNodes(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "self") {
		t.Fatalf("err = %v, want a /self error on HTTP failure", err)
	}
}

func TestListNodesNodeFaceDecodeError(t *testing.T) {
	// Valid JSON (so the raw GET succeeds) that is not a valid Node (so
	// the codec decode fails) — the two-step self path has a decode arm
	// distinct from the fetch arm.
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":12345}`))
	}))
	defer srv.Close()

	c, _ := NewNodeClient(srv.URL, v13.New())
	if _, err := c.ListNodes(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "self") {
		t.Fatalf("err = %v, want a /self decode error", err)
	}
}

// ----------------------------------------------------------------------
// fetchListRaw edge cases the pagination tests don't reach.
// ----------------------------------------------------------------------

// TestFetchListEmptyFirstPage: an empty head page is a legitimately
// empty collection — return it without chasing prev/next chains.
func TestFetchListEmptyFirstPage(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL, v13.New())
	raw, err := c.fetchListRaw(context.Background(), "senders", nil)
	if err != nil {
		t.Fatalf("fetchListRaw: %v", err)
	}
	if len(raw) != 0 {
		t.Fatalf("got %d, want 0 for an empty collection", len(raw))
	}
}

// TestFetchListFirstPageError: a transport failure on the anchor fetch
// is fatal — there is no page to fall back to.
func TestFetchListFirstPageError(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusBadGateway)
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL, v13.New())
	if _, err := c.fetchListRaw(context.Background(), "senders", nil); err == nil ||
		!strings.Contains(err.Error(), "list senders") {
		t.Fatalf("err = %v, want a list error on the anchor fetch", err)
	}
}

// TestFetchListChainPageError: the anchor page succeeds and offers a
// prev link, but following it fails. A mid-walk failure must abort the
// walk with an error rather than silently truncate the catalogue.
func TestFetchListChainPageError(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		// The anchor GET (no paging.until) returns one item plus a prev
		// link at a URL that this same handler answers with a 500.
		if r.URL.Query().Get("paging.until") != "" {
			w.WriteHeader(stdhttp.StatusInternalServerError)
			return
		}
		w.Header().Set("Link",
			fmt.Sprintf(`<%s/x-nmos/query/v1.3/senders?paging.until=100:0>; rel="prev"`, srv.URL))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"00000000-0000-4000-8000-000000000009"}]`))
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL, v13.New())
	if _, err := c.fetchListRaw(context.Background(), "senders", nil); err == nil ||
		!strings.Contains(err.Error(), "list senders") {
		t.Fatalf("err = %v, want a list error when a chain page fails", err)
	}
}

// TestFetchListDeduplicatesOverlap: the two-direction walk can revisit a
// resource when a buggy server serves overlapping windows. The walker
// dedupes by resource id, so an id seen on the anchor page must not be
// counted again when a chain page repeats it.
func TestFetchListDeduplicatesOverlap(t *testing.T) {
	id := func(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", n) }
	var srv *httptest.Server
	srv = httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		q := r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case q.Get("paging.until") != "":
			// Prev chain: window [1,2] — id 1 overlaps the anchor page.
			_, _ = fmt.Fprintf(w, `[{"id":%q},{"id":%q}]`, id(1), id(2))
		case q.Get("paging.since") != "":
			// Next chain: nothing newer than the head.
			_, _ = w.Write([]byte(`[]`))
		default:
			// Anchor: window [0,1] with both cursors offered.
			w.Header().Set("Link", fmt.Sprintf(
				`<%s/x-nmos/query/v1.3/senders?paging.until=100:1>; rel="prev", `+
					`<%s/x-nmos/query/v1.3/senders?paging.since=100:9>; rel="next"`, srv.URL, srv.URL))
			_, _ = fmt.Fprintf(w, `[{"id":%q},{"id":%q}]`, id(0), id(1))
		}
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL, v13.New())
	raw, err := c.fetchListRaw(context.Background(), "senders", nil)
	if err != nil {
		t.Fatalf("fetchListRaw: %v", err)
	}
	if len(raw) != 3 {
		t.Fatalf("got %d, want 3 unique ids — id 1 must be deduped across the overlap", len(raw))
	}
}

// ----------------------------------------------------------------------
// decodeList: the error arm. Unlike the deviation case (a Reporter
// absorbs a schema miss), malformed JSON in an element aborts the decode
// and returns the partial slice with the error naming the bad index.
// ----------------------------------------------------------------------

func TestDecodeListErrorsOnMalformedElement(t *testing.T) {
	raw := []json.RawMessage{json.RawMessage(`{"id":12345}`)} // id must be a string
	got, err := decodeList(raw, "node", v13.New().DecodeNode)
	if err == nil || !strings.Contains(err.Error(), "decode node[0]") {
		t.Fatalf("err = %v, want a decode error naming node[0]", err)
	}
	if len(got) != 0 {
		t.Fatalf("nothing decoded before the failure, got %d", len(got))
	}
}
