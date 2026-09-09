package registry

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	codec "dhs/internal/amwa/codec/dnssd"
	session "dhs/internal/amwa/session/dnssd"
	"dhs/internal/plugin"
	registryslot "dhs/internal/registry"
)

// scriptedRegistryResponder records what the Registry announced and
// can be made to refuse the announce.
type scriptedRegistryResponder struct {
	mu          sync.Mutex
	announced   []codec.Instance
	closed      int
	announceErr error
}

func (r *scriptedRegistryResponder) Announce(_ context.Context, ins codec.Instance) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.announceErr != nil {
		return r.announceErr
	}
	r.announced = append(r.announced, ins)
	return nil
}

func (r *scriptedRegistryResponder) Update(context.Context, codec.Instance) error { return nil }

func (r *scriptedRegistryResponder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed++
	return nil
}

func (r *scriptedRegistryResponder) snapshot() ([]codec.Instance, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]codec.Instance(nil), r.announced...), r.closed
}

// useRegistryResponder routes the package's mDNS seam at the given
// fake. A nil fake makes opening the responder fail.
func useRegistryResponder(t *testing.T, fake *scriptedRegistryResponder) {
	t.Helper()
	prev := newServeResponder
	newServeResponder = func(*slog.Logger) (session.Responder, error) {
		if fake == nil {
			return nil, errors.New("mdns: responder socket unavailable (scripted)")
		}
		return fake, nil
	}
	t.Cleanup(func() { newServeResponder = prev })
}

// The plugin slot hands the Registry its dependencies; a Registry
// built without a metrics set mints its own rather than counting into
// a nil pointer.
func TestFactoryNewSuppliesDependencies(t *testing.T) {
	r, ok := Factory{}.New(plugin.Deps{}).(*Registry)
	if !ok || r == nil {
		t.Fatal("Factory.New must return a *Registry")
	}
	if r.logger == nil {
		t.Error("Deps.WithDefaults must supply a logger")
	}
	met := r.Metrics()
	if met == nil {
		t.Fatal("Metrics must never be nil")
	}
	if r.Metrics() != met {
		t.Error("Metrics must return the same counter set on every call")
	}

	// A Registry built by hand (as the unit tests do) mints one on
	// first use.
	bare := &Registry{}
	if bare.Metrics() == nil {
		t.Error("a hand-built Registry must still report metrics")
	}
}

// Stop closes the responder once, however often it is called — the
// Serve path calls it on the way out and an operator may call it
// again.
func TestRegistryStopIsIdempotent(t *testing.T) {
	fake := &scriptedRegistryResponder{}
	r := &Registry{responder: fake, cancel: func() {}}
	if err := r.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := r.Stop(); err != nil {
		t.Errorf("second Stop: %v", err)
	}
	if _, closed := fake.snapshot(); closed != 1 {
		t.Errorf("responder closed %d times, want once", closed)
	}
}

