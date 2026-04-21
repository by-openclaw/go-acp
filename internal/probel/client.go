package probel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"
)

// DefaultDialTimeout caps how long Client.Dial waits for a TCP connect.
const DefaultDialTimeout = 5 * time.Second

// DefaultReadBufferSize is the capacity of the accumulating read buffer.
// SW-P-08 frames are small (<300 bytes in the largest tally dump); 4 KiB
// is plenty for one or two in-flight frames.
const DefaultReadBufferSize = 4096

// Client is the TCP transport around the Probel SW-P-08 codec. It owns
// the connection, runs a single reader goroutine that demultiplexes
// incoming frames into (a) replies correlated with an outstanding Send
// call and (b) asynchronous events broadcast to registered listeners.
//
// SW-P-08 has no message-id field: replies are identified by their CMD
// byte (e.g. RxCrosspointConnect=0x02 yields TxCrosspointConnected=0x04).
// Client.Send therefore takes an expected-reply matcher rather than a
// correlation id.
type Client struct {
	logger *slog.Logger

	mu      sync.Mutex
	conn    net.Conn
	closed  bool
	readers []eventFunc    // async-event listeners (tallies, unsolicited)
	pending *pendingWaiter // single-flight reply waiter

	// readerDone is closed when the reader goroutine exits.
	readerDone chan struct{}

	// wireHexLog, when true, logs every Pack() send and every Unpack()
	// receive as space-separated lowercase hex at INFO level. Useful for
	// debugging; off-path for operational code.
	wireHexLog bool

	// onTx / onRx are optional observer callbacks invoked with the raw
	// wire bytes (pre-escape on send, exactly-received on receive). Used
	// by higher layers to plug in traffic capture or metrics without
	// coupling this package to any capture implementation. Empty by
	// default — keeps this package stdlib-only.
	onTx func([]byte)
	onRx func([]byte)
}

// eventFunc is an async-event callback. Listeners receive every frame
// that isn't claimed by a pending Send matcher (typically tallies).
type eventFunc func(Frame)

// pendingWaiter captures a single in-flight Send expecting a reply.
// SW-P-08 is half-duplex per logical transaction: we only allow one
// outstanding request at a time. Multiple concurrent Send calls serialise
// through Client.mu for the duration of the request/reply round-trip.
type pendingWaiter struct {
	match  func(Frame) bool
	result chan replyResult
}

type replyResult struct {
	frame Frame
	err   error
}

// ClientConfig tunes the Client. Zero value is fine for most callers.
type ClientConfig struct {
	// DialTimeout bounds the TCP connect. Defaults to DefaultDialTimeout.
	DialTimeout time.Duration
	// ReadBufferSize is the accumulating read-loop buffer. Defaults to
	// DefaultReadBufferSize.
	ReadBufferSize int
	// WireHexLog enables "probel TX/RX: <hex>" INFO logs of every
	// framed exchange. Defaults to true; useful during development.
	WireHexLog *bool
	// OnTx / OnRx are optional raw-byte observer callbacks invoked on
	// every send and receive respectively. Kept as plain funcs to
	// avoid pulling any other acp package into this codec — callers
	// wire capture / metrics at their own layer.
	OnTx func([]byte)
	OnRx func([]byte)
}

// Dial opens a TCP connection to addr (host:port) and starts the reader
// goroutine. The returned Client is ready for Send / Subscribe.
func Dial(ctx context.Context, addr string, logger *slog.Logger, cfg ClientConfig) (*Client, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = DefaultDialTimeout
	}
	if cfg.ReadBufferSize <= 0 {
		cfg.ReadBufferSize = DefaultReadBufferSize
	}
	hex := true
	if cfg.WireHexLog != nil {
		hex = *cfg.WireHexLog
	}

	d := net.Dialer{Timeout: cfg.DialTimeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("probel dial %s: %w", addr, err)
	}
	c := &Client{
		logger:     logger,
		conn:       conn,
		readerDone: make(chan struct{}),
		wireHexLog: hex,
		onTx:       cfg.OnTx,
		onRx:       cfg.OnRx,
	}
	go c.readLoop(cfg.ReadBufferSize)
	c.logger.Info("probel client connected",
		slog.String("remote", conn.RemoteAddr().String()),
	)
	return c, nil
}

