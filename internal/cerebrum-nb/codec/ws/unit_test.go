package ws

import (
	"bufio"
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
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// selfSignedCert builds a throwaway TLS cert for the in-test wss:// server.
func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// errWriter fails every Write — drives the write-error arms of
// upgradeRequest / writeFrame.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("write boom") }

func TestUpgradeRequest_WriteError(t *testing.T) {
	u, _ := url.Parse("ws://host:40007/")
	_, err := upgradeRequest(errWriter{}, u, nil)
	if err == nil || !strings.Contains(err.Error(), "write boom") {
		t.Fatalf("want write boom, got %v", err)
	}
}

func TestUpgradeRequest_RandError(t *testing.T) {
	orig := randRead
	randRead = func([]byte) (int, error) { return 0, errors.New("rand fail") }
	defer func() { randRead = orig }()
	u, _ := url.Parse("ws://host/")
	_, err := upgradeRequest(errWriter{}, u, nil)
	if err == nil || !strings.Contains(err.Error(), "rand fail") {
		t.Fatalf("want rand fail, got %v", err)
	}
}

func TestUpgradeRequest_StarResource(t *testing.T) {
	// A URL whose RequestURI is empty must map to "/" — exercise the
	// resource-defaulting branch.
	u := &url.URL{Scheme: "ws", Host: "h:1", Path: ""}
	var w writeCollector
	if _, err := upgradeRequest(&w, u, nil); err != nil {
		t.Fatalf("upgradeRequest: %v", err)
	}
	if !strings.HasPrefix(string(w.buf), "GET / HTTP/1.1\r\n") {
		t.Fatalf("resource not defaulted to /: %q", string(w.buf[:20]))
	}
}

func TestWriteFrame_HeaderWriteError(t *testing.T) {
	err := writeFrame(errWriter{}, true, OpText, []byte("hi"), [4]byte{}, false)
	if err == nil || !strings.Contains(err.Error(), "write boom") {
		t.Fatalf("want header write error, got %v", err)
	}
}

// partialWriter writes the header successfully then fails on the body,
// driving the payload-write error arm (both masked and unmasked).
type partialWriter struct{ wrote bool }

func (p *partialWriter) Write(b []byte) (int, error) {
	if !p.wrote {
		p.wrote = true
		return len(b), nil
	}
	return 0, errors.New("body boom")
}

func TestWriteFrame_BodyWriteError_Unmasked(t *testing.T) {
	err := writeFrame(&partialWriter{}, true, OpText, []byte("payload"), [4]byte{}, false)
	if err == nil || !strings.Contains(err.Error(), "body boom") {
		t.Fatalf("want unmasked body write error, got %v", err)
	}
}

func TestWriteFrame_BodyWriteError_Masked(t *testing.T) {
	err := writeFrame(&partialWriter{}, true, OpText, []byte("payload"), [4]byte{1, 2, 3, 4}, true)
	if err == nil || !strings.Contains(err.Error(), "body boom") {
		t.Fatalf("want masked body write error, got %v", err)
	}
}

func TestHeaderHasToken(t *testing.T) {
	if !headerHasToken("keep-alive, Upgrade", "upgrade") {
		t.Fatal("token not found case-insensitively")
	}
	if headerHasToken("close", "upgrade") {
		t.Fatal("false positive token match")
	}
	if headerHasToken("", "upgrade") {
		t.Fatal("empty header matched")
	}
}

func TestIsControl(t *testing.T) {
	for _, op := range []byte{OpClose, OpPing, OpPong} {
		if !IsControl(op) {
			t.Fatalf("%#x should be control", op)
		}
	}
	for _, op := range []byte{OpText, OpBinary, OpContinuation} {
		if IsControl(op) {
			t.Fatalf("%#x should not be control", op)
		}
	}
}

// fakeAddr / closedConn drive Conn.Close's underlying-close error arm.
type closedConn struct{ net.Conn }

func (closedConn) Write(b []byte) (int, error) { return len(b), nil }
func (closedConn) Close() error                { return errors.New("close boom") }
func (closedConn) LocalAddr() net.Addr         { return nil }
func (closedConn) RemoteAddr() net.Addr        { return nil }
func (closedConn) SetWriteDeadline(time.Time) error { return nil }
func (closedConn) SetReadDeadline(time.Time) error  { return nil }
func (closedConn) SetDeadline(time.Time) error      { return nil }

func TestClose_UnderlyingCloseError(t *testing.T) {
	c := newConn(closedConn{}, nil, 0)
	err := c.Close(1000, "x")
	if err == nil || !strings.Contains(err.Error(), "close boom") {
		t.Fatalf("want underlying close error, got %v", err)
	}
}

// ctrlWriteErrConn lets the close control-frame write fail so Close's
// writeControl-error arm runs.
type ctrlWriteErrConn struct{ closedConn }

func (ctrlWriteErrConn) Write([]byte) (int, error) { return 0, errors.New("ctrl write boom") }

func TestClose_ControlWriteError(t *testing.T) {
	c := newConn(ctrlWriteErrConn{}, nil, 0)
	err := c.Close(1000, "x")
	if err == nil || !strings.Contains(err.Error(), "ctrl write boom") {
		t.Fatalf("want control write error, got %v", err)
	}
}

// ----------------------------------------------------------------------
// TLS dial: real TLS listener exercises the wss:// success path + the
// TLSConfig ServerName-clone branch.
// ----------------------------------------------------------------------

func newSelfSignedTLSServer(t *testing.T) (addr string, stop func()) {
	t.Helper()
	cert := selfSignedCert(t)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatalf("tls listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				br := bufio.NewReader(c)
				req, err := http.ReadRequest(br)
				if err != nil {
					_ = c.Close()
					return
				}
				accept := computeAccept(req.Header.Get("Sec-WebSocket-Key"))
				_, _ = c.Write([]byte("HTTP/1.1 101 Switching Protocols\r\n" +
					"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
					"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"))
				time.Sleep(50 * time.Millisecond)
				_ = c.Close()
			}(conn)
		}
	}()
	return ln.Addr().String(), func() { _ = ln.Close() }
}

