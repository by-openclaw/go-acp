package consumer

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dhs/internal/amwa/codec/bcp"
	dnssdcodec "dhs/internal/amwa/codec/dnssd"
	"dhs/internal/amwa/codec/is04"
	_ "dhs/internal/amwa/codec/is04/v10" // register v1.0 so version selection has >1 codec
	_ "dhs/internal/amwa/codec/is04/v11" // register v1.1
	_ "dhs/internal/amwa/codec/is04/v12" // register v1.2
	_ "dhs/internal/amwa/codec/is04/v13" // register v1.3 (the default/highest)
	"dhs/internal/amwa/codec/spec"
	dnssdsession "dhs/internal/amwa/session/dnssd"
)

// --- Walk -------------------------------------------------------------

// TestWalkCollectsEverySnapshotCollection: Walk must fetch all six
// catalogue collections in one snapshot, stamped with the negotiated
// wire minor. A collection the walk skips is a slice of the plant a
// routing decision would be taken blind to.
func TestWalkCollectsEverySnapshotCollection(t *testing.T) {
	h := newHarness(t)
	h.cat.devices = []is04.Device{deviceWith(testUUID, h.controlHref)}
	h.cat.senders = []is04.Sender{senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportRTPMcast)}
	h.cat.receivers = []is04.Receiver{receiverOn(uuidN(2), testUUID, is04.TransportRTPMcast)}

	snap, errs := h.ctrl.Walk(context.Background())
	if len(errs) != 0 {
		t.Fatalf("clean catalogue must walk without errors: %v", errs)
	}
	if snap.APIVer != "v1.3" {
		t.Errorf("snapshot APIVer = %q, want the negotiated v1.3", snap.APIVer)
	}
	if len(snap.Devices) != 1 || len(snap.Senders) != 1 || len(snap.Receivers) != 1 {
		t.Errorf("snapshot missing resources: devices=%d senders=%d receivers=%d",
			len(snap.Devices), len(snap.Senders), len(snap.Receivers))
	}
}

// TestWalkContinuesPastOneFailedCollection: a single collection that
// 500s must not abort the walk. The caller gets the partial snapshot,
// an error naming the failed collection, and a compliance event — not
// an empty catalogue.
func TestWalkContinuesPastOneFailedCollection(t *testing.T) {
	h := newHarness(t)
	h.cat.senders = []is04.Sender{senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportRTPMcast)}
	h.cat.receivers = []is04.Receiver{receiverOn(uuidN(2), testUUID, is04.TransportRTPMcast)}
	h.failColl = "senders"

	snap, errs := h.ctrl.Walk(context.Background())
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "senders") {
		t.Fatalf("want exactly one error naming senders, got %v", errs)
	}
	if len(snap.Receivers) != 1 {
		t.Errorf("the surviving collections must still populate: receivers=%d", len(snap.Receivers))
	}
	if !hasCode(h.rep, "nmos_query_collection_failed") {
		t.Error("a failed collection must fire nmos_query_collection_failed")
	}
}

// TestWalkRunsReceiverCapsValidators: Walk feeds every receiver through
// the BCP-004-01 receiver-caps validators (#851). A receiver whose caps
// name a constraint URN outside the AMWA register must surface an event
// rather than being silently accepted (and later dropped by a filtering
// controller).
func TestWalkRunsReceiverCapsValidators(t *testing.T) {
	rcv := receiverOn(uuidN(3), testUUID, is04.TransportRTPMcast)
	rcv.Caps = is04.ReceiverCaps{
		ConstraintSets: []map[string]any{{"urn:x-nmos:cap:not:a:real:cap": map[string]any{"enum": []any{"x"}}}},
		Version:        "0:0",
	}
	h := newHarness(t)
	h.cat.receivers = []is04.Receiver{rcv}

	snap, errs := h.ctrl.Walk(context.Background())
	if len(errs) != 0 {
		t.Fatalf("caps deviations are events, not walk errors: %v", errs)
	}
	if len(snap.Receivers) != 1 {
		t.Fatalf("receiver must still reach the snapshot")
	}
	if len(h.rep.Snapshot()) == 0 {
		t.Error("a receiver with an unregistered cap URN must fire at least one event")
	}
}

