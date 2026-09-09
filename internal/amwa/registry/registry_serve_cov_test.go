package registry

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
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

	"dhs/internal/amwa/codec/est"
	registryslot "dhs/internal/registry"
)

// serveOn brings a Registry up on a real port and returns its address.
// The caller's options decide which of the BCP-003 faces are armed.
func serveOn(t *testing.T, opts registryslot.ServeOptions) (*Registry, string, context.CancelFunc) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	opts.BindAddrs = []string{addr}

	r := &Registry{logger: newRegistryLogTap().logger()}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Serve(ctx, opts) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("Serve did not return after its context ended")
		}
	})
	// A configuration Serve refuses outright must fail the test here
	// rather than as a timeout waiting for a face that never comes up.
	select {
	case err := <-done:
		t.Fatalf("Serve returned before the face came up: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	return r, addr, cancel
}

// waitForFace polls until the served face answers, so a test never
// races the listener coming up.
func waitForFace(t *testing.T, client *stdhttp.Client, url string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the served face never answered at %s", url)
}

// A Registry served over TLS with the BCP-003-02 gate armed answers
// its REST face over https, and refuses a WebSocket upgrade that
// carries no token — the spec says a server SHALL NOT upgrade on an
// invalid one, and that check sits outside the route table.
func TestServeTLSAuthAndTheWebSocketDispatcher(t *testing.T) {
	useRegistryResponder(t, &scriptedRegistryResponder{})
	certPath, keyPath := selfSignedPair(t)

	_, addr, _ := serveOn(t, registryslot.ServeOptions{
		DiscoveryMode: "static",
		APIVer:        "v1.3",
		TLSCertFile:   certPath,
		TLSKeyFile:    keyPath,
		TLSDataDir:    t.TempDir(),
		AuthURL:       "http://127.0.0.1:1", // nothing is listening: the JWKS never arrives
	})

	client := &stdhttp.Client{Transport: &stdhttp.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // a self-signed test cert
	}}
	base := "https://" + addr
	waitForFace(t, client, base+"/x-nmos")

	// The route table answers the discovery root even while the gate
	// has no keys — an unauthenticated read is still refused, but the
	// face is up.
	resp, err := client.Get(base + "/x-nmos/query/v1.3/nodes")
	if err != nil {
		t.Fatalf("GET over TLS: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != stdhttp.StatusUnauthorized && resp.StatusCode != stdhttp.StatusOK {
		t.Errorf("a gated read = %d", resp.StatusCode)
	}

	// A WebSocket upgrade with no token is answered by the gate, not
	// upgraded.
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
	_ = resp.Body.Close()
	if resp.StatusCode == stdhttp.StatusSwitchingProtocols {
		t.Error("an unauthenticated upgrade must not switch protocols")
	}
}

// With no advertise host the Registry derives one from its bind, and
// with no TLS data directory it takes its own default — an operator
// who names neither still gets a served, discoverable face.
func TestServeDerivesAdvertiseAndDataDir(t *testing.T) {
	useRegistryResponder(t, &scriptedRegistryResponder{})
	certPath, keyPath := selfSignedPair(t)

	// The default data directory is relative, so run from a temp cwd.
	dir := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	_, addr, _ := serveOn(t, registryslot.ServeOptions{
		DiscoveryMode: "static",
		APIVer:        "v1.3",
		TLSCertFile:   certPath,
		TLSKeyFile:    keyPath,
		// No AdvertiseHost, no TLSDataDir.
	})
	client := &stdhttp.Client{Transport: &stdhttp.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // a self-signed test cert
	}}
	waitForFace(t, client, "https://"+addr+"/x-nmos")
}

// A certificate the operator named but the host cannot read is a
// startup failure, and so is a data directory that cannot be created.
func TestServeRefusesUnusableTLSMaterial(t *testing.T) {
	useRegistryResponder(t, &scriptedRegistryResponder{})
	certPath, keyPath := selfSignedPair(t)

	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	for name, opts := range map[string]registryslot.ServeOptions{
		"a data directory that cannot be created": {
			BindAddrs: []string{"127.0.0.1:0"}, AdvertiseHost: "127.0.0.1:8235",
			DiscoveryMode: "static", TLSCertFile: certPath, TLSKeyFile: keyPath,
			TLSDataDir: filepath.Join(blocker, "tls"),
		},
		"a certificate file that is not there": {
			BindAddrs: []string{"127.0.0.1:0"}, AdvertiseHost: "127.0.0.1:8235",
			DiscoveryMode: "static",
			TLSCertFile:   filepath.Join(t.TempDir(), "absent.pem"), TLSKeyFile: keyPath,
			TLSDataDir: t.TempDir(),
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := (&Registry{}).Serve(context.Background(), opts); err == nil {
				t.Error("was accepted")
			}
		})
	}
}

// A Registry whose host cannot name itself still enrols a certificate:
// the identity list falls back to a fixed subject rather than being
// left empty, which no CA would sign.
func TestServeWithoutAHostname(t *testing.T) {
	useRegistryResponder(t, &scriptedRegistryResponder{})
	prevHost := osHostnameFn
	osHostnameFn = func() (string, error) { return "", errors.New("no hostname") }
	t.Cleanup(func() { osHostnameFn = prevHost })

	certPath, keyPath := selfSignedPair(t)
	_, addr, _ := serveOn(t, registryslot.ServeOptions{
		AdvertiseHost: "10.6.239.113:8235", // an IP: not a DNS identity
		DiscoveryMode: "static",
		APIVer:        "v1.3",
		TLSCertFile:   certPath,
		TLSKeyFile:    keyPath,
		TLSDataDir:    t.TempDir(),
	})
	client := &stdhttp.Client{Transport: &stdhttp.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // a self-signed test cert
	}}
	waitForFace(t, client, "https://"+addr+"/x-nmos")
}

