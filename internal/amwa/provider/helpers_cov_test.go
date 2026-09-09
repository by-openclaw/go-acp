package provider

import (
	"context"
	"crypto/x509"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/ms05"
	"dhs/internal/amwa/codec/spec"
)

// A class id matches a wanted one when it is that class, or — when
// derived classes count — one derived from it. A shorter id never
// matches a longer want.
func TestClassIDMatches(t *testing.T) {
	base := ms05.NcClassId{1, 2}
	cases := []struct {
		name           string
		have, want     ms05.NcClassId
		includeDerived bool
		match          bool
	}{
		{"exact", ms05.NcClassId{1, 2}, base, false, true},
		{"exact with derived allowed", ms05.NcClassId{1, 2}, base, true, true},
		{"derived, exact only", ms05.NcClassId{1, 2, 3}, base, false, false},
		{"derived, derived allowed", ms05.NcClassId{1, 2, 3}, base, true, true},
		{"shorter than wanted", ms05.NcClassId{1}, base, true, false},
		{"same length, different class", ms05.NcClassId{1, 9}, base, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classIDMatches(tc.have, tc.want, tc.includeDerived); got != tc.match {
				t.Errorf("classIDMatches(%v, %v, %v) = %v", tc.have, tc.want, tc.includeDerived, got)
			}
		})
	}
}

// The IS-05 transport type collapses every RTP flavour to the base urn —
// the Connection API declares the family, not the cast mode — and leaves
// anything else alone.
func TestIS05TransportTypeAndNotFound(t *testing.T) {
	for in, want := range map[string]string{
		"urn:x-nmos:transport:rtp":       "urn:x-nmos:transport:rtp",
		"urn:x-nmos:transport:rtp.mcast": "urn:x-nmos:transport:rtp",
		"urn:x-nmos:transport:rtp.ucast": "urn:x-nmos:transport:rtp",
		"urn:x-nmos:transport:websocket": "urn:x-nmos:transport:websocket",
	} {
		if got := is05TransportType(in); got != want {
			t.Errorf("is05TransportType(%q) = %q, want %q", in, got, want)
		}
	}

	status, body, err := notFound(errRegistryGone)
	if status != stdhttp.StatusNotFound || err != nil {
		t.Fatalf("notFound = %d, %v", status, err)
	}
	raw, _ := json.Marshal(body)
	if !strings.Contains(string(raw), errRegistryGone.Error()) || !strings.Contains(string(raw), `"code":404`) {
		t.Errorf("notFound body = %s, want the IS-04 envelope naming the cause", raw)
	}
}

var errRegistryGone = errTest("registry gone")

type errTest string

func (e errTest) Error() string { return string(e) }

// Small pure helpers: a resource version is TAI seconds before the colon,
// maxInt is the larger of two, and every DhsFaultControl method is
// recognised as one.
func TestTaiSecondsMaxIntAndFaultMethods(t *testing.T) {
	for in, want := range map[string]string{
		"1600000000:250": "1600000000",
		"1600000000":     "1600000000",
		"":               "0",
		":250":           ":250", // no seconds part: returned unchanged
	} {
		if got := taiSeconds(in); got != want {
			t.Errorf("taiSeconds(%q) = %q, want %q", in, got, want)
		}
	}
	if maxInt(2, 7) != 7 || maxInt(7, 2) != 7 || maxInt(3, 3) != 3 {
		t.Error("maxInt must return the larger operand")
	}
	for _, m := range []string{"InjectMonitorFault", "ClearMonitorFault", "SetMonitorSyncSource", "AddMonitorPacketCounters"} {
		if !isFaultMethod(m) {
			t.Errorf("%s is a DhsFaultControl method", m)
		}
	}
	if isFaultMethod("Get") {
		t.Error("a standard NCP method is not a fault method")
	}
}

// A sender's fallback SDP names the sender and is valid enough to parse:
// an unlabelled sender is named by id, and both RTP cast modes produce a
// session description.
func TestSDPForSender(t *testing.T) {
	labelled := sdpFor(is04.Sender{
		ResourceCore: is04.ResourceCore{ID: "s-1", Label: "CAM 1"},
		Transport:    "urn:x-nmos:transport:rtp.mcast",
	})
	if !strings.Contains(labelled, "s=CAM 1") || !strings.HasPrefix(labelled, "v=0") {
		t.Errorf("labelled sender SDP =\n%s", labelled)
	}
	unlabelled := sdpFor(is04.Sender{
		ResourceCore: is04.ResourceCore{ID: "s-2"},
		Transport:    "urn:x-nmos:transport:rtp.ucast",
	})
	if !strings.Contains(unlabelled, "s=dhs-sender-s-2") {
		t.Errorf("unlabelled sender SDP =\n%s", unlabelled)
	}
}

