package ws

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// fakeServer is an in-process RFC 6455 server side built directly on a
// net.Listener. It is intentionally minimal — just enough to drive the
// stdlib-only client in this package end to end (handshake + framed
// read/write) without a third-party WS library or a live Cerebrum peer.
type fakeServer struct {
	ln net.Listener

	// handshake controls how the server answers the upgrade. When nil it
	// performs a correct RFC 6455 §4.2 accept; tests substitute it to
	// drive the client's rejection paths.
	handshake func(req *http.Request, br *bufio.Reader, conn net.Conn) (proceed bool)

	// onConn, when non-nil, runs after a successful handshake with the
	// post-handshake net.Conn so a test can push server frames.
	onConn func(conn net.Conn)
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeServer{ln: ln}
	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *fakeServer) url() string {
	return "ws://" + s.ln.Addr().String() + "/"
}

func (s *fakeServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *fakeServer) handle(conn net.Conn) {
	br := bufio.NewReader(conn)
	req, err := http.ReadRequest(br)
	if err != nil {
		_ = conn.Close()
		return
	}
	if s.handshake != nil {
		if !s.handshake(req, br, conn) {
			_ = conn.Close()
			return
		}
	} else {
		key := req.Header.Get("Sec-WebSocket-Key")
		accept := computeAccept(key)
		resp := "HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
		if _, err := io.WriteString(conn, resp); err != nil {
			_ = conn.Close()
			return
		}
	}
	if s.onConn != nil {
		s.onConn(conn)
		return
	}
	_ = conn.Close()
}

// writeServerText sends an unmasked server->client text frame.
func writeServerText(conn net.Conn, payload []byte) error {
	return writeFrame(conn, true, OpText, payload, [4]byte{}, false)
}

// ----------------------------------------------------------------------
// Dial — success + every rejection path
// ----------------------------------------------------------------------

func TestDial_Success_AndTextRoundTrip(t *testing.T) {
	s := newFakeServer(t)
	got := make(chan []byte, 1)
	s.onConn = func(conn net.Conn) {
		br := bufio.NewReader(conn)
		// Read the client's (masked) text frame and echo its payload back.
		f, err := readFrame(br, 0)
		if err != nil {
			return
		}
		_ = writeServerText(conn, f.payload)
		got <- f.payload
		// Hold open briefly so the client can read the echo.
		time.Sleep(50 * time.Millisecond)
		_ = conn.Close()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := Dial(ctx, s.url(), nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = conn.Close(1000, "bye") }()

	if conn.LocalAddr() == nil || conn.RemoteAddr() == nil {
		t.Fatal("Local/RemoteAddr returned nil")
	}
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	if err := conn.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetWriteDeadline: %v", err)
	}

	if err := conn.WriteText(ctx, []byte("ping-xml")); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	op, payload, err := conn.ReadMessage(ctx)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if op != OpText || string(payload) != "ping-xml" {
		t.Fatalf("echo: op=%#x payload=%q", op, payload)
	}
	if g := <-got; string(g) != "ping-xml" {
		t.Fatalf("server saw %q", g)
	}
}

func TestDial_DefaultPortInjected(t *testing.T) {
	// A ws:// URL without an explicit port must have :80 injected. We can't
	// reach a real :80, so we only assert the dial fails on connection
	// rather than on scheme/parse — proving the port-injection branch ran.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err := Dial(ctx, "ws://127.0.0.1/", &DialOptions{
		Dialer: &net.Dialer{Timeout: 200 * time.Millisecond},
	})
	if err == nil {
		t.Fatal("expected dial failure to :80")
	}
	// Prove the port-injection branch ran. We can't assert the *kind* of
	// failure, and it's environment-dependent:
	//   - :80 closed        → "tcp dial" connection-refused naming :80
	//   - :80 held, no WS    → WS upgrade fails (the TCP connect to the
	//                          injected :80 succeeded, then upgrade failed —
	//                          e.g. CI runners / Windows http.sys answer :80
	//                          with a plain HTTP response, "upgrade failed").
	// Either outcome proves the default port was injected and dialed, so
	// accept both rather than assuming :80 refuses connections.
	if !strings.Contains(err.Error(), ":80") && !strings.Contains(err.Error(), "upgrade failed") {
		t.Fatalf("want error proving :80 injection (named :80 or WS upgrade), got %v", err)
	}
}

