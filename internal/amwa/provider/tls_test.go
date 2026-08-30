package provider

// BCP-003-01 serving-side e2e: a Node armed with a (manually
// installed) TLS pair serves HTTPS only, mints https hrefs
// everywhere, and declares HSTS. The EST provisioning path is
// covered in session/certmgr; this exercises the provider wiring.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	stdhttp "net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeTLSPair writes a self-signed CA + a leaf for "localhost" into
// dir and returns (certFile, keyFile, caPool).
func writeTLSPair(t *testing.T, dir string) (string, string, *x509.CertPool) {
	t.Helper()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "dhs test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true,
	}
	caDER, _ := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	caCert, _ := x509.ParseCertificate(caDER)

	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "localhost"},
		DNSNames:  []string{"localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, _ := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &leafKey.PublicKey, caKey)
	keyDER, _ := x509.MarshalPKCS8PrivateKey(leafKey)

	certFile := filepath.Join(dir, "srv.crt")
	keyFile := filepath.Join(dir, "srv.key")
	if err := os.WriteFile(certFile,
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile,
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	return certFile, keyFile, pool
}

func TestNodeServesTLS(t *testing.T) {
	certFile, keyFile, pool := writeTLSPair(t, t.TempDir())
	addr := freeAddr(t)
	s, err := NewIS04NodeServer(nil, validBundle(), IS04NodeConfig{
		Bind: addr, DiscoveryMode: "static",
		TLSCertFile: certFile, TLSKeyFile: keyFile,
		// Keep provisioned material in a temp dir — never let the
		// default .cache/nmos-tls litter the package directory during
		// `go test` (a stray root-owned dir broke the EL runners'
		// post-run cache hashFiles step).
		TLSDataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewIS04NodeServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); _ = s.Stop() })
	go func() { _ = s.Serve(ctx) }()

	hc := &stdhttp.Client{
		Timeout: 3 * time.Second,
		Transport: &stdhttp.Transport{TLSClientConfig: &tls.Config{
			RootCAs: pool, ServerName: "localhost", MinVersion: tls.VersionTLS12,
		}},
	}
	var resp *stdhttp.Response
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = hc.Get("https://" + addr + "/x-nmos/")
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("HTTPS GET: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"node/"`) {
		t.Fatalf("https /x-nmos/ = %d %s", resp.StatusCode, body)
	}
	// HSTS declared (BCP-003-01 SHOULD, 12-month max-age).
	if hsts := resp.Header.Get("Strict-Transport-Security"); !strings.Contains(hsts, "max-age=31536000") {
		t.Errorf("HSTS header = %q", hsts)
	}

	// The Node's own href and every minted control href say https.
	resp, err = hc.Get("https://" + addr + "/x-nmos/node/v1.3/self")
	if err != nil {
		t.Fatalf("self: %v", err)
	}
	var self struct {
		Href string `json:"href"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&self)
	_ = resp.Body.Close()
	if !strings.HasPrefix(self.Href, "https://") {
		t.Errorf("node href = %q, want https", self.Href)
	}
	resp, err = hc.Get("https://" + addr + "/x-nmos/node/v1.3/devices")
	if err != nil {
		t.Fatalf("devices: %v", err)
	}
	devBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if strings.Contains(string(devBody), `"href":"http://`) {
		t.Errorf("a control href still says plain http: %s", devBody)
	}

	// Plain HTTP on the same port must fail — TLS-only listener.
	plain := &stdhttp.Client{Timeout: 2 * time.Second}
	if r2, err := plain.Get("http://" + addr + "/x-nmos/"); err == nil {
		_ = r2.Body.Close()
		if r2.StatusCode == 200 {
			t.Error("plain HTTP answered on a TLS-only listener")
		}
	}
}
