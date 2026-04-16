package transport

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
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
		return nil, fmt.Errorf("transport: DialTCP: empty host")
	}
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("transport: DialTCP: port out of range: %d", port)
	}
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
	if err != nil {
		return nil, fmt.Errorf("tcp dial %s:%d: %w", host, port, err)
	}
	tc, ok := c.(*net.TCPConn)
	if !ok {
		_ = c.Close()
		return nil, fmt.Errorf("tcp dial %s:%d: not a *net.TCPConn (%T)", host, port, c)
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
		return errors.New("tcp: send on nil conn")
	}
	if len(payload) == 0 {
		return errors.New("tcp: send empty payload")
	}
	if len(payload) > 0xFFFFFFFF {
		return fmt.Errorf("tcp: payload too large: %d", len(payload))
	}

	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	if dl, ok := ctx.Deadline(); ok {
		if err := t.conn.SetWriteDeadline(dl); err != nil {
			return fmt.Errorf("tcp set write deadline: %w", err)
		}
	} else {
		_ = t.conn.SetWriteDeadline(time.Time{})
	}

	// Single WriteVector would avoid two syscalls, but the payload is so
	// small (<150 bytes) that it doesn't matter. Two Writes is simpler
	// and still one TCP segment in practice thanks to NODELAY.
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(payload)))
	if _, err := t.conn.Write(lenBuf[:]); err != nil {
		return fmt.Errorf("tcp write len: %w", err)
	}
	if _, err := t.conn.Write(payload); err != nil {
		return fmt.Errorf("tcp write payload: %w", err)
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
		return nil, errors.New("tcp: receive on nil conn")
	}
	if maxPayload <= 0 {
		return nil, fmt.Errorf("tcp: invalid maxPayload %d", maxPayload)
	}

	if dl, ok := ctx.Deadline(); ok {
		if err := t.conn.SetReadDeadline(dl); err != nil {
			return nil, fmt.Errorf("tcp set read deadline: %w", err)
		}
	} else {
		_ = t.conn.SetReadDeadline(time.Time{})
	}

	// Read MLEN (4 bytes, big-endian).
	var lenBuf [4]byte
	if _, err := io.ReadFull(t.conn, lenBuf[:]); err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return nil, context.DeadlineExceeded
		}
		return nil, fmt.Errorf("tcp read len: %w", err)
	}
	mlen := binary.BigEndian.Uint32(lenBuf[:])

	// Spec: MLEN > 8 for any valid ACP1 TCP message. We enforce a
	// floor of 8 (one-byte-MDATA error reply) and a ceiling of
	// maxPayload. Anything outside is a framing error — resync would
	// be guesswork, so we return the error and let the client
	// reconnect if it wants.
	if mlen < 8 {
		return nil, fmt.Errorf("tcp: MLEN %d below minimum 8", mlen)
	}
	if int(mlen) > maxPayload {
		return nil, fmt.Errorf("tcp: MLEN %d > max %d", mlen, maxPayload)
	}

	payload := make([]byte, mlen)
	if _, err := io.ReadFull(t.conn, payload); err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return nil, context.DeadlineExceeded
		}
		return nil, fmt.Errorf("tcp read payload: %w", err)
	}
	return payload, nil
}

// Close releases the TCP socket.
func (t *TCPConn) Close() error {
	if t == nil || t.conn == nil {
		return nil
	}
	err := t.conn.Close()
	t.conn = nil
	if err != nil {
		return fmt.Errorf("tcp close: %w", err)
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
