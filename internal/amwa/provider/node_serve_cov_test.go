package provider

// Serve is where a Node's configuration becomes a running device: TLS
// armed or not, gated or not, announced or not, registered one of four
// ways. Each of those is a decision an operator made on the command
// line, and each has a failure the operator has to be told about
// rather than discover from a controller that never sees the Node.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	dnssdcodec "dhs/internal/amwa/codec/dnssd"
)

// servedNode starts a Node on a free port with the given config
// applied, and returns the address plus the error Serve ended with (or
// nil while it is still running). The server is always stopped.
type servedNode struct {
	s    *IS04NodeServer
	addr string
	errc chan error
}

// startNode builds and serves a Node, waiting for it to answer before
// returning. tweak receives the config with Bind already set.
func startNode(t *testing.T, tweak func(*IS04NodeConfig)) *servedNode {
	t.Helper()
	return startNodeWith(t, validBundle(), tweak)
}

// startNodeWith is startNode over a caller-supplied bundle.
func startNodeWith(t *testing.T, bundle *NodeConfig, tweak func(*IS04NodeConfig)) *servedNode {
	t.Helper()
	cfg := IS04NodeConfig{Bind: freeAddr(t), DiscoveryMode: "static"}
	if tweak != nil {
		tweak(&cfg)
	}
	s, err := NewIS04NodeServer(newLogTap().logger(), bundle, cfg)
	if err != nil {
		t.Fatalf("NewIS04NodeServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); errc <- s.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = s.Stop()
		wg.Wait()
	})
	if !waitReachable(t, "http://"+cfg.Bind+"/__ready__", 5*time.Second) {
		select {
		case err := <-errc:
			t.Fatalf("the Node never came up: %v", err)
		default:
			t.Fatal("the Node never came up")
		}
	}
	return &servedNode{s: s, addr: cfg.Bind, errc: errc}
}