// TestWalkContinuesPastReceiverMarshalError: a receiver that fails to
// re-marshal must be skipped (continue) rather than aborting the
// caps-validation pass — the snapshot and its other collections still
// stand. The marshal seam is overridden to fail because a decoded
// is04.Receiver always re-marshals cleanly, leaving the guard otherwise
// unreachable.
func TestWalkContinuesPastReceiverMarshalError(t *testing.T) {
	orig := marshalJSON
	marshalJSON = func(any) ([]byte, error) { return nil, fmt.Errorf("marshal boom") }
	defer func() { marshalJSON = orig }()

	h := newHarness(t)
	h.cat.receivers = []is04.Receiver{receiverOn(uuidN(2), testUUID, is04.TransportRTPMcast)}

	snap, errs := h.ctrl.Walk(context.Background())
	if len(errs) != 0 {
		t.Fatalf("a per-receiver marshal error is skipped, not a walk error: %v", errs)
	}
	if len(snap.Receivers) != 1 {
		t.Fatalf("the receiver must still reach the snapshot despite the skip")
	}
	if len(h.rep.Snapshot()) != 0 {
		t.Errorf("a skipped receiver must run no validators, got %d events", len(h.rep.Snapshot()))
	}
}

// zeroAtValidator is a receiver-caps validator that returns one event
// with a zero At, so a test can drive Walk's At-normalisation — the
// only wired validator (bcp00401) always stamps At itself.
type zeroAtValidator struct{}

func (zeroAtValidator) SpecID() string     { return "test-bcp" }
func (zeroAtValidator) APIVer() string     { return "v1.0" }
func (zeroAtValidator) SpecPatch() string  { return "0" }
func (zeroAtValidator) HostKind() bcp.Kind { return bcp.KindReceiver }
func (zeroAtValidator) Validate([]byte) []spec.ComplianceEvent {
	return []spec.ComplianceEvent{{Code: "test_zero_at"}}
}

// TestWalkStampsValidatorEventWithoutAt: an event a receiver-caps
// validator returns without an At must be stamped by Walk before it
// reaches the Reporter — a compliance event with no "when" is a defect.
// The validator-set seam injects an event with a zero At.
func TestWalkStampsValidatorEventWithoutAt(t *testing.T) {
	orig := receiverCapsValidators
	receiverCapsValidators = func(bcp.Kind) []bcp.Validator { return []bcp.Validator{zeroAtValidator{}} }
	defer func() { receiverCapsValidators = orig }()

	h := newHarness(t)
	h.cat.receivers = []is04.Receiver{receiverOn(uuidN(2), testUUID, is04.TransportRTPMcast)}

	if _, errs := h.ctrl.Walk(context.Background()); len(errs) != 0 {
		t.Fatalf("validator events are not walk errors: %v", errs)
	}
	evs := h.rep.Snapshot()
	if len(evs) != 1 || evs[0].Code != "test_zero_at" {
		t.Fatalf("want the single injected event, got %+v", evs)
	}
	if evs[0].At.IsZero() {
		t.Error("Walk must stamp a zero-At validator event with the observation time")
	}
}

// --- getters ----------------------------------------------------------

// TestControllerGetters: the three label-truthfulness accessors report
// the negotiated codec, the origin spoken to, and (for a Registry-bound
// Controller) that it is NOT a Node face.
func TestControllerGetters(t *testing.T) {
	h := newHarness(t)
	if h.ctrl.Codec().APIVer() != "v1.3" {
		t.Errorf("Codec().APIVer = %q, want v1.3", h.ctrl.Codec().APIVer())
	}
	if h.ctrl.BaseURL() != h.ctrl.client.Base {
		t.Errorf("BaseURL = %q, want the client base", h.ctrl.BaseURL())
	}
	if h.ctrl.IsNodeFace() {
		t.Error("a Registry-bound Controller must not report IsNodeFace")
	}
}

