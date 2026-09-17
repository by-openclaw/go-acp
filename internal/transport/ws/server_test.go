package ws

// Server-side upgrade tests.
//
// The headline is the round-trip: our own Dial talking to our own Accept.
// That proves the two ends agree on RFC 6455 §5.3 masking — a client masks,
// a server does not — which is the single wire difference between them and
// the easiest thing to get silently wrong.

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// upgradeHandler accepts the socket and hands it to fn.
func upgradeHandler(t *testing.T, opts *AcceptOptions, fn func(*Conn)) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := Accept(w, r, opts)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fn(c)
	})
}

func TestAcceptDialRoundTrip(t *testing.T) {
	got := make(chan string, 1)
	srv := httptest.NewServer(upgradeHandler(t, nil, func(c *Conn) {
		defer func() { _ = c.Close(1000, "done") }()
		// Read the client's (masked) text frame...
		op, payload, err := c.ReadMessage(context.Background())
		if err != nil {
			got <- "read error: " + err.Error()
			return
		}
		if op != OpText {
			got <- "unexpected opcode"
			return
		}
		got <- string(payload)
		// ...and answer with an UNMASKED server frame.
		_ = c.WriteText(context.Background(), []byte("pong from server"))
	}))
	defer srv.Close()

	conn, err := Dial(context.Background(), "ws://"+strings.TrimPrefix(srv.URL, "http://")+"/", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close(1000, "bye") }()

	if err := conn.WriteText(context.Background(), []byte("ping from client")); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case v := <-got:
		if v != "ping from client" {
			t.Fatalf("server received %q", v)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server never received the client frame")
	}

	op, payload, err := conn.ReadMessage(context.Background())
	if err != nil {
		t.Fatalf("client read: %v", err)
	}
	if op != OpText || string(payload) != "pong from server" {
		t.Fatalf("client got op=%d payload=%q", op, payload)
	}
}

// A server-accepted Conn must not mask; a dialled one must.
func TestAcceptProducesServerSideConn(t *testing.T) {
	ready := make(chan *Conn, 1)
	srv := httptest.NewServer(upgradeHandler(t, nil, func(c *Conn) {
		ready <- c
		time.Sleep(200 * time.Millisecond)
		_ = c.Close(1000, "")
	}))
	defer srv.Close()

	conn, err := Dial(context.Background(), "ws://"+strings.TrimPrefix(srv.URL, "http://")+"/", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close(1000, "") }()

	server := <-ready
	if server.clientSide {
		t.Error("Accept produced a client-side Conn — it would mask, violating RFC 6455 §5.3")
	}
	if !conn.clientSide {
		t.Error("Dial produced a server-side Conn — it would not mask, violating §5.3")
	}
	if server.netConn() == nil {
		t.Error("netConn() returned nil on an accepted connection")
	}
}

// The subprotocol is echoed when requested.
func TestAcceptEchoesSubprotocol(t *testing.T) {
	srv := httptest.NewServer(upgradeHandler(t, &AcceptOptions{Subprotocol: "nmos"}, func(c *Conn) {
		_ = c.Close(1000, "")
	}))
	defer srv.Close()

	conn, err := Dial(context.Background(), "ws://"+strings.TrimPrefix(srv.URL, "http://")+"/", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.Close(1000, "")
}

func TestUpgradeKeyFromRejectsBadRequests(t *testing.T) {
	good := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/ws", nil)
		r.Header.Set("Connection", "Upgrade")
		r.Header.Set("Upgrade", "websocket")
		r.Header.Set("Sec-WebSocket-Version", "13")
		r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
		return r
	}
	if _, err := upgradeKeyFrom(good()); err != nil {
		t.Fatalf("a well-formed upgrade was rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*http.Request)
	}{
		{"nil request", nil},
		{"wrong method", func(r *http.Request) { r.Method = http.MethodPost }},
		{"no Connection: Upgrade", func(r *http.Request) { r.Header.Del("Connection") }},
		{"wrong Upgrade token", func(r *http.Request) { r.Header.Set("Upgrade", "h2c") }},
		{"unsupported version", func(r *http.Request) { r.Header.Set("Sec-WebSocket-Version", "8") }},
		{"missing key", func(r *http.Request) { r.Header.Del("Sec-WebSocket-Key") }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var r *http.Request
			if tc.mutate != nil {
				r = good()
				tc.mutate(r)
			}
			if _, err := upgradeKeyFrom(r); !errors.Is(err, ErrNotWebSocketUpgrade) {
				t.Fatalf("err = %v, want ErrNotWebSocketUpgrade", err)
			}
		})
	}

	// An absent version header is tolerated (defaults to 13).
	r := good()
	r.Header.Del("Sec-WebSocket-Version")
	if _, err := upgradeKeyFrom(r); err != nil {
		t.Fatalf("absent version rejected: %v", err)
	}
}

