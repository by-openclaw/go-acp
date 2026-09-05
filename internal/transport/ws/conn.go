package ws

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultMaxPayload caps incoming WebSocket frames at 16 MiB per the
// Cerebrum CLAUDE.md "RX max payload" rule. Larger frames trigger
// cerebrum_response_too_large in the consumer.
const DefaultMaxPayload int64 = 16 * 1024 * 1024

// Conn is a duplex WebSocket connection. Use ReadMessage / WriteText /
// Ping / Close. Concurrent readers are not supported; one TX-side and
// one RX-side goroutine is the expected pattern.
type Conn struct {
	c          net.Conn
	br         *bufio.Reader
	maxPayload int64

	// idleTimeout, when > 0, is the maximum time the peer may be silent
	// before a read fails. It is re-armed before EVERY frame read —
	// including control frames handled inline — so it means "no bytes at
	// all from the peer", not "no application message". That distinction
	// matters: a connection carrying only Pongs is alive, and must not be
	// declared dead by the watchdog it is answering.
	//
	// Without this, a read blocks forever on a half-open connection (a NAT
	// or firewall that dropped the flow without sending an RST), which is
	// exactly how a 24/7 watcher goes silent without crashing.
	idleTimeout atomic.Int64 // time.Duration

	// writeMu serialises all outbound frames so control + data frames
	// don't interleave on the wire.
	writeMu sync.Mutex

	closeOnce sync.Once
	closeErr  error
}

// SetIdleTimeout arms (d > 0) or disables (d <= 0) the per-frame read
// deadline. Safe to call concurrently with an in-flight ReadMessage; the new
// value applies from the next frame.
func (c *Conn) SetIdleTimeout(d time.Duration) {
	if d < 0 {
		d = 0
	}
	c.idleTimeout.Store(int64(d))
	// Apply immediately. A reader is normally already blocked in the kernel
	// on the PREVIOUS deadline, and storing the new value alone would not
	// reach it — the change would not take effect until that old (possibly
	// 90 s, possibly infinite) deadline expired. Re-arming the socket here
	// makes a tightened window take hold at once.
	if d > 0 {
		_ = c.c.SetReadDeadline(time.Now().Add(d))
	} else {
		_ = c.c.SetReadDeadline(time.Time{})
	}
}

// IdleTimeout reports the currently armed per-frame read deadline.
func (c *Conn) IdleTimeout() time.Duration {
	return time.Duration(c.idleTimeout.Load())
}

// newConn wraps a post-handshake net.Conn into a *Conn. br carries any
// bytes already buffered past the HTTP upgrade response.
func newConn(c net.Conn, br *bufio.Reader, maxPayload int64) *Conn {
	if maxPayload <= 0 {
		maxPayload = DefaultMaxPayload
	}
	return &Conn{c: c, br: br, maxPayload: maxPayload}
}

// LocalAddr / RemoteAddr expose the underlying transport.
func (c *Conn) LocalAddr() net.Addr  { return c.c.LocalAddr() }
func (c *Conn) RemoteAddr() net.Addr { return c.c.RemoteAddr() }

// SetReadDeadline / SetWriteDeadline pass through to the transport.
func (c *Conn) SetReadDeadline(t time.Time) error  { return c.c.SetReadDeadline(t) }
func (c *Conn) SetWriteDeadline(t time.Time) error { return c.c.SetWriteDeadline(t) }