// --- NewController (no discovery: RegistryURL / NodeURL) ---------------

// TestNewControllerRegistryURL: a Mode-B unicast Registry URL skips
// discovery entirely and binds straight to the named host with our
// highest codec. A nil Reporter must be tolerated (defaulted to Nop).
func TestNewControllerRegistryURL(t *testing.T) {
	c, err := NewController(context.Background(), ControllerOptions{
		RegistryURL: "http://10.6.239.113:8235/",
	})
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	if c.BaseURL() != "http://10.6.239.113:8235" {
		t.Errorf("BaseURL = %q (trailing slash must be trimmed)", c.BaseURL())
	}
	if c.Codec().APIVer() != is04.Default().APIVer() {
		t.Errorf("unicast-with-no-discovery must assume our highest codec, got %q", c.Codec().APIVer())
	}
}

// TestNewControllerAPIVerOverride: an explicit --api-ver pins the codec
// without any peer negotiation.
func TestNewControllerAPIVerOverride(t *testing.T) {
	c, err := NewController(context.Background(), ControllerOptions{
		RegistryURL: "http://h:8235", APIVer: "v1.1",
	})
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	if c.Codec().APIVer() != "v1.1" {
		t.Errorf("APIVer override ignored: got %q", c.Codec().APIVer())
	}
}

// TestNewControllerErrors covers every constructor error arm that does
// not need discovery: an unregistered override, an unroutable
// DiscoveryMode, and a base URL that already carries a path (which
// query.NewClient refuses).
func TestNewControllerErrors(t *testing.T) {
	cases := []struct {
		name string
		opts ControllerOptions
		want string
	}{
		{"override not registered",
			ControllerOptions{RegistryURL: "http://h:8235", APIVer: "v9.9"},
			"not registered"},
		{"unknown discovery mode",
			ControllerOptions{DiscoveryMode: "carrier-pigeon"},
			"unknown DiscoveryMode"},
		{"base carries a path",
			ControllerOptions{RegistryURL: "http://h:8235/x-nmos/query"},
			"must not include a path"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewController(context.Background(), tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want contains %q", err, tc.want)
			}
		})
	}
}

// --- newNodeController / nodeAPIVersions -------------------------------

// TestNewControllerNodeFace: NodeURL binds a Controller straight to one
// Node, negotiating the version from the Node's OWN /x-nmos/node/ index
// (there is no DNS-SD api_ver TXT on the peer-to-peer path). The
// resulting Controller must report IsNodeFace.
func TestNewControllerNodeFace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/x-nmos/node/") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`["v1.2/","v1.3/"]`))
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	c, err := NewController(context.Background(), ControllerOptions{NodeURL: srv.URL})
	if err != nil {
		t.Fatalf("NewController(NodeURL): %v", err)
	}
	if !c.IsNodeFace() {
		t.Error("a NodeURL-bound Controller must report IsNodeFace")
	}
	if c.Codec().APIVer() != "v1.3" {
		t.Errorf("must pick the highest common minor v1.3, got %q", c.Codec().APIVer())
	}
}

