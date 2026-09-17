package certmgr

// The failure half of the BCP-003-03 client: an EST server that
// answers wrongly, a data directory that cannot be written, a manual
// pair that does not load, a renewal loop that must keep going. Each
// test states the rule it pins. Waits go through the `after` seam so
// no test sits through a Retry-After or a ten-minute check.

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/amwa/codec/est"
)

const faultTimeout = 5 * time.Second

// waitUntil polls cond until it holds or the deadline passes; the
// deadline is the bound, the interval only stops the loop spinning.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(faultTimeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// instantAfter replaces the `after` seam: every wait fires at once
// unless fire says otherwise, and each requested duration is recorded
// so a test can assert on the backoff the code chose.
type instantAfter struct {
	mu    sync.Mutex
	calls []time.Duration
	fire  func(time.Duration) bool
}

func (f *instantAfter) after(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	f.calls = append(f.calls, d)
	f.mu.Unlock()
	if f.fire != nil && !f.fire(d) {
		return nil // parks the select until ctx is done
	}
	ch := make(chan time.Time, 1)
	ch <- time.Time{}
	return ch
}

func (f *instantAfter) count(pred func(time.Duration) bool) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, d := range f.calls {
		if pred(d) {
			n++
		}
	}
	return n
}

func installAfter(t *testing.T, f *instantAfter) {
	t.Helper()
	after = f.after
	t.Cleanup(func() { after = time.After })
}