// ReadMessage reads one application-level message. Control frames
// (Ping / Pong / Close) are handled inline: Ping triggers an automatic
// Pong, Pong is dropped, Close triggers a Close echo + io.EOF on the
// next call. The returned opcode is OpText or OpBinary; payload is
// the assembled (possibly de-fragmented) message body.
func (c *Conn) ReadMessage(ctx context.Context) (opcode byte, payload []byte, err error) {
	if d, ok := ctx.Deadline(); ok {
		_ = c.c.SetReadDeadline(d)
		defer func() { _ = c.c.SetReadDeadline(time.Time{}) }()
	}
	var (
		dataOpcode byte
		buf        []byte
	)
	// An explicit ctx deadline (a bounded request) wins over the rolling
	// idle timeout (an unbounded watch); only arm the idle deadline when the
	// caller did not set one of its own.
	_, ctxHasDeadline := ctx.Deadline()
	for {
		if !ctxHasDeadline {
			if d := c.IdleTimeout(); d > 0 {
				if err := c.c.SetReadDeadline(time.Now().Add(d)); err != nil {
					return 0, nil, err
				}
			}
		}
		f, err := readFrame(c.br, c.maxPayload)
		if err != nil {
			return 0, nil, err
		}
		switch f.opcode {
		case OpPing:
			if err := c.writeControl(OpPong, f.payload); err != nil {
				return 0, nil, err
			}
		case OpPong:
			// Drop.
		case OpClose:
			// Echo close. Body shape: 2-byte BE code + UTF-8 reason.
			_ = c.writeControl(OpClose, f.payload)
			c.closeUnderlying()
			return 0, nil, io.EOF
		case OpContinuation:
			if dataOpcode == 0 {
				return 0, nil, errors.New("ws: continuation without preceding data frame")
			}
			buf = append(buf, f.payload...)
			if f.fin {
				return dataOpcode, buf, nil
			}
		case OpText, OpBinary:
			if dataOpcode != 0 {
				return 0, nil, errors.New("ws: new data frame mid-fragmentation")
			}
			if f.fin {
				return f.opcode, f.payload, nil
			}
			dataOpcode = f.opcode
			buf = append(buf[:0], f.payload...)
		default:
			return 0, nil, fmt.Errorf("ws: unknown opcode %#x", f.opcode)
		}
	}
}

// WriteText sends payload as a single FIN'd text frame. Always masked
// per RFC 6455 §5.3 (client-to-server).
func (c *Conn) WriteText(ctx context.Context, payload []byte) error {
	return c.writeData(ctx, OpText, payload)
}

func (c *Conn) writeData(ctx context.Context, op byte, payload []byte) error {
	if d, ok := ctx.Deadline(); ok {
		_ = c.c.SetWriteDeadline(d)
		defer func() { _ = c.c.SetWriteDeadline(time.Time{}) }()
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	key, err := newMaskKey()
	if err != nil {
		return err
	}
	return writeFrame(c.c, true, op, payload, key, true)
}

// writeControl emits a control frame (Ping / Pong / Close). Body is
// always small (≤125), per RFC 6455 §5.5.
func (c *Conn) writeControl(op byte, payload []byte) error {
	if len(payload) > 125 {
		return fmt.Errorf("ws: control frame too large (%d)", len(payload))
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	key, err := newMaskKey()
	if err != nil {
		return err
	}
	return writeFrame(c.c, true, op, payload, key, true)
}

// Ping sends a Ping with the given payload (≤125 bytes).
func (c *Conn) Ping(_ context.Context, payload []byte) error {
	return c.writeControl(OpPing, payload)
}

// Close sends a Close frame with code + reason and tears down the
// transport. Subsequent calls return the first close error.
func (c *Conn) Close(code uint16, reason string) error {
	c.closeOnce.Do(func() {
		body := make([]byte, 2+len(reason))
		binary.BigEndian.PutUint16(body[:2], code)
		copy(body[2:], reason)
		if len(body) > 125 {
			body = body[:125]
		}
		if err := c.writeControl(OpClose, body); err != nil {
			c.closeErr = err
		}
		if err := c.c.Close(); err != nil && c.closeErr == nil {
			c.closeErr = err
		}
	})
	return c.closeErr
}

// closeUnderlying is the no-frame variant used after we received a Close
// from the peer (we already echoed it inline).
func (c *Conn) closeUnderlying() {
	c.closeOnce.Do(func() {
		c.closeErr = c.c.Close()
	})
}

// newMaskKey returns 4 random bytes for client-to-server masking. The
// crypto/rand read goes through the randRead seam (seam.go) so the
// otherwise-unreachable error arm in writeData / writeControl can be
// driven by a test without weakening the guard.
func newMaskKey() ([4]byte, error) {
	var k [4]byte
	_, err := randRead(k[:])
	return k, err
}
