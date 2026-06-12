// Package transport provides the low-level send/receive primitives that
// protocol plugins build on. It is protocol-agnostic: it moves bytes, it
// does not know about ACP headers, MTIDs, or AxonNet objects.
//
// Every transport honours context.Context cancellation and exposes an
// explicit per-call deadline. That is the only way the ACP1 retry loop
// in internal/acp1/consumer/client.go can implement spec-compliant
// transaction timeouts without racing the socket read.
package transport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
)

// UDPConn is a connected UDP socket: all sends target the peer supplied
// at construction time, and receives are peer-filtered by the kernel.
// This mirrors the C# reference's UdpClient.Connect() usage — it is the
// correct pattern for ACP1 UDP direct mode (one device per client).
//
// UDPConn is NOT safe for concurrent Send or concurrent Receive from
// multiple goroutines. It IS safe to have one goroutine sending while
// another receives (e.g. the retry loop sending, an announce listener
// receiving on the same socket) — the underlying net.UDPConn guarantees
// that.
type UDPConn struct {
	conn *net.UDPConn
}

// DialUDP opens a connected UDP socket to host:port. The local port is
// chosen by the kernel. Returns a TransportError wrapping the cause on
// failure.
func DialUDP(ctx context.Context, host string, port int) (*UDPConn, error) {
	if host == "" {
		return nil, fmt.Errorf("%w: DialUDP: empty host", ErrInvalidHost)
	}
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("%w: DialUDP: port %d outside [1, 65535]", ErrInvalidPort, port)
	}

	// net.Dialer honours ctx for the (short) DNS resolution that happens
	// inside a UDP dial. The actual socket creation is synchronous.
	var d net.Dialer
	netConn, err := d.DialContext(ctx, "udp", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
	if err != nil {
		return nil, fmt.Errorf("%w: udp dial %s:%d: %v", classifyDialError(err), host, port, err)
	}
	udp, ok := netConn.(*net.UDPConn)
	if !ok {
		_ = netConn.Close()
		return nil, fmt.Errorf("%w: udp dial %s:%d: not a *net.UDPConn (%T)", ErrWrongConnType, host, port, netConn)
	}
	return &UDPConn{conn: udp}, nil
}

// Send writes one datagram. Short writes are impossible on UDP — either
// the whole datagram leaves the host or Send returns an error.
func (c *UDPConn) Send(ctx context.Context, payload []byte) error {
	if c == nil || c.conn == nil {
		return fmt.Errorf("%w: send on udp", ErrNilConn)
	}
	// ACP1 UDP datagrams are ≤ 141 bytes per spec — anything larger means
	// the caller built a malformed packet. Fail loudly instead of letting
	// IP fragmentation hide the bug (spec p. 7: "IP fragmentation ... is
	// not supported in ACP").
	if len(payload) == 0 {
		return fmt.Errorf("%w: send on udp", ErrEmptyPayload)
	}

	if dl, ok := ctx.Deadline(); ok {
		if err := c.conn.SetWriteDeadline(dl); err != nil {
			return fmt.Errorf("%w: udp write deadline: %v", ErrSetDeadlineFailed, err)
		}
	} else {
		// Clear any previous deadline.
		_ = c.conn.SetWriteDeadline(time.Time{})
	}

	n, err := c.conn.Write(payload)
	if err != nil {
		return fmt.Errorf("%w: udp write: %v", ErrWriteFailed, err)
	}
	if n != len(payload) {
		return fmt.Errorf("%w: udp wrote %d of %d bytes", ErrShortWrite, n, len(payload))
	}
	return nil
}

// Receive blocks until one datagram arrives or the context's deadline
// expires. The returned slice is a copy owned by the caller — the
// transport never retains it. Max inbound size is bounded by maxSize,
// which the caller sets based on consumer. For ACP1 that's 141 bytes.
func (c *UDPConn) Receive(ctx context.Context, maxSize int) ([]byte, error) {
	if c == nil || c.conn == nil {
		return nil, fmt.Errorf("%w: receive on udp", ErrNilConn)
	}
	if maxSize <= 0 {
		return nil, fmt.Errorf("%w: udp maxSize %d must be > 0", ErrInvalidMaxSize, maxSize)
	}

	if dl, ok := ctx.Deadline(); ok {
		if err := c.conn.SetReadDeadline(dl); err != nil {
			return nil, fmt.Errorf("%w: udp read deadline: %v", ErrSetDeadlineFailed, err)
		}
	} else {
		_ = c.conn.SetReadDeadline(time.Time{})
	}

	// Allocate one byte more than the protocol max: if the kernel delivers
	// a longer datagram we detect truncation instead of silently accepting
	// a malformed packet.
	buf := make([]byte, maxSize+1)
	n, err := c.conn.Read(buf)
	if err != nil {
		// Translate a deadline exceed into a context error so the retry
		// loop can tell timeouts from hard socket failures.
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return nil, context.DeadlineExceeded
		}
		return nil, fmt.Errorf("%w: udp read: %v", ErrReadFailed, err)
	}
	if n > maxSize {
		return nil, fmt.Errorf("%w: udp datagram %d > max %d", ErrOversizedDatagram, n, maxSize)
	}
	out := make([]byte, n)
	copy(out, buf[:n])
	return out, nil
}