// leafFrom issues a leaf for cn under the given CA, with the given
// validity window, and returns it with its key.
func leafFrom(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, cn string, key crypto.Signer, notBefore, notAfter time.Time) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     []string{cn},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, key.Public(), caKey)
	if err != nil {
		t.Fatalf("issue leaf: %v", err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	return c
}

// writePEM writes a cert+key pair to dir under name and returns both
// paths, the way an operator installs a manual certificate.
func writePEM(t *testing.T, dir, name string, cert *x509.Certificate, key crypto.Signer) (certFile, keyFile string) {
	t.Helper()
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	certFile = filepath.Join(dir, name+".crt")
	keyFile = filepath.Join(dir, name+".key")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

func manualManager(t *testing.T) *Manager {
	t.Helper()
	m, err := New(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

// --- constructor ---

func TestNewValidatesOptions(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		opts Options
		want string
	}{
		{"data dir is required", Options{}, "DataDir required"},
		{"EST mode needs a hostname for the CSR", Options{DataDir: t.TempDir(), ESTBase: "https://est/.well-known/est"}, "at least one hostname"},
		{"data dir must be creatable", Options{DataDir: filepath.Join(file, "under-a-file")}, "create data dir"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("New = %v, want %q", err, tc.want)
			}
		})
	}
}

// --- manual certificates ---

// TestLoadManualPairs: BCP-003-01 says a server SHOULD offer both an
// RSA and an ECDSA certificate. The first pair loaded becomes the
// live certificate; every pair goes into the TLS config so the stack
// selects per ClientHello.
func TestLoadManualPairs(t *testing.T) {
	ca, caKey := newCA(t, "manual CA")
	dir := t.TempDir()
	now := time.Now()

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rsaCrt, rsaKeyFile := writePEM(t, dir, "rsa",
		leafFrom(t, ca, caKey, "node.rsa", rsaKey, now.Add(-time.Minute), now.Add(time.Hour)), rsaKey)
	ecKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ecCrt, ecKeyFile := writePEM(t, dir, "ec",
		leafFrom(t, ca, caKey, "node.ec", ecKey, now.Add(-time.Minute), now.Add(time.Hour)), ecKey)

	m := manualManager(t)
	if _, err := m.GetCertificate(nil); err == nil {
		t.Fatal("GetCertificate before any certificate must fail, not serve nil")
	}
	if err := m.LoadManual(rsaCrt, rsaKeyFile); err != nil {
		t.Fatalf("load RSA pair: %v", err)
	}
	if err := m.LoadManual(ecCrt, ecKeyFile); err != nil {
		t.Fatalf("load ECDSA pair: %v", err)
	}
	if got := m.Certificate().Leaf.Subject.CommonName; got != "node.rsa" {
		t.Errorf("live certificate = %q, want the first pair loaded", got)
	}
	cfg := m.TLSServerConfig()
	if len(cfg.Certificates) != 2 || cfg.GetCertificate != nil {
		t.Errorf("two manual pairs must be handed to the TLS stack as a set: %d certs, GetCertificate=%v",
			len(cfg.Certificates), cfg.GetCertificate != nil)
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %x, want TLS 1.2 floor", cfg.MinVersion)
	}
}

func TestLoadManualFaults(t *testing.T) {
	t.Run("missing files", func(t *testing.T) {
		m := manualManager(t)
		err := m.LoadManual(filepath.Join(t.TempDir(), "none.crt"), filepath.Join(t.TempDir(), "none.key"))
		if err == nil || !strings.Contains(err.Error(), "load manual pair") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("loader hands back an unparseable leaf", func(t *testing.T) {
		loadX509KeyPair = func(string, string) (tls.Certificate, error) {
			return tls.Certificate{Certificate: [][]byte{[]byte("not DER")}}, nil
		}
		t.Cleanup(func() { loadX509KeyPair = tls.LoadX509KeyPair })
		m := manualManager(t)
		err := m.LoadManual("x", "y")
		if err == nil || !strings.Contains(err.Error(), "parse manual leaf") {
			t.Fatalf("err = %v", err)
		}
		if m.Certificate() != nil {
			t.Fatal("a pair that failed to parse must not become the live certificate")
		}
	})
}

// TestLoadManualRoots: the operator's CA file may carry keys and
// comments beside the certificates; only CERTIFICATE blocks count,
// a file with none is an error, and a block that is not a certificate
// is an error rather than a silently shorter trust store.
func TestLoadManualRoots(t *testing.T) {
	ca, _ := newCA(t, "plant root")
	dir := t.TempDir()
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("opaque")})
	junkPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not DER")})
	write := func(name string, parts ...[]byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, append([]byte(nil), joinBytes(parts...)...), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cases := []struct {
		name  string
		file  string
		want  string
		roots bool
	}{
		{"missing file", filepath.Join(dir, "none.pem"), "read CA file", false},
		{"no certificate blocks", write("keyonly.pem", keyPEM), "no certificates", false},
		{"certificate block that is not DER", write("junk.pem", certPEM, junkPEM), "no certificates", false},
		{"key block skipped, certificate taken", write("mixed.pem", keyPEM, certPEM), "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := manualManager(t)
			err := m.LoadManualRoots(tc.file)
			if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if tc.want == "" && err != nil {
				t.Fatalf("err = %v", err)
			}
			if (m.Roots() != nil) != tc.roots {
				t.Fatalf("roots installed = %v, want %v", m.Roots() != nil, tc.roots)
			}
		})
	}
}

func joinBytes(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// TestExplicitTrustDisabledNeedsAnInstalledRoot: with the bootstrap
// deviation switched off, the very first /cacerts exchange is
// verified like any other — so it fails against a CA the trust store
// has never seen and succeeds once the operator installed it.
func TestExplicitTrustDisabledNeedsAnInstalledRoot(t *testing.T) {
	mk := newMockEST(t)
	ts := mk.server(t, time.Hour)
	base := strings.Replace(ts.URL, "https://", "", 1)
	newStrict := func() *Manager {
		m, err := New(Options{
			ESTBase: est.BaseURL(base, ""), Hostnames: []string{"node.test.local"},
			DataDir: t.TempDir(), ExplicitTrustDisabled: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	if err := newStrict().Bootstrap(context.Background()); err == nil {
		t.Fatal("bootstrap verified against an unknown CA must fail when explicit trust is disabled")
	}
	m := newStrict()
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: mk.caCert.Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.LoadManualRoots(caFile); err != nil {
		t.Fatal(err)
	}
	if err := m.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap with the CA installed: %v", err)
	}
}

// --- bootstrap ---

func TestBootstrapFaults(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T) *Manager
		want  string
	}{
		{"manual mode has no EST server", func(t *testing.T) *Manager {
			return manualManager(t)
		}, "no EST server configured"},
		{"EST base that is not a URL", func(t *testing.T) *Manager {
			m, err := New(Options{ESTBase: "http://[::1", Hostnames: []string{"n"}, DataDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			return m
		}, "build"},
		{"server answers /cacerts with 500", func(t *testing.T) *Manager {
			mk := newMockEST(t)
			mk.override["/.well-known/est/cacerts"] = func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
				w.WriteHeader(stdhttp.StatusInternalServerError)
			}
			return newManager(t, mk.server(t, time.Hour))
		}, "HTTP 500"},
		{"server answers /cacerts with something that is not PKCS#7", func(t *testing.T) *Manager {
			mk := newMockEST(t)
			mk.override["/.well-known/est/cacerts"] = func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
				_, _ = w.Write([]byte("hello"))
			}
			return newManager(t, mk.server(t, time.Hour))
		}, "est:"},
		{"server truncates the body", func(t *testing.T) *Manager {
			mk := newMockEST(t)
			mk.override["/.well-known/est/cacerts"] = func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
				// Promise more than is sent: the client's read ends in
				// an unexpected EOF, which is a read error, not a body.
				w.Header().Set("Content-Length", "4096")
				_, _ = w.Write([]byte("short"))
			}
			return newManager(t, mk.server(t, time.Hour))
		}, "read"},
		{"data dir vanished before the roots could be persisted", func(t *testing.T) *Manager {
			m := newManager(t, newMockEST(t).server(t, time.Hour))
			if err := os.RemoveAll(m.opts.DataDir); err != nil {
				t.Fatal(err)
			}
			return m
		}, "write"},
		{"ca.pem is a directory", func(t *testing.T) *Manager {
			m := newManager(t, newMockEST(t).server(t, time.Hour))
			if err := os.Mkdir(filepath.Join(m.opts.DataDir, "ca.pem"), 0o700); err != nil {
				t.Fatal(err)
			}
			return m
		}, "rename"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.setup(t)
			err := m.Bootstrap(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Bootstrap = %v, want %q", err, tc.want)
			}
		})
	}
}