// TestNewControllerNodeFaceErrors covers the three node-path error arms:
// an index that cannot be read, an index advertising no usable version,
// and a NodeURL whose path defeats NewNodeClient even though the index
// read succeeds.
func TestNewControllerNodeFaceErrors(t *testing.T) {
	t.Run("index unreadable", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer srv.Close()
		if _, err := NewController(context.Background(), ControllerOptions{NodeURL: srv.URL}); err == nil {
			t.Fatal("an unreadable node index must fail construction")
		}
	})

	t.Run("index advertises no versions", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`["","/"]`)) // both trim to empty
		}))
		defer srv.Close()
		_, err := NewController(context.Background(), ControllerOptions{NodeURL: srv.URL})
		if err == nil || !strings.Contains(err.Error(), "no API versions") {
			t.Fatalf("err = %v, want it to name the empty version list", err)
		}
	})

	t.Run("override not registered on node path", func(t *testing.T) {
		// The index read succeeds, but an --api-ver the process never
		// registered defeats pickCodec on the node path too.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/x-nmos/node/") {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`["v1.3/"]`))
				return
			}
			http.Error(w, "nope", http.StatusNotFound)
		}))
		defer srv.Close()
		_, err := NewController(context.Background(), ControllerOptions{NodeURL: srv.URL, APIVer: "v9.9"})
		if err == nil || !strings.Contains(err.Error(), "not registered") {
			t.Fatalf("err = %v, want the unregistered-override error on the node path", err)
		}
	})

	t.Run("node client rejects path", func(t *testing.T) {
		// The index read succeeds under the /sub prefix, but the same
		// base carries a path, which NewNodeClient (via NewClient)
		// refuses — the arm after a good version negotiation.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/x-nmos/node/") {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`["v1.3/"]`))
				return
			}
			http.Error(w, "nope", http.StatusNotFound)
		}))
		defer srv.Close()
		_, err := NewController(context.Background(), ControllerOptions{NodeURL: srv.URL + "/sub"})
		if err == nil || !strings.Contains(err.Error(), "must not include a path") {
			t.Fatalf("err = %v, want NewNodeClient to reject the path", err)
		}
	})
}

// TestNodeAPIVersionsTrimsAndFilters: the index carries versions as
// "v1.3/" with a trailing slash and may include blanks; nodeAPIVersions
// must normalise them.
func TestNodeAPIVersionsTrimsAndFilters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`["/v1.2/","v1.3","",  "/"]`))
	}))
	defer srv.Close()
	got, err := nodeAPIVersions(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("nodeAPIVersions: %v", err)
	}
	if len(got) != 2 || got[0] != "v1.2" || got[1] != "v1.3" {
		t.Fatalf("got %v, want [v1.2 v1.3] normalised", got)
	}
}

// --- resolveRegistry / discovery --------------------------------------

// TestResolveRegistryUnicastError: the Mode-B authoritative-DNS path
// wraps a resolver failure. Pointing at a closed port with a tight
// timeout exercises the unicast branch and its error return without a
// live resolver.
func TestResolveRegistryUnicastError(t *testing.T) {
	_, _, err := resolveRegistry(context.Background(), ControllerOptions{
		DiscoveryMode:    "unicast",
		UnicastResolver:  "127.0.0.1:1",
		UnicastDomain:    "example.invalid",
		DiscoveryTimeout: 300 * time.Millisecond,
	})
	if err == nil || !strings.Contains(err.Error(), "unicast browse") {
		t.Fatalf("err = %v, want a wrapped unicast browse failure", err)
	}
}

// TestResolveRegistryUnicastEmptyResponse drives the unicast SUCCESS
// return: a UDP resolver that answers the PTR query with a valid but
// empty DNS response makes ResolveUnicast return no instances and no
// error, so resolveRegistry falls through to pickQueryInstance (which
// then reports the empty result). This is the only path that reaches
// the unicast branch's success return rather than its error arm.
func TestResolveRegistryUnicastEmptyResponse(t *testing.T) {
	addr := udpDNSResponder(t)
	_, _, err := resolveRegistry(context.Background(), ControllerOptions{
		DiscoveryMode:    "unicast",
		UnicastResolver:  addr,
		UnicastDomain:    "example.invalid",
		DiscoveryTimeout: 500 * time.Millisecond,
	})
	if err == nil || !strings.Contains(err.Error(), "no _nmos-query._tcp") {
		t.Fatalf("err = %v, want the empty resolver result reported by pickQueryInstance", err)
	}
}