// In mDNS mode the Registry advertises both faces, adds the legacy
// registration service name for the minors that pre-date the v1.3
// rename, and publishes the TXT keys peers select on.
func TestServeAnnouncesBothFaces(t *testing.T) {
	fake := &scriptedRegistryResponder{}
	useRegistryResponder(t, fake)

	r := &Registry{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- r.Serve(ctx, registryslot.ServeOptions{
			BindAddrs:        []string{"127.0.0.1:0"},
			AdvertiseHost:    "127.0.0.1:8235",
			DiscoveryMode:    "mdns",
			APIVer:           "v1.2", // a minor that pre-dates the rename
			InstanceName:     "dhs-test-registry",
			Priority:         -1, // clamped to 0
			PageLimitDefault: 7,
		})
	}()

	deadline := time.Now().Add(10 * time.Second)
	var announced []codec.Instance
	for {
		announced, _ = fake.snapshot()
		if len(announced) >= 3 || !time.Now().Before(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(announced) != 3 {
		t.Fatalf("announced %d instances, want registration + query + legacy", len(announced))
	}

	byService := map[string]codec.Instance{}
	for _, ins := range announced {
		byService[ins.Service] = ins
	}
	for _, svc := range []string{codec.ServiceRegister, codec.ServiceQuery, codec.ServiceRegisterLegacy} {
		if _, ok := byService[svc]; !ok {
			t.Errorf("nothing announced for %s", svc)
		}
	}
	reg := byService[codec.ServiceRegister]
	if reg.Name != "dhs-test-registry" {
		t.Errorf("instance name = %q, want the operator's own", reg.Name)
	}
	if got := byService[codec.ServiceRegisterLegacy].Name; got != "dhs-test-registry-legacy" {
		t.Errorf("legacy instance name = %q, want the -legacy suffix", got)
	}
	if reg.TXT[codec.TXTKeyAPIVer] != "v1.2" {
		t.Errorf("api_ver TXT = %q", reg.TXT[codec.TXTKeyAPIVer])
	}
	if reg.TXT[codec.TXTKeyAPIProto] != "http" || reg.TXT[codec.TXTKeyAPIAuth] != "false" {
		t.Errorf("TXT = %v, want a plain unauthenticated face", reg.TXT)
	}
	if reg.TXT[codec.TXTKeyPriority] != "0" {
		t.Errorf("pri TXT = %q, want a negative priority clamped to 0", reg.TXT[codec.TXTKeyPriority])
	}
	if r.Stats().Registrations != 3 {
		t.Errorf("Stats.Registrations = %d, want one per announce", r.Stats().Registrations)
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("Serve returned %v", err)
	}
	if _, closed := fake.snapshot(); closed == 0 {
		t.Error("Serve must close the responder on the way out")
	}
}

// mDNS being unavailable, and a responder that refuses the announce,
// are both fatal to Serve — a Registry nobody can discover is not
// serving.
func TestServeReportsMDNSFailures(t *testing.T) {
	useRegistryResponder(t, nil)
	r := &Registry{}
	err := r.Serve(context.Background(), registryslot.ServeOptions{
		BindAddrs:     []string{"127.0.0.1:0"},
		AdvertiseHost: "127.0.0.1:8235",
	})
	if err == nil || !strings.Contains(err.Error(), "open mDNS responder") {
		t.Errorf("responder that cannot open = %v", err)
	}

	refusing := &scriptedRegistryResponder{announceErr: errors.New("group busy")}
	useRegistryResponder(t, refusing)
	r2 := &Registry{}
	err = r2.Serve(context.Background(), registryslot.ServeOptions{
		BindAddrs:     []string{"127.0.0.1:0"},
		AdvertiseHost: "127.0.0.1:8235",
		DiscoveryMode: "mdns",
	})
	if err == nil || !strings.Contains(err.Error(), "announce") {
		t.Errorf("refused announce = %v", err)
	}
	if _, closed := refusing.snapshot(); closed != 1 {
		t.Errorf("a refused announce must close the responder (%d)", closed)
	}
}

// Serve refuses a configuration it cannot honour rather than coming
// up half-configured.
func TestServeRejectsBadConfiguration(t *testing.T) {
	useRegistryResponder(t, &scriptedRegistryResponder{})
	for name, tc := range map[string]struct {
		opts registryslot.ServeOptions
		want string
	}{
		"no bind address": {
			registryslot.ServeOptions{AdvertiseHost: "127.0.0.1:8235", DiscoveryMode: "static"},
			"BindAddrs required",
		},
		"advertise host without a port": {
			registryslot.ServeOptions{BindAddrs: []string{"127.0.0.1:0"}, AdvertiseHost: "no-port"},
			"split",
		},
		"TLS cert and key lists disagree": {
			registryslot.ServeOptions{
				BindAddrs: []string{"127.0.0.1:0"}, AdvertiseHost: "127.0.0.1:8235",
				DiscoveryMode: "static",
				TLSCertFile:   "a.pem,b.pem", TLSKeyFile: "a.key",
				TLSDataDir: filepath.Join(os.TempDir(), "dhs-registry-tls-test"),
			},
			"differ in length",
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := (&Registry{}).Serve(context.Background(), tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Serve = %v, want an error mentioning %q", err, tc.want)
			}
		})
	}
}

// An HTTP face that cannot bind reports the listener's error rather
// than blocking on a server that never came up.
func TestServeReportsAnUnavailablePort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	r := &Registry{}
	serveErr := r.Serve(context.Background(), registryslot.ServeOptions{
		BindAddrs:     []string{ln.Addr().String()},
		AdvertiseHost: "127.0.0.1:8235",
		DiscoveryMode: "static",
	})
	if serveErr == nil {
		t.Fatal("Serve on an occupied port must report the bind failure")
	}
}

// The advertised host falls back to the OS hostname, and to
// "localhost" when the OS will not name the machine.
func TestOSHostnameFallback(t *testing.T) {
	prev := osHostnameFn
	t.Cleanup(func() { osHostnameFn = prev })

	osHostnameFn = func() (string, error) { return "named-host", nil }
	if got := osHostname(); got != "named-host" {
		t.Errorf("osHostname = %q", got)
	}
	osHostnameFn = func() (string, error) { return "", errors.New("no hostname") }
	if got := osHostname(); got != "localhost" {
		t.Errorf("osHostname without a name = %q, want localhost", got)
	}
	osHostnameFn = func() (string, error) { return "", nil }
	if got := osHostname(); got != "localhost" {
		t.Errorf("osHostname with an empty name = %q, want localhost", got)
	}

	// The production seam reads the real machine name.
	if name, err := netHostname(); err == nil && name == "" {
		t.Error("netHostname returned neither a name nor an error")
	}
}

// The SRV announce carries a literal address when it has one, and
// otherwise offers the machine's own routable addresses — never
// loopback or link-local, which no peer can reach.
func TestLocalIPv4Candidates(t *testing.T) {
	got := localIPv4Candidates("10.6.239.113")
	if len(got) != 1 || got[0].String() != "10.6.239.113" {
		t.Errorf("a literal host advertises itself: %v", got)
	}
	if got := localIPv4Candidates("10.6.239.113."); len(got) != 1 {
		t.Errorf("a fully-qualified literal is still a literal: %v", got)
	}
	for _, ip := range localIPv4Candidates("some-host.local") {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			t.Errorf("advertised an unreachable address: %v", ip)
		}
	}
}

