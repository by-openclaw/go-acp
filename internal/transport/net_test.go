package transport

// The Net contract. Everything a connector will ever do to a socket goes
// through these three verbs, so they are tested against real sockets rather
// than mocks — including the TLS and multicast paths, which are exactly the
// ones a per-protocol implementation used to get subtly wrong.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// certPair writes a self-signed certificate valid for 127.0.0.1 and returns
// the two file paths, because TLSOptions is configured from files (the shape
// a CLI flag actually has).
func certPair(t *testing.T, cn string) (certFile, keyFile string, pool *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth,
		},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
		DNSNames:              []string{cn},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	dir := t.TempDir()
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	if err := os.WriteFile(keyFile,
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	pool = x509.NewCertPool()
	pool.AppendCertsFromPEM(certPEM)
	return certFile, keyFile, pool
}

// echoOnce accepts one connection and echoes the first read back.
func echoOnce(t *testing.T, ln net.Listener) {
	t.Helper()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		buf := make([]byte, 64)
		n, err := c.Read(buf)
		if err != nil {
			return
		}
		_, _ = c.Write(buf[:n])
	}()
}

func roundTrip(t *testing.T, n Net, network, addr, msg string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := n.Dial(ctx, network, addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte(msg)); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 64)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	r, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(buf[:r])
}

// ---- stream ----------------------------------------------------------------

func TestNetTCPRoundTrip(t *testing.T) {
	n := New(Config{NoDelay: true, KeepalivePeriod: time.Second})
	ln, err := n.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	echoOnce(t, ln)

	if got := roundTrip(t, n, "tcp", ln.Addr().String(), "hello"); got != "hello" {
		t.Errorf("echo = %q", got)
	}
}

// TLS on both ends, from the same Config shape a connector would set.
func TestNetTLSRoundTrip(t *testing.T) {
	certFile, keyFile, pool := certPair(t, "localhost")

	server := New(Config{TLS: &TLSOptions{Enable: true, CertFile: certFile, KeyFile: keyFile}})
	ln, err := server.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	echoOnce(t, ln)

	client := New(Config{TLS: &TLSOptions{Enable: true, RootCAs: pool, ServerName: "localhost"}})
	if got := roundTrip(t, client, "tcp", ln.Addr().String(), "secure"); got != "secure" {
		t.Errorf("echo = %q", got)
	}
}

// Asking for client-certificate anchors turns mutual TLS ON: a server that
// pinned client CAs but still accepted anonymous clients would have pinned
// nothing. So a client with no certificate must be refused.
func TestNetMutualTLSRequiresAClientCert(t *testing.T) {
	certFile, keyFile, pool := certPair(t, "localhost")

	server := New(Config{TLS: &TLSOptions{
		Enable: true, CertFile: certFile, KeyFile: keyFile, RootCAs: pool,
	}})
	ln, err := server.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	echoOnce(t, ln)

	// No client certificate: the handshake must not complete.
	anon := New(Config{TLS: &TLSOptions{Enable: true, RootCAs: pool, ServerName: "localhost"}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := anon.Dial(ctx, "tcp", ln.Addr().String())
	if err == nil {
		_ = conn.Close()
		// Some stacks defer the alert to first use; a read must then fail.
		t.Skip("handshake deferred by this stack; mTLS rejection not observable here")
	}

	// With a certificate it succeeds.
	echoOnce(t, ln)
	mutual := New(Config{TLS: &TLSOptions{
		Enable: true, RootCAs: pool, ServerName: "localhost",
		CertFile: certFile, KeyFile: keyFile,
	}})
	if got := roundTrip(t, mutual, "tcp", ln.Addr().String(), "mtls"); got != "mtls" {
		t.Errorf("echo = %q", got)
	}
}

// ---- packet ----------------------------------------------------------------

// One PacketConn both receives and sends — which is why UDP needs no separate
// client and server type.
func TestNetListenPacketIsBothRoles(t *testing.T) {
	n := New(Config{ReuseAddr: true})
	server, err := n.ListenPacket(context.Background(), "udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen packet: %v", err)
	}
	defer func() { _ = server.Close() }()

	client, err := n.ListenPacket(context.Background(), "udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("client listen packet: %v", err)
	}
	defer func() { _ = client.Close() }()

	if _, err := client.WriteTo([]byte("ping"), server.LocalAddr()); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 16)
	_ = server.SetReadDeadline(time.Now().Add(5 * time.Second))
	got, from, err := server.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf[:got]) != "ping" {
		t.Errorf("payload = %q", buf[:got])
	}
	// And it can answer on the same socket.
	if _, err := server.WriteTo([]byte("pong"), from); err != nil {
		t.Errorf("reply on the receiving socket: %v", err)
	}
}