// TestBootstrapSortsRootsFromIntermediates: /cacerts may return the
// whole chain. Self-signed CAs become trust anchors, the rest are
// intermediates offered during verification; a response with no
// self-signed certificate anchors trust at whatever it did return.
func TestBootstrapSortsRootsFromIntermediates(t *testing.T) {
	mk := newMockEST(t)
	interKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	interTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "issuing CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true,
	}
	interDER, err := x509.CreateCertificate(rand.Reader, interTmpl, mk.caCert, &interKey.PublicKey, mk.caKey)
	if err != nil {
		t.Fatal(err)
	}
	inter, _ := x509.ParseCertificate(interDER)

	serve := func(certs ...*x509.Certificate) stdhttp.HandlerFunc {
		return func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
			body, err := est.EncodeCertsResponse(certs)
			if err != nil {
				t.Errorf("encode: %v", err)
			}
			_, _ = w.Write(body)
		}
	}
	cases := []struct {
		name         string
		chain        []*x509.Certificate
		roots, inter int
	}{
		{"intermediate then root", []*x509.Certificate{inter, mk.caCert}, 1, 1},
		{"intermediate only anchors at itself", []*x509.Certificate{inter}, 1, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mk.override["/.well-known/est/cacerts"] = serve(tc.chain...)
			m := newManager(t, mk.server(t, time.Hour))
			if err := m.Bootstrap(context.Background()); err != nil {
				t.Fatalf("bootstrap: %v", err)
			}
			m.mu.RLock()
			defer m.mu.RUnlock()
			if len(m.roots) != tc.roots || len(m.inters) != tc.inter {
				t.Fatalf("roots=%d inters=%d, want %d/%d", len(m.roots), len(m.inters), tc.roots, tc.inter)
			}
		})
	}
}

// --- enrol ---

