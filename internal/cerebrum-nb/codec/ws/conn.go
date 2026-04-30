package ws

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
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

	// writeMu serialises all outbound frames so control + data frames
	// don't interleave on the wire.
	writeMu sync.Mutex

	closeOnce sync.Once
	closeErr  error
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
	for {
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

// newMaskKey returns 4 random bytes for client-to-server masking.
func newMaskKey() ([4]byte, error) {
	var k [4]byte
	_, err := rand.Read(k[:])
	return k, err
}