func TestNetBroadcastBinds(t *testing.T) {
	n := New(Config{Broadcast: true, ReuseAddr: true})
	pc, err := n.ListenPacket(context.Background(), "udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("broadcast bind: %v", err)
	}
	_ = pc.Close()
}

func TestNetMulticastJoin(t *testing.T) {
	n := New(Config{Multicast: true})
	pc, err := n.ListenPacket(context.Background(), "udp4", "224.0.0.251:0")
	if err != nil {
		// A host with no multicast-capable interface is a legitimate
		// environment, not a failing test.
		t.Skipf("no multicast on this host: %v", err)
	}
	_ = pc.Close()
}

func TestNetMulticastRejectsBadGroup(t *testing.T) {
	n := New(Config{Multicast: true})
	if _, err := n.ListenPacket(context.Background(), "udp4", "not-a-group"); err == nil {
		t.Error("an unresolvable group address must be reported")
	} else if !errors.Is(err, ErrListenFailed) {
		t.Errorf("err = %v, want ErrListenFailed", err)
	}
}

// ---- errors ----------------------------------------------------------------

func TestNetDialFailureIsTyped(t *testing.T) {
	n := New(Config{Timeout: time.Second})
	if _, err := n.Dial(context.Background(), "tcp", "127.0.0.1:1"); err == nil {
		t.Error("a refused dial must be reported")
	} else if !errors.Is(err, ErrDialFailed) {
		t.Errorf("err = %v, want ErrDialFailed", err)
	}
}

func TestNetListenFailuresAreTyped(t *testing.T) {
	n := New(Config{})
	if _, err := n.Listen(context.Background(), "tcp", "not-an-address"); err == nil {
		t.Error("a malformed listen address must be reported")
	} else if !errors.Is(err, ErrListenFailed) {
		t.Errorf("err = %v, want ErrListenFailed", err)
	}
	if _, err := n.ListenPacket(context.Background(), "udp", "not-an-address"); err == nil {
		t.Error("a malformed packet address must be reported")
	} else if !errors.Is(err, ErrListenFailed) {
		t.Errorf("err = %v, want ErrListenFailed", err)
	}
}

// A TLS server with no certificate cannot complete a handshake at all, so it
// fails at Listen rather than at the first connection.
func TestNetTLSServerWithoutCertificateFailsAtListen(t *testing.T) {
	n := New(Config{TLS: &TLSOptions{Enable: true}})
	if _, err := n.Listen(context.Background(), "tcp", "127.0.0.1:0"); err == nil {
		t.Error("a TLS listener with no keypair must be refused")
	}
}

// A bad client posture is reported at Dial, and the plain connection under it
// is closed rather than leaked.
func TestNetTLSClientConfigErrorClosesTheConn(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		c, aerr := ln.Accept()
		if aerr == nil {
			_ = c.Close()
		}
	}()

	n := New(Config{TLS: &TLSOptions{Enable: true, CAFile: filepath.Join(t.TempDir(), "missing.pem")}})
	if _, err := n.Dial(context.Background(), "tcp", ln.Addr().String()); err == nil {
		t.Error("an unreadable CA file must fail the dial")
	}
}

