package codec

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
// SW-P-02 frames are small (§3.1 command table tops out under 256 bytes
// per frame); 4 KiB comfortably holds one or two in-flight frames.
const DefaultReadBufferSize = 4096

// Send errors surfaced to callers.
var (
	// ErrSendInFlight means another Send is already awaiting a reply on
	// this client. SW-P-02 is half-duplex per logical transaction —
	// serialise or use a second Client instance.
	ErrSendInFlight = errors.New("probel-sw02p: another Send already in flight")
)

// Client is the TCP transport around the SW-P-02 codec. It owns the
// connection, runs a single reader goroutine that decodes inbound
// frames and routes them to (a) the current in-flight Send's matcher
// or (b) every registered async-event listener.
//
// SW-P-02 has no message-id correlation field: replies are identified
// by their COMMAND byte, so Client.Send takes an expected-reply matcher
// rather than a correlation id.
//
// ACK / NAK semantics differ from SW-P-08 — in SW-P-02 acknowledgement
// lives at the application command layer (specific commands), not at
// the framing layer. The client therefore does NOT auto-emit framing-
// level ACKs. Per-command files in follow-up commits wire any
// application-level confirm traffic via Subscribe.
type Client struct {
	logger *slog.Logger

	mu      sync.Mutex
	conn    net.Conn
	closed  bool
	readers []eventFunc    // async-event listeners
	pending *pendingWaiter // single-flight reply waiter

	// readerDone is closed when the reader goroutine exits.
	readerDone chan struct{}

	// wireHexLog, when true, logs every TX/RX frame as space-separated
	// lowercase hex at INFO level.
	wireHexLog bool

	// Observer callbacks — stdlib-only hooks for higher layers to plug
	// in traffic capture, metrics, or compliance counters without
	// coupling this package to any specific implementation.
	onTx func([]byte)
	onRx func([]byte)
}

// eventFunc is an async-event callback. Listeners receive every frame
// that isn't claimed by a pending Send matcher.
type eventFunc func(Frame)

