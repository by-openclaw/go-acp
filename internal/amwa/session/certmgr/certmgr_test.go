package certmgr

// Manager tests against an in-repo mock EST server (the AMWA tool
// ships none for BCP-003-03). The mock implements /cacerts +
// /simpleenroll + /simplereenroll per RFC 7030's certs-only shapes,
// signs whatever CSR arrives with a test CA, and exercises the
// Retry-After and client-certificate rules.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dhs/internal/amwa/codec/est"
)

type mockEST struct {
	caCert *x509.Certificate
	caKey  *ecdsa.PrivateKey
	serial int64

	enrolls      atomic.Int32
	reenrolls    atomic.Int32
	retryFirst   bool
	sawPeerCert  atomic.Bool
	lastReenroll atomic.Value // string CN of the peer cert on reenroll
}

func newMockEST(t *testing.T) *mockEST {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "dhs Mock EST CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(48 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("mock CA: %v", err)
	}
	cert, _ := x509.ParseCertificate(der)
	return &mockEST{caCert: cert, caKey: key, serial: 100}
}

func (mk *mockEST) sign(t *testing.T, req *x509.CertificateRequest, life time.Duration) *x509.Certificate {
	t.Helper()
	mk.serial++
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(mk.serial),
		Subject:      req.Subject,
		DNSNames:     req.DNSNames,
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(life),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		// EKU server+client per BCP-003-03 (the cert authenticates
		// renewals too).
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, mk.caCert, req.PublicKey, mk.caKey)
	if err != nil {
		t.Fatalf("mock sign: %v", err)
	}
	c, _ := x509.ParseCertificate(der)
	return c
}

func (mk *mockEST) server(t *testing.T, leafLife time.Duration) *httptest.Server {
	t.Helper()
	mux := stdhttp.NewServeMux()
	readCSR := func(r *stdhttp.Request) *x509.CertificateRequest {
		raw, _ := io.ReadAll(r.Body)
		der, err := est.DecodeBase64Robust(raw)
		if err != nil {
			t.Fatalf("mock: csr b64: %v", err)
		}
		req, err := x509.ParseCertificateRequest(der)
		if err != nil {
			t.Fatalf("mock: csr parse: %v", err)
		}
		return req
	}
	respond := func(w stdhttp.ResponseWriter, certs ...*x509.Certificate) {
		body, err := est.EncodeCertsResponse(certs)
		if err != nil {
			t.Fatalf("mock: encode: %v", err)
		}
		w.Header().Set("Content-Type", est.ContentTypePKCS7)
		w.Header().Set("Content-Transfer-Encoding", "base64")
		_, _ = w.Write(body)
	}
	mux.HandleFunc("/.well-known/est/cacerts", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		respond(w, mk.caCert)
	})
	mux.HandleFunc("/.well-known/est/simpleenroll", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if mk.retryFirst && mk.enrolls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(stdhttp.StatusServiceUnavailable)
			return
		}
		if len(r.TLS.PeerCertificates) > 0 {
			mk.sawPeerCert.Store(true)
		}
		respond(w, mk.sign(t, readCSR(r), leafLife), mk.caCert)
	})
	mux.HandleFunc("/.well-known/est/simplereenroll", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		mk.reenrolls.Add(1)
		if len(r.TLS.PeerCertificates) == 0 {
			w.WriteHeader(stdhttp.StatusUnauthorized)
			return
		}
		mk.lastReenroll.Store(r.TLS.PeerCertificates[0].Subject.CommonName)
		respond(w, mk.sign(t, readCSR(r), leafLife), mk.caCert)
	})
	ts := httptest.NewUnstartedServer(mux)
	// The mock EST server presents a cert signed by ITS OWN CA — that
	// is what the client verifies against after bootstrap (the spec's
	// explicit-trust flow), so httptest's default self-signed cert
	// would be rejected exactly as a rogue server should be.
	srvKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	mk.serial++
	srvTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(mk.serial),
		Subject:      pkix.Name{CommonName: "est.test.local"},
		DNSNames:     []string{"est.test.local", "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	srvDER, err := x509.CreateCertificate(rand.Reader, srvTmpl, mk.caCert, &srvKey.PublicKey, mk.caKey)
	if err != nil {
		t.Fatalf("mock server cert: %v", err)
	}
	ts.TLS = &tls.Config{
		ClientAuth:   tls.RequestClientCert,
		Certificates: []tls.Certificate{{Certificate: [][]byte{srvDER}, PrivateKey: srvKey}},
	}
	ts.StartTLS()
	t.Cleanup(ts.Close)
	return ts
}

