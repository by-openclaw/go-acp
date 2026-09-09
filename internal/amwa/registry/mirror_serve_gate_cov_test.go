package registry

import (
	"context"
	"encoding/json"
	"io"
	"net"
	stdhttp "net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startServeOn brings the mirror's served Query face up and returns
// its bound address.
func startServeOn(t *testing.T, opts MirrorOptions) (*Mirror, string) {
	t.Helper()
	if opts.Source == "" {
		opts.Source = "http://source.invalid:1"
	}
	if opts.Target == "" {
		opts.Target = "http://target.invalid:2"
	}
	if opts.APIVer == "" {
		opts.APIVer = "v1.3"
	}
	if opts.ServeAddr == "" {
		opts.ServeAddr = "127.0.0.1:0"
	}
	m, err := NewMirror(opts)
	if err != nil {
		t.Fatal(err)
	}
	m.logger = newRegistryLogTap().logger()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m.mu.Lock()
	m.runCtx = ctx
	m.mu.Unlock()
	if err := m.startServe(ctx); err != nil {
		t.Fatalf("startServe: %v", err)
	}
	return m, m.ServeAddr()
}

// A served face that cannot bind its address fails the run rather
// than leaving the operator to discover a mirror that mirrors into
// nothing.
func TestStartServeRefusesAnOccupiedAddress(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	m := mirrorTo(t, "http://target:8235")
	m.logger = newRegistryLogTap().logger()
	m.opts.ServeAddr = ln.Addr().String()
	if err := m.startServe(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "serve listen") {
		t.Errorf("= %v, want the bind failure reported", err)
	}
}

// TLS material the served face cannot use is a startup failure, not a
// silent fallback to plaintext under an https advertisement.
func TestStartServeRefusesUnusableTLSMaterial(t *testing.T) {
	certPath, keyPath := selfSignedPair(t)
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A certificate file that is not there.
	m := mirrorTo(t, "http://target:8235")
	m.logger = newRegistryLogTap().logger()
	m.opts.ServeAddr = "127.0.0.1:0"
	m.opts.ServeTLSCert = filepath.Join(t.TempDir(), "absent.pem")
	m.opts.ServeTLSKey = keyPath
	if err := m.startServe(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "serve TLS") {
		t.Errorf("an unreadable certificate = %v", err)
	}

	_ = certPath // the working-directory failure has its own test below
}

// The served face is read-only and, once the BCP-003-02 gate is
// armed, answers 401 before it will even say so — the refusal itself
// is behind the gate.
func TestServedFaceGatesEveryBranch(t *testing.T) {
	_, addr := startServeOn(t, MirrorOptions{
		ServeAuthURL: "http://127.0.0.1:1", // nothing listening: no keys ever arrive
	})

	client := &stdhttp.Client{Timeout: 5 * time.Second}
	base := "http://" + addr

	// A registration attempt: gated, not answered with the read-only
	// refusal.
	resp, err := client.Post(base+"/x-nmos/registration/v1.3/resource", "application/json", nil)
	if err != nil {
		t.Fatalf("POST registration: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != stdhttp.StatusUnauthorized {
		t.Errorf("a registration attempt through the gate = %d, want 401", resp.StatusCode)
	}

	// A WebSocket upgrade: same gate, before any hijack.
	req, err := stdhttp.NewRequest(stdhttp.MethodGet,
		base+"/x-nmos/query/v1.3/subscriptions/does-not-exist/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Sec-WebSocket-Version", "13")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("upgrade attempt: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != stdhttp.StatusUnauthorized {
		t.Errorf("an unauthenticated upgrade = %d, want 401", resp.StatusCode)
	}
}

// With no gate armed the same two branches answer for themselves: a
// registration is refused as read-only, and a subscriber upgrades.
func TestServedFaceWithoutAGate(t *testing.T) {
	_, addr := startServeOn(t, MirrorOptions{})

	client := &stdhttp.Client{Timeout: 5 * time.Second}
	resp, err := client.Post("http://"+addr+"/x-nmos/registration/v1.3/resource",
		"application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST registration: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode == stdhttp.StatusOK || resp.StatusCode == stdhttp.StatusCreated {
		t.Errorf("the mirror is read-only: %d %s", resp.StatusCode, body)
	}

	peer, _ := openSubscriptionAt(t, addr, "v1.3", SubscriptionRequest{ResourcePath: "/nodes"})
	if op, _, arrived := peer.tryFrame(t, 2*time.Second); arrived && op != 0x1 && op != 0x9 {
		t.Errorf("the upgraded socket sent opcode 0x%x", op)
	}
}

// A CORS preflight carries no credentials by browser design, so the
// gate lets it through — even on the branches that bypass the route
// table.
func TestServedFaceLetsAPreflightThrough(t *testing.T) {
	_, addr := startServeOn(t, MirrorOptions{ServeAuthURL: "http://127.0.0.1:1"})

	req, err := stdhttp.NewRequest(stdhttp.MethodOptions,
		"http://"+addr+"/x-nmos/registration/v1.3/resource", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "http://controller.local")
	req.Header.Set("Access-Control-Request-Method", stdhttp.MethodPost)
	resp, err := (&stdhttp.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode == stdhttp.StatusUnauthorized {
		t.Error("a preflight must not be gated")
	}
}

// TLS material the certmgr cannot even open a working directory for
// fails the served face, rather than coming up plaintext under an
// https advertisement.
func TestStartServeRefusesAnUnusableTLSDataDir(t *testing.T) {
	certPath, keyPath := selfSignedPair(t)
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := mirrorServeTLSDataDir
	mirrorServeTLSDataDir = filepath.Join(blocker, "tls")
	t.Cleanup(func() { mirrorServeTLSDataDir = prev })

	m := mirrorTo(t, "http://target:8235")
	m.logger = newRegistryLogTap().logger()
	m.opts.ServeAddr = "127.0.0.1:0"
	m.opts.ServeTLSCert = certPath
	m.opts.ServeTLSKey = keyPath
	if err := m.startServe(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "serve TLS") {
		t.Errorf("= %v, want the TLS setup failure reported", err)
	}
}

// The served face stops with its context: the listener is shut down,
// and the port stops answering.
func TestServedFaceStopsWithItsContext(t *testing.T) {
	m, err := NewMirror(MirrorOptions{
		Source: "http://source.invalid:1", Target: "http://target.invalid:2",
		APIVer: "v1.3", ServeAddr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatal(err)
	}
	m.logger = newRegistryLogTap().logger()
	ctx, cancel := context.WithCancel(context.Background())
	if err := m.startServe(ctx); err != nil {
		t.Fatal(err)
	}
	addr := m.ServeAddr()

	client := &stdhttp.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + addr + "/x-nmos")
	if err != nil {
		t.Fatalf("the served face must answer while it runs: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	cancel()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := client.Get("http://" + addr + "/x-nmos"); err != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("the served face must stop with its context")
}

// A replay whose cached child has no parent in the embedded store is
// reported, not applied: the store's referential rules hold on the
// mirror's own copy too.
func TestServeReplayReportsARefusedIngest(t *testing.T) {
	m := mirrorTo(t, "http://target:8235")
	tap := newRegistryLogTap()
	m.logger = tap.logger()
	m.serve = &mirrorServe{store: NewStore()}
	m.mu.Lock()
	m.cache["devices"] = map[string]json.RawMessage{
		fxDevice: mustJSONBytes(t, validDevice(fxDevice, fxNode)), // its node is not cached
	}
	m.mu.Unlock()

	m.serveReplay()
	if !tap.has("replay ingest failed") {
		t.Errorf("the refused ingest must be reported; saw %v", tap.snapshot())
	}
}