// Which minors the Registry serves: the operator's override alone
// when they named one, every registered codec otherwise.
func TestPickAPIVersions(t *testing.T) {
	if got := pickAPIVersions("v1.1"); len(got) != 1 || got[0] != "v1.1" {
		t.Errorf("override = %v, want the named minor alone", got)
	}
	got := pickAPIVersions("")
	if len(got) == 0 {
		t.Fatal("with no override the Registry serves every registered minor")
	}
	for _, v := range got {
		if !strings.HasPrefix(v, "v") {
			t.Errorf("served minor %q is not a wire version", v)
		}
	}
}

// A Registry with no codecs registered still serves a face: it falls
// back to the newest minor rather than installing no routes at all.
func TestPickAPIVersionsFallsBackWithoutCodecs(t *testing.T) {
	prev := supportedVersions
	supportedVersions = func() []string { return nil }
	t.Cleanup(func() { supportedVersions = prev })

	if got := pickAPIVersions(""); len(got) != 1 || got[0] != "v1.3" {
		t.Errorf("pickAPIVersions with no codecs = %v, want the newest minor", got)
	}
}

// A host that cannot enumerate its own interfaces advertises no A
// records rather than failing the announce.
func TestLocalIPv4CandidatesWithoutInterfaces(t *testing.T) {
	prev := interfaceAddrs
	interfaceAddrs = func() ([]net.Addr, error) { return nil, errors.New("no interfaces") }
	t.Cleanup(func() { interfaceAddrs = prev })

	if got := localIPv4Candidates("some-host.local"); got != nil {
		t.Errorf("localIPv4Candidates = %v, want nil when the host cannot be enumerated", got)
	}
}

// selfSignedPair writes a usable cert/key pair into a temp dir and
// returns the two paths.
func selfSignedPair(t *testing.T) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "dhs-nmos-registry"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              []string{"dhs-nmos-registry", "localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

// BCP-003-01: a Registry given a certificate serves both faces over
// TLS and says so on the wire — api_proto flips to https, which is
// how a peer knows to speak TLS to the port it just discovered.
func TestServeWithTLSAdvertisesHTTPS(t *testing.T) {
	certPath, keyPath := selfSignedPair(t)
	fake := &scriptedRegistryResponder{}
	useRegistryResponder(t, fake)

	r := &Registry{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- r.Serve(ctx, registryslot.ServeOptions{
			BindAddrs:     []string{"127.0.0.1:0"},
			AdvertiseHost: "127.0.0.1:8235",
			DiscoveryMode: "mdns",
			APIVer:        "v1.3",
			TLSCertFile:   certPath,
			TLSKeyFile:    keyPath,
			TLSDataDir:    t.TempDir(),
		})
	}()

	deadline := time.Now().Add(10 * time.Second)
	var announced []codec.Instance
	for {
		announced, _ = fake.snapshot()
		if len(announced) > 0 || !time.Now().Before(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(announced) == 0 {
		t.Fatal("nothing was announced")
	}
	if got := announced[0].TXT[codec.TXTKeyAPIProto]; got != "https" {
		t.Errorf("api_proto TXT = %q, want https", got)
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("Serve returned %v", err)
	}
}

// BCP-003-02: a Registry given an authorization server advertises
// api_auth=true, and keeps serving while the JWKS is still out of
// reach — requests 401 until the keys arrive rather than the face
// refusing to come up.
func TestServeWithAuthAdvertisesAPIAuth(t *testing.T) {
	fake := &scriptedRegistryResponder{}
	useRegistryResponder(t, fake)

	r := &Registry{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- r.Serve(ctx, registryslot.ServeOptions{
			BindAddrs:     []string{"127.0.0.1:0"},
			AdvertiseHost: "127.0.0.1:8235",
			DiscoveryMode: "mdns",
			APIVer:        "v1.3",
			AuthURL:       "http://127.0.0.1:1", // nothing is listening
		})
	}()

	deadline := time.Now().Add(20 * time.Second)
	var announced []codec.Instance
	for {
		announced, _ = fake.snapshot()
		if len(announced) > 0 || !time.Now().Before(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(announced) == 0 {
		t.Fatal("nothing was announced")
	}
	if got := announced[0].TXT[codec.TXTKeyAPIAuth]; got != "true" {
		t.Errorf("api_auth TXT = %q, want true", got)
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("Serve returned %v", err)
	}
}

// An EST server the Registry cannot reach is a startup failure, not a
// silent fallback to plaintext.
func TestServeReportsESTBootstrapFailure(t *testing.T) {
	useRegistryResponder(t, &scriptedRegistryResponder{})
	err := (&Registry{}).Serve(context.Background(), registryslot.ServeOptions{
		BindAddrs:     []string{"127.0.0.1:0"},
		AdvertiseHost: "127.0.0.1:8235",
		DiscoveryMode: "static",
		ESTHost:       "127.0.0.1:1",
		TLSDataDir:    t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "EST") {
		t.Errorf("Serve with an unreachable EST server = %v", err)
	}
}