// serveRefusal runs Serve to completion and returns the error it
// ended with. Used for the configurations that must not come up at
// all.
func serveRefusal(t *testing.T, tweak func(*IS04NodeConfig)) error {
	t.Helper()
	cfg := IS04NodeConfig{Bind: freeAddr(t), DiscoveryMode: "static"}
	if tweak != nil {
		tweak(&cfg)
	}
	s, err := NewIS04NodeServer(newLogTap().logger(), validBundle(), cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	t.Cleanup(func() { _ = s.Stop() })
	return s.Serve(ctx)
}

// ---------------------------------------------------------------
// construction
// ---------------------------------------------------------------

// A server is refused before it can serve anything a controller would
// have to interpret: no bundle, no address to bind, or an IS-04 minor
// this build carries no codec for.
func TestNewNodeServerRefusals(t *testing.T) {
	if _, err := NewIS04NodeServer(nil, nil, IS04NodeConfig{Bind: "127.0.0.1:0"}); err == nil ||
		!strings.Contains(err.Error(), "nil bundle") {
		t.Errorf("a nil bundle = %v", err)
	}
	if _, err := NewIS04NodeServer(nil, validBundle(), IS04NodeConfig{}); err == nil ||
		!strings.Contains(err.Error(), "Bind required") {
		t.Errorf("no bind address = %v", err)
	}
	if _, err := NewIS04NodeServer(nil, validBundle(), IS04NodeConfig{
		Bind: "127.0.0.1:0", APIVer: "v9.9",
	}); err == nil || !strings.Contains(err.Error(), "no IS-04 codec") {
		t.Errorf("an unknown minor = %v", err)
	}
}

// A second Serve on a running server is a programming error, not a
// second listener: the first one owns the port, the routes and the
// announcement.
func TestNodeRefusesToServeTwice(t *testing.T) {
	n := startNode(t, nil)
	if err := n.s.Serve(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "already serving") {
		t.Fatalf("= %v, want the second Serve refused", err)
	}
}

// ---------------------------------------------------------------
// BCP-003-01 TLS
// ---------------------------------------------------------------

// The pair flags are lists so a Node can serve both an RSA and an
// ECDSA certificate, per the BCP-003-01 SHOULD. Lists of different
// lengths name no pair at all, and coming up with half of them armed
// would leave a controller negotiating a certificate the operator
// never configured.
func TestNodeRefusesMismatchedTLSPairs(t *testing.T) {
	dir := t.TempDir()
	cert, key, _ := writeTLSPair(t, dir)

	err := serveRefusal(t, func(c *IS04NodeConfig) {
		c.TLSCertFile = cert + "," + cert
		c.TLSKeyFile = key
		c.TLSDataDir = filepath.Join(dir, "tls")
	})
	if err == nil || !strings.Contains(err.Error(), "same number of files") {
		t.Fatalf("= %v, want the mismatch reported", err)
	}
}

// TLS material the Node cannot load is a refusal to serve, never a
// quiet fall back to plain HTTP: BCP-003-01 says a secured API SHALL
// NOT answer unsecured, so the fallback a controller would meet is a
// Node it cannot talk to at all.
func TestNodeRefusesTLSMaterialItCannotLoad(t *testing.T) {
	dir := t.TempDir()
	cert, key, _ := writeTLSPair(t, dir)
	absent := filepath.Join(dir, "not-here.pem")

	t.Run("a certificate that is not there", func(t *testing.T) {
		err := serveRefusal(t, func(c *IS04NodeConfig) {
			c.TLSCertFile, c.TLSKeyFile = absent, key
			c.TLSDataDir = filepath.Join(t.TempDir(), "tls")
		})
		if err == nil {
			t.Fatal("want the load failure reported")
		}
	})

	t.Run("a trust anchor that is not there", func(t *testing.T) {
		err := serveRefusal(t, func(c *IS04NodeConfig) {
			c.TLSCertFile, c.TLSKeyFile = cert, key
			c.TLSCAFile = absent
			c.TLSDataDir = filepath.Join(t.TempDir(), "tls")
		})
		if err == nil {
			t.Fatal("want the trust-anchor failure reported")
		}
	})

	t.Run("a data directory that cannot be made", func(t *testing.T) {
		blocker := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		err := serveRefusal(t, func(c *IS04NodeConfig) {
			c.TLSCertFile, c.TLSKeyFile = cert, key
			c.TLSDataDir = filepath.Join(blocker, "tls")
		})
		if err == nil {
			t.Fatal("want the data-directory failure reported")
		}
	})
}

// BCP-003-03 certificate provisioning: an EST server that is not there
// fails the Node at bootstrap. Serving anyway would mean serving under
// a certificate nobody issued.
func TestNodeReportsAnESTServerItCannotReach(t *testing.T) {
	err := serveRefusal(t, func(c *IS04NodeConfig) {
		c.ESTHost = "127.0.0.1:1" // nothing listening
		c.TLSDataDir = filepath.Join(t.TempDir(), "tls")
	})
	if err == nil || !strings.Contains(err.Error(), "EST bootstrap") {
		t.Fatalf("= %v, want the EST bootstrap failure reported", err)
	}
}

// ---------------------------------------------------------------
// BCP-003-02 authorization
// ---------------------------------------------------------------

// An Authorization Server that is not answering yet does not stop the
// Node: the key cache keeps trying, and every gated request 401s in
// the meantime. Refusing to start would take a Node off the network
// for the duration of an auth server restart.
func TestNodeStartsWithoutItsJWKSAndSaysSo(t *testing.T) {
	tap := newLogTap()
	cfg := IS04NodeConfig{
		Bind: freeAddr(t), DiscoveryMode: "static",
		AuthURL: "http://127.0.0.1:1", AuthClientID: "node", AuthClientSecret: "s",
	}
	s, err := NewIS04NodeServer(tap.logger(), validBundle(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = s.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = s.Stop()
		wg.Wait()
	})
	if !waitReachable(t, "http://"+cfg.Bind+"/__ready__", 5*time.Second) {
		t.Fatal("the Node never came up")
	}
	tap.until(t, "initial JWKS fetch failed")
}

// ---------------------------------------------------------------
// DNS-SD announcement
// ---------------------------------------------------------------

// A Node that cannot announce itself is a Node no Registry in Mode A
// will ever hear of. Coming up silently would look identical to a
// working Node from the inside and identical to a dead one from the
// network.
func TestNodeReportsAnAnnouncementItCannotMake(t *testing.T) {
	useResponder(t, nil)
	useFakeBrowser(t, newFakeBrowser())

	err := serveRefusal(t, func(c *IS04NodeConfig) {
		c.DiscoveryMode = "mdns"
		c.NoRegistry = true
	})
	if err == nil || !strings.Contains(err.Error(), "responder") {
		t.Fatalf("= %v, want the announcement failure reported", err)
	}
}

// The announced api_ver TXT carries every minor the bundle declares,
// not just the wire one: a Node advertising only v1.3 reads as
// v1.3-only to a controller, which AMWA's test_12_01 flags even when
// the Node is registered and working.
func TestNodeAnnouncesEveryMinorItSupports(t *testing.T) {
	rs := &scriptedResponder{}
	useResponder(t, rs)
	useFakeBrowser(t, newFakeBrowser())

	startNode(t, func(c *IS04NodeConfig) {
		c.DiscoveryMode = "mdns"
		c.NoRegistry = true
	})

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if announced, _, _ := rs.counts(); announced > 0 {
			ins := rs.lastAnnounce(t)
			if ins.Service != dnssdcodec.ServiceNode {
				t.Fatalf("announced %q, want the Node service", ins.Service)
			}
			want := strings.Join(validBundle().Node.API.Versions, ",")
			if got := ins.TXT[dnssdcodec.TXTKeyAPIVer]; got != want {
				t.Fatalf("api_ver = %q, want the bundle's full version list %q", got, want)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the Node never announced itself")
}

// ---------------------------------------------------------------
// registration modes
// ---------------------------------------------------------------

// --no-registry is peer-to-peer operation, and it is said out loud:
// an operator who reaches for it wants to know it took.
func TestNodeStaysPeerToPeerWhenToldTo(t *testing.T) {
	tap := newLogTap()
	cfg := IS04NodeConfig{Bind: freeAddr(t), DiscoveryMode: "static", NoRegistry: true}
	s, err := NewIS04NodeServer(tap.logger(), validBundle(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = s.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = s.Stop()
		wg.Wait()
	})
	if !waitReachable(t, "http://"+cfg.Bind+"/__ready__", 5*time.Second) {
		t.Fatal("the Node never came up")
	}
	tap.until(t, "registry disabled")
}

// Mode B: an explicit Registry URL is used as given, with no
// discovery of any kind. Whether that Registry answers is its own
// business — the Node keeps serving either way.
func TestNodeRegistersAgainstAnExplicitRegistry(t *testing.T) {
	n := startNode(t, func(c *IS04NodeConfig) {
		c.RegistryURL = "http://127.0.0.1:1"
	})
	if n.s.regClient == nil {
		t.Fatal("an explicit registry URL must arm the registration client")
	}
}

// Mode B with discovery: registries come from a conventional DNS zone.
// A resolver that answers nothing is a Node that keeps serving and
// keeps asking — the same posture as an mDNS network with no Registry
// on it yet.
func TestNodeDiscoversRegistriesOverUnicastDNS(t *testing.T) {
	n := startNode(t, func(c *IS04NodeConfig) {
		c.DiscoveryMode = "unicast"
		c.UnicastResolver = "127.0.0.1:1"
		c.UnicastDomain = "example.arpa"
	})
	if n.s.regClient == nil {
		t.Fatal("unicast discovery must arm the registration client")
	}
}

// Mode A: browse for Registries on the link. The watcher owns the
// browser, so a link with no multicast is a refusal to serve rather
// than a Node that silently never registers.
func TestNodeBrowsesForRegistries(t *testing.T) {
	fb := newFakeBrowser()
	useFakeBrowser(t, fb)
	useResponder(t, &scriptedResponder{})

	n := startNode(t, func(c *IS04NodeConfig) { c.DiscoveryMode = "mdns" })
	if n.s.watcher == nil {
		t.Fatal("mDNS mode must arm the registry watcher")
	}
}

func TestNodeReportsAWatcherItCannotOpen(t *testing.T) {
	useResponder(t, &scriptedResponder{})
	useFakeBrowser(t, nil) // the constructor fails

	err := serveRefusal(t, func(c *IS04NodeConfig) { c.DiscoveryMode = "mdns" })
	if err == nil || !strings.Contains(err.Error(), "open registry watcher") {
		t.Fatalf("= %v, want the watcher open failure reported", err)
	}
}

// A browser that opens but will not browse fails the same way, and
// the watcher is closed on the way out rather than left holding a
// socket.
func TestNodeReportsAWatcherItCannotStart(t *testing.T) {
	fb := newFakeBrowser()
	fb.browseErr[dnssdcodec.ServiceRegister] = errors.New("no multicast group")
	useFakeBrowser(t, fb)
	useResponder(t, &scriptedResponder{})

	err := serveRefusal(t, func(c *IS04NodeConfig) { c.DiscoveryMode = "mdns" })
	if err == nil || !strings.Contains(err.Error(), "start registry watcher") {
		t.Fatalf("= %v, want the watcher start failure reported", err)
	}
	if fb.closed == 0 {
		t.Error("a watcher that will not start must not keep its browser")
	}
}

// ---------------------------------------------------------------
// encoding helpers
// ---------------------------------------------------------------

// A resource the wire minor cannot express is dropped rather than
// served as null: a list with a null element has no element type, and
// a controller decoding it fails on the whole collection instead of
// the one resource.
func TestEncodeDropsWhatTheMinorCannotExpress(t *testing.T) {
	refuse := func(int) ([]byte, error) { return nil, errors.New("not in this minor") }
	ok := func(i int) ([]byte, error) { return []byte(`{"n":` + string(rune('0'+i)) + `}`), nil }

	if got := encodeOne(refuse, 1); got != nil {
		t.Errorf("a resource that will not encode = %s, want nothing served", got)
	}
	if got := string(encodeList(refuse, []int{1, 2})); got != "[]" {
		t.Errorf("a list of unencodable resources = %s, want an empty array", got)
	}
	if got := string(encodeList(ok, []int{1, 2})); got != `[{"n":1},{"n":2}]` {
		t.Errorf("a list = %s", got)
	}
	if got := string(encodeOne(ok, 1)); got != `{"n":1}` {
		t.Errorf("one resource = %s", got)
	}
}
