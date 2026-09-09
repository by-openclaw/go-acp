package provider

// The last arms: guards that keep an absent subsystem from becoming a
// crash, the paths a bundle takes when it declares nothing, and the
// IS-09 discovery the Node does in the background.

import (
	"context"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is05"
	"dhs/internal/amwa/codec/is09"
)

// ---------------------------------------------------------------
// IS-09 on the provider side
// ---------------------------------------------------------------

// The System API server serves what the operator gave it, and refuses
// what is not a Global: a device answering /global with something the
// schema does not describe sends every Node on the network back to
// picking a Registry from mDNS priority alone.
func TestSystemServerRefusals(t *testing.T) {
	if _, err := NewIS09Server(nil, nil, IS09Config{Bind: "127.0.0.1:0"}); err == nil ||
		!strings.Contains(err.Error(), "nil Global") {
		t.Errorf("a nil Global = %v", err)
	}
	if _, err := NewIS09Server(nil, &is09.Global{}, IS09Config{Bind: "127.0.0.1:0"}); err == nil {
		t.Error("a Global that is not one must be refused")
	}
}

// A config file that is not there and one that is not a Global are
// told apart: an operator who mistyped a path should not be reading a
// schema error.
func TestLoadSystemGlobalRefusals(t *testing.T) {
	if _, err := LoadIS09GlobalFromFile(filepath.Join(t.TempDir(), "absent.json")); err == nil ||
		!strings.Contains(err.Error(), "read ") {
		t.Errorf("a file that is not there = %v", err)
	}

	path := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadIS09GlobalFromFile(path); err == nil ||
		strings.Contains(err.Error(), "read ") {
		t.Errorf("a file that is not a Global = %v", err)
	}
}

// A host:port whose port is not a number carries no port. Half an
// address is more use than a made-up one.
func TestSplitHostPortWithAPortThatIsNotANumber(t *testing.T) {
	host, port := splitHostPort("", "node.local:not-a-port")
	if host == "" || port != 0 {
		t.Errorf("= %q, %d, want the host alone", host, port)
	}
}

// The Node reads a System API in the background, and asking twice does
// not open a second watcher: one Node, one IS-09 subscription.
func TestSystemDiscoveryIsStartedOnce(t *testing.T) {
	useFakeBrowser(t, newFakeBrowser())
	s := nodeFor(t, validBundle(), func(c *IS04NodeConfig) { c.DiscoveryMode = "mdns" })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.fetchSystemGlobal(ctx)
	first := s.systemWatcher
	s.fetchSystemGlobal(ctx)
	if s.systemWatcher != first {
		t.Error("a second call opened a second watcher")
	}
}

// A watcher that cannot open its browser is reported rather than
// leaving the Node quietly without the operator's chosen Registry.
func TestSystemDiscoveryReportsAWatcherItCannotOpen(t *testing.T) {
	useFakeBrowser(t, nil) // the constructor fails
	tap := newLogTap()
	s, err := NewIS04NodeServer(tap.logger(), validBundle(), IS04NodeConfig{
		Bind: "127.0.0.1:0", DiscoveryMode: "mdns",
	})
	if err != nil {
		t.Fatal(err)
	}

	s.fetchSystemGlobal(context.Background())

	if !tap.has("cannot watch for a System API") {
		t.Errorf("the failure must be reported; saw %v", tap.snapshot())
	}
}

// ---------------------------------------------------------------
// bundles that declare nothing
// ---------------------------------------------------------------

// A projection of nothing is nothing: a nil bundle has no resources to
// narrow, and a bundle every minor can carry is returned untouched so
// the common case costs one pass and no allocation.
func TestProjectionEdges(t *testing.T) {
	if got := projectForMinor(nil, "v1.0"); got != nil {
		t.Errorf("a nil bundle = %+v, want nothing", got)
	}

	b := routableBundle(t)
	if got := projectForMinor(b, "v1.3"); got != b {
		t.Error("a bundle needing no projection must come back as itself")
	}

	// A transport a minor cannot describe is dropped, and what is left
	// is a new bundle rather than the caller's.
	b.Senders[0].Transport = is04.TransportMXL
	got := projectForMinor(b, "v1.0")
	if got == b {
		t.Error("a projection that drops something must not mutate the caller's bundle")
	}
	if len(got.Senders) != 0 {
		t.Errorf("senders = %+v, want the MXL sender dropped at v1.0", got.Senders)
	}
}