func newManager(t *testing.T, ts *httptest.Server) *Manager {
	t.Helper()
	base := strings.Replace(ts.URL, "https://", "", 1)
	m, err := New(Options{
		ESTBase:   est.BaseURL(base, ""),
		Hostnames: []string{"node.test.local", "dhs-node"},
		Serial:    "SN-0001",
		DataDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

func TestBootstrapEnrollRenew(t *testing.T) {
	mk := newMockEST(t)
	ts := mk.server(t, 10*time.Hour)
	m := newManager(t, ts)
	ctx := context.Background()

	// Bootstrap: explicit trust, roots stored + persisted.
	if err := m.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if m.Roots() == nil {
		t.Fatal("roots missing after bootstrap")
	}
	if _, err := os.Stat(filepath.Join(m.opts.DataDir, "ca.pem")); err != nil {
		t.Errorf("ca.pem not persisted: %v", err)
	}

	// Enroll: certificate arrives, validates, persists.
	if err := m.Enroll(ctx); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	pair := m.Certificate()
	if pair == nil || pair.Leaf == nil {
		t.Fatal("no live certificate after enroll")
	}
	if pair.Leaf.Subject.CommonName != "node.test.local" {
		t.Errorf("leaf CN = %s", pair.Leaf.Subject.CommonName)
	}
	firstSerial := pair.Leaf.SerialNumber.Int64()
	for _, f := range []string{"server.crt", "server.key"} {
		if _, err := os.Stat(filepath.Join(m.opts.DataDir, f)); err != nil {
			t.Errorf("%s not persisted: %v", f, err)
		}
	}

	// Renew: presents the CURRENT cert as client identity, new serial.
	if err := m.Renew(ctx); err != nil {
		t.Fatalf("renew: %v", err)
	}
	if cn, _ := mk.lastReenroll.Load().(string); cn != "node.test.local" {
		t.Errorf("reenroll peer cert CN = %q, want the current leaf", cn)
	}
	if got := m.Certificate().Leaf.SerialNumber.Int64(); got == firstSerial {
		t.Error("renewal must mint a new certificate")
	}
	// GetCertificate serves the live pair for TLS.
	if c, err := m.GetCertificate(nil); err != nil || c.Leaf.SerialNumber.Int64() == firstSerial {
		t.Errorf("GetCertificate = %v (serial unchanged?)", err)
	}
}

func TestEnrollRetryAfter(t *testing.T) {
	mk := newMockEST(t)
	mk.retryFirst = true
	ts := mk.server(t, time.Hour)
	m := newManager(t, ts)
	ctx := context.Background()
	if err := m.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	start := time.Now()
	if err := m.Enroll(ctx); err != nil {
		t.Fatalf("enroll after 503: %v", err)
	}
	if time.Since(start) < time.Second {
		t.Error("Retry-After must be honoured before the retry")
	}
}

func TestEnrollWithoutBootstrapFails(t *testing.T) {
	mk := newMockEST(t)
	ts := mk.server(t, time.Hour)
	m := newManager(t, ts)
	if err := m.Enroll(context.Background()); err == nil {
		t.Error("enroll without trust anchors must fail")
	}
}

func TestRenewalTimingHelpers(t *testing.T) {
	mk := newMockEST(t)
	ts := mk.server(t, time.Hour)
	m := newManager(t, ts)
	ctx := context.Background()
	if err := m.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if err := m.Enroll(ctx); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	leaf := m.Certificate().Leaf
	if est.RenewalDue(leaf, time.Now(), est.RenewalRecommendedFraction) {
		t.Error("fresh certificate must not be renewal-due")
	}
	if !est.RenewalDue(leaf, leaf.NotAfter.Add(-time.Minute), est.RenewalRecommendedFraction) {
		t.Error("nearly-expired certificate must be renewal-due")
	}
	// The TLS server floor: TLS 1.2 minimum + mandatory suite present.
	cfg := m.TLSServerConfig()
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %x", cfg.MinVersion)
	}
	found := false
	for _, cs := range cfg.CipherSuites {
		if cs == tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256 {
			found = true
		}
	}
	if !found {
		t.Error("mandatory TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256 missing")
	}
}
