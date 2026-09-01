package registry

// Served-face advertise + announce + version-fidelity tests (fleet
// fixes on issue #946):
//
//   - ws_href must carry the operator-provided advertise identity, not
//     an unresolvable OS-hostname fallback (AMWA IS-04-02 test_22_2 /
//     test_23_1 / test_24_1 / test_31 "Name or service not known");
//   - the served face announces _nmos-query._tcp with the registry's
//     own TXT shape (test_02), api_auth tracking the Bearer gate;
//   - a row that arrived on a vX source subscription is stamped vX in
//     the embedded store, so the IS-04 §6.1.5 downgrade view survives
//     the mirrored hop (test_22 / test_32).

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	codec "dhs/internal/amwa/codec/dnssd"
	"dhs/internal/amwa/codec/is04"
	session "dhs/internal/amwa/session/dnssd"
)

// fakeResponder records Announce calls — the mirror's twin of testing
// the registry announce path without touching the real mDNS stack.
type fakeResponder struct {
	announced chan codec.Instance
}

func (f *fakeResponder) Announce(_ context.Context, ins codec.Instance) error {
	f.announced <- ins
	return nil
}
func (f *fakeResponder) Update(context.Context, codec.Instance) error { return nil }
func (f *fakeResponder) Close() error                                 { return nil }

// stubServeResponder swaps the mDNS seam for a recording fake for the
// duration of one test.
func stubServeResponder(t *testing.T) *fakeResponder {
	t.Helper()
	fr := &fakeResponder{announced: make(chan codec.Instance, 4)}
	orig := newServeResponder
	newServeResponder = func(*slog.Logger) (session.Responder, error) { return fr, nil }
	t.Cleanup(func() { newServeResponder = orig })
	return fr
}

// startAdvertisedMirror boots a served mirror with an explicit
// advertise identity (which also arms the announce path).
func startAdvertisedMirror(t *testing.T, advHost string) *Mirror {
	t.Helper()
	plant := &fakePlant{}
	target := httptest.NewServer(plant.targetHandler())
	t.Cleanup(target.Close)
	push := newPushSource()
	src := httptest.NewServer(stdhttp.NotFoundHandler())
	src.Config.Handler = push.handler(t, func() string { return src.URL })
	t.Cleanup(src.Close)

	m, err := NewMirror(MirrorOptions{
		Source: src.URL, Target: target.URL, APIVer: "v1.3",
		ServeAddr: "127.0.0.1:0", ServeAdvertiseHost: advHost, ServePri: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = m.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return m.ServeAddr() != "" }, "served face to bind")
	return m
}

