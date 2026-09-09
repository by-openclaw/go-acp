package registry

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	codec "dhs/internal/amwa/codec/dnssd"
	"dhs/internal/amwa/codec/is04"
	httpsession "dhs/internal/amwa/session/http"
)

// oddAddr is a net.Addr whose String() is not a host:port — the shape
// serveAdvertise must survive rather than mangle.
type oddAddr struct{}

func (oddAddr) Network() string { return "tcp" }
func (oddAddr) String() string  { return "a-socket-with-no-port" }

// The advertised endpoint follows the bind: a concrete address
// advertises itself, an unspecified one advertises the machine's
// name, and anything unparseable is passed through untouched.
func TestServeAdvertiseFollowsTheBind(t *testing.T) {
	prev := osHostnameFn
	osHostnameFn = func() (string, error) { return "mirror-host", nil }
	t.Cleanup(func() { osHostnameFn = prev })

	if got := serveAdvertise(&net.TCPAddr{IP: net.ParseIP("10.6.239.113"), Port: 8335}); got != "10.6.239.113:8335" {
		t.Errorf("concrete bind = %q", got)
	}
	if got := serveAdvertise(&net.TCPAddr{IP: net.IPv4zero, Port: 8335}); got != "mirror-host:8335" {
		t.Errorf("unspecified bind = %q, want the machine's name", got)
	}
	if got := serveAdvertise(oddAddr{}); got != "a-socket-with-no-port" {
		t.Errorf("unparseable address = %q, want it passed through", got)
	}

	// The auth gate matches host identities, not endpoints.
	if got := advertiseHostOnly("mirror-host:8335"); got != "mirror-host" {
		t.Errorf("advertiseHostOnly = %q", got)
	}
	if got := advertiseHostOnly("mirror-host"); got != "mirror-host" {
		t.Errorf("a bare host = %q, want it unchanged", got)
	}
}

// The served face advertises one instance: _nmos-query._tcp, because
// the mirror refuses registrations and must never look like a
// registration target. A negative priority clamps to 0 and an unset
// protocol reads as plain http.
func TestServeAnnounceInstance(t *testing.T) {
	ins := serveAnnounceInstance("mirror-host", 8335, []string{"v1.2", "v1.3"}, -5, true, "")
	if ins.Service != codec.ServiceQuery {
		t.Errorf("service = %q, want the Query face alone", ins.Service)
	}
	if ins.TXT[codec.TXTKeyPriority] != "0" {
		t.Errorf("pri = %q, want a negative priority clamped", ins.TXT[codec.TXTKeyPriority])
	}
	if ins.TXT[codec.TXTKeyAPIProto] != "http" {
		t.Errorf("api_proto = %q, want the plain default", ins.TXT[codec.TXTKeyAPIProto])
	}
	if ins.TXT[codec.TXTKeyAPIVer] != "v1.2,v1.3" {
		t.Errorf("api_ver = %q", ins.TXT[codec.TXTKeyAPIVer])
	}
	if ins.TXT[codec.TXTKeyAPIAuth] != "true" {
		t.Errorf("api_auth = %q", ins.TXT[codec.TXTKeyAPIAuth])
	}
}