// udpDNSResponder listens on loopback UDP and answers each datagram with
// a valid, empty DNS response (response bit set, zero answers). Returns
// its host:port. Used to make ResolveUnicast succeed with no instances.
func udpDNSResponder(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })

	var msg dnssdcodec.Message
	msg.Header.SetResponse(true)
	reply, err := msg.Encode()
	if err != nil {
		t.Fatalf("encode empty response: %v", err)
	}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			_ = n
			_, _ = pc.WriteTo(reply, from)
		}
	}()
	return pc.LocalAddr().String()
}

// TestResolveRegistryMDNSNoInstances: with the mDNS browser running but
// nothing on the link answering _nmos-query._tcp, discovery must report
// the empty result rather than hang or guess. Exercises browseQueryMDNS
// end to end (browser open, timed browse, empty drain) and
// pickQueryInstance's no-instances arm.
func TestResolveRegistryMDNSNoInstances(t *testing.T) {
	_, _, err := resolveRegistry(context.Background(), ControllerOptions{
		DiscoveryMode:    "mdns",
		DiscoveryTimeout: 150 * time.Millisecond,
	})
	if err == nil || !strings.Contains(err.Error(), "no _nmos-query._tcp") {
		t.Fatalf("err = %v, want the no-instances-discovered report", err)
	}
}

// TestResolveRegistryMDNSDiscoversInstance drives the full mDNS success
// path: a crafted _nmos-query._tcp announcement is multicast on the
// link while the browser runs, and discovery must select it and build a
// base URL from the advertised A record. This is the only path that
// exercises browseQueryMDNS's instance-collecting loop and
// resolveRegistry's mDNS success return.
func TestResolveRegistryMDNSDiscoversInstance(t *testing.T) {
	pkt, err := dnssdcodec.EncodeAnnounce(dnssdcodec.Instance{
		Name: "dhs-query-1", Service: dnssdcodec.ServiceQuery, Domain: "local",
		Host: "dhs-query-1.local", Port: 8235,
		IPv4: []net.IP{net.IPv4(10, 6, 0, 9)},
		TXT:  map[string]string{"api_proto": "http", "api_ver": "v1.2,v1.3", "pri": "10"},
	}, true)
	if err != nil {
		t.Fatalf("encode announce: %v", err)
	}

	stop := make(chan struct{})
	go func() {
		conn, derr := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353})
		if derr != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = conn.Write(pkt)
				time.Sleep(20 * time.Millisecond)
			}
		}
	}()
	defer close(stop)

	base, vers, err := resolveRegistry(context.Background(), ControllerOptions{
		DiscoveryMode:    "mdns",
		DiscoveryTimeout: 1500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("mDNS discovery must find the announced instance: %v", err)
	}
	if base != "http://10.6.0.9:8235" {
		t.Errorf("base = %q, want the advertised A record and port", base)
	}
	if len(vers) != 2 || vers[0] != "v1.2" || vers[1] != "v1.3" {
		t.Errorf("vers = %v, want the api_ver TXT split", vers)
	}
}

// fakeBrowser is a dnssd session Browser whose Browse can be made to
// fail, so a test can drive browseQueryMDNS's Browse-error arm without a
// real transport (the stdlib browser does not fail Browse on this host).
type fakeBrowser struct{ browseErr error }

func (fakeBrowser) Close() error { return nil }

func (b fakeBrowser) Browse(context.Context, string) (<-chan dnssdcodec.Instance, error) {
	if b.browseErr != nil {
		return nil, b.browseErr
	}
	ch := make(chan dnssdcodec.Instance)
	close(ch)
	return ch, nil
}

// TestResolveRegistryMDNSNewBrowserError: a browser that will not open
// must surface as a wrapped mDNS browse error, not a silent empty
// result. The browser-constructor seam is overridden to fail, covering
// browseQueryMDNS's NewBrowser arm and resolveRegistry's mDNS wrap.
func TestResolveRegistryMDNSNewBrowserError(t *testing.T) {
	orig := newQueryBrowser
	newQueryBrowser = func(*slog.Logger) (dnssdsession.Browser, error) {
		return nil, fmt.Errorf("no transport")
	}
	defer func() { newQueryBrowser = orig }()

	_, _, err := resolveRegistry(context.Background(), ControllerOptions{
		DiscoveryMode: "mdns", DiscoveryTimeout: 100 * time.Millisecond,
	})
	if err == nil || !strings.Contains(err.Error(), "mDNS browse") ||
		!strings.Contains(err.Error(), "no transport") {
		t.Fatalf("err = %v, want the wrapped NewBrowser failure", err)
	}
}