func TestDial_WSSDefaultPortInjected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err := Dial(ctx, "wss://127.0.0.1/", &DialOptions{
		Dialer: &net.Dialer{Timeout: 200 * time.Millisecond},
	})
	if err == nil {
		t.Fatal("expected dial failure to :443")
	}
	// As above: assert the injected :443 is named, not the failure kind
	// (refused vs handshake/TLS timeout varies by host).
	if !strings.Contains(err.Error(), ":443") {
		t.Fatalf("want error naming injected :443, got %v", err)
	}
}

func TestDial_BadURL(t *testing.T) {
	_, err := Dial(context.Background(), "://bad-url", nil)
	if err == nil || !strings.Contains(err.Error(), "parse url") {
		t.Fatalf("want parse url error, got %v", err)
	}
}

func TestDial_UnsupportedScheme(t *testing.T) {
	_, err := Dial(context.Background(), "http://host/", nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported scheme") {
		t.Fatalf("want unsupported scheme error, got %v", err)
	}
}

func TestDial_TCPRefused(t *testing.T) {
	// Listen then immediately close to get a guaranteed-dead port.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	_ = ln.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := Dial(ctx, "ws://"+addr+"/", nil)
	if err == nil || !strings.Contains(err.Error(), "tcp dial") {
		t.Fatalf("want tcp dial error, got %v", err)
	}
}

func TestDial_TLSHandshakeFails(t *testing.T) {
	// A plain-TCP server speaking no TLS: the wss:// client's TLS
	// handshake must fail.
	s := newFakeServer(t)
	addr := s.ln.Addr().String()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := Dial(ctx, "wss://"+addr+"/", &DialOptions{
		TLSConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	})
	if err == nil || !strings.Contains(err.Error(), "tls handshake") {
		t.Fatalf("want tls handshake error, got %v", err)
	}
}

