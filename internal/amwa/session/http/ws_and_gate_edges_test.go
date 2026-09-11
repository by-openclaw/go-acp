package http

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	jwt "dhs/internal/auth"
	"dhs/internal/transport/ws"
)

// The WebSocket wrapper round-trips text both ways over a real upgrade,
// forwards pings, skips non-text frames on read, is idempotent on Close,
// and refuses to send or read once closed.
func TestWebSocketRoundTripAndClose(t *testing.T) {
	serverDone := make(chan error, 1)
	s := NewServer(nil)
	s.HandleRaw("/ws", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		c, err := AcceptWebSocket(w, r)
		if err != nil {
			serverDone <- err
			return
		}
		// Echo until the peer goes away; a ping in between must not be
		// surfaced by the client's ReadText. The handler is the socket's
		// only reader; it reports how the read loop ended.
		for {
			msg, err := c.ReadText()
			if err != nil {
				serverDone <- err
				return
			}
			_ = c.SendPing([]byte("p"))
			if err := c.SendText(append([]byte("echo:"), msg...)); err != nil {
				serverDone <- err
				return
			}
		}
	}))
	ts := httptest.NewServer(s.MuxHandler())
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	client, err := DialWebSocket(context.Background(), wsURL, stdhttp.Header{"X-Test": []string{"1"}})
	if err != nil {
		t.Fatal(err)
	}
	client.SetIdleTimeout(time.Minute)
	if client.IdleTimeout() != time.Minute {
		t.Errorf("IdleTimeout = %v", client.IdleTimeout())
	}
	if err := client.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := client.SendText([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	got, err := client.ReadText()
	if err != nil || string(got) != "echo:hi" {
		t.Fatalf("ReadText = %q, %v", got, err)
	}
	if err := client.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Errorf("second Close must be a no-op, got %v", err)
	}
	if err := client.SendText([]byte("x")); !errors.Is(err, ErrWebSocketClosed) {
		t.Errorf("SendText after Close = %v", err)
	}
	if err := client.SendPing(nil); !errors.Is(err, ErrWebSocketClosed) {
		t.Errorf("SendPing after Close = %v", err)
	}
	if _, err := client.ReadText(); !errors.Is(err, ErrWebSocketClosed) {
		t.Errorf("ReadText after Close = %v", err)
	}
	// The server side sees the peer's close as EOF and reports closed.
	select {
	case err := <-serverDone:
		if !errors.Is(err, ErrWebSocketClosed) {
			t.Errorf("server read loop ended with %v, want ErrWebSocketClosed", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("server side never observed the peer's close")
	}
}

// A read error that is not EOF (a deadline) is returned as is, without
// marking the socket closed.
func TestWebSocketReadDeadlineError(t *testing.T) {
	s := NewServer(nil)
	s.HandleRaw("/ws", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		c, err := AcceptWebSocket(w, r)
		if err != nil {
			return
		}
		_, _ = c.ReadText() // hold the socket open until the client leaves
	}))
	ts := httptest.NewServer(s.MuxHandler())
	defer ts.Close()
	client, err := DialWebSocket(context.Background(), "ws"+strings.TrimPrefix(ts.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	_ = client.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	if _, err := client.ReadText(); err == nil || errors.Is(err, ErrWebSocketClosed) {
		t.Errorf("deadline read = %v, want a timeout that is not 'closed'", err)
	}
}

// DialWebSocket refuses non-ws schemes and reports a dial failure; an
// upgrade against a plain handler fails on the server side.
func TestDialAndAcceptFailures(t *testing.T) {
	if _, err := DialWebSocket(context.Background(), "http://x/ws", nil); err == nil || !strings.Contains(err.Error(), "scheme") {
		t.Errorf("http scheme = %v", err)
	}
	if _, err := DialWebSocket(context.Background(), "ws://127.0.0.1:1/ws", nil); err == nil || !strings.Contains(err.Error(), "dial") {
		t.Errorf("refused dial = %v", err)
	}
	w := httptest.NewRecorder()
	if _, err := AcceptWebSocket(w, httptest.NewRequest(stdhttp.MethodGet, "/ws", nil)); err == nil {
		t.Error("a plain GET is not an upgrade")
	}
	if NewClient() == nil {
		t.Error("NewClient must return a client")
	}
	_ = ws.OpText
}

// ReadText hands back text frames only: a binary frame from a foreign peer
// is skipped, not surfaced. Our own client cannot send binary, so a raw
// RFC 6455 client performs the upgrade and writes one masked binary frame
// followed by one masked text frame.
func TestWebSocketReadTextSkipsBinaryFrames(t *testing.T) {
	got := make(chan string, 1)
	s := NewServer(nil)
	s.HandleRaw("/ws", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		c, err := AcceptWebSocket(w, r)
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		msg, err := c.ReadText()
		if err != nil {
			got <- "error: " + err.Error()
			return
		}
		got <- string(msg)
	}))
	ts := httptest.NewServer(s.MuxHandler())
	defer ts.Close()

	conn, err := net.Dial("tcp", strings.TrimPrefix(ts.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_, _ = fmt.Fprintf(conn, "GET /ws HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n")
	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil || !strings.Contains(status, "101") {
		t.Fatalf("upgrade = %q, %v", status, err)
	}
	for { // drain the response headers
		line, err := br.ReadString('\n')
		if err != nil || line == "\r\n" {
			break
		}
	}
	frame := func(opcode byte, payload string) []byte {
		mask := [4]byte{1, 2, 3, 4}
		f := []byte{0x80 | opcode, 0x80 | byte(len(payload))}
		f = append(f, mask[:]...)
		for i := 0; i < len(payload); i++ {
			f = append(f, payload[i]^mask[i%4])
		}
		return f
	}
	_, _ = conn.Write(frame(0x2, "binary"))
	_, _ = conn.Write(frame(0x1, "text"))
	select {
	case msg := <-got:
		if msg != "text" {
			t.Errorf("ReadText returned %q, want the text frame after skipping binary", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server never read the text frame")
	}
}

// fetchKeys knows no keys but can be asked to fetch an issuer's set.
type fetchKeys struct {
	mu      sync.Mutex
	fetched []string
	err     error
}

func (f *fetchKeys) Keys() []jwt.JWK { return nil }

func (f *fetchKeys) FetchIssuer(_ context.Context, iss string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetched = append(f.fetched, iss)
	return f.err
}

// The gate's client id travels in the request context; a token signed by
// an unknown key triggers ONE issuer fetch per two seconds (503 with
// Retry-After while fetching, 401 right after), and the fetcher's error is
// only logged.
func TestClientIDContextAndIssuerFetch(t *testing.T) {
	if ClientIDFrom(context.Background()) != "" {
		t.Error("no client id in an empty context")
	}
	if ClientIDFrom(WithClientID(context.Background(), "c9")) != "c9" {
		t.Error("client id must round-trip through the context")
	}

	fk := &fetchKeys{err: errors.New("issuer down")}
	g := &AuthGate{Keys: fk, Hosts: []string{"node.test"}}
	// A preflight is credential-less by browser design: the gate itself
	// lets it through even when called outside the dispatcher.
	if _, _, _, _, ok := g.Check(httptest.NewRequest(stdhttp.MethodOptions, "/x-nmos/node/v1.3/self", nil)); !ok {
		t.Error("OPTIONS must pass the gate")
	}
	tok := gateMint(t, gateClaims())
	req := httptest.NewRequest(stdhttp.MethodGet, "/x-nmos/node/v1.3/self", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	status, hdrs, _, _, ok := g.Check(req)
	if ok || status != stdhttp.StatusServiceUnavailable || hdrs["Retry-After"] == "" {
		t.Fatalf("first unknown-key request = %d %v ok=%v, want 503 + Retry-After", status, hdrs, ok)
	}
	status, _, _, _, ok = g.Check(req)
	if ok || status != stdhttp.StatusUnauthorized {
		t.Errorf("second request within the fetch window = %d, want 401 (no second fetch)", status)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		fk.mu.Lock()
		n := len(fk.fetched)
		fk.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	fk.mu.Lock()
	fetched := append([]string(nil), fk.fetched...)
	fk.mu.Unlock()
	if len(fetched) != 1 || fetched[0] != "https://auth.test" {
		t.Errorf("issuer fetches = %v, want exactly one for the token's iss", fetched)
	}
	g.mu.Lock()
	g.fetching["https://auth.test"] = time.Now().Add(-3 * time.Second)
	g.mu.Unlock()
	if !g.startIssuerFetch("https://auth.test") {
		t.Error("a fetch older than the window must be allowed again")
	}
}