// NewClientFromConn wraps an already-connected net.Conn in a Client. Used
// by loopback tests where the caller supplies both ends of a net.Pipe.
func NewClientFromConn(conn net.Conn, logger *slog.Logger, cfg ClientConfig) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.ReadBufferSize <= 0 {
		cfg.ReadBufferSize = DefaultReadBufferSize
	}
	hex := true
	if cfg.WireHexLog != nil {
		hex = *cfg.WireHexLog
	}
	c := &Client{
		logger:     logger,
		conn:       conn,
		readerDone: make(chan struct{}),
		wireHexLog: hex,
		onTx:       cfg.OnTx,
		onRx:       cfg.OnRx,
	}
	go c.readLoop(cfg.ReadBufferSize)
	return c
}

// Close stops the reader goroutine and releases the socket. Safe to
// call multiple times.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	conn := c.conn
	pending := c.pending
	c.pending = nil
	c.mu.Unlock()

	if pending != nil {
		select {
		case pending.result <- replyResult{err: io.EOF}:
		default:
		}
	}
	var err error
	if conn != nil {
		err = conn.Close()
	}
	<-c.readerDone
	return err
}

// Subscribe registers an async-event listener. Frames that aren't claimed
// by an outstanding Send matcher are delivered to every listener in the
// order they were registered. The listener MUST NOT block.
func (c *Client) Subscribe(fn eventFunc) {
	c.mu.Lock()
	c.readers = append(c.readers, fn)
	c.mu.Unlock()
}

// Send writes one framed command, optionally waits for a matching reply,
// and returns it. If match is nil the call returns as soon as the write
// succeeds and the second return value is the zero Frame.
//
// A non-nil match is called for every inbound frame; the first frame for
// which match returns true is the reply. Frames that don't match are
// broadcast to Subscribe listeners.
//
// The call blocks at most until ctx expires.
func (c *Client) Send(ctx context.Context, f Frame, match func(Frame) bool) (Frame, error) {
	raw := Pack(f)
	c.mu.Lock()
	if c.closed || c.conn == nil {
		c.mu.Unlock()
		return Frame{}, net.ErrClosed
	}
	if c.pending != nil {
		c.mu.Unlock()
		return Frame{}, errors.New("probel: another Send already in flight")
	}
	var waiter *pendingWaiter
	if match != nil {
		waiter = &pendingWaiter{match: match, result: make(chan replyResult, 1)}
		c.pending = waiter
	}
	conn := c.conn
	hex := c.wireHexLog
	c.mu.Unlock()

	if hex {
		c.logger.Info("probel TX",
			slog.Int("cmd", int(f.ID)),
			slog.Int("payload_len", len(f.Payload)),
			slog.Int("wire_len", len(raw)),
			slog.String("hex", HexDump(raw)),
		)
	}
	if c.onTx != nil {
		c.onTx(raw)
	}
	if _, err := conn.Write(raw); err != nil {
		if waiter != nil {
			c.mu.Lock()
			c.pending = nil
			c.mu.Unlock()
		}
		return Frame{}, fmt.Errorf("probel write: %w", err)
	}
	if waiter == nil {
		return Frame{}, nil
	}

	select {
	case r := <-waiter.result:
		return r.frame, r.err
	case <-ctx.Done():
		c.mu.Lock()
		if c.pending == waiter {
			c.pending = nil
		}
		c.mu.Unlock()
		return Frame{}, ctx.Err()
	}
}

// Write sends a pre-built raw byte sequence (typically a DLE ACK / DLE
// NAK). Bypasses Pack. Used by servers echoing ACKs for received frames.
func (c *Client) Write(raw []byte) error {
	c.mu.Lock()
	if c.closed || c.conn == nil {
		c.mu.Unlock()
		return net.ErrClosed
	}
	conn := c.conn
	onTx := c.onTx
	c.mu.Unlock()
	if onTx != nil {
		onTx(raw)
	}
	_, err := conn.Write(raw)
	return err
}