func TestDial_WSS_Success_ServerNameClone(t *testing.T) {
	addr, stop := newSelfSignedTLSServer(t)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// Provide a TLSConfig WITHOUT ServerName so Dial takes the clone branch,
	// and InsecureSkipVerify so the self-signed cert is accepted.
	conn, err := Dial(ctx, "wss://"+addr+"/", &DialOptions{
		TLSConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	})
	if err != nil {
		t.Fatalf("wss Dial: %v", err)
	}
	_ = conn.Close(1000, "")
}

func TestDial_WSS_NilTLSConfig(t *testing.T) {
	// nil TLSConfig drives the `cfg == nil` branch in Dial (default verify).
	// The self-signed cert is rejected, but only AFTER the nil-config branch
	// runs — which is what we want to cover.
	addr, stop := newSelfSignedTLSServer(t)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := Dial(ctx, "wss://"+addr+"/", &DialOptions{TLSConfig: nil})
	if err == nil || !strings.Contains(err.Error(), "tls handshake") {
		t.Fatalf("want tls handshake verify failure, got %v", err)
	}
}

// hangupConn accepts then closes the TCP socket immediately so the client's
// upgrade-request write fails — driving Dial's "write upgrade" error wrap.
func TestDial_WriteUpgradeError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.(*net.TCPConn).SetLinger(0) // RST on close
			_ = c.Close()
		}
	}()
	// Retry a few times: the write may occasionally land in the socket
	// buffer before the RST is observed. We only assert that when Dial
	// fails it is on the write-upgrade path (not handshake/parse).
	var lastErr error
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		_, lastErr = Dial(ctx, "ws://"+ln.Addr().String()+"/", nil)
		cancel()
		if lastErr != nil && strings.Contains(lastErr.Error(), "write upgrade") {
			return
		}
	}
	t.Skipf("could not provoke a write-upgrade error on this host (last err %v)", lastErr)
}

func TestUpgradeRequest_StarResourceURI(t *testing.T) {
	u := &url.URL{Scheme: "ws", Host: "h:1", Opaque: "*"}
	var w writeCollector
	if _, err := upgradeRequest(&w, u, nil); err != nil {
		t.Fatalf("upgradeRequest: %v", err)
	}
	if !strings.HasPrefix(string(w.buf), "GET / HTTP/1.1\r\n") {
		t.Fatalf("'*' resource not defaulted to /: %q", string(w.buf[:20]))
	}
}

func TestReadFrame_NegativeLengthGuard(t *testing.T) {
	orig := plenSeam
	plenSeam = func(int64) int64 { return -1 }
	defer func() { plenSeam = orig }()
	r := bufioReader([]byte{0x81, 1, 'x'})
	if _, err := readFrame(r, 0); err == nil ||
		!strings.Contains(err.Error(), "negative payload length") {
		t.Fatalf("want negative-length guard, got %v", err)
	}
}

// TestReadMessage_PongWriteError drives the OpPing -> writeControl(OpPong)
// error arm in ReadMessage: the server sends a Ping, and the auto-Pong
// write fails because the mask-key seam is forced to error.
func TestReadMessage_PongWriteError(t *testing.T) {
	s := newFakeServer(t)
	ready := make(chan struct{})
	s.onConn = func(conn net.Conn) {
		<-ready
		_ = writeFrame(conn, true, OpPing, []byte("hb"), [4]byte{}, false)
		time.Sleep(100 * time.Millisecond)
		_ = conn.Close()
	}
	conn := dialEcho(t, s)
	orig := randRead
	defer func() { randRead = orig; _ = conn.c.Close() }()
	randRead = func([]byte) (int, error) { return 0, errors.New("pong rand boom") }
	close(ready)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := conn.ReadMessage(ctx)
	if err == nil || !strings.Contains(err.Error(), "pong rand boom") {
		t.Fatalf("want pong write error, got %v", err)
	}
}
