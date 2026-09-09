package transport

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"sync"
	"time"
)

// TCPConn is an MLEN-framed TCP connection used by ACP1 TCP direct mode
// and (in a later commit) by the AN2 transport for ACP2.
//
// Wire framing per ACP1 spec §"ACP Header" p. 10:
//
//	MLEN  u32 big-endian   byte count starting at MTID
//	...   payload          the full ACP header + MDATA
//
// TCPConn knows nothing about ACP contents. It moves framed byte blobs
// on and off a TCP connection. Higher layers (acp1.TCPClient) decode
// the payload and handle multiplexing replies vs announcements.
//
// Safe for one writer goroutine and one reader goroutine concurrently.
// The internal write mutex serialises multiple writers if the caller
// chooses to share the conn.
type TCPConn struct {
	conn    *net.TCPConn
	writeMu sync.Mutex
}

// DialTCP opens a TCP connection to host:port with the supplied context
// honoured for the connect handshake. The socket is returned in nagle-off
// mode (TCP_NODELAY) so small ACP1 messages don't sit in the kernel's
// coalesce buffer waiting for more data.
func DialTCP(ctx context.Context, host string, port int) (*TCPConn, error) {
	if host == "" {
		return nil, fmt.Errorf("%w: DialTCP: empty host", ErrInvalidHost)
	}
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("%w: DialTCP: port %d outside [1, 65535]", ErrInvalidPort, port)
	}
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
	if err != nil {
		return nil, fmt.Errorf("%w: dial %s:%d: %v", classifyDialError(err), host, port, err)
	}
	tc, ok := dialTCPAssert(c)
	if !ok {
		_ = c.Close()
		return nil, fmt.Errorf("%w: dial %s:%d: not a *net.TCPConn (%T)", ErrWrongConnType, host, port, c)
	}
	// ACP messages are small (≤ 141 bytes) and latency-sensitive. Disable
	// Nagle so we don't sit 40 ms in a send buffer waiting for an ACK.
	_ = tc.SetNoDelay(true)
	return &TCPConn{conn: tc}, nil
}

// Send writes one MLEN-framed message. The payload MUST be the full
// ACP header starting at MTID (7 bytes) plus MDATA — everything the
// receiver needs after stripping the 4-byte MLEN prefix.
func (t *TCPConn) Send(ctx context.Context, payload []byte) error {
	if t == nil || t.conn == nil {
		return fmt.Errorf("%w: send on tcp", ErrNilConn)
	}
	if len(payload) == 0 {
		return fmt.Errorf("%w: send on tcp", ErrEmptyPayload)
	}
	mlen, err := mlenFor(len(payload))
	if err != nil {
		return err
	}

	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	if dl, ok := ctx.Deadline(); ok {
		if err := t.conn.SetWriteDeadline(dl); err != nil {
			return fmt.Errorf("%w: tcp write deadline: %v", ErrSetDeadlineFailed, err)
		}
	} else {
		_ = t.conn.SetWriteDeadline(time.Time{})
	}

	// Single WriteVector would avoid two syscalls, but the payload is so
	// small (<150 bytes) that it doesn't matter. Two Writes is simpler
	// and still one TCP segment in practice thanks to NODELAY.
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], mlen)
	if _, err := tcpWrite(t.conn, lenBuf[:]); err != nil {
		return fmt.Errorf("%w: tcp write len: %v", ErrWriteFailed, err)
	}
	if _, err := tcpWrite(t.conn, payload); err != nil {
		return fmt.Errorf("%w: tcp write payload: %v", ErrWriteFailed, err)
	}
	return nil
}