// BCP-007-03: an MXL sender has no transport file, so its
// manifest_href stays null rather than pointing at a route that 404s.
func TestManifestHrefsSkipMXL(t *testing.T) {
	senders := []is04.Sender{
		{ResourceCore: is04.ResourceCore{ID: "a"}, Transport: is04.TransportMXL},
		{ResourceCore: is04.ResourceCore{ID: "b"}, Transport: is04.TransportRTP},
	}
	rewriteManifestHrefs(senders, "node.local:8080", "v1.3", "")

	if senders[0].ManifestHref != nil {
		t.Errorf("an MXL sender = %v, want no manifest", *senders[0].ManifestHref)
	}
	if senders[1].ManifestHref == nil || !strings.HasPrefix(*senders[1].ManifestHref, "http://") {
		t.Errorf("an RTP sender = %v, want the default scheme applied", senders[1].ManifestHref)
	}

	// With nothing advertised there is no authority to point at, and a
	// href naming none is worse than the one the bundle already has.
	before := senders[1].ManifestHref
	rewriteManifestHrefs(senders, "", "v1.3", "http")
	if senders[1].ManifestHref != before {
		t.Error("no advertise host must leave the hrefs alone")
	}
}

// The endpoint list is best-effort: a host that will not enumerate its
// interfaces yields none rather than failing the Node's startup.
func TestLocalIPv4WhenTheHostWillNotSay(t *testing.T) {
	prev := interfaceAddrs
	interfaceAddrs = func() ([]net.Addr, error) { return nil, errTest("no interfaces") }
	t.Cleanup(func() { interfaceAddrs = prev })

	if got := localIPv4(); len(got) != 0 {
		t.Errorf("= %v, want none", got)
	}
}

// ---------------------------------------------------------------
// absent subsystems
// ---------------------------------------------------------------

// The IS-08 activation handler reads its body from the request, and a
// body the transport cut short is a bad request rather than a re-map
// applied from half a message.
func TestChannelMapActivationRefusesABodyItCannotRead(t *testing.T) {
	s := nodeFor(t, audioBundle(), nil)
	if s.channelMapping == nil {
		t.Fatal("the audio bundle mounts IS-08")
	}

	req := httptest.NewRequest(stdhttp.MethodPost, cmBase+"/map/activations/",
		iotest.ErrReader(errTest("connection reset")))
	code, _, err := s.channelMapping.handleActivationPost(req)
	if err != nil || code != stdhttp.StatusBadRequest {
		t.Fatalf("= %d (%v), want 400", code, err)
	}
}

// The IS-14 version listing answers at both spellings, with and
// without the trailing slash: a controller that guesses wrong should
// find the tree rather than a 404.
func TestConfigurationVersionListings(t *testing.T) {
	n := startNode(t, nil)

	for _, path := range []string{"/x-nmos/configuration", "/x-nmos/configuration/"} {
		if code, body := get(t, n.addr, path); code != stdhttp.StatusOK {
			t.Errorf("GET %s = %d %s", path, code, body)
		}
	}
}

// The version listings answer at both spellings — with and without the
// trailing slash — because a controller that guesses wrong should
// still find the tree rather than a 404.
func TestConnectionVersionListings(t *testing.T) {
	b := routableBundle(t)
	s := NewIS05ConnectionServer(newLogTap().logger(), b, IS05ConnectionConfig{APIVer: "v1.2"})
	addr := newTestServer(t, s)

	for _, path := range []string{
		"/x-nmos/connection", "/x-nmos/connection/",
		"/x-nmos/connection/v1.2/", "/x-nmos/connection/v1.2/single/",
		"/x-nmos/connection/v1.2/bulk/",
	} {
		if code, body := get(t, addr, path); code != stdhttp.StatusOK {
			t.Errorf("GET %s = %d %s", path, code, body)
		}
	}
}

// A sender the Connection API holds but whose SDP will not render
// falls back to the transport file cached at activation, so
// manifest_href keeps resolving.
func TestSenderSDPFallsBackToTheCachedFile(t *testing.T) {
	b := routableBundle(t)
	// A transport with no SDP of its own: whatever was cached at
	// activation is the only transport file this sender has.
	b.Senders[0].Transport = "urn:x-nmos:transport:websocket"
	node, err := NewIS04NodeServer(newLogTap().logger(), b, IS04NodeConfig{
		Bind: "127.0.0.1:0", DiscoveryMode: "static", ConnectionAPIVer: "v1.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	sid := b.Senders[0].ID
	e, err := node.connection.Store().get("senders", sid)
	if err != nil {
		t.Fatal(err)
	}
	e.transportFile = "v=0\r\ncached\r\n"

	if got := node.senderSDP(sid); !strings.Contains(got, "cached") {
		t.Errorf("= %q, want the cached transport file", got)
	}
}

var _ = is05.TransportParams{}
