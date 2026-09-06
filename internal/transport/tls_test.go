package transport

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeKeyPair generates a self-signed certificate and writes cert + key as
// PEM into dir, returning both paths. Generated rather than committed so the
// test never carries an expiring fixture.
func writeKeyPair(t *testing.T, dir string) (certPath, keyPath string, certPEM []byte) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "dhs-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certPath, keyPath, certPEM
}

// The zero value must stay "no TLS": adding TLSOptions to a struct must not
// turn a plaintext connector into a TLS one.
func TestTLSOptionsDisabledReturnsNilConfig(t *testing.T) {
	cfg, err := TLSOptions{}.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	if cfg != nil {
		t.Fatalf("disabled TLSOptions returned a config: %+v", cfg)
	}
}

// The whole point of the file: every enabled config carries the version
// floor, whatever else the connector asked for.
func TestTLSOptionsAlwaysSetsMinVersion(t *testing.T) {
	for _, o := range []TLSOptions{
		{Enable: true},
		{Enable: true, Insecure: true},
		{Enable: true, ServerName: "cerebrum"},
	} {
		cfg, err := o.Client()
		if err != nil {
			t.Fatalf("Client(%+v): %v", o, err)
		}
		if cfg.MinVersion != MinTLSVersion {
			t.Errorf("Client(%+v).MinVersion = %#x, want %#x",
				o, cfg.MinVersion, MinTLSVersion)
		}
		if cfg.MinVersion != tls.VersionTLS12 {
			t.Errorf("MinTLSVersion drifted below TLS 1.2")
		}
	}
}

func TestTLSOptionsCarriesPosture(t *testing.T) {
	cfg, err := TLSOptions{Enable: true, Insecure: true, ServerName: "nb"}.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	if !cfg.InsecureSkipVerify {
		t.Error("Insecure not carried into the config")
	}
	if cfg.ServerName != "nb" {
		t.Errorf("ServerName = %q, want %q", cfg.ServerName, "nb")
	}
	if cfg.RootCAs != nil {
		t.Error("RootCAs set without being asked for")
	}
	if cfg.Certificates != nil {
		t.Error("Certificates set without being asked for")
	}
}

func TestTLSOptionsRootCAsPoolWins(t *testing.T) {
	pool := x509.NewCertPool()
	cfg, err := TLSOptions{Enable: true, RootCAs: pool, CAFile: "does-not-exist"}.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	if cfg.RootCAs != pool {
		t.Error("in-memory RootCAs did not take precedence over CAFile")
	}
}

func TestTLSOptionsCAFile(t *testing.T) {
	dir := t.TempDir()
	_, _, certPEM := writeKeyPair(t, dir)
	caPath := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caPath, certPEM, 0o600); err != nil {
		t.Fatalf("write ca: %v", err)
	}

	cfg, err := TLSOptions{Enable: true, CAFile: caPath}.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	if cfg.RootCAs == nil {
		t.Fatal("CAFile did not populate RootCAs")
	}
}

// A missing or empty CA file must fail loudly. Falling back to the system
// pool would verify against anchors the operator did not choose.
func TestTLSOptionsCAFileErrors(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.pem")
	if err := os.WriteFile(empty, []byte("not a certificate\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	tests := []struct {
		name string
		file string
		want string
	}{
		{"missing", filepath.Join(dir, "absent.pem"), "read CA file"},
		{"no certificate", empty, "contains no certificate"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := TLSOptions{Enable: true, CAFile: tc.file}.Client()
			if err == nil {
				t.Fatalf("Client succeeded, want error; cfg=%+v", cfg)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestTLSOptionsClientCertificate(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath, _ := writeKeyPair(t, dir)

	cfg, err := TLSOptions{Enable: true, CertFile: certPath, KeyFile: keyPath}.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	if len(cfg.Certificates) != 1 {
		t.Fatalf("Certificates = %d, want 1", len(cfg.Certificates))
	}
}

// Not every identity is a file. The BCP-003-03 certificate manager enrols
// over EST and holds the result in memory, renewing it on a timer, so there
// is no path to point CertFile at — before this it built its own
// *tls.Config, which is how it came to be on the transport allowlist.
func TestTLSOptionsInMemoryCertificate(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath, _ := writeKeyPair(t, dir)
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatalf("LoadX509KeyPair: %v", err)
	}

	cfg, err := TLSOptions{Enable: true, Certificates: []tls.Certificate{pair}}.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	if len(cfg.Certificates) != 1 {
		t.Fatalf("Certificates = %d, want 1", len(cfg.Certificates))
	}
	if cfg.MinVersion != MinTLSVersion {
		t.Errorf("MinVersion = %d — the whole point is not having to remember it", cfg.MinVersion)
	}
}

// An in-memory identity wins over a file pair, the same way RootCAs wins
// over CAFile: the caller that went to the trouble of loading one means it.
func TestTLSOptionsInMemoryCertificateWins(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath, _ := writeKeyPair(t, dir)
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatalf("LoadX509KeyPair: %v", err)
	}

	cfg, err := TLSOptions{
		Enable:       true,
		Certificates: []tls.Certificate{pair},
		CertFile:     "does-not-exist",
		KeyFile:      "does-not-exist",
	}.Client()
	if err != nil {
		t.Fatalf("Client must not read the files it was told to ignore: %v", err)
	}
	if len(cfg.Certificates) != 1 {
		t.Fatalf("Certificates = %d, want 1", len(cfg.Certificates))
	}
}

func TestTLSOptionsClientCertificateErrors(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath, _ := writeKeyPair(t, dir)

	tests := []struct {
		name            string
		cert, key, want string
	}{
		{"key only", "", keyPath, "both CertFile and KeyFile"},
		{"cert only", certPath, "", "both CertFile and KeyFile"},
		{"unreadable", filepath.Join(dir, "absent.pem"), keyPath, "load client certificate"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := TLSOptions{Enable: true, CertFile: tc.cert, KeyFile: tc.key}.Client()
			if err == nil {
				t.Fatal("Client succeeded, want error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}