// findReceiverByID returns a pointer INTO the slice, so a caller mutates
// the bundle's own receiver, and nil when the id is unknown.
func TestFindReceiverByID(t *testing.T) {
	rs := []is04.Receiver{
		{ResourceCore: is04.ResourceCore{ID: "a"}},
		{ResourceCore: is04.ResourceCore{ID: "b"}},
	}
	got := findReceiverByID(rs, "b")
	if got == nil || got.ID != "b" {
		t.Fatalf("findReceiverByID = %+v", got)
	}
	got.Label = "mutated"
	if rs[1].Label != "mutated" {
		t.Error("the returned receiver must be the slice's own element")
	}
	if findReceiverByID(rs, "zzz") != nil {
		t.Error("an unknown id must return nil")
	}
}

// The log reporter maps each severity onto the matching log level and
// carries the peer only when there is one; without a logger there is no
// reporter at all.
func TestLogReporter(t *testing.T) {
	if newLogReporter(nil) != nil {
		t.Fatal("no logger means no reporter")
	}
	tap := newLogTap()
	r := newLogReporter(tap.logger())
	r.Report(spec.ComplianceEvent{SpecID: "is-04", Code: "x", Severity: spec.SeverityError, Detail: "an error", PeerHost: "reg:8235"})
	r.Report(spec.ComplianceEvent{SpecID: "is-04", Code: "y", Severity: spec.SeverityWarn, Detail: "a warning"})
	r.Report(spec.ComplianceEvent{SpecID: "is-04", Code: "z", Severity: spec.SeverityInfo, Detail: "a note"})
	for _, want := range []string{"an error", "a warning", "a note"} {
		if !tap.has(want) {
			t.Errorf("reporter dropped %q; saw %v", want, tap.snapshot())
		}
	}
}

// stubWatcher is a registrySource a test drives directly.
type stubWatcher struct {
	best         RegistryCandidate
	ok           bool
	disqualified []string
}

func (w *stubWatcher) Best() (RegistryCandidate, bool) { return w.best, w.ok }
func (w *stubWatcher) Disqualify(full string)          { w.disqualified = append(w.disqualified, full) }

// With a watcher the client follows whichever Registry the watcher names
// (its own api_ver, or the client's when the advertisement omits one),
// reports "no registry" when the watcher has none, and disqualifies the
// one it is talking to on demand. Without a watcher it stays on the URL
// it was built with.
func TestRegistrationClientPickBaseAndDisqualify(t *testing.T) {
	c := NewRegistrationClient(nil, "http://built-in:8235", "v1.3", validBundle())
	if base, ok := c.pickBase(); !ok || !strings.HasPrefix(base, "http://built-in:8235") {
		t.Fatalf("no watcher: pickBase = %q, %v", base, ok)
	}

	w := &stubWatcher{ok: true, best: RegistryCandidate{FullName: "reg-1._nmos-register._tcp.local", URL: "http://reg-1:8235/", APIVer: "v1.2"}}
	c.SetWatcher(w)
	base, ok := c.pickBase()
	if !ok || base != "http://reg-1:8235/x-nmos/registration/v1.2" {
		t.Fatalf("watcher base = %q, %v", base, ok)
	}
	c.disqualifyCurrent()
	if len(w.disqualified) != 1 || w.disqualified[0] != w.best.FullName {
		t.Errorf("disqualified %v, want the Registry in use", w.disqualified)
	}

	// An advertisement without an api_ver takes the client's.
	w.best.APIVer = ""
	if base, _ := c.pickBase(); !strings.HasSuffix(base, "/v1.3") {
		t.Errorf("base without an advertised api_ver = %q, want the client's version", base)
	}

	// No candidate: no base, and nothing to disqualify.
	w.ok = false
	if base, ok := c.pickBase(); ok || base != "" {
		t.Errorf("watcher with no candidate = %q, %v", base, ok)
	}
	c.disqualifyCurrent()
	if len(w.disqualified) != 1 {
		t.Errorf("nothing to disqualify once the Registry is gone: %v", w.disqualified)
	}
}