// Receive blocks until one framed message arrives, then returns the
// payload bytes (without the MLEN prefix). A caller-supplied maxPayload
// caps acceptable frame sizes so a malicious or buggy sender cannot
// force unbounded allocations.
//
// Honours ctx deadlines via SetReadDeadline. On timeout returns
// context.DeadlineExceeded so the acp1 client can distinguish it from
// hard socket errors.
func (t *TCPConn) Receive(ctx context.Context, maxPayload int) ([]byte, error) {
	if t == nil || t.conn == nil {
		return nil, fmt.Errorf("%w: receive on tcp", ErrNilConn)
	}
	if maxPayload <= 0 {
		return nil, fmt.Errorf("%w: tcp maxPayload %d must be > 0", ErrInvalidMaxSize, maxPayload)
	}

	if dl, ok := ctx.Deadline(); ok {
		if err := t.conn.SetReadDeadline(dl); err != nil {
			return nil, fmt.Errorf("%w: tcp read deadline: %v", ErrSetDeadlineFailed, err)
		}
	} else {
		_ = t.conn.SetReadDeadline(time.Time{})
	}

	// A deadline covers a timeout; it does not cover a CANCEL, because the
	// socket knows nothing about the context. See cancel.go.
	stop := watchCancel(ctx, t.conn)
	defer stop()

	// Read MLEN (4 bytes, big-endian).
	var lenBuf [4]byte
	if _, err := readFull(t.conn, lenBuf[:]); err != nil {
		if cerr := cancelledReadErr(ctx, err); cerr != nil {
			return nil, cerr
		}
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return nil, context.DeadlineExceeded
		}
		return nil, fmt.Errorf("%w: tcp read len: %v", ErrReadFailed, err)
	}
	mlen := binary.BigEndian.Uint32(lenBuf[:])

	// Spec: MLEN > 8 for any valid ACP1 TCP message. We enforce a
	// floor of 8 (one-byte-MDATA error reply) and a ceiling of
	// maxPayload. Anything outside is a framing error — resync would
	// be guesswork, so we return the error and let the client
	// reconnect if it wants.
	if mlen < 8 {
		return nil, fmt.Errorf("%w: tcp MLEN %d below minimum 8", ErrMLENOutOfRange, mlen)
	}
	if int(mlen) > maxPayload {
		return nil, fmt.Errorf("%w: tcp MLEN %d above max %d", ErrMLENOutOfRange, mlen, maxPayload)
	}

	payload := make([]byte, mlen)
	if _, err := readFull(t.conn, payload); err != nil {
		if cerr := cancelledReadErr(ctx, err); cerr != nil {
			return nil, cerr
		}
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return nil, context.DeadlineExceeded
		}
		return nil, fmt.Errorf("%w: tcp read payload: %v", ErrReadFailed, err)
	}
	return payload, nil
}

// Close releases the TCP socket.
func (t *TCPConn) Close() error {
	if t == nil || t.conn == nil {
		return nil
	}
	// Don't nil t.conn: the reader goroutine may be in Receive()
	// concurrently; writing the field here would race that read
	// (go test -race). Tolerate the already-closed error for idempotency.
	if err := closeConn(t.conn); err != nil && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("%w: tcp close: %v", ErrCloseFailed, err)
	}
	return nil
}

// RemoteAddr returns the peer endpoint, useful for logs.
func (t *TCPConn) RemoteAddr() net.Addr {
	if t == nil || t.conn == nil {
		return nil
	}
	return t.conn.RemoteAddr()
}

// Test seams, in the transparent-package-var pattern listen.go uses: each
// holds the real implementation, is never reassigned in production, and is
// swapped for one test with a deferred restore.
//
// Why each arm cannot be reached on a live socket:
//   - dialTCPAssert: a "tcp" dial always yields a *net.TCPConn.
//   - mlenFor: the overflow arm needs a payload above 4 GiB. The arithmetic
//     is real and tested with that length directly; the seam is only so
//     Send's propagation of the refusal can be proven without allocating
//     it. On a 32-bit int the comparison is simply never true.
//   - tcpWrite / readFull: the MLEN and payload writes (and reads) are back
//     to back, so a cancel or an expiring deadline cannot be placed BETWEEN
//     them on purpose — only by racing a timer or a peer reset against the
//     first call, which is the sleep-as-synchronisation a deterministic
//     test must not depend on. Stalling the peer is no substitute either:
//     Windows loopback absorbs tens of megabytes without blocking the
//     sender. Both seams keep the real call and let a test act in the gap.
//   - closeConn: a second Close on a real socket reports net.ErrClosed,
//     which Close tolerates by design; no fd state yields any OTHER error.
//     Shared with the UDP types, which have the same arm for the same reason.
var (
	dialTCPAssert = func(c net.Conn) (*net.TCPConn, bool) {
		tc, ok := c.(*net.TCPConn)
		return tc, ok
	}
	// mlenFor converts a payload length into the u32 MLEN the wire carries
	// (ACP1 spec §"ACP Header" p. 10), refusing one that does not fit.
	mlenFor = func(n int) (uint32, error) {
		if uint64(n) > math.MaxUint32 {
			return 0, fmt.Errorf("%w: tcp payload %d > 4GiB", ErrPayloadTooLarge, n)
		}
		return uint32(n), nil
	}
	tcpWrite  = (*net.TCPConn).Write
	readFull  = io.ReadFull
	closeConn = func(c net.Conn) error { return c.Close() }
)
