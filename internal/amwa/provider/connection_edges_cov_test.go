package provider

// The IS-05 face under conditions the happy path never reaches: an
// endpoint that has left the store, a body the transport cut short, a
// bulk request where some ids succeed and others do not, and the
// receiver that has to derive its own transport parameters from the
// SDP a controller copied across.

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"dhs/internal/amwa/codec/is05"
)

// is05Server builds a mounted IS-05 server over the routable bundle
// and returns it with the ids it carries.
func is05Server(t *testing.T) (*IS05ConnectionServer, string, string) {
	t.Helper()
	b := routableBundle(t)
	s := NewIS05ConnectionServer(newLogTap().logger(), b, IS05ConnectionConfig{APIVer: "v1.2"})
	return s, b.Senders[0].ID, b.Receivers[0].ID
}

// forget removes an endpoint from the store, leaving its routes in
// place — the state a runtime bundle change produces.
func forget(t *testing.T, s *IS05ConnectionServer, kind, id string) {
	t.Helper()
	st := s.Store()
	st.mu.Lock()
	defer st.mu.Unlock()
	if kind == "senders" {
		delete(st.senders, id)
		return
	}
	delete(st.receivers, id)
}

// Every per-endpoint route answers 404 for an endpoint the store no
// longer holds. The routing table is built once at mount and outlives
// the endpoint, so each route has to say so for itself rather than
// panicking on a nil.
func TestEndpointRoutesAnswer404WhenTheEndpointIsGone(t *testing.T) {
	s, sid, _ := is05Server(t)
	srv := newTestServer(t, s)
	base := "/x-nmos/connection/v1.2/single/senders/" + sid

	for _, leaf := range []string{"constraints", "transporttype", "staged", "active", "transportfile"} {
		if code, _ := get(t, srv, base+"/"+leaf+"/"); code != stdhttp.StatusOK {
			t.Fatalf("%s must answer while the sender is there: %d", leaf, code)
		}
	}

	forget(t, s, "senders", sid)

	for _, leaf := range []string{"constraints", "transporttype", "staged", "active", "transportfile"} {
		if code, body := get(t, srv, base+"/"+leaf+"/"); code != stdhttp.StatusNotFound {
			t.Errorf("%s = %d %s, want 404", leaf, code, body)
		}
	}
}

// newTestServer mounts the connection server on an httptest server and
// returns its address.
func newTestServer(t *testing.T, s *IS05ConnectionServer) string {
	t.Helper()
	addr := freeAddr(t)
	node, err := NewIS04NodeServer(newLogTap().logger(), routableBundle(t), IS04NodeConfig{
		Bind: addr, DiscoveryMode: "static", ConnectionAPIVer: "v1.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	node.connection = s
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = node.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = node.Stop()
	})
	if !waitReachable(t, "http://"+addr+"/__ready__", 5*time.Second) {
		t.Fatal("the node never came up")
	}
	return addr
}

// A PATCH body the transport cut short is a bad request, not a partial
// stage: the endpoint must not be left half-configured by a request
// that never finished arriving.
func TestStagedPatchRefusesABodyItCannotRead(t *testing.T) {
	s, sid, _ := is05Server(t)
	r := httptest.NewRequest(stdhttp.MethodPatch, "/staged", iotest.ErrReader(errTest("reset")))

	status, _, err := s.handlePatch("senders", sid, r)
	if err != nil || status != stdhttp.StatusBadRequest {
		t.Fatalf("= %d (%v), want 400", status, err)
	}
}

// A body that is not a staged object is refused, and so is one naming
// an endpoint that is not there — with the status the store chose, not
// a blanket 400.
func TestStagedPatchRefusals(t *testing.T) {
	s, sid, _ := is05Server(t)

	patch := func(kind, id, body string) (int, any) {
		t.Helper()
		r := httptest.NewRequest(stdhttp.MethodPatch, "/staged", strings.NewReader(body))
		status, out, err := s.handlePatch(kind, id, r)
		if err != nil {
			t.Fatalf("handlePatch: %v", err)
		}
		return status, out
	}

	if status, _ := patch("senders", sid, `{`); status != stdhttp.StatusBadRequest {
		t.Errorf("a body that is not JSON = %d, want 400", status)
	}
	if status, _ := patch("senders", sid, `{"nonsense":true}`); status != stdhttp.StatusBadRequest {
		t.Errorf("an unknown field = %d, want 400 — the staged schemas forbid extras", status)
	}
	if status, _ := patch("senders", "not-an-endpoint", `{"master_enable":true}`); status != stdhttp.StatusNotFound {
		t.Errorf("an endpoint that is not there = %d, want 404", status)
	}
}