// pendingWaiter captures a single in-flight Send.
type pendingWaiter struct {
	match func(Frame) bool
	reply chan replyResult
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
	// WireHexLog enables "probel-sw02p TX/RX: <hex>" INFO logs of every
	// framed exchange. Defaults to true; useful during development.
	WireHexLog *bool

	// OnTx / OnRx are optional raw-byte observer callbacks invoked on
	// every send and receive respectively.
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
	d := net.Dialer{Timeout: cfg.DialTimeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("probel-sw02p dial %s: %w", addr, err)
	}
	c := newClient(conn, logger, cfg)
	go c.readLoop(cfg.ReadBufferSize)
	c.logger.Info("probel-sw02p client connected",
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
	c := newClient(conn, logger, cfg)
	go c.readLoop(cfg.ReadBufferSize)
	return c
}

func newClient(conn net.Conn, logger *slog.Logger, cfg ClientConfig) *Client {
	hex := true
	if cfg.WireHexLog != nil {
		hex = *cfg.WireHexLog
	}
	return &Client{
		logger:     logger,
		conn:       conn,
		readerDone: make(chan struct{}),
		wireHexLog: hex,
		onTx:       cfg.OnTx,
		onRx:       cfg.OnRx,
	}
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
		case pending.reply <- replyResult{err: io.EOF}:
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

// Subscribe registers an async-event listener. Frames that aren't
// claimed by an outstanding Send matcher are delivered to every
// listener in the order they were registered. The listener MUST NOT
// block.
func (c *Client) Subscribe(fn eventFunc) {
	c.mu.Lock()
	c.readers = append(c.readers, fn)
	c.mu.Unlock()
}

// Send writes one framed command and optionally waits for a matching
// reply. If match is nil, Send returns as soon as the frame is written.
//
// The returned Frame is the zero value when match is nil.
//
// Errors:
//   - ErrSendInFlight if another Send is already waiting on this Client.
//   - ctx.Err() if ctx expires before the peer replies.
//   - any net I/O error from the underlying conn.
func (c *Client) Send(ctx context.Context, f Frame, match func(Frame) bool) (Frame, error) {
	raw := Pack(f)

	waiter := &pendingWaiter{
		match: match,
		reply: make(chan replyResult, 1),
	}
	c.mu.Lock()
	if c.closed || c.conn == nil {
		c.mu.Unlock()
		return Frame{}, net.ErrClosed
	}
	if c.pending != nil {
		c.mu.Unlock()
		return Frame{}, ErrSendInFlight
	}
	c.pending = waiter
	conn := c.conn
	hex := c.wireHexLog
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		if c.pending == waiter {
			c.pending = nil
		}
		c.mu.Unlock()
	}()

	if hex {
		c.logger.Info("probel-sw02p TX",
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
		return Frame{}, fmt.Errorf("probel-sw02p write: %w", err)
	}

	if match == nil {
		return Frame{}, nil
	}
	select {
	case r := <-waiter.reply:
		return r.frame, r.err
	case <-ctx.Done():
		return Frame{}, ctx.Err()
	}
}

// Write sends a pre-built raw byte sequence. Bypasses Pack. Used by
// higher layers that need to emit a specific wire sequence verbatim.
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

// readLoop accumulates bytes from the connection, decodes frames via a
// length-unaware primitive scan, and routes them. Because SW-P-02 has
// no in-frame length field, the scaffold implementation relies on
// per-command decoders to know how many bytes to consume — the reader
// therefore only matches SOM-prefixed runs and feeds decoded frames
// through when a whole one has arrived.
//
// Follow-up per-command commits will wire a per-CMD length table so
// the reader can chunk the stream deterministically.
func (c *Client) readLoop(bufSize int) {
	defer close(c.readerDone)
	buf := make([]byte, 0, bufSize)
	tmp := make([]byte, bufSize)

	for {
		n, err := c.conn.Read(tmp)
		if err != nil {
			c.logger.Debug("probel-sw02p reader exit", slog.String("err", err.Error()))
			c.failPending(err)
			return
		}
		buf = append(buf, tmp[:n]...)

		for len(buf) >= 3 {
			if buf[0] != SOM {
				c.logger.Warn("probel-sw02p rx desync: dropping byte",
					slog.String("byte", fmt.Sprintf("%02x", buf[0])))
				buf = buf[1:]
				continue
			}
			// Without per-command length info the primitive decoder
			// treats the entire accumulated buffer as one frame.
			// This is correct when frames arrive as discrete packets
			// and a single command's MESSAGE length is known by the
			// peer — the scaffold matches this minimal case; per-
			// command commits add streaming decode.
			f, consumed, perr := Unpack(buf)
			if errors.Is(perr, io.ErrUnexpectedEOF) {
				break
			}
			if perr != nil {
				c.logger.Warn("probel-sw02p rx decode error",
					slog.String("err", perr.Error()),
					slog.String("hex", HexDump(buf[:min(len(buf), 64)])))
				// Without per-command length we cannot resync
				// precisely; drop one byte and retry.
				buf = buf[1:]
				continue
			}
			if c.wireHexLog {
				c.logger.Info("probel-sw02p RX",
					slog.Int("cmd", int(f.ID)),
					slog.Int("payload_len", len(f.Payload)),
					slog.Int("wire_len", consumed),
					slog.String("hex", HexDump(buf[:consumed])),
				)
			}
			if c.onRx != nil {
				c.onRx(buf[:consumed])
			}
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
	c.mu.Unlock()

	if waiter != nil && waiter.match != nil && waiter.match(f) {
		select {
		case waiter.reply <- replyResult{frame: f}:
			return
		default:
			// reply slot already filled — fall through.
		}
	}
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
		select {
		case waiter.reply <- replyResult{err: err}:
		default:
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