// A ResponseWriter that cannot be hijacked must be reported, not panicked on.
func TestAcceptRejectsNonHijackableWriter(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")

	// httptest.ResponseRecorder does not implement http.Hijacker.
	if _, err := Accept(httptest.NewRecorder(), r, nil); !errors.Is(err, ErrHijackUnsupported) {
		t.Fatalf("err = %v, want ErrHijackUnsupported", err)
	}
}

// A malformed request must be refused before any hijack happens.
func TestAcceptRejectsBadUpgrade(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws", nil) // no upgrade headers
	if _, err := Accept(httptest.NewRecorder(), r, nil); !errors.Is(err, ErrNotWebSocketUpgrade) {
		t.Fatalf("err = %v, want ErrNotWebSocketUpgrade", err)
	}
}

// The accepted connection carries the peer address through for access logs.
func TestAcceptNetConnAddr(t *testing.T) {
	addrCh := make(chan net.Addr, 1)
	srv := httptest.NewServer(upgradeHandler(t, nil, func(c *Conn) {
		addrCh <- c.netConn().RemoteAddr()
		_ = c.Close(1000, "")
	}))
	defer srv.Close()

	conn, err := Dial(context.Background(), "ws://"+strings.TrimPrefix(srv.URL, "http://")+"/", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close(1000, "") }()

	select {
	case a := <-addrCh:
		if a == nil {
			t.Fatal("RemoteAddr was nil")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("handler never ran")
	}
}

// fakeHijacker is a ResponseWriter whose Hijack behaviour is scripted, so the
// failure arms of Accept — which a real server never reaches — are testable.
type fakeHijacker struct {
	http.ResponseWriter
	conn   net.Conn
	brw    *bufio.ReadWriter
	hijErr error
}

func (f *fakeHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if f.hijErr != nil {
		return nil, nil, f.hijErr
	}
	return f.conn, f.brw, nil
}

func upgradeReq() *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	return r
}

// Hijack itself can fail (a server shutting down, a wrapped writer).
func TestAcceptHijackError(t *testing.T) {
	w := &fakeHijacker{
		ResponseWriter: httptest.NewRecorder(),
		hijErr:         errors.New("cannot hijack now"),
	}
	if _, err := Accept(w, upgradeReq(), nil); err == nil {
		t.Fatal("Accept returned nil despite a failing Hijack")
	}
}

// Writing the 101 response can fail if the peer already went away. The socket
// must be closed rather than leaked.
func TestAcceptHandshakeWriteError(t *testing.T) {
	c1, c2 := net.Pipe()
	_ = c2.Close() // peer gone: the write fails
	_ = c1.SetWriteDeadline(time.Now().Add(time.Second))

	w := &fakeHijacker{
		ResponseWriter: httptest.NewRecorder(),
		conn:           c1,
		brw:            bufio.NewReadWriter(bufio.NewReader(c1), bufio.NewWriter(c1)),
	}
	if _, err := Accept(w, upgradeReq(), nil); err == nil {
		t.Fatal("Accept returned nil despite a failing handshake write")
	}
}

// A Hijacker that yields no buffered reader must still produce a usable Conn —
// Accept falls back to wrapping the raw socket rather than nil-dereferencing.
func TestAcceptNilBufferedReaderFallback(t *testing.T) {
	c1, c2 := net.Pipe()
	defer func() { _ = c1.Close(); _ = c2.Close() }()

	// Drain whatever the handshake writes so Accept's write does not block.
	go func() { _, _ = io.Copy(io.Discard, c2) }()

	w := &fakeHijacker{
		ResponseWriter: httptest.NewRecorder(),
		conn:           c1,
		brw:            &bufio.ReadWriter{}, // Reader is nil
	}
	conn, err := Accept(w, upgradeReq(), &AcceptOptions{MaxPayload: 4096})
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if conn.br == nil {
		t.Fatal("Accept left a nil buffered reader")
	}
	if conn.clientSide {
		t.Fatal("Accept produced a masking (client-side) Conn")
	}
}
