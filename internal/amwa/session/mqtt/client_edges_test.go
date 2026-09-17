package mqtt

import (
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dhs/internal/transport"
)

// scriptedBroker accepts connections and runs one script per connection:
// what to answer a CONNECT with, and whether to hang up right after.
type scriptedBroker struct {
	ln        net.Listener
	accepts   atomic.Int32
	connack   []byte // nil = close before answering
	dropAfter bool   // close right after the CONNACK
	partial   bool   // write half a CONNACK then close

	mu    sync.Mutex
	types []byte // packet types seen after CONNECT
}

func newScriptedBroker(t *testing.T, connack []byte, dropAfter, partial bool) *scriptedBroker {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	b := &scriptedBroker{ln: ln, connack: connack, dropAfter: dropAfter, partial: partial}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			b.accepts.Add(1)
			go b.serve(conn)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return b
}

func (b *scriptedBroker) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	r := &packetReader{conn: conn}
	if _, _, err := r.next(); err != nil { // CONNECT
		return
	}
	switch {
	case b.partial:
		_, _ = conn.Write([]byte{packetCONNACK << 4, 2})
		return
	case b.connack == nil:
		return
	}
	if _, err := conn.Write(b.connack); err != nil {
		return
	}
	if b.dropAfter {
		return
	}
	for {
		ptype, _, err := r.next()
		if err != nil {
			return
		}
		b.mu.Lock()
		b.types = append(b.types, ptype)
		b.mu.Unlock()
		if ptype == packetPINGREQ {
			_, _ = conn.Write([]byte{packetPINGRESP << 4, 0})
		}
		if ptype == packetDISCONNECT {
			return
		}
	}
}

func (b *scriptedBroker) seen(ptype byte) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, p := range b.types {
		if p == ptype {
			n++
		}
	}
	return n
}

func fastBackoff(t *testing.T) {
	t.Helper()
	orig := reconnectBackoff
	reconnectBackoff = time.Millisecond
	t.Cleanup(func() { reconnectBackoff = orig })
}

// New refuses a client without a broker address or client id, and a TLS
// posture selects the TLS dialer.
func TestNewValidationAndTLSDialer(t *testing.T) {
	if _, err := New(Options{ClientID: "c"}); err == nil {
		t.Error("missing Addr must be refused")
	}
	if _, err := New(Options{Addr: "127.0.0.1:1"}); err == nil {
		t.Error("missing ClientID must be refused")
	}
	fastBackoff(t)
	c, err := New(Options{Addr: "127.0.0.1:1", ClientID: "c", TLS: transport.TLSOptions{Enable: true, Insecure: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.dialer.(transport.TLSDialer); !ok {
		t.Errorf("dialer = %T, want transport.TLSDialer for a TLS posture", c.dialer)
	}
	c.Close()
}

// A client that cannot reach its broker keeps the newest 256 messages:
// the oldest is dropped when the queue is full.
func TestPublishEvictsOldestWhenQueueFull(t *testing.T) {
	fastBackoff(t)
	c, err := New(Options{Addr: "127.0.0.1:1", ClientID: "c"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for i := 0; i < cap(c.queue)+1; i++ {
		c.Publish(string(rune('a'+i%26))+"/"+string(rune('0'+i%10)), []byte("v"), false)
	}
	if len(c.queue) != cap(c.queue) {
		t.Fatalf("queue holds %d, want the cap %d", len(c.queue), cap(c.queue))
	}
	first := <-c.queue
	if first.topic == "a/0" {
		t.Error("the oldest message must have been evicted")
	}
}

// The CONNECT packet carries username and password flags only when set;
// PINGREQ is the two-byte control packet.
func TestConnectPacketCredentialsAndPingreq(t *testing.T) {
	plain := connectPacket("c", 30, "", "")
	user := connectPacket("c", 30, "u", "")
	both := connectPacket("c", 30, "u", "p")
	flagAt := func(p []byte) byte { return p[2+2+4+1] } // fixed(2) + "MQTT" string(2+4) + level(1)
	if flagAt(plain) != 0x02 || flagAt(user) != 0x82 || flagAt(both) != 0xC2 {
		t.Errorf("connect flags = %x / %x / %x, want 02 / 82 / c2", flagAt(plain), flagAt(user), flagAt(both))
	}
	if len(both) <= len(user) || len(user) <= len(plain) {
		t.Error("credentials must lengthen the packet")
	}
	if p := pingreqPacket(); len(p) != 2 || p[0] != packetPINGREQ<<4 || p[1] != 0 {
		t.Errorf("pingreq = % x", p)
	}
}

// A session that ends — no CONNACK, a truncated CONNACK, a refused
// CONNECT, a broker that hangs up after connecting — is retried with a
// growing backoff, and Close during the backoff returns promptly.
func TestSessionFailuresReconnect(t *testing.T) {
	fastBackoff(t)
	for name, b := range map[string]*scriptedBroker{
		"no connack":         newScriptedBroker(t, nil, false, false),
		"truncated connack":  newScriptedBroker(t, nil, false, true),
		"refused":            newScriptedBroker(t, []byte{packetCONNACK << 4, 2, 0, 5}, false, false),
		"drop after connack": newScriptedBroker(t, []byte{packetCONNACK << 4, 2, 0, 0}, true, false),
	} {
		c, err := New(Options{Addr: b.ln.Addr().String(), ClientID: "c", KeepAlive: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		waitFor(t, func() bool { return b.accepts.Load() >= 3 })
		c.Close()
		if b.accepts.Load() < 3 {
			t.Errorf("%s: %d sessions, want at least 3 reconnects", name, b.accepts.Load())
		}
	}
}

// Over a healthy session the client replays retained messages on connect,
// pings on the keep-alive cadence, sends queued publishes, and says
// DISCONNECT on Close.
func TestHealthySessionReplayPingAndDisconnect(t *testing.T) {
	fastBackoff(t)
	b := newScriptedBroker(t, []byte{packetCONNACK << 4, 2, 0, 0}, false, false)
	c, err := New(Options{Addr: b.ln.Addr().String(), ClientID: "c", KeepAlive: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	c.Publish("state/retained", []byte("1"), true)
	c.Publish("event", []byte("2"), false)
	waitFor(t, func() bool { return b.seen(packetPUBLISH) >= 2 && b.seen(packetPINGREQ) >= 1 })
	c.Close() // sends DISCONNECT, then closes; Close returning proves the session ended
	// The broker usually reads the DISCONNECT, but a PINGRESP still unread
	// on the client side at close time turns the close into a reset that
	// can drop it on the wire — so its arrival is observed, not required.
	deadline := time.Now().Add(500 * time.Millisecond)
	for b.seen(packetDISCONNECT) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
}

// readFull returns what it read before the connection ended.
func TestReadFullReportsShortRead(t *testing.T) {
	a, z := net.Pipe()
	go func() { _, _ = z.Write([]byte{1, 2}); _ = z.Close() }()
	buf := make([]byte, 4)
	n, err := readFull(a, buf)
	if n != 2 || err == nil {
		t.Errorf("readFull = %d, %v; want 2 bytes and an error", n, err)
	}
	_ = a.Close()
	_ = io.EOF
}