// estServer is a minimal BCP-003-01 EST server: it publishes its CA
// and signs whatever CSR is enrolled with it.
func estServer(t *testing.T) string {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "dhs test EST CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}

	mux := stdhttp.NewServeMux()
	mux.HandleFunc("/.well-known/est/cacerts", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		body, err := est.EncodeCertsResponse([]*x509.Certificate{ca})
		if err != nil {
			stdhttp.Error(w, err.Error(), stdhttp.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", est.ContentTypePKCS7)
		_, _ = w.Write(body)
	})
	enroll := func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			stdhttp.Error(w, err.Error(), stdhttp.StatusBadRequest)
			return
		}
		der, err := est.DecodeBase64Robust(raw)
		if err != nil {
			stdhttp.Error(w, err.Error(), stdhttp.StatusBadRequest)
			return
		}
		csr, err := x509.ParseCertificateRequest(der)
		if err != nil {
			stdhttp.Error(w, err.Error(), stdhttp.StatusBadRequest)
			return
		}
		leaf := &x509.Certificate{
			SerialNumber: big.NewInt(time.Now().UnixNano()),
			Subject:      csr.Subject,
			DNSNames:     csr.DNSNames,
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(24 * time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, csr.PublicKey, caKey)
		if err != nil {
			stdhttp.Error(w, err.Error(), stdhttp.StatusInternalServerError)
			return
		}
		issued, err := x509.ParseCertificate(leafDER)
		if err != nil {
			stdhttp.Error(w, err.Error(), stdhttp.StatusInternalServerError)
			return
		}
		body, err := est.EncodeCertsResponse([]*x509.Certificate{issued})
		if err != nil {
			stdhttp.Error(w, err.Error(), stdhttp.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", est.ContentTypePKCS7)
		_, _ = w.Write(body)
	}
	mux.HandleFunc("/.well-known/est/simpleenroll", enroll)
	mux.HandleFunc("/.well-known/est/simplereenroll", enroll)

	// The server presents a certificate signed by the CA it publishes,
	// so the enrolment leg — which verifies against the bootstrapped
	// roots, unlike the bootstrap leg — trusts it.
	srvKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	srvTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "est.local"},
		DNSNames:     []string{"est.local", "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	srvDER, err := x509.CreateCertificate(rand.Reader, srvTmpl, ca, &srvKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewUnstartedServer(mux)
	ts.TLS = &tls.Config{ //nolint:gosec // the floor is set by the code under test, not here
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{srvDER, caDER},
			PrivateKey:  srvKey,
		}},
	}
	ts.StartTLS()
	t.Cleanup(ts.Close)
	return strings.TrimPrefix(ts.URL, "https://")
}

// BCP-003-01 enrolment end to end: the Registry bootstraps the CA from
// the EST server, enrols its own certificate, and serves HTTPS on it.
func TestServeEnrolsViaEST(t *testing.T) {
	useRegistryResponder(t, &scriptedRegistryResponder{})
	host := estServer(t)

	_, addr, _ := serveOn(t, registryslot.ServeOptions{
		AdvertiseHost: "127.0.0.1:8235",
		DiscoveryMode: "static",
		APIVer:        "v1.3",
		ESTHost:       host,
		TLSDataDir:    t.TempDir(),
	})
	client := &stdhttp.Client{Transport: &stdhttp.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // the enrolled chain is not the point here
	}}
	waitForFace(t, client, "https://"+addr+"/x-nmos")
}

// An mDNS-mode Registry announces both faces and says so, and a
// subscriber can upgrade a socket on the served face — the dispatcher
// path that sits outside the route table.
func TestServeAnnouncesAndUpgradesOverPlainHTTP(t *testing.T) {
	fake := &scriptedRegistryResponder{}
	useRegistryResponder(t, fake)

	_, addr, _ := serveOn(t, registryslot.ServeOptions{
		AdvertiseHost: "127.0.0.1:8235",
		DiscoveryMode: "mdns",
		APIVer:        "v1.3",
	})
	client := &stdhttp.Client{}
	waitForFace(t, client, "http://"+addr+"/x-nmos")

	deadline := time.Now().Add(10 * time.Second)
	for {
		if announced, _ := fake.snapshot(); len(announced) >= 2 {
			break
		}
		if !time.Now().Before(deadline) {
			t.Fatal("both faces must be announced")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// A real subscription, upgraded through the dispatcher.
	peer, _ := openSubscription(t, addr, SubscriptionRequest{ResourcePath: "/nodes"})
	if op, _, arrived := peer.tryFrame(t, 2*time.Second); arrived && op != 0x1 && op != 0x9 {
		t.Errorf("the upgraded socket sent opcode 0x%x", op)
	}
}

// A TLS Registry advertised under a NAME puts that name in the
// certificate's identity list — an IP is not a DNS SAN under
// BCP-003-01, a name is.
func TestServeTLSWithANamedAdvertiseHost(t *testing.T) {
	useRegistryResponder(t, &scriptedRegistryResponder{})
	certPath, keyPath := selfSignedPair(t)

	_, addr, _ := serveOn(t, registryslot.ServeOptions{
		AdvertiseHost: "registry.local:8235",
		DiscoveryMode: "static",
		APIVer:        "v1.3",
		TLSCertFile:   certPath,
		TLSKeyFile:    keyPath,
		TLSDataDir:    t.TempDir(),
	})
	client := &stdhttp.Client{Transport: &stdhttp.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // a self-signed test cert
	}}
	waitForFace(t, client, "https://"+addr+"/x-nmos")
}