func TestEnrollFaults(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T) (*Manager, context.Context)
		want  string
	}{
		{"manual mode has no EST server", func(t *testing.T) (*Manager, context.Context) {
			return manualManager(t), context.Background()
		}, "no EST server configured"},
		{"hostname that is an IP address cannot go in a CSR", func(t *testing.T) (*Manager, context.Context) {
			mk := newMockEST(t)
			m := newManager(t, mk.server(t, time.Hour))
			m.opts.Hostnames = []string{"10.0.0.1"}
			return m, context.Background()
		}, "IP address"},
		{"500 without Retry-After is final", func(t *testing.T) (*Manager, context.Context) {
			mk := newMockEST(t)
			mk.override["/.well-known/est/simpleenroll"] = func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
				w.WriteHeader(stdhttp.StatusInternalServerError)
			}
			m := bootstrapped(t, mk)
			return m, context.Background()
		}, "HTTP 500"},
		{"Retry-After is honoured a bounded number of times", func(t *testing.T) (*Manager, context.Context) {
			installAfter(t, &instantAfter{})
			mk := newMockEST(t)
			// Unparseable and sub-second values fall back to one
			// second; after the fourth 503 the client gives up.
			answers := []string{"soon", "0", "1", "1", "1"}
			mk.override["/.well-known/est/simpleenroll"] = func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
				w.Header().Set("Retry-After", answers[0])
				if len(answers) > 1 {
					answers = answers[1:]
				}
				w.WriteHeader(stdhttp.StatusServiceUnavailable)
			}
			return bootstrapped(t, mk), context.Background()
		}, "HTTP 503"},
		{"Retry-After wait ends when the context does", func(t *testing.T) (*Manager, context.Context) {
			mk := newMockEST(t)
			mk.override["/.well-known/est/simpleenroll"] = func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(stdhttp.StatusServiceUnavailable)
			}
			m := bootstrapped(t, mk)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			// The context goes while the client is waiting out the
			// Retry-After, which is the wait that must observe it.
			installAfter(t, &instantAfter{fire: func(time.Duration) bool {
				cancel()
				return false
			}})
			return m, ctx
		}, context.Canceled.Error()},
		{"enrol response that is not PKCS#7", func(t *testing.T) (*Manager, context.Context) {
			mk := newMockEST(t)
			mk.override["/.well-known/est/simpleenroll"] = func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
				_, _ = w.Write([]byte("hello"))
			}
			return bootstrapped(t, mk), context.Background()
		}, "est:"},
		{"a certificate arrives but nothing was bootstrapped", func(t *testing.T) (*Manager, context.Context) {
			// Plain HTTP: the exchange itself succeeds without a trust
			// store, which is the only way to reach the anchor check.
			mk := newMockEST(t)
			ts := httptest.NewServer(mk.handler(t, time.Hour))
			t.Cleanup(ts.Close)
			m, err := New(Options{ESTBase: ts.URL + est.WellKnownESTPath, Hostnames: []string{"node.test.local"}, DataDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			return m, context.Background()
		}, "no trust anchors"},
		{"issued certificate does not chain to the bootstrapped CA", func(t *testing.T) (*Manager, context.Context) {
			mk := newMockEST(t)
			m := bootstrapped(t, mk)
			mk.signCert, mk.signKey = newCA(t, "rogue CA")
			return m, context.Background()
		}, "does not chain"},
		{"CSR key type the TLS pair cannot hold", func(t *testing.T) (*Manager, context.Context) {
			newCSR = func(opts est.CSROptions) ([]byte, crypto.Signer, error) {
				_, key, err := ed25519.GenerateKey(rand.Reader)
				if err != nil {
					return nil, nil, err
				}
				der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
					Subject: pkix.Name{CommonName: opts.CommonName}, DNSNames: opts.DNSNames,
				}, key)
				return der, key, err
			}
			t.Cleanup(func() { newCSR = est.NewCSR })
			return bootstrapped(t, newMockEST(t)), context.Background()
		}, "unsupported key type"},
		{"server.crt is a directory", func(t *testing.T) (*Manager, context.Context) {
			m := bootstrapped(t, newMockEST(t))
			if err := os.Mkdir(filepath.Join(m.opts.DataDir, "server.crt"), 0o700); err != nil {
				t.Fatal(err)
			}
			return m, context.Background()
		}, "rename"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, ctx := tc.setup(t)
			err := m.Enroll(ctx)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Enroll = %v, want %q", err, tc.want)
			}
		})
	}
}

// bootstrapped returns a manager that has already taken the mock's CA
// as its trust anchor.
func bootstrapped(t *testing.T, mk *mockEST) *Manager {
	t.Helper()
	m := newManager(t, mk.server(t, time.Hour))
	if err := m.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	return m
}

// TestRenewWithoutCertificateEnrolls: there is nothing to present on
// /simplereenroll before the first certificate exists, so Renew is
// Enroll until then.
func TestRenewWithoutCertificateEnrolls(t *testing.T) {
	mk := newMockEST(t)
	m := bootstrapped(t, mk)
	if err := m.Renew(context.Background()); err != nil {
		t.Fatalf("renew: %v", err)
	}
	if mk.reenrolls.Load() != 0 || m.Certificate() == nil {
		t.Fatalf("first Renew must enrol (reenrolls=%d, cert=%v)", mk.reenrolls.Load(), m.Certificate() != nil)
	}
}

// TestPersistPairRejectsAKeyItCannotMarshal pins the guard on the
// key writer directly: a signer PKCS#8 has no encoding for is an
// error, not an empty server.key.
func TestPersistPairRejectsAKeyItCannotMarshal(t *testing.T) {
	ca, caKey := newCA(t, "x")
	ecKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leaf := leafFrom(t, ca, caKey, "n", ecKey, time.Now().Add(-time.Minute), time.Now().Add(time.Hour))
	m := manualManager(t)
	err := m.persistPair(leaf, nil, opaqueSigner{})
	if err == nil || !strings.Contains(err.Error(), "marshal key") {
		t.Fatalf("persistPair = %v", err)
	}
	if _, err := buildPair(leaf, nil, opaqueSigner{}); err == nil {
		t.Fatal("buildPair accepted a key type the TLS stack cannot use")
	}
}