// The registration callback fires only on a CHANGE of state, and can be
// cleared.
func TestRegistrationClientOnRegisteredFiresOnChange(t *testing.T) {
	c := NewRegistrationClient(nil, "http://reg:8235", "v1.3", validBundle())
	var calls atomic.Int32
	var last atomic.Bool
	c.SetOnRegistered(func(v bool) { calls.Add(1); last.Store(v) })

	c.setRegistered(true)
	c.setRegistered(true) // unchanged: silent
	if calls.Load() != 1 || !last.Load() {
		t.Errorf("calls=%d last=%v, want one call reporting registered", calls.Load(), last.Load())
	}
	c.setRegistered(false)
	if calls.Load() != 2 || last.Load() {
		t.Errorf("calls=%d last=%v, want a second call reporting unregistered", calls.Load(), last.Load())
	}
	c.SetOnRegistered(nil)
	c.setRegistered(true)
	if calls.Load() != 2 {
		t.Errorf("a cleared callback must not fire (%d)", calls.Load())
	}
}

// rejoinOrRegister heartbeats first: a Registry that still knows this
// Node is simply rejoined, one that answers 404 gets the whole bundle
// re-registered, and any other failure is reported as is.
func TestRegistrationClientRejoinOrRegister(t *testing.T) {
	var heartbeat, posts atomic.Int32
	status := atomic.Int32{}
	status.Store(stdhttp.StatusOK)
	mux := stdhttp.NewServeMux()
	mux.HandleFunc("/x-nmos/registration/v1.3/health/nodes/", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		heartbeat.Add(1)
		w.WriteHeader(int(status.Load()))
	})
	mux.HandleFunc("/x-nmos/registration/v1.3/resource", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		posts.Add(1)
		w.WriteHeader(stdhttp.StatusCreated)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	c := NewRegistrationClient(nil, ts.URL, "v1.3", validBundle())
	if _, ok := c.pickBase(); !ok {
		t.Fatal("the client must have a base")
	}
	if err := c.rejoinOrRegister(context.Background()); err != nil {
		t.Fatalf("healthy Registry: %v", err)
	}
	if posts.Load() != 0 {
		t.Error("a Registry that still knows this Node must not be re-registered")
	}

	status.Store(stdhttp.StatusNotFound)
	if err := c.rejoinOrRegister(context.Background()); err != nil {
		t.Fatalf("evicted Node: %v", err)
	}
	if posts.Load() == 0 {
		t.Error("a 404 heartbeat must trigger a full re-registration")
	}

	status.Store(stdhttp.StatusInternalServerError)
	if err := c.rejoinOrRegister(context.Background()); err == nil {
		t.Error("a 500 heartbeat must be reported, not re-registered")
	}
}

// The Node's own resource marshals as the Registration API sends it, and
// the two injection points on the client are wired from the Node server:
// a token source only when tokens are configured, a trust root only when
// certificates are.
func TestRegistrationClientMarshalNodeAndAttachments(t *testing.T) {
	c := NewRegistrationClient(nil, "http://reg:8235", "v1.3", validBundle())
	raw, err := c.MarshalNode()
	if err != nil || !strings.Contains(string(raw), `"id"`) {
		t.Fatalf("MarshalNode = %s, %v", raw, err)
	}

	s, err := NewIS04NodeServer(nil, validBundle(), IS04NodeConfig{Bind: "127.0.0.1:0", DiscoveryMode: "static"})
	if err != nil {
		t.Fatal(err)
	}
	// Nothing configured: both attachments are no-ops.
	s.attachAuthToken(c)
	s.attachTLSTrust(c)
	if c.tokenSource != nil {
		t.Error("a Node with no authorization server must not attach a token source")
	}

	c.SetTokenSource(func(context.Context) (string, error) { return "tok", nil })
	if c.tokenSource == nil {
		t.Error("SetTokenSource must attach")
	}
	c.SetTLSRoots(nil) // nil roots change nothing
	c.SetTLSRoots(x509.NewCertPool())
	if c.http.Transport == nil {
		t.Error("SetTLSRoots must install a transport carrying the roots")
	}
}