// mDNS is a convenience for the served face, never a requirement: an
// advertise host it cannot parse, a responder it cannot open, and a
// refused announce are each a warning, and the REST face keeps
// serving.
func TestAnnounceServeFailuresAreWarnings(t *testing.T) {
	m := mirrorTo(t, "http://target:8235")
	tap := newRegistryLogTap()
	m.logger = tap.logger()

	m.announceServe(context.Background(), "no-port", []string{"v1.3"})
	if !tap.has("bad advertise host") {
		t.Errorf("an unparseable advertise host must be warned; saw %v", tap.snapshot())
	}

	useRegistryResponder(t, nil)
	m.announceServe(context.Background(), "mirror-host:8335", []string{"v1.3"})
	if !tap.has("mDNS unavailable") {
		t.Errorf("a responder that cannot open must be warned; saw %v", tap.snapshot())
	}

	useRegistryResponder(t, &scriptedRegistryResponder{announceErr: errors.New("group busy")})
	m.announceServe(context.Background(), "mirror-host:8335", []string{"v1.3"})
	if !tap.has("serve announce failed") {
		t.Errorf("a refused announce must be warned; saw %v", tap.snapshot())
	}

	// A successful announce holds until the context ends.
	fake := &scriptedRegistryResponder{}
	useRegistryResponder(t, fake)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.announceServe(ctx, "mirror-host:8335", []string{"v1.3"})
		close(done)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if n, _ := fake.snapshot(); len(n) > 0 || !time.Now().Before(deadline) {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if n, _ := fake.snapshot(); len(n) == 0 {
		t.Fatal("nothing was announced")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the announce must end with its context")
	}
	if _, closed := fake.snapshot(); closed == 0 {
		t.Error("the responder must be closed on the way out")
	}
}

// The served face's discovery roots list the Query face only: a
// walking client never learns a registration URL the mirror would
// refuse anyway.
func TestMirrorServeRootsOmitRegistration(t *testing.T) {
	srv := httpsession.NewServer(nil)
	installMirrorServeRoots(srv, []string{"v1.3"})
	handler := srv.MuxHandler()

	for _, path := range []string{"/x-nmos", "/x-nmos/"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(stdhttp.MethodGet, path, nil))
		var got []string
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("GET %s: %v (%s)", path, err, rec.Body)
		}
		if len(got) != 1 || got[0] != "query/" {
			t.Errorf("GET %s = %v, want the Query face alone", path, got)
		}
	}
	for _, path := range []string{"/x-nmos/query", "/x-nmos/query/"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(stdhttp.MethodGet, path, nil))
		var got []string
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("GET %s: %v (%s)", path, err, rec.Body)
		}
		if len(got) != 1 || got[0] != "v1.3/" {
			t.Errorf("GET %s = %v", path, got)
		}
	}
}

// serveRow builds one grain row as the source's Query WS delivers it:
// a `post` alone is an addition, a `pre` alone a removal — IS-04 §5.2
// grammar, which is what applyServeRow dispatches on.
func serveRow(t *testing.T, id string, post any) is04.GrainDataRow {
	t.Helper()
	row := is04.GrainDataRow{Path: id}
	if post != nil {
		row.Post = mustJSONBytes(t, post)
	}
	return row
}

func removalRow(t *testing.T, id string, pre any) is04.GrainDataRow {
	t.Helper()
	return is04.GrainDataRow{Path: id, Pre: mustJSONBytes(t, pre)}
}

