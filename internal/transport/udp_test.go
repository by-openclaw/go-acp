package transport

// The UDP arms the shared conformance battery cannot reach: argument
// validation, the nil-receiver guards, and UDPListener — the broadcast
// receiver ACP1 announcements arrive on, which until now had no test at all.

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"syscall"
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

// --- the failure arms, on a live socket where it can produce them ---------

// udpPeer dials a connected UDPConn at a loopback PacketConn and returns
// both ends, so a test can play the far side with stdlib WriteTo.
func udpPeer(t *testing.T) (*UDPConn, net.PacketConn) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	host, port := splitAddr(t, pc.LocalAddr().String())
	c, err := DialUDP(context.Background(), host, port)
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, pc
}

// A dial refused by its own context is the one UDP dial failure that needs
// no unreachable host and no DNS: the dialer checks ctx before the socket.
func TestDialUDPCancelledContextIsTyped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DialUDP(ctx, "127.0.0.1", 2071); !errors.Is(err, ErrDialFailed) {
		t.Fatalf("DialUDP(cancelled) = %v, want ErrDialFailed", err)
	}
}

// A "udp" dial always yields a *net.UDPConn; the arm needs the seam.
func TestDialUDPUnexpectedConnType(t *testing.T) {
	orig := dialUDPAssert
	dialUDPAssert = func(net.Conn) (*net.UDPConn, bool) { return nil, false }
	t.Cleanup(func() { dialUDPAssert = orig })

	if _, err := DialUDP(context.Background(), "127.0.0.1", 2071); !errors.Is(err, ErrWrongConnType) {
		t.Fatalf("DialUDP = %v, want ErrWrongConnType", err)
	}
}

// Both write-side arms of a dead socket: the deadline cannot be armed, and
// without one the write itself fails.
func TestUDPConnSendOnClosedSocket(t *testing.T) {
	c, _ := udpPeer(t)
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Send(ctx, []byte("x")); !errors.Is(err, ErrSetDeadlineFailed) {
		t.Errorf("Send with deadline on closed = %v, want ErrSetDeadlineFailed", err)
	}
	if err := c.Send(context.Background(), []byte("x")); !errors.Is(err, ErrWriteFailed) {
		t.Errorf("Send on closed = %v, want ErrWriteFailed", err)
	}
}

// A datagram either leaves whole or fails, so the kernel never reports a
// short write on a connected UDP socket — the arm exists because a Write
// contract that COULD return n < len must be checked, and it goes through
// the seam.
func TestUDPConnShortWriteIsReported(t *testing.T) {
	orig := udpWrite
	udpWrite = func(_ *net.UDPConn, b []byte) (int, error) { return len(b) - 1, nil }
	t.Cleanup(func() { udpWrite = orig })

	c, _ := udpPeer(t)
	if err := c.Send(context.Background(), []byte("ping")); !errors.Is(err, ErrShortWrite) {
		t.Fatalf("Send = %v, want ErrShortWrite", err)
	}
}

// A read that fails for a reason other than a deadline — here a socket
// closed under it — is a hard error, not a timeout the retry loop would
// wait out again.
func TestUDPConnReceiveOnClosedSocketIsAHardError(t *testing.T) {
	c, _ := udpPeer(t)
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// No deadline: the deadline arm cannot mask the read arm.
	if _, err := c.Receive(context.Background(), 16); !errors.Is(err, ErrReadFailed) {
		t.Fatalf("Receive on closed = %v, want ErrReadFailed", err)
	}
}

// A socket deadline that expires with the context still live is reported
// as context.DeadlineExceeded. See pureDeadline for why WithTimeout cannot
// isolate this arm.
func TestUDPConnReceiveTimesOut(t *testing.T) {
	c, _ := udpPeer(t)
	if _, err := c.Receive(pureDeadline(t, 100*time.Millisecond), 16); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Receive from a silent peer = %v, want context.DeadlineExceeded", err)
	}
}

// The oversized-datagram contract has one reachable arm per OS (Unix
// truncates, Windows fails the read with WSAEMSGSIZE); the seam takes the
// arm this host will never produce so both are proven everywhere. The real
// datagram is udp_oversize_test.go.
func TestUDPConnOversizedDatagramBothArms(t *testing.T) {
	arms := []struct {
		name string
		read func(*net.UDPConn, []byte) (int, error)
	}{
		{"truncating recv", func(_ *net.UDPConn, b []byte) (int, error) { return len(b), nil }},
		{"message too long", func(*net.UDPConn, []byte) (int, error) { return 0, errMessageTooLong }},
	}
	for _, tc := range arms {
		t.Run(tc.name, func(t *testing.T) {
			orig := udpRead
			udpRead = tc.read
			t.Cleanup(func() { udpRead = orig })

			c, _ := udpPeer(t)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if _, err := c.Receive(ctx, 8); !errors.Is(err, ErrOversizedDatagram) {
				t.Fatalf("Receive = %v, want ErrOversizedDatagram", err)
			}
		})
	}
}

// isMessageTooLong recognises the OS's own constant, also when net wraps it
// in an OpError, and nothing else.
func TestIsMessageTooLong(t *testing.T) {
	if !isMessageTooLong(errMessageTooLong) {
		t.Error("the OS's own message-too-long error was not recognised")
	}
	if !isMessageTooLong(&net.OpError{Op: "read", Err: errMessageTooLong}) {
		t.Error("the error wrapped in an OpError was not recognised")
	}
	if isMessageTooLong(errors.New("connection reset")) {
		t.Error("an unrelated error was taken for message-too-long")
	}
}