// TestResolveRegistryMDNSBrowseError: a browser that opens but whose
// Browse call fails must also surface as a wrapped mDNS browse error —
// the arm after the browser is constructed.
func TestResolveRegistryMDNSBrowseError(t *testing.T) {
	orig := newQueryBrowser
	newQueryBrowser = func(*slog.Logger) (dnssdsession.Browser, error) {
		return fakeBrowser{browseErr: fmt.Errorf("browse refused")}, nil
	}
	defer func() { newQueryBrowser = orig }()

	_, _, err := resolveRegistry(context.Background(), ControllerOptions{
		DiscoveryMode: "mdns", DiscoveryTimeout: 100 * time.Millisecond,
	})
	if err == nil || !strings.Contains(err.Error(), "mDNS browse") ||
		!strings.Contains(err.Error(), "browse refused") {
		t.Fatalf("err = %v, want the wrapped Browse failure", err)
	}
}

// --- pickQueryInstance ------------------------------------------------

// TestPickQueryInstance: selection filters to _nmos-query._tcp, picks
// the lowest pri integer (highest priority per IS-04 §3), prefers the
// advertised A record over the SRV hostname, and defaults api_proto to
// http when the TXT omits it.
func TestPickQueryInstance(t *testing.T) {
	insts := []dnssdcodec.Instance{
		{Service: dnssdcodec.ServiceSystem, Host: "sys.local", Port: 1}, // filtered out
		{Service: dnssdcodec.ServiceQuery, Host: "low-pri.local", Port: 8235,
			TXT: map[string]string{"pri": "100", "api_proto": "https", "api_ver": "v1.3"}},
		{Service: dnssdcodec.ServiceQuery, Host: "win.local", Port: 8236,
			IPv4: []net.IP{net.IPv4(10, 0, 0, 5)},
			TXT:  map[string]string{"pri": "5", "api_ver": "v1.2,v1.3"}}, // wins, no api_proto
	}
	base, vers, err := pickQueryInstance(insts)
	if err != nil {
		t.Fatalf("pickQueryInstance: %v", err)
	}
	if base != "http://10.0.0.5:8236" {
		t.Errorf("base = %q: must pick lowest pri, its A record, and default http", base)
	}
	if len(vers) != 2 {
		t.Errorf("vers = %v, want the two-minor api_ver", vers)
	}
}

// TestPickQueryInstanceNone: a discovery result with no
// _nmos-query._tcp records is an error, never a guessed default.
func TestPickQueryInstanceNone(t *testing.T) {
	_, _, err := pickQueryInstance([]dnssdcodec.Instance{
		{Service: dnssdcodec.ServiceSystem, Host: "sys.local", Port: 1},
	})
	if err == nil || !strings.Contains(err.Error(), "no _nmos-query._tcp") {
		t.Fatalf("err = %v, want the no-instances error", err)
	}
}

// TestPickQueryInstanceUsesSRVHostWhenNoARecord: with no A record the
// SRV hostname (trailing dot trimmed) is the fallback host.
func TestPickQueryInstanceUsesSRVHostWhenNoARecord(t *testing.T) {
	base, _, err := pickQueryInstance([]dnssdcodec.Instance{
		{Service: dnssdcodec.ServiceQuery, Host: "only-srv.local.", Port: 8235,
			TXT: map[string]string{"api_proto": "http"}},
	})
	if err != nil {
		t.Fatalf("pickQueryInstance: %v", err)
	}
	if base != "http://only-srv.local:8235" {
		t.Errorf("base = %q, want the SRV host with its trailing dot trimmed", base)
	}
}