// readLoop accumulates bytes from the connection, decodes frames via
// Unpack, and routes them. Per SW-P-08 §2 (Transmission Protocol):
//   - well-framed inbound frame   -> emit DLE ACK back to peer
//   - bad checksum / framing      -> emit DLE NAK back to peer
//   - inbound DLE ACK / DLE NAK   -> log and swallow (they're confirming
//                                    a frame WE sent, not triggering any
//                                    reply on the wire)
//
// Both sides must ACK every valid frame — a matrix emitting a
// Crosspoint Tally expects the controller to ACK it, and vice-versa.
func (c *Client) readLoop(bufSize int) {
	defer close(c.readerDone)
	buf := make([]byte, 0, bufSize)
	tmp := make([]byte, bufSize)

	for {
		n, err := c.conn.Read(tmp)
		if err != nil {
			c.logger.Debug("probel reader exit", slog.String("err", err.Error()))
			c.failPending(err)
			return
		}
		buf = append(buf, tmp[:n]...)

		for len(buf) >= 2 {
			// Control sequences DLE ACK / DLE NAK are 2 bytes.
			if IsACK(buf) {
				c.logger.Debug("probel rx ACK")
				if c.onRx != nil {
					c.onRx(buf[:2])
				}
				buf = buf[2:]
				continue
			}
			if IsNAK(buf) {
				c.logger.Warn("probel rx NAK")
				if c.onRx != nil {
					c.onRx(buf[:2])
				}
				buf = buf[2:]
				continue
			}
			if buf[0] != DLE {
				// Desynced — drop one byte and try again.
				c.logger.Warn("probel rx desync: dropping byte",
					slog.String("byte", fmt.Sprintf("%02x", buf[0])))
				buf = buf[1:]
				continue
			}

			f, consumed, perr := Unpack(buf)
			if errors.Is(perr, io.ErrUnexpectedEOF) {
				break // need more bytes
			}
			if perr != nil {
				c.logger.Warn("probel rx decode — emitting DLE NAK",
					slog.String("err", perr.Error()),
					slog.String("hex", HexDump(buf[:min(len(buf), 64)])))
				// SW-P-08 §2 (Transmission Protocol) — bad frame, negative acknowledge.
				_ = c.Write(PackNAK())
				// Drop SOM and resync.
				buf = buf[2:]
				continue
			}
			if c.wireHexLog {
				c.logger.Info("probel RX",
					slog.Int("cmd", int(f.ID)),
					slog.Int("payload_len", len(f.Payload)),
					slog.Int("wire_len", consumed),
					slog.String("hex", HexDump(buf[:consumed])),
				)
			}
			if c.onRx != nil {
				c.onRx(buf[:consumed])
			}
			// SW-P-08 §2 (Transmission Protocol) — always ACK a valid frame so the peer can
			// free its retransmit buffer.
			_ = c.Write(PackACK())
			buf = buf[consumed:]
			c.dispatch(f)
		}
	}
}

// dispatch routes a decoded frame: a pending Send's matcher gets first
// look; unclaimed frames fan out to async listeners.
func (c *Client) dispatch(f Frame) {
	c.mu.Lock()
	waiter := c.pending
	listeners := append([]eventFunc(nil), c.readers...)
	if waiter != nil && waiter.match(f) {
		c.pending = nil
		c.mu.Unlock()
		waiter.result <- replyResult{frame: f}
		return
	}
	c.mu.Unlock()
	for _, fn := range listeners {
		fn(f)
	}
}

// failPending wakes any in-flight Send with err when the reader exits.
func (c *Client) failPending(err error) {
	c.mu.Lock()
	waiter := c.pending
	c.pending = nil
	c.mu.Unlock()
	if waiter != nil {
		waiter.result <- replyResult{err: err}
	}
}

// HexDump formats bytes as space-separated 2-digit lowercase hex — the
// format SW-P-88 spec examples use and the convention most byte-view
// tools recognise.
//
// Example: [0x10 0x02 0x01 …] → "10 02 01 02 00 05 0c 03 1f 10 03".
func HexDump(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	const hex = "0123456789abcdef"
	out := make([]byte, 0, len(b)*3-1)
	for i, x := range b {
		if i > 0 {
			out = append(out, ' ')
		}
		out = append(out, hex[x>>4], hex[x&0x0F])
	}
	return string(out)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