func TestUDPConnCloseReportsUnexpectedError(t *testing.T) {
	orig := closeConn
	closeConn = func(net.Conn) error { return errors.New("boom") }
	t.Cleanup(func() { closeConn = orig })

	c, _ := udpPeer(t)
	if err := c.Close(); !errors.Is(err, ErrCloseFailed) {
		t.Fatalf("Close = %v, want ErrCloseFailed", err)
	}
}

// ListenUDP's pre-bind hook: a Control dispatch failure fails the bind, and
// it surfaces through ListenPacket as the typed listen error.
func TestListenUDPControlFailureFailsTheBind(t *testing.T) {
	orig := udpRawControl
	udpRawControl = func(syscall.RawConn, func(uintptr)) error { return errors.New("fd already closed") }
	t.Cleanup(func() { udpRawControl = orig })

	if _, err := ListenUDP(context.Background(), 0); !errors.Is(err, ErrListenFailed) {
		t.Fatalf("ListenUDP = %v, want ErrListenFailed", err)
	}
}

func TestListenUDPUnexpectedConnType(t *testing.T) {
	orig := udpAssertConn
	udpAssertConn = func(net.PacketConn) (*net.UDPConn, bool) { return nil, false }
	t.Cleanup(func() { udpAssertConn = orig })

	if _, err := ListenUDP(context.Background(), 0); !errors.Is(err, ErrWrongConnType) {
		t.Fatalf("ListenUDP = %v, want ErrWrongConnType", err)
	}
}

func TestUDPListenerReceiveOnClosedSocket(t *testing.T) {
	l, err := listenUDPEphemeral(t)
	if err != nil {
		t.Skipf("cannot bind a UDP listener here: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, _, err := l.Receive(ctx, 16); !errors.Is(err, ErrSetDeadlineFailed) {
		t.Errorf("Receive with deadline on closed = %v, want ErrSetDeadlineFailed", err)
	}
	if _, _, err := l.Receive(context.Background(), 16); !errors.Is(err, ErrReadFailed) {
		t.Errorf("Receive on closed = %v, want ErrReadFailed", err)
	}
}

func TestUDPListenerReceiveTimesOut(t *testing.T) {
	l, err := listenUDPEphemeral(t)
	if err != nil {
		t.Skipf("cannot bind a UDP listener here: %v", err)
	}
	defer func() { _ = l.Close() }()
	if _, _, err := l.Receive(pureDeadline(t, 100*time.Millisecond), 16); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Receive with nothing sent = %v, want context.DeadlineExceeded", err)
	}
}

// The real datagram, on whichever arm this OS takes.
func TestUDPListenerRejectsOversizedDatagram(t *testing.T) {
	l, err := listenUDPEphemeral(t)
	if err != nil {
		t.Skipf("cannot bind a UDP listener here: %v", err)
	}
	defer func() { _ = l.Close() }()
	_, portStr, _ := net.SplitHostPort(l.LocalAddr().String())
	sender, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", portStr))
	if err != nil {
		t.Fatalf("sender dial: %v", err)
	}
	defer func() { _ = sender.Close() }()
	if _, err := sender.Write([]byte(strings.Repeat("A", 64))); err != nil {
		t.Fatalf("sender write: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, _, err := l.Receive(ctx, 8); !errors.Is(err, ErrOversizedDatagram) {
		t.Fatalf("Receive = %v, want ErrOversizedDatagram", err)
	}
}

// And the arm this OS cannot take, through the seam — see
// TestUDPConnOversizedDatagramBothArms.
func TestUDPListenerOversizedDatagramBothArms(t *testing.T) {
	arms := []struct {
		name string
		read func(*net.UDPConn, []byte) (int, *net.UDPAddr, error)
	}{
		{"truncating recv", func(_ *net.UDPConn, b []byte) (int, *net.UDPAddr, error) {
			return len(b), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}, nil
		}},
		{"message too long", func(*net.UDPConn, []byte) (int, *net.UDPAddr, error) {
			return 0, nil, errMessageTooLong
		}},
	}
	for _, tc := range arms {
		t.Run(tc.name, func(t *testing.T) {
			orig := udpReadFromUDP
			udpReadFromUDP = tc.read
			t.Cleanup(func() { udpReadFromUDP = orig })

			l, err := listenUDPEphemeral(t)
			if err != nil {
				t.Skipf("cannot bind a UDP listener here: %v", err)
			}
			defer func() { _ = l.Close() }()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if _, _, err := l.Receive(ctx, 8); !errors.Is(err, ErrOversizedDatagram) {
				t.Fatalf("Receive = %v, want ErrOversizedDatagram", err)
			}
		})
	}
}

func TestUDPListenerCloseReportsUnexpectedError(t *testing.T) {
	orig := closeConn
	closeConn = func(net.Conn) error { return errors.New("boom") }
	t.Cleanup(func() { closeConn = orig })

	l, err := listenUDPEphemeral(t)
	if err != nil {
		t.Skipf("cannot bind a UDP listener here: %v", err)
	}
	if err := l.Close(); !errors.Is(err, ErrCloseFailed) {
		t.Fatalf("Close = %v, want ErrCloseFailed", err)
	}
}
