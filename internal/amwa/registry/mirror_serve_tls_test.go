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
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
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

// mintServeTLSPairAlgo writes a self-signed serving pair for one
// public-key algorithm ("rsa" | "ecdsa"; SAN: 127.0.0.1 +
// mirror-tls.test) to a temp dir — the two halves of BCP-003-01
// dual-certificate serving.
func mintServeTLSPairAlgo(t *testing.T, algo string) (certFile, keyFile string, parsed *x509.Certificate) {
	t.Helper()
	var pub any
	var signer crypto.Signer
	var keyBlock *pem.Block
	switch algo {
	case "ecdsa":
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		keyDER, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		pub, signer = &key.PublicKey, key
		keyBlock = &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}
	case "rsa":
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		pub, signer = &key.PublicKey, key
		keyBlock = &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}
	default:
		t.Fatalf("unknown algo %q", algo)
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
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, signer)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile = filepath.Join(dir, algo+".serve.crt")
	keyFile = filepath.Join(dir, algo+".serve.key")
	certOut, err := os.Create(certFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatal(err)
	}
	_ = certOut.Close()
	keyOut, err := os.Create(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(keyOut, keyBlock); err != nil {
		t.Fatal(err)
	}
	_ = keyOut.Close()
	parsed, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile, parsed
}

// mintServeTLSPair keeps the single-pair (ECDSA) shape the earlier
// tests use — path pair plus a root pool trusting it.
func mintServeTLSPair(t *testing.T) (certFile, keyFile string, pool *x509.CertPool) {
	t.Helper()
	certFile, keyFile, parsed := mintServeTLSPairAlgo(t, "ecdsa")
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
	// Dual-certificate lists pair positionally — a count mismatch is
	// caught before any file is touched.
	_, err = NewMirror(MirrorOptions{
		Source: "http://s:1", Target: "http://t:2", ServeAddr: ":0",
		ServeTLSCert: "rsa.pem,ecdsa.pem", ServeTLSKey: "rsa.key",
	})
	if err == nil || !strings.Contains(err.Error(), "counts differ") {
		t.Errorf("mismatched pair counts = %v, want the counts-differ error", err)
	}
}

// TestMirrorServeTLSDualCert: BCP-003-01 dual-certificate serving —
// with an RSA AND an ECDSA pair installed, an RSA-only TLS 1.2 client
// (pinned to the spec's mandatory TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256)
// completes the handshake against the RSA certificate, and an
// ECDSA-pinned client against the ECDSA one — Go's handshake selects
// per ClientHello, no custom selection code (the tool's test_02/test_08
// pair of findings, reproduced as a unit test).
func TestMirrorServeTLSDualCert(t *testing.T) {
	stubServeTLSDataDir(t)
	rsaCert, rsaKey, rsaParsed := mintServeTLSPairAlgo(t, "rsa")
	ecdsaCert, ecdsaKey, ecdsaParsed := mintServeTLSPairAlgo(t, "ecdsa")
	pool := x509.NewCertPool()
	pool.AddCert(rsaParsed)
	pool.AddCert(ecdsaParsed)

	plant := &fakePlant{}
	target := httptest.NewServer(plant.targetHandler())
	t.Cleanup(target.Close)
	push := newPushSource()
	src := httptest.NewServer(stdhttp.NotFoundHandler())
	src.Config.Handler = push.handler(t, func() string { return src.URL })
	t.Cleanup(src.Close)

	m, err := NewMirror(MirrorOptions{
		Source: src.URL, Target: target.URL, APIVer: "v1.3",
		ServeAddr:    "127.0.0.1:0",
		ServeTLSCert: rsaCert + "," + ecdsaCert,
		ServeTLSKey:  rsaKey + "," + ecdsaKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = m.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return m.ServeAddr() != "" }, "served face to bind")

	// handshake pins TLS 1.2 (1.3 ignores CipherSuites) and exactly one
	// cipher, then reports the leaf certificate the server presented.
	handshake := func(cipher uint16) *x509.Certificate {
		t.Helper()
		conn, err := tls.Dial("tcp", m.ServeAddr(), &tls.Config{
			RootCAs:      pool,
			MinVersion:   tls.VersionTLS12,
			MaxVersion:   tls.VersionTLS12,
			CipherSuites: []uint16{cipher},
		})
		if err != nil {
			t.Fatalf("handshake with cipher %#x: %v", cipher, err)
		}
		defer func() { _ = conn.Close() }()
		return conn.ConnectionState().PeerCertificates[0]
	}

	rsaLeaf := handshake(tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256)
	if _, ok := rsaLeaf.PublicKey.(*rsa.PublicKey); !ok {
		t.Errorf("RSA-only client got a %T leaf — the mandatory RSA cipher needs the RSA certificate", rsaLeaf.PublicKey)
	}
	ecdsaLeaf := handshake(tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256)
	if _, ok := ecdsaLeaf.PublicKey.(*ecdsa.PublicKey); !ok {
		t.Errorf("ECDSA-pinned client got a %T leaf — want the ECDSA certificate", ecdsaLeaf.PublicKey)
	}
}