// --- pickCodec --------------------------------------------------------

func TestPickCodec(t *testing.T) {
	rep := &spec.SliceReporter{}

	t.Run("override registered", func(t *testing.T) {
		c, err := pickCodec("v1.2", []string{"v1.3"}, rep)
		if err != nil || c.APIVer() != "v1.2" {
			t.Fatalf("override must win over peer list: %v / %v", c, err)
		}
	})

	t.Run("override not registered", func(t *testing.T) {
		if _, err := pickCodec("v9.9", nil, rep); err == nil {
			t.Fatal("an unregistered override must error")
		}
	})

	t.Run("no peer versions assumes highest", func(t *testing.T) {
		c, err := pickCodec("", nil, rep)
		if err != nil || c.APIVer() != is04.Default().APIVer() {
			t.Fatalf("empty peer list must assume our highest: %v / %v", c, err)
		}
	})

	t.Run("selects highest mutual", func(t *testing.T) {
		c, err := pickCodec("", []string{"v1.0", "v1.2"}, rep)
		if err != nil || c.APIVer() != "v1.2" {
			t.Fatalf("must pick the highest mutually supported: %v / %v", c, err)
		}
	})
}

// TestPickCodecNoCommonVersionFiresEvent: when the peer shares no minor
// with our codecs, pickCodec must fire a compliance event AND return
// the error — never silently downgrade.
func TestPickCodecNoCommonVersionFiresEvent(t *testing.T) {
	rep := &spec.SliceReporter{}
	if _, err := pickCodec("", []string{"v0.1", "v9.9"}, rep); err == nil {
		t.Fatal("no common version must be an error")
	}
	if !hasCode(rep, "nmos_no_common_api_ver") {
		t.Error("no common version must fire nmos_no_common_api_ver before bubbling up")
	}
}

// --- readPri / splitAPIVerTXT / trimURLScheme -------------------------

// TestReadPri: a present numeric pri parses; a missing or non-numeric
// pri sorts as lowest priority (maxInt-ish) so it never beats a sibling.
func TestReadPri(t *testing.T) {
	const lowest = 1 << 30
	cases := []struct {
		name string
		txt  map[string]string
		want int
	}{
		{"numeric", map[string]string{"pri": "42"}, 42},
		{"absent", map[string]string{}, lowest},
		{"non-numeric", map[string]string{"pri": "10a"}, lowest},
		{"empty", map[string]string{"pri": ""}, lowest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := readPri(dnssdcodec.Instance{TXT: tc.txt}); got != tc.want {
				t.Errorf("readPri = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestSplitAPIVerTXT: the api_ver TXT is a comma list; parsing must
// trim whitespace, lowercase, and drop empties, yielding nil for "".
func TestSplitAPIVerTXT(t *testing.T) {
	if got := splitAPIVerTXT(""); got != nil {
		t.Errorf("empty TXT must yield nil, got %v", got)
	}
	got := splitAPIVerTXT(" V1.2 , v1.3 ,")
	if len(got) != 2 || got[0] != "v1.2" || got[1] != "v1.3" {
		t.Errorf("got %v, want [v1.2 v1.3] normalised", got)
	}
}

// TestTrimURLScheme: host:port survives with or without a scheme prefix.
func TestTrimURLScheme(t *testing.T) {
	if got := trimURLScheme("http://h:8235"); got != "h:8235" {
		t.Errorf("got %q, want h:8235", got)
	}
	if got := trimURLScheme("h:8235"); got != "h:8235" {
		t.Errorf("a schemeless value must pass through, got %q", got)
	}
}

// hasCode reports whether the reporter captured an event with the given
// code — the assertion used across the compliance-event tests.
func hasCode(rep *spec.SliceReporter, code string) bool {
	for _, e := range rep.Snapshot() {
		if e.Code == code {
			return true
		}
	}
	return false
}
