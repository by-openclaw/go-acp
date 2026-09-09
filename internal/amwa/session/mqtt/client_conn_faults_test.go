package mqtt

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"
)

func newFaultLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// faultConn is a net.Conn whose failures are scripted per step, so the
// session's error arms that a real socket cannot produce on demand
// (deadline refusals, a write that fails mid-session) are exercised.
type faultConn struct {
	mu                sync.Mutex
	failDeadline      bool // SetDeadline refuses (before CONNECT)
	failWriteDeadline bool // SetWriteDeadline refuses (in send)
	failWriteAt       int  // the n-th Write (1-based) fails; 0 = never
	writes            int
	connack           []byte
	ackServed         bool
	closed            chan struct{}
	once              sync.Once
}

var errFault = errors.New("fault injected")

func (f *faultConn) Read(b []byte) (int, error) {
	f.mu.Lock()
	served := f.ackServed
	f.ackServed = true
	f.mu.Unlock()
	if !served && len(f.connack) > 0 {
		return copy(b, f.connack), nil
	}
	<-f.closed // hold the reader until the session closes the conn
	return 0, errors.New("closed")
}

func (f *faultConn) Write(b []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	if f.failWriteAt != 0 && f.writes >= f.failWriteAt {
		return 0, errFault
	}
	return len(b), nil
}

func (f *faultConn) Close() error {
	f.once.Do(func() { close(f.closed) })
	return nil
}
func (f *faultConn) LocalAddr() net.Addr  { return &net.TCPAddr{} }
func (f *faultConn) RemoteAddr() net.Addr { return &net.TCPAddr{} }
func (f *faultConn) SetDeadline(time.Time) error {
	if f.failDeadline {
		return errFault
	}
	return nil
}
func (f *faultConn) SetReadDeadline(time.Time) error { return nil }
func (f *faultConn) SetWriteDeadline(time.Time) error {
	if f.failWriteDeadline {
		return errFault
	}
	return nil
}

// faultDialer hands out one fresh faultConn per session from a factory.
type faultDialer struct {
	make     func() *faultConn
	failOnce bool // the first dial fails, later ones succeed
	mu       sync.Mutex
	last     *faultConn
	dials    int
}

func (d *faultDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	d.mu.Lock()
	d.dials++
	first := d.dials == 1
	d.mu.Unlock()
	if d.failOnce && first {
		return nil, errFault
	}
	c := d.make()
	d.mu.Lock()
	d.last = c
	d.mu.Unlock()
	return c, nil
}

func newFaultClient(t *testing.T, mk func() *faultConn) (*Client, *faultDialer) {
	t.Helper()
	fastBackoff(t)
	d := &faultDialer{make: mk}
	c := &Client{
		opts:     Options{Addr: "fault", ClientID: "c", KeepAlive: 20 * time.Millisecond},
		log:      nil,
		retained: map[string]message{},
		queue:    make(chan message, 256),
		done:     make(chan struct{}),
		dialer:   d,
	}
	c.log = newFaultLogger()
	return c, d
}

// Each scripted fault ends the session with that fault, and the client
// reconnects (a new conn is dialled) rather than giving up.
func TestSessionConnFaults(t *testing.T) {
	ok := []byte{packetCONNACK << 4, 2, 0, 0}
	cases := map[string]func() *faultConn{
		"deadline refused":     func() *faultConn { return &faultConn{failDeadline: true, closed: make(chan struct{})} },
		"connect write fails":  func() *faultConn { return &faultConn{failWriteAt: 1, closed: make(chan struct{})} },
		"replay write fails":   func() *faultConn { return &faultConn{failWriteAt: 2, connack: ok, closed: make(chan struct{})} },
		"queued publish fails": func() *faultConn { return &faultConn{failWriteAt: 2, connack: ok, closed: make(chan struct{})} },
		"ping write fails":     func() *faultConn { return &faultConn{failWriteAt: 2, connack: ok, closed: make(chan struct{})} },
		"write deadline refused": func() *faultConn {
			return &faultConn{failWriteDeadline: true, connack: ok, closed: make(chan struct{})}
		},
		"dial fails once": func() *faultConn { return &faultConn{connack: ok, closed: make(chan struct{})} },
	}
	for name, mk := range cases {
		c, d := newFaultClient(t, mk)
		switch name {
		case "dial fails once":
			d.failOnce = true
		case "replay write fails":
			c.retained["r"] = message{topic: "r", payload: []byte("1"), retain: true}
		case "queued publish fails":
			c.queue <- message{topic: "q", payload: []byte("1")}
		}
		ctx, cancel := context.WithCancel(context.Background())
		go c.run(ctx)
		deadline := time.Now().Add(3 * time.Second)
		dials := 0
		for time.Now().Before(deadline) {
			d.mu.Lock()
			dials = d.dials
			d.mu.Unlock()
			if dials >= 2 {
				break
			}
			time.Sleep(2 * time.Millisecond)
		}
		cancel()
		<-c.done
		if dials < 2 {
			t.Errorf("%s: session never reconnected after the fault (%d dials)", name, dials)
		}
	}
}