// opaqueSigner is a crypto.Signer with no PKCS#8 encoding.
type opaqueSigner struct{}

func (opaqueSigner) Public() crypto.PublicKey { return nil }
func (opaqueSigner) Sign(io.Reader, []byte, crypto.SignerOpts) ([]byte, error) {
	return nil, errors.New("opaque")
}

// --- the renewal loop ---

// TestRunRenewalLoop drives Run through its states with instant
// ticks: nothing to renew, not yet due, due and failing (backs off
// for half the remaining validity and gives up on ctx), due and
// succeeding (a new serial appears).
func TestRunRenewalLoop(t *testing.T) {
	const tick = 10 * time.Minute
	runUntil := func(t *testing.T, m *Manager, f *instantAfter, stop func() bool) {
		t.Helper()
		installAfter(t, f)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { m.Run(ctx); close(done) }()
		waitUntil(t, "the loop to reach the expected state", stop)
		cancel()
		select {
		case <-done:
		case <-time.After(faultTimeout):
			t.Fatal("Run did not return after ctx was cancelled")
		}
	}

	t.Run("nothing to renew keeps checking", func(t *testing.T) {
		f := &instantAfter{}
		runUntil(t, manualManager(t), f, func() bool { return f.count(func(d time.Duration) bool { return d == tick }) >= 3 })
	})

	t.Run("not yet due keeps checking", func(t *testing.T) {
		ca, caKey := newCA(t, "x")
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		crt, keyFile := writePEM(t, t.TempDir(), "fresh",
			leafFrom(t, ca, caKey, "n", key, time.Now().Add(-time.Minute), time.Now().Add(48*time.Hour)), key)
		m := manualManager(t)
		if err := m.LoadManual(crt, keyFile); err != nil {
			t.Fatal(err)
		}
		f := &instantAfter{}
		runUntil(t, m, f, func() bool { return f.count(func(d time.Duration) bool { return d == tick }) >= 3 })
	})

	// A failed renewal backs off for half the remaining validity, and
	// never for less than a minute — a certificate in its last seconds
	// must not turn the loop into a tight retry against a server that
	// just refused.
	for _, tc := range []struct {
		name      string
		remaining time.Duration
		wantWait  func(time.Duration) bool
	}{
		{"an hour left: half of it", time.Hour,
			func(d time.Duration) bool { return d >= 25*time.Minute && d <= 30*time.Minute }},
		{"a minute left: the one-minute floor", time.Minute,
			func(d time.Duration) bool { return d == time.Minute }},
	} {
		t.Run("due but renewal fails, "+tc.name, func(t *testing.T) {
			ca, caKey := newCA(t, "x")
			key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			// 90% through its life: past the RECOMMENDED 80%.
			crt, keyFile := writePEM(t, t.TempDir(), "due",
				leafFrom(t, ca, caKey, "n", key, time.Now().Add(-9*tc.remaining), time.Now().Add(tc.remaining)), key)
			m := manualManager(t) // no EST server: every renewal fails
			if err := m.LoadManual(crt, keyFile); err != nil {
				t.Fatal(err)
			}
			// The tick fires; the backoff parks until ctx is done.
			f := &instantAfter{fire: func(d time.Duration) bool { return d == tick }}
			runUntil(t, m, f, func() bool { return f.count(tc.wantWait) >= 1 })
			if m.Certificate().Leaf.Subject.CommonName != "n" {
				t.Fatal("a failed renewal must keep the current certificate")
			}
		})
	}

	t.Run("due and renewal succeeds", func(t *testing.T) {
		mk := newMockEST(t)
		// The mock backdates NotBefore by a minute; ten seconds of life
		// after that puts the leaf well past 80%.
		m := newManager(t, mk.server(t, 10*time.Second))
		ctx := context.Background()
		if err := m.Bootstrap(ctx); err != nil {
			t.Fatal(err)
		}
		if err := m.Enroll(ctx); err != nil {
			t.Fatal(err)
		}
		first := m.Certificate().Leaf.SerialNumber.Int64()
		// Wait for the INSTALLED certificate to change, not for the mock's
		// request counter: the counter ticks inside the EST handler, before
		// the client has stored the reply (the race CI caught under -race).
		runUntil(t, m, &instantAfter{}, func() bool {
			return m.Certificate().Leaf.SerialNumber.Int64() != first
		})
		if mk.reenrolls.Load() < 1 {
			t.Fatal("the certificate changed without a re-enrollment being served")
		}
	})
}