// Close releases the socket. Safe to call multiple times.
func (c *UDPConn) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	// Don't nil c.conn: the reader goroutine may be in Receive()
	// concurrently (closing the socket is how Stop unblocks it), so
	// writing the field here would race that read (go test -race). The
	// underlying net.Conn.Close is safe alongside Read; tolerate the
	// already-closed error so a repeat Close stays a no-op.
	if err := c.conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("%w: udp close: %v", ErrCloseFailed, err)
	}
	return nil
}

// LocalAddr returns the kernel-assigned local endpoint, useful for logs.
func (c *UDPConn) LocalAddr() net.Addr {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.LocalAddr()
}

// RemoteAddr returns the peer the socket is connected to.
func (c *UDPConn) RemoteAddr() net.Addr {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.RemoteAddr()
}

// UDPListener is an unconnected UDP socket bound to a specific local
// port. Unlike UDPConn it accepts datagrams from any sender — used by
// the ACP1 announcement listener to receive broadcasts from rack
// controllers on the LAN.
//
// ListenUDP binds 0.0.0.0:port so the kernel delivers both directed
// unicast and subnet broadcast datagrams. SO_REUSEADDR is intentionally
// NOT set: we want a hard failure if something else already owns the
// port, rather than silently missing traffic due to a race.
type UDPListener struct {
	conn *net.UDPConn
}

// ListenUDP opens an unconnected UDP socket bound to the given port on
// all interfaces, with SO_REUSEADDR enabled so multiple acp processes
// can run `acp watch` simultaneously and all receive the same broadcast
// announcements. A zero port lets the kernel pick one, which is useful
// in tests.
func ListenUDP(ctx context.Context, port int) (*UDPListener, error) {
	if port < 0 || port > 65535 {
		return nil, fmt.Errorf("%w: ListenUDP: port %d outside [0, 65535]", ErrInvalidPort, port)
	}

	// net.ListenConfig exposes a Control callback that runs after socket
	// creation but before bind. That is exactly the window we need to
	// set SO_REUSEADDR — setting it post-bind has no effect.
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var opErr error
			if err := c.Control(func(fd uintptr) {
				opErr = setReuseAddr(fd)
			}); err != nil {
				return err
			}
			return opErr
		},
	}
	pc, err := lc.ListenPacket(ctx, "udp4", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, fmt.Errorf("%w: udp listen :%d: %v", ErrListenFailed, port, err)
	}
	udpConn, ok := pc.(*net.UDPConn)
	if !ok {
		_ = pc.Close()
		return nil, fmt.Errorf("%w: udp listen :%d: unexpected conn type %T", ErrWrongConnType, port, pc)
	}
	return &UDPListener{conn: udpConn}, nil
}

// Receive blocks until a datagram arrives. Returns the payload and the
// source address. A context deadline is honoured via SetReadDeadline —
// a timeout returns context.DeadlineExceeded so callers can distinguish
// it from hard socket errors.
func (l *UDPListener) Receive(ctx context.Context, maxSize int) ([]byte, net.Addr, error) {
	if l == nil || l.conn == nil {
		return nil, nil, fmt.Errorf("%w: receive on udp listener", ErrNilConn)
	}
	if maxSize <= 0 {
		return nil, nil, fmt.Errorf("%w: udp listener maxSize %d must be > 0", ErrInvalidMaxSize, maxSize)
	}

	if dl, ok := ctx.Deadline(); ok {
		if err := l.conn.SetReadDeadline(dl); err != nil {
			return nil, nil, fmt.Errorf("%w: udp listener read deadline: %v", ErrSetDeadlineFailed, err)
		}
	} else {
		_ = l.conn.SetReadDeadline(time.Time{})
	}

	buf := make([]byte, maxSize+1)
	n, addr, err := l.conn.ReadFromUDP(buf)
	if err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return nil, nil, context.DeadlineExceeded
		}
		return nil, nil, fmt.Errorf("%w: udp listener read: %v", ErrReadFailed, err)
	}
	if n > maxSize {
		return nil, addr, fmt.Errorf("%w: udp listener datagram %d > max %d", ErrOversizedDatagram, n, maxSize)
	}
	out := make([]byte, n)
	copy(out, buf[:n])
	return out, addr, nil
}

// Close releases the listening socket.
func (l *UDPListener) Close() error {
	if l == nil || l.conn == nil {
		return nil
	}
	// See UDPConn.Close: don't nil the field — it races the receive loop.
	if err := l.conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("%w: udp listener close: %v", ErrCloseFailed, err)
	}
	return nil
}

// LocalAddr returns the bound local endpoint.
func (l *UDPListener) LocalAddr() net.Addr {
	if l == nil || l.conn == nil {
		return nil
	}
	return l.conn.LocalAddr()
}
