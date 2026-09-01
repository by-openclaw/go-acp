package registry

// Served-face TLS tests (--serve-tls-cert/--serve-tls-key, issue
// #948): once armed the face is HTTPS/WSS ONLY — a CA-validated https
// read works, plain http does not, ws_href is minted wss://, the
// announce TXT carries api_proto=https, /status.json reports
// serve_tls=true, and the option validation fails fast. The pair is
// a self-signed in-test certificate (crypto/x509, the certmgr test
// pattern) — no registry TLS serving test existed to borrow.

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
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	codec "dhs/internal/amwa/codec/dnssd"
)

// mintServeTLSPair writes a self-signed serving pair (SAN: 127.0.0.1
// + mirror-tls.test) to a temp dir and returns the paths plus a root
// pool trusting it.
func mintServeTLSPair(t *testing.T) (certFile, keyFile string, pool *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "mirror-tls.test"},
		DNSNames:              []string{"mirror-tls.test"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile = filepath.Join(dir, "serve.crt")
	keyFile = filepath.Join(dir, "serve.key")
	certOut, err := os.Create(certFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatal(err)
	}
	_ = certOut.Close()
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyOut, err := os.Create(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
		t.Fatal(err)
	}
	_ = keyOut.Close()
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool = x509.NewCertPool()
	pool.AddCert(parsed)
	return certFile, keyFile, pool
}

// stubServeTLSDataDir keeps certmgr's working dir out of the package
// tree for the duration of one test.
func stubServeTLSDataDir(t *testing.T) {
	t.Helper()
	orig := mirrorServeTLSDataDir
	mirrorServeTLSDataDir = filepath.Join(t.TempDir(), "certmgr")
	t.Cleanup(func() { mirrorServeTLSDataDir = orig })
}

// TestMirrorServeTLS: the armed face answers CA-validated https,
// refuses plain http, mints wss:// ws_hrefs, announces
// api_proto=https, and reports serve_tls=true on /status.json.
func TestMirrorServeTLS(t *testing.T) {
	stubServeTLSDataDir(t)
	fr := stubServeResponder(t)
	certFile, keyFile, pool := mintServeTLSPair(t)

	plant := &fakePlant{}
	target := httptest.NewServer(plant.targetHandler())
	t.Cleanup(target.Close)
	push := newPushSource()
	src := httptest.NewServer(stdhttp.NotFoundHandler())
	src.Config.Handler = push.handler(t, func() string { return src.URL })
	t.Cleanup(src.Close)

	statusAddr := freeLoopbackAddr(t)
	m, err := NewMirror(MirrorOptions{
		Source: src.URL, Target: target.URL, APIVer: "v1.3",
		ServeAddr: "127.0.0.1:0", ServeAdvertiseHost: "mirror-tls.test",
		ServePri: 100, ServeTLSCert: certFile, ServeTLSKey: keyFile,
		StatusAddr: statusAddr,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = m.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return m.ServeAddr() != "" }, "served face to bind")
	base := "https://" + m.ServeAddr()

	client := &stdhttp.Client{Transport: &stdhttp.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool},
	}}

	// CA-validated https read works.
	resp, err := client.Get(base + "/x-nmos/query/v1.3/nodes")
	if err != nil {
		t.Fatalf("https GET: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("https GET = %d, want 200", resp.StatusCode)
	}

	// Plain http against the secured face must be refused — BCP-003-01
	// (a secured server SHALL NOT accept plain HTTP). Go's TLS server
	// renders the refusal as either a broken handshake or net/http's
	// "Client sent an HTTP request to an HTTPS server" 400 — anything
	// but a served 200.
	plainResp, err := stdhttp.Get("http://" + m.ServeAddr() + "/x-nmos/query/v1.3/nodes")
	if err == nil {
		st := plainResp.StatusCode
		_ = plainResp.Body.Close()
		if st == 200 {
			t.Fatal("plain http against the TLS face answered 200 — must refuse")
		}
	}

	// ws_href on a subscription is wss://.
	subResp, err := client.Post(base+"/x-nmos/query/v1.3/subscriptions", "application/json",
		strings.NewReader(`{"resource_path":"/nodes","persist":false}`))
	if err != nil {
		t.Fatalf("https subscription POST: %v", err)
	}
	body, _ := io.ReadAll(subResp.Body)
	_ = subResp.Body.Close()
	if subResp.StatusCode != 200 && subResp.StatusCode != 201 {
		t.Fatalf("subscription POST = %d (%s)", subResp.StatusCode, body)
	}
	var sub struct {
		WSHref string `json:"ws_href"`
	}
	if err := json.Unmarshal(body, &sub); err != nil {
		t.Fatalf("subscription body: %v (%s)", err, body)
	}
	if !strings.HasPrefix(sub.WSHref, "wss://mirror-tls.test:") {
		t.Errorf("ws_href = %q, want wss://mirror-tls.test:…", sub.WSHref)
	}

	// The announce carries api_proto=https.
	select {
	case ins := <-fr.announced:
		if got := ins.TXT[codec.TXTKeyAPIProto]; got != "https" {
			t.Errorf("announce api_proto = %q, want https", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no mDNS announce for the secured served face")
	}

	// /status.json (plain http — the status endpoint is separate from
	// the served face) says serve_tls=true.
	waitFor(t, 5*time.Second, func() bool {
		st, _, body := getWithToken(t, "http://"+statusAddr+"/status.json", "")
		return st == 200 && strings.Contains(string(body), `"serve_tls":true`)
	}, `"serve_tls":true on /status.json`)
}

// TestNewMirrorServeTLSValidation: the pair travels together and
// requires --serve — both caught before anything runs.
func TestNewMirrorServeTLSValidation(t *testing.T) {
	_, err := NewMirror(MirrorOptions{
		Source: "http://s:1", Target: "http://t:2",
		ServeAddr: ":0", ServeTLSCert: "cert.pem",
	})
	if err == nil || !strings.Contains(err.Error(), "travel together") {
		t.Errorf("cert without key = %v, want the travel-together error", err)
	}
	_, err = NewMirror(MirrorOptions{
		Source: "http://s:1", Target: "http://t:2",
		ServeTLSCert: "cert.pem", ServeTLSKey: "key.pem",
	})
	if err == nil || !strings.Contains(err.Error(), "--serve") {
		t.Errorf("TLS pair without serve = %v, want the add---serve error", err)
	}
}