// Rows applied to the embedded store follow the same rules the store
// enforces for anyone else: a child whose parent has not arrived is
// refused and arms the ordered replay, and a removal that finds
// nothing left (the parent's delete already cascaded) is not an
// error.
func TestApplyServeRowIngestsAndRepairs(t *testing.T) {
	m := mirrorTo(t, "http://target:8235")
	tap := newRegistryLogTap()
	m.logger = tap.logger()

	// With no served face there is nothing to apply to.
	m.applyServeRow("nodes", "v1.3", serveRow(t, fxNode, validNode(fxNode)), true)

	m.serve = &mirrorServe{store: NewStore()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.mu.Lock()
	m.runCtx = ctx
	m.mu.Unlock()

	// A topic IS-04 does not define is reported, not applied.
	m.applyServeRow("widgets", "v1.3", serveRow(t, fxNode, validNode(fxNode)), false)
	if !tap.has("unknown topic") {
		t.Errorf("an unknown topic must be reported; saw %v", tap.snapshot())
	}

	// A device whose node has not arrived yet: refused, and the
	// ordered replay is armed.
	m.applyServeRow("devices", "v1.3", serveRow(t, fxDevice, validDevice(fxDevice, fxNode)), true)
	if !tap.has("store ingest failed") {
		t.Errorf("the refusal must be reported; saw %v", tap.snapshot())
	}
	m.mu.Lock()
	armed := m.serveReplayTimer != nil
	m.mu.Unlock()
	if !armed {
		t.Error("a parent-missing row must arm the ordered replay")
	}

	// Its node arrives, then the device lands.
	m.applyServeRow("nodes", "v1.3", serveRow(t, fxNode, validNode(fxNode)), false)
	m.applyServeRow("devices", "v1.3", serveRow(t, fxDevice, validDevice(fxDevice, fxNode)), false)
	if _, err := m.serve.store.GetDevice(fxDevice); err != nil {
		t.Errorf("the device did not land: %v", err)
	}

	// A removal row (no post) deletes it; a second one finds nothing
	// left, which is fine — a node delete cascades in the store.
	m.applyServeRow("devices", "v1.3", removalRow(t, fxDevice, validDevice(fxDevice, fxNode)), false)
	if _, err := m.serve.store.GetDevice(fxDevice); !errors.Is(err, ErrNotFound) {
		t.Errorf("the device was not removed: %v", err)
	}
	before := len(tap.snapshot())
	m.applyServeRow("devices", "v1.3", removalRow(t, fxDevice, validDevice(fxDevice, fxNode)), false)
	for _, line := range tap.snapshot()[before:] {
		if line == "registry/mirror: serve: store delete failed" {
			t.Error("a cascaded-away child must not be reported as a failed delete")
		}
	}
}

// The ordered replay re-applies the whole cache parent-first, so
// every child finds its parent registered — and re-ingesting an
// unchanged document emits no grain, which is what makes a replay
// safe to run at any time.
func TestServeReplayAppliesTheCacheInOrder(t *testing.T) {
	m := mirrorTo(t, "http://target:8235")
	m.logger = newRegistryLogTap().logger()

	// No served face: nothing to replay.
	m.serveReplay()

	m.serve = &mirrorServe{store: NewStore()}
	m.mu.Lock()
	m.cache["nodes"] = map[string]json.RawMessage{fxNode: mustJSONBytes(t, validNode(fxNode))}
	m.cache["devices"] = map[string]json.RawMessage{fxDevice: mustJSONBytes(t, validDevice(fxDevice, fxNode))}
	m.cache["sources"] = map[string]json.RawMessage{fxSource: mustJSONBytes(t, validSource(fxSource, fxDevice))}
	// The device's minor is tracked; the node's is not, so it takes
	// the mirror's primary one.
	m.cacheVer["devices"] = map[string]string{fxDevice: "v1.3"}
	m.mu.Unlock()

	var grains int
	m.serve.store.AddListener(func(Change) { grains++ })
	m.serveReplay()

	for _, check := range []struct {
		name string
		err  error
	}{
		{"node", firstErr(m.serve.store.GetNode(fxNode))},
		{"device", firstErr(m.serve.store.GetDevice(fxDevice))},
		{"source", firstErr(m.serve.store.GetSource(fxSource))},
	} {
		if check.err != nil {
			t.Errorf("%s did not land: %v", check.name, check.err)
		}
	}
	if grains != 3 {
		t.Fatalf("first replay emitted %d grains, want one per resource", grains)
	}

	// A second replay of the same cache is silent — no same-body
	// `modified` grain reaches a subscriber (AMWA test_24_1).
	grains = 0
	m.serveReplay()
	if grains != 0 {
		t.Errorf("a replay of unchanged documents emitted %d grains", grains)
	}
}

// The debounced replay does not arm outside a live run, and fires
// once the debounce elapses.
func TestScheduleServeReplayNeedsALiveRun(t *testing.T) {
	m := mirrorTo(t, "http://target:8235")
	m.logger = newRegistryLogTap().logger()
	m.serve = &mirrorServe{store: NewStore()}

	m.scheduleServeReplay()
	m.mu.Lock()
	armed := m.serveReplayTimer != nil
	m.mu.Unlock()
	if armed {
		t.Fatal("a replay must not arm outside a run")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.mu.Lock()
	m.runCtx = ctx
	m.cache["nodes"] = map[string]json.RawMessage{fxNode: mustJSONBytes(t, validNode(fxNode))}
	m.mu.Unlock()

	m.scheduleServeReplay()
	m.scheduleServeReplay() // a second arm extends the same timer

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := m.serve.store.GetNode(fxNode); err == nil {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Error("the debounced replay never fired")
}