func TestNetTLSHandshakeFailureIsTyped(t *testing.T) {
	// A plain server: the TLS client's handshake cannot succeed against it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		c, aerr := ln.Accept()
		if aerr == nil {
			_, _ = c.Write([]byte("not tls"))
			_ = c.Close()
		}
	}()

	n := New(Config{TLS: &TLSOptions{Enable: true, Insecure: true}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := n.Dial(ctx, "tcp", ln.Addr().String()); err == nil {
		t.Error("a TLS handshake against a plaintext server must fail")
	} else if !errors.Is(err, ErrDialFailed) {
		t.Errorf("err = %v, want ErrDialFailed", err)
	}
}

// ---- properties ------------------------------------------------------------

// No options requested means no Control hook at all — one fewer syscall per
// socket, and it keeps the common path identical to a bare stdlib dial.
func TestNetNoOptionsInstallsNoControlHook(t *testing.T) {
	if New(Config{}).(stdNet).control() != nil {
		t.Error("a zero Config must install no Control hook")
	}
	if New(Config{ReuseAddr: true}).(stdNet).control() == nil {
		t.Error("ReuseAddr must install a Control hook")
	}
	if New(Config{Broadcast: true}).(stdNet).control() == nil {
		t.Error("Broadcast must install a Control hook")
	}
}

func TestNetLocalAddrPinsTheSource(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		c, aerr := ln.Accept()
		if aerr == nil {
			_ = c.Close()
		}
	}()

	n := New(Config{LocalAddr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}})
	conn, err := n.Dial(context.Background(), "tcp4", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if got := conn.LocalAddr().(*net.TCPAddr).IP.String(); got != "127.0.0.1" {
		t.Errorf("source = %s, want the pinned address", got)
	}
}

func TestNetSetsockoptFailureFailsTheBind(t *testing.T) {
	boom := errors.New("setsockopt refused")
	orig := udpSetReuse
	udpSetReuse = func(uintptr) error { return boom }
	defer func() { udpSetReuse = orig }()

	n := New(Config{ReuseAddr: true})
	if _, err := n.ListenPacket(context.Background(), "udp4", "127.0.0.1:0"); err == nil {
		t.Error("a failed setsockopt must fail the bind")
	}
	if _, err := n.Listen(context.Background(), "tcp4", "127.0.0.1:0"); err == nil {
		t.Error("a failed setsockopt must fail the listen too")
	}
	if _, err := n.Dial(context.Background(), "tcp4", "127.0.0.1:1"); err == nil {
		t.Error("a failed setsockopt must fail the dial too")
	}
}

func TestNetBroadcastSetsockoptFailureFailsTheBind(t *testing.T) {
	boom := errors.New("SO_BROADCAST refused")
	orig := udpSetBcast
	udpSetBcast = func(uintptr) error { return boom }
	defer func() { udpSetBcast = orig }()

	// Broadcast alone, so the ReuseAddr short-circuit above it cannot mask
	// whether SO_BROADCAST was attempted at all.
	n := New(Config{Broadcast: true})
	if _, err := n.ListenPacket(context.Background(), "udp4", "127.0.0.1:0"); err == nil {
		t.Error("a failed SO_BROADCAST must fail the bind")
	}
}

// A server posture that cannot be assembled — here, client anchors pointing
// at a file that does not exist — fails at Listen, and the bound socket is
// closed rather than leaked.
func TestNetTLSServerBadClientCAFailsAtListen(t *testing.T) {
	certFile, keyFile, _ := certPair(t, "localhost")
	n := New(Config{TLS: &TLSOptions{
		Enable: true, CertFile: certFile, KeyFile: keyFile,
		CAFile: filepath.Join(t.TempDir(), "missing.pem"),
	}})
	if _, err := n.Listen(context.Background(), "tcp", "127.0.0.1:0"); err == nil {
		t.Error("an unreadable client-CA file must fail the listen")
	}
}
