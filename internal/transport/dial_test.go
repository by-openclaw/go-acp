package transport

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"testing"
	"time"
)

// The seam is only worth having if the stdlib type satisfies it with no
// adapter — that is what makes injecting it a no-op for existing callers.
func TestNetDialerSatisfiesDialer(t *testing.T) {
	var _ Dialer = &net.Dialer{}
	var _ Dialer = TCPDialer{}
}

func TestTCPDialerDialsAndAppliesOptions(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	d := TCPDialer{
		Timeout: 5 * time.Second,
		Options: SocketOptions{KeepalivePeriod: 5 * time.Second, NoDelay: true},
	}
	conn, err := d.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, ok := conn.(*net.TCPConn); !ok {
		t.Fatalf("DialContext returned %T, want *net.TCPConn", conn)
	}
}

func TestTCPDialerReportsDialFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // nothing is listening now

	_, err = TCPDialer{}.DialContext(context.Background(), "tcp", addr)
	if err == nil {
		t.Fatal("DialContext to a closed port returned nil")
	}
}

// A failed setsockopt must not fail the dial, mirroring Accept.
func TestTCPDialerSurvivesOptionFailure(t *testing.T) {
	orig := applySocketOptions
	defer func() { applySocketOptions = orig }()
	applySocketOptions = func(net.Conn, SocketOptions) error {
		return errors.New("boom")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	conn, err := TCPDialer{}.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("DialContext failed after an option failure: %v", err)
	}
	_ = conn.Close()
}

// --- TLSDialer ---

// loopbackTLSCert makes an ECDSA server certificate with a 127.0.0.1 IP SAN
// and a pool trusting it, so a TLSDialer can verify the handshake against the
// dialled host without InsecureSkipVerify. writeKeyPair (tls_test.go) has no
// SAN, so it cannot serve this role.
func loopbackTLSCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "dhs-loopback"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}

// serveTLSOnce binds a TLS loopback listener, accepts one connection and
// discards it, and returns the listener address. The listener closes on
// cleanup.
func serveTLSOnce(t *testing.T, cert tls.Certificate) string {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatalf("tls.Listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			go func() {
				// Force the handshake, then drain until the peer leaves.
				buf := make([]byte, 64)
				for {
					if _, rerr := c.Read(buf); rerr != nil {
						_ = c.Close()
						return
					}
				}
			}()
		}
	}()
	return ln.Addr().String()
}

// The happy path: a verified TLS handshake with ServerName filled from the
// dialled host (empty ServerName, verification on), returning a *tls.Conn.
func TestTLSDialerHandshakeVerifiedFillsServerName(t *testing.T) {
	cert, pool := loopbackTLSCert(t)
	addr := serveTLSOnce(t, cert)

	d := TLSDialer{TLS: TLSOptions{Enable: true, RootCAs: pool}} // ServerName empty
	conn, err := d.DialContext(context.Background(), "tcp", addr)
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer func() { _ = conn.Close() }()
	tc, ok := conn.(*tls.Conn)
	if !ok {
		t.Fatalf("DialContext returned %T, want *tls.Conn", conn)
	}
	// The handshake verified against the cert's 127.0.0.1 IP SAN with
	// verification ON and no ServerName supplied — which only succeeds
	// because the dialer filled ServerName from the host. Had it stayed
	// empty, tls would refuse with "either ServerName or InsecureSkipVerify
	// must be specified". (Go omits the SNI extension for IP literals, so
	// ConnectionState().ServerName is empty here; the VerifiedChains prove
	// the fill instead.)
	if len(tc.ConnectionState().VerifiedChains) == 0 {
		t.Error("handshake did not verify the server certificate")
	}
}

// Enable=false makes the TLSDialer plaintext: it returns the base connection
// untouched, not a *tls.Conn.
func TestTLSDialerDisabledIsPlaintext(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	conn, err := TLSDialer{TLS: TLSOptions{}}.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, ok := conn.(*tls.Conn); ok {
		t.Error("a disabled TLSDialer must not return a *tls.Conn")
	}
}

// A base-dial failure propagates unchanged (nothing to hand back).
func TestTLSDialerBaseDialError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // nothing listening

	if _, err := (TLSDialer{TLS: TLSOptions{Enable: true, Insecure: true}}).
		DialContext(context.Background(), "tcp", addr); err == nil {
		t.Fatal("DialContext to a closed port returned nil")
	}
}

// A bad TLS posture (CA file that does not exist) makes Client() fail AFTER
// the base connection is up; the dialer surfaces the error and closes the raw
// connection rather than leaking it.
func TestTLSDialerConfigError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	d := TLSDialer{TLS: TLSOptions{Enable: true, CAFile: "/no/such/ca.pem"}}
	if _, err := d.DialContext(context.Background(), "tcp", ln.Addr().String()); err == nil {
		t.Fatal("a bad CA file must fail the dial")
	}
}

// A handshake failure (the server speaks plaintext) is returned and the raw
// connection closed.
func TestTLSDialerHandshakeFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0") // plain TCP, no TLS
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		c, aerr := ln.Accept()
		if aerr == nil {
			_ = c.Close() // hang up; no TLS server here
		}
	}()

	d := TLSDialer{TLS: TLSOptions{Enable: true, Insecure: true}}
	if _, err := d.DialContext(context.Background(), "tcp", ln.Addr().String()); err == nil {
		t.Fatal("handshake against a plaintext server must fail")
	}
}