// A bulk request can partially succeed, and the answer is one entry
// per id rather than a single code — a controller has to be able to
// tell which endpoint refused.
func TestBulkReportsPerEndpointOutcomes(t *testing.T) {
	s, sid, _ := is05Server(t)

	body := `[
		{"id":"` + sid + `","params":{"master_enable":false}},
		{"id":"not-an-endpoint","params":{"master_enable":false}},
		{"id":"` + sid + `","params":{"nonsense":true}}
	]`
	r := httptest.NewRequest(stdhttp.MethodPost, "/bulk/senders", strings.NewReader(body))
	status, out, err := s.handleBulk("senders", r)
	if err != nil || status != stdhttp.StatusOK {
		t.Fatalf("= %d (%v), want 200 with per-id outcomes", status, err)
	}
	// The body is a slice of an anonymous per-id result type, so it is
	// checked through its rendering rather than by naming that type
	// here: three entries, one per id offered.
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("the bulk answer is not a list: %v (%s)", err, raw)
	}
	if len(got) != 3 {
		t.Fatalf("outcomes = %s, want one per id offered", raw)
	}
	if got[0]["code"].(float64) != 200 {
		t.Errorf("the endpoint that exists = %v", got[0])
	}
	if got[1]["code"].(float64) != 404 {
		t.Errorf("the endpoint that does not = %v", got[1])
	}
	if got[2]["code"].(float64) != 400 {
		t.Errorf("the body with an unknown field = %v", got[2])
	}
}

// A bulk body that is not an array of {id, params} is refused whole:
// there are no ids to report outcomes against.
func TestBulkRefusesABodyItCannotRead(t *testing.T) {
	s, _, _ := is05Server(t)

	r := httptest.NewRequest(stdhttp.MethodPost, "/bulk/senders", iotest.ErrReader(errTest("reset")))
	if status, _, err := s.handleBulk("senders", r); err != nil || status != stdhttp.StatusBadRequest {
		t.Errorf("a body that cannot be read = %d (%v), want 400", status, err)
	}

	r = httptest.NewRequest(stdhttp.MethodPost, "/bulk/senders", strings.NewReader(`{"not":"an array"}`))
	if status, _, err := s.handleBulk("senders", r); err != nil || status != stdhttp.StatusBadRequest {
		t.Errorf("a body that is not an array = %d (%v), want 400", status, err)
	}
}

// IS-05 §4.3: the controller copies the Sender's transport file across
// verbatim and never translates it. A Receiver that files the blob
// away and leaves its transport parameters untouched has accepted a
// connection it will not make.
func TestReceiverDerivesItsParametersFromTheSDP(t *testing.T) {
	s, _, rid := is05Server(t)

	sdp := strings.Join([]string{
		"v=0",
		"o=- 1 1 IN IP4 192.0.2.1",
		"s=test",
		"t=0 0",
		"m=video 5004 RTP/AVP 96",
		"c=IN IP4 239.10.10.10/64",
		"a=rtpmap:96 raw/90000",
		"",
	}, "\r\n")
	data := sdp
	patch := is05.StagedSender{
		TransportFile: &is05.TransportFile{
			Type: strp("application/sdp"),
			Data: &data,
		},
	}
	out, status, err := s.Store().applyPatch("receivers", rid, patch,
		patchFields{TransportFile: true})
	if err != nil || status != stdhttp.StatusOK {
		t.Fatalf("= %d (%v)", status, err)
	}
	if len(out.TransportParams) == 0 {
		t.Fatal("a receiver has at least one leg")
	}
	if got := out.TransportParams[0]["multicast_ip"]; got != "239.10.10.10" {
		t.Errorf("multicast_ip = %v, want the address the SDP named", got)
	}
}

// The leg count is fixed by the endpoint: a 2022-7 sender has two
// legs, and a single-leg PATCH against it is a controller error rather
// than a reconfiguration to one leg.
func TestPatchWithTheWrongLegCount(t *testing.T) {
	s, sid, _ := is05Server(t)

	_, status, err := s.Store().applyPatch("senders", sid, is05.StagedSender{
		TransportParams: []is05.TransportParams{{}, {}, {}},
	}, patchFields{TransportParams: true})
	if err == nil || status != stdhttp.StatusBadRequest {
		t.Fatalf("= %d (%v), want 400", status, err)
	}
}

// An IS-04 bundle the Connection API was built without cannot have its
// subscriptions updated, and saying nothing is the right answer rather
// than a nil dereference on activation.
func TestSubscriptionUpdateWithoutABundle(t *testing.T) {
	s, sid, _ := is05Server(t)
	s.bundle = nil
	s.updateIS04Subscription("senders", sid, is05.StagedSender{})
}

// A sender the IS-04 face asks about that the Connection API does not
// hold has no SDP to render, and answering with an empty one would
// advertise a stream nobody can join.
func TestSenderSDPForAnEndpointTheStoreDoesNotHold(t *testing.T) {
	node, err := NewIS04NodeServer(newLogTap().logger(), routableBundle(t), IS04NodeConfig{
		Bind: "127.0.0.1:0", DiscoveryMode: "static",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := node.senderSDP("not-a-sender"); got != "" {
		t.Errorf("= %q, want nothing", got)
	}

	node.connection = nil
	if got := node.senderSDP(routableBundle(t).Senders[0].ID); got != "" {
		t.Errorf("with no Connection API = %q, want nothing", got)
	}
}
