package transport

// The UDP arms the shared conformance battery cannot reach: argument
// validation, the nil-receiver guards, and UDPListener — the broadcast
// receiver ACP1 announcements arrive on, which until now had no test at all.

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestDialUDPRejectsBadArguments(t *testing.T) {
	tests := []struct {
		name string
		host string
		port int
		want error
	}{
		{"empty host", "", 2071, ErrInvalidHost},
		{"port zero", "127.0.0.1", 0, ErrInvalidPort},
		{"port negative", "127.0.0.1", -1, ErrInvalidPort},
		{"port too high", "127.0.0.1", 65536, ErrInvalidPort},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DialUDP(context.Background(), tc.host, tc.port)
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// Every method tolerates a nil receiver and a nil socket rather than
// panicking: a connector that failed to dial still calls Close in a defer.
func TestUDPConnNilGuards(t *testing.T) {
	var c *UDPConn
	ctx := context.Background()

	if err := c.Send(ctx, []byte("x")); !errors.Is(err, ErrNilConn) {
		t.Errorf("Send on nil = %v, want ErrNilConn", err)
	}
	if _, err := c.Receive(ctx, 16); !errors.Is(err, ErrNilConn) {
		t.Errorf("Receive on nil = %v, want ErrNilConn", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close on nil = %v, want nil", err)
	}
	if a := c.LocalAddr(); a != nil {
		t.Errorf("LocalAddr on nil = %v, want nil", a)
	}
	if a := c.RemoteAddr(); a != nil {
		t.Errorf("RemoteAddr on nil = %v, want nil", a)
	}
}

// A connected socket reports both ends, which is what the logs key on.
func TestUDPConnAddrs(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = pc.Close() }()
	host, portStr, _ := net.SplitHostPort(pc.LocalAddr().String())
	port, _ := strconv.Atoi(portStr)

	c, err := DialUDP(context.Background(), host, port)
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer func() { _ = c.Close() }()

	if c.LocalAddr() == nil {
		t.Error("LocalAddr is nil on a live socket")
	}
	if got := c.RemoteAddr(); got == nil || got.String() != pc.LocalAddr().String() {
		t.Errorf("RemoteAddr = %v, want %v", got, pc.LocalAddr())
	}
}

// --- UDPListener: the ACP1 broadcast receiver, previously untested --------

// ListenUDP's accepted range is [0, 65535], one wider than DialUDP's
// [1, 65535], and the difference is deliberate: a zero port asks the kernel
// to pick one, which has a meaning for a listener and none for a dial.
func TestListenUDPPortRange(t *testing.T) {
	for _, port := range []int{-1, 65536} {
		if _, err := ListenUDP(context.Background(), port); !errors.Is(err, ErrInvalidPort) {
			t.Errorf("ListenUDP(%d) = %v, want ErrInvalidPort", port, err)
		}
	}

	l, err := ListenUDP(context.Background(), 0)
	if err != nil {
		t.Fatalf("ListenUDP(0) = %v, want a kernel-assigned port", err)
	}
	defer func() { _ = l.Close() }()
	if l.LocalAddr() == nil {
		t.Error("a kernel-assigned listener reports no address")
	}
}

func TestUDPListenerReceivesFromAnySender(t *testing.T) {
	l, err := listenUDPEphemeral(t)
	if err != nil {
		t.Skipf("cannot bind a UDP listener here: %v", err)
	}
	defer func() { _ = l.Close() }()

	if l.LocalAddr() == nil {
		t.Fatal("LocalAddr is nil on a bound listener")
	}
	_, portStr, _ := net.SplitHostPort(l.LocalAddr().String())

	// Two different senders: the listener is unconnected, so both land.
	for _, msg := range []string{"first", "second"} {
		sender, derr := net.Dial("udp", net.JoinHostPort("127.0.0.1", portStr))
		if derr != nil {
			t.Fatalf("sender dial: %v", derr)
		}
		if _, werr := sender.Write([]byte(msg)); werr != nil {
			t.Fatalf("sender write: %v", werr)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		got, from, rerr := l.Receive(ctx, 64)
		cancel()
		_ = sender.Close()
		if rerr != nil {
			t.Fatalf("Receive: %v", rerr)
		}
		if string(got) != msg {
			t.Errorf("got %q, want %q", got, msg)
		}
		if from == nil {
			t.Error("Receive returned no sender address")
		}
	}
}

func TestUDPListenerReceiveArgumentAndDeadline(t *testing.T) {
	l, err := listenUDPEphemeral(t)
	if err != nil {
		t.Skipf("cannot bind a UDP listener here: %v", err)
	}
	defer func() { _ = l.Close() }()

	if _, _, err := l.Receive(context.Background(), 0); !errors.Is(err, ErrInvalidMaxSize) {
		t.Errorf("Receive(max=0) = %v, want ErrInvalidMaxSize", err)
	}

	// Nothing is sent: the deadline must return, not hang.
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, _, err := l.Receive(ctx, 64); err == nil {
		t.Error("Receive returned with nothing sent")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("Receive took %s to honour a 150ms deadline", d)
	}
}

func TestUDPListenerCloseIsIdempotent(t *testing.T) {
	l, err := listenUDPEphemeral(t)
	if err != nil {
		t.Skipf("cannot bind a UDP listener here: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Errorf("second Close: %v, want nil", err)
	}
	// After Close the listener reports no address rather than a stale one.
	if _, _, err := l.Receive(context.Background(), 64); err == nil {
		t.Error("Receive on a closed listener returned no error")
	}
}

func TestUDPListenerNilGuards(t *testing.T) {
	var l *UDPListener
	if _, _, err := l.Receive(context.Background(), 16); !errors.Is(err, ErrNilConn) {
		t.Errorf("Receive on nil = %v, want ErrNilConn", err)
	}
	if err := l.Close(); err != nil {
		t.Errorf("Close on nil = %v, want nil", err)
	}
	if a := l.LocalAddr(); a != nil {
		t.Errorf("LocalAddr on nil = %v, want nil", a)
	}
}

// listenUDPEphemeral binds a listener on a kernel-assigned port — the
// documented meaning of port 0 — so parallel test runs cannot collide.
func listenUDPEphemeral(t *testing.T) (*UDPListener, error) {
	t.Helper()
	return ListenUDP(context.Background(), 0)
}