func TestDial_UpgradeRejected_Non101(t *testing.T) {
	s := newFakeServer(t)
	s.handshake = func(_ *http.Request, _ *bufio.Reader, conn net.Conn) bool {
		_, _ = io.WriteString(conn, "HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n")
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := Dial(ctx, s.url(), nil)
	if err == nil || !strings.Contains(err.Error(), "upgrade failed") {
		t.Fatalf("want upgrade failed error, got %v", err)
	}
}

func TestDial_UpgradeMissingUpgradeHeader(t *testing.T) {
	s := newFakeServer(t)
	s.handshake = func(req *http.Request, _ *bufio.Reader, conn net.Conn) bool {
		accept := computeAccept(req.Header.Get("Sec-WebSocket-Key"))
		_, _ = io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Accept: "+accept+"\r\n\r\n")
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := Dial(ctx, s.url(), nil)
	if err == nil || !strings.Contains(err.Error(), "missing Upgrade") {
		t.Fatalf("want missing Upgrade error, got %v", err)
	}
}

func TestDial_UpgradeMissingConnectionHeader(t *testing.T) {
	s := newFakeServer(t)
	s.handshake = func(req *http.Request, _ *bufio.Reader, conn net.Conn) bool {
		accept := computeAccept(req.Header.Get("Sec-WebSocket-Key"))
		_, _ = io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\n"+
			"Upgrade: websocket\r\n"+
			"Sec-WebSocket-Accept: "+accept+"\r\n\r\n")
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := Dial(ctx, s.url(), nil)
	if err == nil || !strings.Contains(err.Error(), "Connection: Upgrade") {
		t.Fatalf("want missing Connection error, got %v", err)
	}
}

func TestDial_UpgradeMissingAccept(t *testing.T) {
	s := newFakeServer(t)
	s.handshake = func(_ *http.Request, _ *bufio.Reader, conn net.Conn) bool {
		_, _ = io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n\r\n")
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := Dial(ctx, s.url(), nil)
	if err == nil || !strings.Contains(err.Error(), "missing Sec-WebSocket-Accept") {
		t.Fatalf("want missing Accept error, got %v", err)
	}
}

func TestDial_UpgradeAcceptMismatch(t *testing.T) {
	s := newFakeServer(t)
	s.handshake = func(_ *http.Request, _ *bufio.Reader, conn net.Conn) bool {
		_, _ = io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Accept: WRONGVALUE=\r\n\r\n")
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := Dial(ctx, s.url(), nil)
	if err == nil || !strings.Contains(err.Error(), "Accept mismatch") {
		t.Fatalf("want accept mismatch error, got %v", err)
	}
}

func TestDial_UpgradeResponseGarbage(t *testing.T) {
	s := newFakeServer(t)
	s.handshake = func(_ *http.Request, _ *bufio.Reader, conn net.Conn) bool {
		_, _ = io.WriteString(conn, "not-an-http-response\r\n")
		_ = conn.Close()
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := Dial(ctx, s.url(), nil)
	if err == nil || !strings.Contains(err.Error(), "read upgrade response") {
		t.Fatalf("want read upgrade response error, got %v", err)
	}
}

func TestDial_ExtraHeadersMerged(t *testing.T) {
	s := newFakeServer(t)
	seen := make(chan string, 1)
	s.handshake = func(req *http.Request, _ *bufio.Reader, conn net.Conn) bool {
		seen <- req.Header.Get("Authorization")
		accept := computeAccept(req.Header.Get("Sec-WebSocket-Key"))
		_, _ = io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Accept: "+accept+"\r\n\r\n")
		return false // proceed=false so handle() doesn't fall through to onConn
	}
	h := http.Header{}
	h.Set("Authorization", "Bearer xyz")
	// Headers we own must be dropped even if the caller sets them.
	h.Set("Upgrade", "nonsense")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _ = Dial(ctx, s.url(), &DialOptions{Header: h})
	if got := <-seen; got != "Bearer xyz" {
		t.Fatalf("Authorization header not merged: %q", got)
	}
}

// ----------------------------------------------------------------------
// ReadMessage control-frame handling + fragmentation
// ----------------------------------------------------------------------

func dialEcho(t *testing.T, s *fakeServer) *Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := Dial(ctx, s.url(), nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	return conn
}

func TestReadMessage_PingTriggersPong_ThenText(t *testing.T) {
	s := newFakeServer(t)
	pongSeen := make(chan struct{}, 1)
	s.onConn = func(conn net.Conn) {
		// Server sends a Ping; client must auto-Pong, then we send text.
		_ = writeFrame(conn, true, OpPing, []byte("hb"), [4]byte{}, false)
		br := bufio.NewReader(conn)
		// Expect the client's masked Pong.
		f, err := readFrame(br, 0)
		if err == nil && f.opcode == OpPong {
			pongSeen <- struct{}{}
		}
		_ = writeServerText(conn, []byte("after-ping"))
		time.Sleep(50 * time.Millisecond)
		_ = conn.Close()
	}
	conn := dialEcho(t, s)
	defer func() { _ = conn.Close(1000, "") }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	op, payload, err := conn.ReadMessage(ctx)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if op != OpText || string(payload) != "after-ping" {
		t.Fatalf("got op=%#x payload=%q", op, payload)
	}
	select {
	case <-pongSeen:
	case <-time.After(time.Second):
		t.Fatal("server never saw the auto-Pong")
	}
}

func TestReadMessage_PongDropped(t *testing.T) {
	s := newFakeServer(t)
	s.onConn = func(conn net.Conn) {
		_ = writeFrame(conn, true, OpPong, []byte("unsolicited"), [4]byte{}, false)
		_ = writeServerText(conn, []byte("real"))
		time.Sleep(50 * time.Millisecond)
		_ = conn.Close()
	}
	conn := dialEcho(t, s)
	defer func() { _ = conn.Close(1000, "") }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, payload, err := conn.ReadMessage(ctx)
	if err != nil || string(payload) != "real" {
		t.Fatalf("Pong not dropped: payload=%q err=%v", payload, err)
	}
}

func TestReadMessage_ServerCloseGivesEOF(t *testing.T) {
	s := newFakeServer(t)
	s.onConn = func(conn net.Conn) {
		body := []byte{0x03, 0xe8} // code 1000
		_ = writeFrame(conn, true, OpClose, body, [4]byte{}, false)
		time.Sleep(50 * time.Millisecond)
		_ = conn.Close()
	}
	conn := dialEcho(t, s)
	defer func() { _ = conn.Close(1000, "") }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := conn.ReadMessage(ctx)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("want io.EOF on close, got %v", err)
	}
}

func TestReadMessage_Fragmented(t *testing.T) {
	s := newFakeServer(t)
	s.onConn = func(conn net.Conn) {
		// Text frame split into two continuation fragments.
		_ = writeFrame(conn, false, OpText, []byte("<root"), [4]byte{}, false)
		_ = writeFrame(conn, true, OpContinuation, []byte("/>"), [4]byte{}, false)
		time.Sleep(50 * time.Millisecond)
		_ = conn.Close()
	}
	conn := dialEcho(t, s)
	defer func() { _ = conn.Close(1000, "") }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	op, payload, err := conn.ReadMessage(ctx)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if op != OpText || string(payload) != "<root/>" {
		t.Fatalf("reassembly: op=%#x payload=%q", op, payload)
	}
}

func TestReadMessage_ContinuationWithoutData(t *testing.T) {
	s := newFakeServer(t)
	s.onConn = func(conn net.Conn) {
		_ = writeFrame(conn, true, OpContinuation, []byte("orphan"), [4]byte{}, false)
		time.Sleep(50 * time.Millisecond)
		_ = conn.Close()
	}
	conn := dialEcho(t, s)
	defer func() { _ = conn.Close(1000, "") }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := conn.ReadMessage(ctx)
	if err == nil || !strings.Contains(err.Error(), "continuation without preceding") {
		t.Fatalf("want orphan-continuation error, got %v", err)
	}
}

func TestReadMessage_NewDataMidFragmentation(t *testing.T) {
	s := newFakeServer(t)
	s.onConn = func(conn net.Conn) {
		_ = writeFrame(conn, false, OpText, []byte("a"), [4]byte{}, false)
		_ = writeFrame(conn, true, OpText, []byte("b"), [4]byte{}, false) // illegal: new data frame
		time.Sleep(50 * time.Millisecond)
		_ = conn.Close()
	}
	conn := dialEcho(t, s)
	defer func() { _ = conn.Close(1000, "") }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := conn.ReadMessage(ctx)
	if err == nil || !strings.Contains(err.Error(), "mid-fragmentation") {
		t.Fatalf("want mid-fragmentation error, got %v", err)
	}
}

func TestReadMessage_UnknownOpcode(t *testing.T) {
	s := newFakeServer(t)
	s.onConn = func(conn net.Conn) {
		// Opcode 0x3 is a reserved non-control data opcode.
		_ = writeFrame(conn, true, 0x3, []byte("x"), [4]byte{}, false)
		time.Sleep(50 * time.Millisecond)
		_ = conn.Close()
	}
	conn := dialEcho(t, s)
	defer func() { _ = conn.Close(1000, "") }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := conn.ReadMessage(ctx)
	if err == nil || !strings.Contains(err.Error(), "unknown opcode") {
		t.Fatalf("want unknown opcode error, got %v", err)
	}
}

func TestReadMessage_ReadError(t *testing.T) {
	s := newFakeServer(t)
	s.onConn = func(conn net.Conn) {
		_ = conn.Close() // hang up immediately after handshake
	}
	conn := dialEcho(t, s)
	defer func() { _ = conn.Close(1000, "") }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := conn.ReadMessage(ctx)
	if err == nil {
		t.Fatal("want read error on closed conn")
	}
}

// ----------------------------------------------------------------------
// Write side: Ping, Close, control-too-large, deadline ctx
// ----------------------------------------------------------------------

func TestPing_And_Close(t *testing.T) {
	s := newFakeServer(t)
	frames := make(chan byte, 4)
	s.onConn = func(conn net.Conn) {
		br := bufio.NewReader(conn)
		for {
			f, err := readFrame(br, 0)
			if err != nil {
				return
			}
			frames <- f.opcode
			if f.opcode == OpClose {
				return
			}
		}
	}
	conn := dialEcho(t, s)
	ctx := context.Background()
	if err := conn.Ping(ctx, []byte("p")); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if err := conn.Close(1000, "client closing"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Second Close is a no-op returning the first error (nil here).
	if err := conn.Close(1001, "again"); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	var sawPing, sawClose bool
	timeout := time.After(time.Second)
	for !sawPing || !sawClose {
		select {
		case op := <-frames:
			switch op {
			case OpPing:
				sawPing = true
			case OpClose:
				sawClose = true
			}
		case <-timeout:
			t.Fatalf("frames seen: ping=%v close=%v", sawPing, sawClose)
		}
	}
}

func TestClose_LongReasonTruncated(t *testing.T) {
	s := newFakeServer(t)
	bodyLen := make(chan int, 1)
	s.onConn = func(conn net.Conn) {
		br := bufio.NewReader(conn)
		f, err := readFrame(br, 0)
		if err == nil {
			bodyLen <- len(f.payload)
		}
	}
	conn := dialEcho(t, s)
	long := strings.Repeat("x", 300)
	if err := conn.Close(1000, long); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case n := <-bodyLen:
		if n != 125 {
			t.Fatalf("close body len = %d, want 125 (truncated)", n)
		}
	case <-time.After(time.Second):
		t.Fatal("server never saw the close frame")
	}
}

func TestWriteControl_TooLarge(t *testing.T) {
	s := newFakeServer(t)
	s.onConn = func(conn net.Conn) { time.Sleep(100 * time.Millisecond); _ = conn.Close() }
	conn := dialEcho(t, s)
	defer func() { _ = conn.Close(1000, "") }()
	err := conn.Ping(context.Background(), make([]byte, 200))
	if err == nil || !strings.Contains(err.Error(), "control frame too large") {
		t.Fatalf("want control-too-large error, got %v", err)
	}
}

func TestWriteText_ContextDeadlineApplied(t *testing.T) {
	s := newFakeServer(t)
	s.onConn = func(conn net.Conn) {
		br := bufio.NewReader(conn)
		_, _ = readFrame(br, 0)
		time.Sleep(50 * time.Millisecond)
		_ = conn.Close()
	}
	conn := dialEcho(t, s)
	defer func() { _ = conn.Close(1000, "") }()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second))
	defer cancel()
	if err := conn.WriteText(ctx, []byte("deadline-set")); err != nil {
		t.Fatalf("WriteText with deadline: %v", err)
	}
}

// ----------------------------------------------------------------------
// newMaskKey error seam (genuinely-unreachable crypto/rand failure)
// ----------------------------------------------------------------------

func TestWrite_MaskKeyError(t *testing.T) {
	s := newFakeServer(t)
	s.onConn = func(conn net.Conn) { time.Sleep(100 * time.Millisecond); _ = conn.Close() }
	// Dial FIRST with the real seam (upgradeRequest needs a working
	// randRead), then swap the seam to fail only the per-write mask-key
	// generation.
	conn := dialEcho(t, s)
	restore := setRandRead(func([]byte) (int, error) { return 0, errors.New("rand boom") })
	defer func() { restore(); _ = conn.c.Close() }()

	// writeData path.
	if err := conn.WriteText(context.Background(), []byte("x")); err == nil ||
		!strings.Contains(err.Error(), "rand boom") {
		t.Fatalf("WriteText mask err: %v", err)
	}
	// writeControl path.
	if err := conn.Ping(context.Background(), []byte("x")); err == nil ||
		!strings.Contains(err.Error(), "rand boom") {
		t.Fatalf("Ping mask err: %v", err)
	}
}

// ----------------------------------------------------------------------
// frame.go remaining branches: 64-bit extended length, zero-length frame
// ----------------------------------------------------------------------

func TestReadFrame_64BitExtendedLength(t *testing.T) {
	// Hand-build a 127-marker header declaring a tiny real payload so the
	// 64-bit length branch in readFrame runs without allocating 4 GiB.
	hdr := []byte{0x81, 127, 0, 0, 0, 0, 0, 0, 0, 3}
	body := []byte("abc")
	r := bufioReader(append(hdr, body...))
	f, err := readFrame(r, 0)
	if err != nil {
		t.Fatalf("readFrame 64-bit: %v", err)
	}
	if string(f.payload) != "abc" {
		t.Fatalf("payload = %q", f.payload)
	}
}

func TestReadFrame_64BitHighBitSet(t *testing.T) {
	hdr := []byte{0x81, 127, 0xFF, 0, 0, 0, 0, 0, 0, 0}
	r := bufioReader(hdr)
	if _, err := readFrame(r, 0); err == nil ||
		!strings.Contains(err.Error(), "high bit set") {
		t.Fatalf("want 64-bit high-bit error, got %v", err)
	}
}

func TestReadFrame_ReservedBits(t *testing.T) {
	hdr := []byte{0xF1, 0} // RSV1..3 set
	r := bufioReader(hdr)
	if _, err := readFrame(r, 0); err == nil ||
		!strings.Contains(err.Error(), "reserved bits") {
		t.Fatalf("want reserved-bits error, got %v", err)
	}
}

func TestReadFrame_ZeroLength(t *testing.T) {
	hdr := []byte{0x81, 0}
	r := bufioReader(hdr)
	f, err := readFrame(r, 0)
	if err != nil {
		t.Fatalf("readFrame zero-len: %v", err)
	}
	if len(f.payload) != 0 {
		t.Fatalf("want empty payload, got %q", f.payload)
	}
}

func TestReadFrame_TruncatedExt16(t *testing.T) {
	// 126 marker but no following 2-byte length.
	r := bufioReader([]byte{0x81, 126})
	if _, err := readFrame(r, 0); err == nil {
		t.Fatal("want truncated ext16 error")
	}
}

func TestReadFrame_TruncatedExt64(t *testing.T) {
	r := bufioReader([]byte{0x81, 127, 0, 0})
	if _, err := readFrame(r, 0); err == nil {
		t.Fatal("want truncated ext64 error")
	}
}

func TestReadFrame_TruncatedMaskKey(t *testing.T) {
	// masked bit set, payload len 1, but no mask key bytes.
	r := bufioReader([]byte{0x81, 0x81})
	if _, err := readFrame(r, 0); err == nil {
		t.Fatal("want truncated mask-key error")
	}
}

func TestReadFrame_TruncatedPayload(t *testing.T) {
	// declares 5 bytes, supplies 2.
	r := bufioReader([]byte{0x81, 5, 'h', 'i'})
	if _, err := readFrame(r, 0); err == nil {
		t.Fatal("want truncated payload error")
	}
}

func TestReadFrame_MaskedRoundTripUnmask(t *testing.T) {
	// masked server frame (rare, but the unmask path must run on RX).
	var b []byte
	key := [4]byte{1, 2, 3, 4}
	tmp := bufioWriterBytes(func(w *writeCollector) {
		_ = writeFrame(w, true, OpText, []byte("masked!"), key, true)
	})
	b = tmp
	f, err := readFrame(bufioReader(b), 0)
	if err != nil {
		t.Fatalf("readFrame masked: %v", err)
	}
	if string(f.payload) != "masked!" || !f.masked {
		t.Fatalf("masked roundtrip: %q masked=%v", f.payload, f.masked)
	}
}

func TestWriteFrame_ZeroLengthUnmasked(t *testing.T) {
	tmp := bufioWriterBytes(func(w *writeCollector) {
		_ = writeFrame(w, true, OpText, nil, [4]byte{}, false)
	})
	f, err := readFrame(bufioReader(tmp), 0)
	if err != nil {
		t.Fatalf("readFrame zero unmasked: %v", err)
	}
	if len(f.payload) != 0 {
		t.Fatalf("want empty payload")
	}
}

// ----------------------------------------------------------------------
// small test plumbing
// ----------------------------------------------------------------------

func bufioReader(b []byte) *bufio.Reader { return bufio.NewReader(bytes.NewReader(b)) }

type writeCollector struct{ buf []byte }

func (w *writeCollector) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	return len(p), nil
}

func bufioWriterBytes(fn func(*writeCollector)) []byte {
	w := &writeCollector{}
	fn(w)
	return w.buf
}