// subscribeWSHref POSTs a subscription on the served face and returns
// the minted ws_href.
func subscribeWSHref(t *testing.T, base string) string {
	t.Helper()
	resp, err := stdhttp.Post(base+"/x-nmos/query/v1.3/subscriptions", "application/json",
		strings.NewReader(`{"resource_path":"/nodes","persist":false}`))
	if err != nil {
		t.Fatalf("POST subscription: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		t.Fatalf("subscription POST = %d (%s)", resp.StatusCode, body)
	}
	var sub struct {
		WSHref string `json:"ws_href"`
	}
	if err := json.Unmarshal(body, &sub); err != nil {
		t.Fatalf("subscription body: %v (%s)", err, body)
	}
	return sub.WSHref
}

// TestMirrorServeAdvertiseWSHref: an explicit --serve-advertise-host
// wins over the bound-address fallback in ws_href — bare host takes
// the bound serve port, host:port is used verbatim.
func TestMirrorServeAdvertiseWSHref(t *testing.T) {
	stubServeResponder(t)

	t.Run("bare host takes the bound port", func(t *testing.T) {
		m := startAdvertisedMirror(t, "mirror-adv.test")
		_, boundPort, err := net.SplitHostPort(m.ServeAddr())
		if err != nil {
			t.Fatal(err)
		}
		href := subscribeWSHref(t, "http://"+m.ServeAddr())
		want := "ws://mirror-adv.test:" + boundPort + "/"
		if !strings.HasPrefix(href, want) {
			t.Errorf("ws_href = %q, want prefix %q", href, want)
		}
	})

	t.Run("host:port used verbatim", func(t *testing.T) {
		m := startAdvertisedMirror(t, "mirror-adv.test:9443")
		href := subscribeWSHref(t, "http://"+m.ServeAddr())
		if !strings.HasPrefix(href, "ws://mirror-adv.test:9443/") {
			t.Errorf("ws_href = %q, want ws://mirror-adv.test:9443/…", href)
		}
	})
}

// TestMirrorServeAnnounce: an advertise identity arms the
// _nmos-query._tcp announce with the registry's TXT shape.
func TestMirrorServeAnnounce(t *testing.T) {
	fr := stubServeResponder(t)
	startAdvertisedMirror(t, "mirror-adv.test:8335")

	select {
	case ins := <-fr.announced:
		if ins.Service != codec.ServiceQuery {
			t.Errorf("service = %q, want %q", ins.Service, codec.ServiceQuery)
		}
		if ins.Host != "mirror-adv.test" || ins.Port != 8335 {
			t.Errorf("host:port = %s:%d, want mirror-adv.test:8335", ins.Host, ins.Port)
		}
		if got := ins.TXT[codec.TXTKeyPriority]; got != "100" {
			t.Errorf("pri TXT = %q, want 100", got)
		}
		if got := ins.TXT[codec.TXTKeyAPIAuth]; got != "false" {
			t.Errorf("api_auth TXT = %q, want false (disarmed)", got)
		}
		if got := ins.TXT[codec.TXTKeyAPIVer]; !strings.Contains(got, "v1.3") {
			t.Errorf("api_ver TXT = %q, want it to name v1.3", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no mDNS announce for the served Query face")
	}
}

// TestServeAnnounceInstanceTXT pins the pure instance builder both
// auth ways — the mirror twin of TestPickRegistryServices.
func TestServeAnnounceInstanceTXT(t *testing.T) {
	armed := serveAnnounceInstance("h.test", 8335, []string{"v1.2", "v1.3"}, 100, true)
	if armed.TXT[codec.TXTKeyAPIAuth] != "true" {
		t.Errorf("armed api_auth = %q, want true", armed.TXT[codec.TXTKeyAPIAuth])
	}
	if armed.TXT[codec.TXTKeyAPIVer] != "v1.2,v1.3" {
		t.Errorf("api_ver = %q, want v1.2,v1.3", armed.TXT[codec.TXTKeyAPIVer])
	}
	if armed.Service != codec.ServiceQuery {
		t.Errorf("service = %q, want %q", armed.Service, codec.ServiceQuery)
	}
	disarmed := serveAnnounceInstance("h.test", 8335, []string{"v1.3"}, -3, false)
	if disarmed.TXT[codec.TXTKeyAPIAuth] != "false" {
		t.Errorf("disarmed api_auth = %q, want false", disarmed.TXT[codec.TXTKeyAPIAuth])
	}
	if disarmed.TXT[codec.TXTKeyPriority] != "0" {
		t.Errorf("negative pri = %q, want clamped to 0", disarmed.TXT[codec.TXTKeyPriority])
	}
}

// TestMirrorServeVersionFidelity: a row that arrived on a v1.0 source
// subscription is stamped v1.0 in the embedded store — hidden from an
// un-downgraded v1.3 read of the served face, visible with
// query.downgrade=v1.0, exactly as on the source (AMWA IS-04-02
// test_22 / test_32).
func TestMirrorServeVersionFidelity(t *testing.T) {
	m, _, _ := startServedMirror(t)
	base := "http://" + m.ServeAddr()

	const nodeID = "0f0e0d0c-58cc-4372-a567-0e02b2c3d422"
	nodeDoc, _ := json.Marshal(validNode(nodeID))
	// The row arrives on the v1.0 subscription (watchTopic threads the
	// socket's minor through) — forwardRow is the exact live path.
	m.forwardRow(context.Background(), "nodes", "v1.0",
		is04.GrainDataRow{Path: nodeID, Post: nodeDoc})

	if got := m.serve.store.APIVerOf(is04.ResourceNode, nodeID); got != "v1.0" {
		t.Fatalf("embedded store stamp = %q, want v1.0", got)
	}
	// Un-downgraded v1.3 read: hidden (no-downgrade-by-default).
	status, body := getBody(t, base+"/x-nmos/query/v1.3/nodes")
	if status != 200 || strings.Contains(string(body), nodeID) {
		t.Errorf("v1.3 read = %d %s — a v1.0 resource must stay hidden without downgrade", status, body)
	}
	// Downgraded read: visible.
	status, body = getBody(t, base+"/x-nmos/query/v1.3/nodes?query.downgrade=v1.0")
	if status != 200 || !strings.Contains(string(body), nodeID) {
		t.Errorf("downgraded read = %d %s — want the v1.0 resource", status, body)
	}
	// The ordered replay must not flatten the stamp.
	m.serveReplay()
	if got := m.serve.store.APIVerOf(is04.ResourceNode, nodeID); got != "v1.0" {
		t.Errorf("post-replay stamp = %q, want v1.0", got)
	}
}

// TestMirrorSourceVersions: every registered minor is subscribed, and
// the target-leg primary always rides along.
func TestMirrorSourceVersions(t *testing.T) {
	// The registry test binary registers v1.3 only (mirror_test.go).
	got := mirrorSourceVersions("v1.3")
	if len(got) == 0 || got[len(got)-1] != "v1.3" && got[0] != "v1.3" {
		t.Fatalf("mirrorSourceVersions(v1.3) = %v, want v1.3 present", got)
	}
	withPin := mirrorSourceVersions("v9.9")
	found := false
	for _, v := range withPin {
		if v == "v9.9" {
			found = true
		}
	}
	if !found {
		t.Errorf("mirrorSourceVersions(v9.9) = %v, want the pinned minor appended", withPin)
	}
}
