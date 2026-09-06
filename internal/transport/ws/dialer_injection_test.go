package ws

// The payoff for widening DialOptions.Dialer to an interface: the RFC 6455
// upgrade now runs over an injected pipe, with no listener and no port.
//
// While the field was *net.Dialer the only substitution possible was another
// real dialer — you could change the timeout, but you could not hand the
// handshake a connection — so every handshake test needed a live socket.

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// pipeDialer hands back one end of a net.Pipe instead of opening a socket.
type pipeDialer struct {
	conn    net.Conn
	err     error
	network string
	address string
}

func (d *pipeDialer) DialContext(_ context.Context, network, address string) (net.Conn, error) {
	d.network, d.address = network, address
	if d.err != nil {
		return nil, d.err
	}
	return d.conn, nil
}

// serveUpgrade answers one RFC 6455 upgrade on conn, then returns so the
// caller can drive frames over the same pipe.
func serveUpgrade(t *testing.T, conn net.Conn) {
	t.Helper()
	br := bufio.NewReader(conn)
	req, err := http.ReadRequest(br)
	if err != nil {
		t.Errorf("read upgrade request: %v", err)
		return
	}
	accept := computeAccept(req.Header.Get("Sec-WebSocket-Key"))
	_, _ = conn.Write([]byte(
		"HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"))
}

func TestDialUsesTheInjectedDialer(t *testing.T) {
	ours, theirs := net.Pipe()
	t.Cleanup(func() { _ = theirs.Close() })

	// net.Pipe is synchronous and unbuffered, so the far end must keep
	// reading after the handshake — otherwise the client's Close frame has
	// nobody to write to and blocks for ever.
	handshook := make(chan struct{})
	go func() {
		serveUpgrade(t, theirs)
		close(handshook)
		_, _ = io.Copy(io.Discard, theirs)
	}()

	d := &pipeDialer{conn: ours}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := Dial(ctx, "ws://10.6.250.5:40009/", &DialOptions{Dialer: d})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = conn.Close(1000, "") }()
	<-handshook

	if d.network != "tcp" {
		t.Errorf("dialed network %q, want tcp", d.network)
	}
	if d.address != "10.6.250.5:40009" {
		t.Errorf("dialed address %q, want 10.6.250.5:40009", d.address)
	}
	// A client-side connection must mask its frames (RFC 6455 §5.3).
	if !conn.clientSide {
		t.Error("Dial produced a connection that does not consider itself the client")
	}
}

// The default port is filled in from the scheme when the URL omits one —
// checked here because the injected dialer records exactly what was asked
// for, which a real socket cannot report as cleanly.
func TestDialDefaultPortsReachTheDialer(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"ws://cerebrum/", "cerebrum:80"},
		{"wss://cerebrum/", "cerebrum:443"},
	}
	for _, tc := range tests {
		t.Run(tc.url, func(t *testing.T) {
			// The dial itself fails; the address it asked for is the point.
			d := &pipeDialer{err: errors.New("no route")}
			_, err := Dial(context.Background(), tc.url, &DialOptions{Dialer: d})
			if err == nil {
				t.Fatal("Dial succeeded with a failing dialer")
			}
			if d.address != tc.want {
				t.Errorf("dialed %q, want %q", d.address, tc.want)
			}
		})
	}
}
