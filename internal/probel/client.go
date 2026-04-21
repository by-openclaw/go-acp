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

// DefaultACKTimeout is the per-attempt wait for a peer's DLE ACK / DLE NAK
// response. SW-P-08 §2 specifies a "notional 1 second timeout" for the
// low-level acknowledgement, noted as ideally <=10 ms in practice but
// 1 s being the contract.
const DefaultACKTimeout = 1 * time.Second

// DefaultMaxAttempts is the "5 times retry procedure" from SW-P-08 §2.
// Counts the original send; so 5 = send + up to 4 retries. The spec
// wording is ambiguous ("5 times retry") but the TS emulator and real
// matrices treat 5 as the total attempt ceiling.
const DefaultMaxAttempts = 5

// DataCapSoft is the guaranteed-portable DATA-field size per SW-P-08 §2:
// "The maximum size of the DATA field (before DLE padding) that is
// guaranteed to work with all systems is 128 bytes". Frames with a
// DATA field between 129 and 255 bytes are "custom applications" and
// fire the OnCapSoft compliance callback — they are allowed but may
// trip bugs in third-party controllers.
const DataCapSoft = 128

// DataCapHard is the absolute ceiling on DATA (ID + Payload): BTC is a
// single byte (u8) so 255 is the physical limit. Pack refuses anything
// larger because it would overflow the byte count.
const DataCapHard = 255

// Send errors surfaced to callers.
var (
	// ErrDataFieldTooLarge means f.Payload + ID byte exceeds DataCapHard.
	// No write happens; caller must shrink the frame (split, paginate, ...).
	ErrDataFieldTooLarge = errors.New("probel: DATA field > 255 bytes (BTC u8 overflow)")

	// ErrMaxAttempts means the peer NAKed or ignored all DefaultMaxAttempts
	// attempts to deliver the frame. Per SW-P-08 §2 "5 times retry".
	ErrMaxAttempts = errors.New("probel: max retry attempts exceeded (SW-P-08 §2)")

	// ErrSendInFlight means another Send is already awaiting ACK on this
	// client. SW-P-08 is half-duplex per logical transaction — serialise
	// or use a second Client instance.
	ErrSendInFlight = errors.New("probel: another Send already in flight")
)

// Client is the TCP transport around the Probel SW-P-08 codec. It owns
// the connection, runs a single reader goroutine that demultiplexes
// incoming bytes into (a) DLE ACK / DLE NAK signals for the current
// in-flight Send, (b) replies correlated with an outstanding Send's
// matcher, and (c) asynchronous events broadcast to Subscribe listeners.
//
// SW-P-08 has no message-id field: replies are identified by their CMD
// byte (e.g. RxCrosspointConnect=0x02 yields TxCrosspointConnected=0x04).
// Client.Send therefore takes an expected-reply matcher rather than a
// correlation id.
//
// Retry + timeout policy (SW-P-08 §2):
//   - After writing a frame, wait up to ACKTimeout (1 s default) for
//     DLE ACK.
//   - On DLE NAK: retry, up to MaxAttempts (5 default).
//   - On ack-timeout: retry, up to MaxAttempts (5 default).
//   - Once ACK is observed, wait for the matching reply bounded only
//     by the caller's context.
type Client struct {
	logger *slog.Logger

	mu      sync.Mutex
	conn    net.Conn
	closed  bool
	readers []eventFunc    // async-event listeners (tallies, unsolicited)
	pending *pendingWaiter // single-flight reply waiter

	// readerDone is closed when the reader goroutine exits.
	readerDone chan struct{}

	// wireHexLog, when true, logs every TX/RX frame as space-separated
	// lowercase hex at INFO level. Useful for debugging; off-path for
	// operational code.
	wireHexLog bool

	// Retry + timeout knobs — read under Client.mu at Send time.
	ackTimeout  time.Duration
	maxAttempts int

	// Observer callbacks — stdlib-only hooks for higher layers to plug
	// in traffic capture, metrics, or compliance counters without
	// coupling this package to any specific implementation.
	onTx      func([]byte)
	onRx      func([]byte)
	onCapSoft func(dataLen int) // DATA > 128 bytes but <= 255
	onNAK     func()            // peer sent DLE NAK for our frame
	onTimeout func()            // ACK not seen within ackTimeout
	onRetry   func(attempt int) // retrying after NAK or ack-timeout
	onNoACK   func()            // reply arrived before ACK (spec deviation)
}

// eventFunc is an async-event callback. Listeners receive every frame
// that isn't claimed by a pending Send matcher (typically tallies).
type eventFunc func(Frame)

// pendingWaiter captures a single in-flight Send. SW-P-08 is half-duplex
// per logical transaction: only one outstanding request at a time.
type pendingWaiter struct {
	match func(Frame) bool

	// reply carries the decoded frame (or an I/O error from failPending).
	// Lives for the whole Send (all retry attempts).
	reply chan replyResult

	// ack / nak are signal channels installed fresh per attempt by
	// installSignals. The reader closes whichever signal arrives; the
	// Send's per-attempt select observes it. Stale channels from a
	// previous attempt are left to GC.
	sigMu sync.Mutex
	ack   chan struct{}
	nak   chan struct{}
}

// installSignals resets the ACK/NAK channels before an attempt. Returns
// the freshly-installed channels so the caller can select on them.
func (w *pendingWaiter) installSignals() (chan struct{}, chan struct{}) {
	ack := make(chan struct{})
	nak := make(chan struct{})
	w.sigMu.Lock()
	w.ack = ack
	w.nak = nak
	w.sigMu.Unlock()
	return ack, nak
}

// closeACK is called by the reader on DLE ACK. No-op when no Send is
// awaiting ACK. Idempotent within one attempt (the reader claims the
// channel under sigMu).
func (w *pendingWaiter) closeACK() {
	w.sigMu.Lock()
	ch := w.ack
	w.ack = nil
	w.sigMu.Unlock()
	if ch != nil {
		close(ch)
	}
}

// closeNAK mirrors closeACK for DLE NAK.
func (w *pendingWaiter) closeNAK() {
	w.sigMu.Lock()
	ch := w.nak
	w.nak = nil
	w.sigMu.Unlock()
	if ch != nil {
		close(ch)
	}
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

	// ACKTimeout overrides DefaultACKTimeout per-attempt.
	ACKTimeout time.Duration
	// MaxAttempts overrides DefaultMaxAttempts (send + retries).
	MaxAttempts int

	// OnTx / OnRx are optional raw-byte observer callbacks invoked on
	// every send and receive respectively. Kept as plain funcs to
	// avoid pulling any other acp package into this codec — callers
	// wire capture / metrics at their own layer.
	OnTx func([]byte)
	OnRx func([]byte)

	// OnCapSoft fires when an outbound frame's DATA field exceeds
	// DataCapSoft (128) but is within DataCapHard (255). Argument is
	// the DATA byte count (ID + Payload). Frame is still sent.
	OnCapSoft func(dataLen int)
	// OnNAK fires when an inbound DLE NAK is observed for a frame we
	// sent. The Send retries internally up to MaxAttempts.
	OnNAK func()
	// OnTimeout fires when ACK isn't observed within ACKTimeout. The
	// Send retries internally up to MaxAttempts.
	OnTimeout func()
	// OnRetry fires once per retry decision, with the attempt number
	// that just failed (1-based). Fires after OnNAK / OnTimeout.
	OnRetry func(attempt int)
	// OnNoACK fires when a reply frame arrives before the peer's ACK.
	// Spec deviation per SW-P-08 §2; frame accepted regardless.
	OnNoACK func()
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
		return nil, fmt.Errorf("probel dial %s: %w", addr, err)
	}
	c := newClient(conn, logger, cfg)
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
	c := newClient(conn, logger, cfg)
	go c.readLoop(cfg.ReadBufferSize)
	return c
}

func newClient(conn net.Conn, logger *slog.Logger, cfg ClientConfig) *Client {
	hex := true
	if cfg.WireHexLog != nil {
		hex = *cfg.WireHexLog
	}
	ack := cfg.ACKTimeout
	if ack <= 0 {
		ack = DefaultACKTimeout
	}
	max := cfg.MaxAttempts
	if max <= 0 {
		max = DefaultMaxAttempts
	}
	return &Client{
		logger:      logger,
		conn:        conn,
		readerDone:  make(chan struct{}),
		wireHexLog:  hex,
		ackTimeout:  ack,
		maxAttempts: max,
		onTx:        cfg.OnTx,
		onRx:        cfg.OnRx,
		onCapSoft:   cfg.OnCapSoft,
		onNAK:       cfg.OnNAK,
		onTimeout:   cfg.OnTimeout,
		onRetry:     cfg.OnRetry,
		onNoACK:     cfg.OnNoACK,
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

// Subscribe registers an async-event listener. Frames that aren't claimed
// by an outstanding Send matcher are delivered to every listener in the
// order they were registered. The listener MUST NOT block.
func (c *Client) Subscribe(fn eventFunc) {
	c.mu.Lock()
	c.readers = append(c.readers, fn)
	c.mu.Unlock()
}

// Send writes one framed command, waits for the peer's DLE ACK (retrying
// on NAK or ack-timeout up to MaxAttempts per SW-P-08 §2), and optionally
// waits for a matching reply. If match is nil, Send returns as soon as
// the peer ACKs the frame (or immediately if no ack within MaxAttempts).
//
// The returned Frame is the zero value when match is nil.
//
// Errors:
//   - ErrDataFieldTooLarge if the frame exceeds DataCapHard bytes.
//   - ErrSendInFlight if another Send is already waiting on this Client.
//   - ErrMaxAttempts after NAKs or ack-timeouts exhaust MaxAttempts.
//   - ctx.Err() if ctx expires before the peer ACKs or replies.
//   - any net I/O error from the underlying conn.
func (c *Client) Send(ctx context.Context, f Frame, match func(Frame) bool) (Frame, error) {
	total := 1 + len(f.Payload)
	if total > DataCapHard {
		return Frame{}, fmt.Errorf("%w: got %d bytes", ErrDataFieldTooLarge, total)
	}
	if total > DataCapSoft && c.onCapSoft != nil {
		c.onCapSoft(total)
	}

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
	ackTimeout := c.ackTimeout
	maxAttempts := c.maxAttempts
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		if c.pending == waiter {
			c.pending = nil
		}
		c.mu.Unlock()
	}()

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		ackCh, nakCh := waiter.installSignals()

		if hex {
			c.logger.Info("probel TX",
				slog.Int("attempt", attempt),
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
			return Frame{}, fmt.Errorf("probel write: %w", err)
		}

		timer := time.NewTimer(ackTimeout)
		select {
		case <-ackCh:
			timer.Stop()
			// ACK observed — proceed to reply phase.
		case <-nakCh:
			timer.Stop()
			if c.onNAK != nil {
				c.onNAK()
			}
			if attempt == maxAttempts {
				return Frame{}, fmt.Errorf("probel: peer NAKed all %d attempts: %w",
					maxAttempts, ErrMaxAttempts)
			}
			if c.onRetry != nil {
				c.onRetry(attempt)
			}
			continue
		case <-timer.C:
			if c.onTimeout != nil {
				c.onTimeout()
			}
			if attempt == maxAttempts {
				return Frame{}, fmt.Errorf("probel: no ACK after %d attempts: %w",
					maxAttempts, ErrMaxAttempts)
			}
			if c.onRetry != nil {
				c.onRetry(attempt)
			}
			continue
		case r := <-waiter.reply:
			// Reply arrived before ACK — spec deviation (§2 requires ACK)
			// but accept so lax matrices don't hang the client.
			timer.Stop()
			if c.onNoACK != nil {
				c.onNoACK()
			}
			return r.frame, r.err
		case <-ctx.Done():
			timer.Stop()
			return Frame{}, ctx.Err()
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
	return Frame{}, ErrMaxAttempts
}

// Write sends a pre-built raw byte sequence (typically a DLE ACK / DLE
// NAK). Bypasses Pack and the retry loop. Used by the reader itself to
// emit ACK/NAK for received frames — these control sequences don't
// carry a DATA field and don't themselves expect confirmation.
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
//   - inbound DLE ACK / DLE NAK   -> signal the in-flight Send (if any)
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
			if IsACK(buf) {
				c.logger.Debug("probel rx ACK")
				if c.onRx != nil {
					c.onRx(buf[:2])
				}
				c.signalACK()
				buf = buf[2:]
				continue
			}
			if IsNAK(buf) {
				c.logger.Warn("probel rx NAK")
				if c.onRx != nil {
					c.onRx(buf[:2])
				}
				c.signalNAK()
				buf = buf[2:]
				continue
			}
			if buf[0] != DLE {
				c.logger.Warn("probel rx desync: dropping byte",
					slog.String("byte", fmt.Sprintf("%02x", buf[0])))
				buf = buf[1:]
				continue
			}

			f, consumed, perr := Unpack(buf)
			if errors.Is(perr, io.ErrUnexpectedEOF) {
				break
			}
			if perr != nil {
				c.logger.Warn("probel rx decode — emitting DLE NAK",
					slog.String("err", perr.Error()),
					slog.String("hex", HexDump(buf[:min(len(buf), 64)])))
				_ = c.Write(PackNAK())
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
			_ = c.Write(PackACK())
			buf = buf[consumed:]
			c.dispatch(f)
		}
	}
}

// signalACK routes an inbound DLE ACK to the current in-flight Send,
// if any. No-op when no Send is pending.
func (c *Client) signalACK() {
	c.mu.Lock()
	waiter := c.pending
	c.mu.Unlock()
	if waiter != nil {
		waiter.closeACK()
	}
}

// signalNAK mirrors signalACK for DLE NAK.
func (c *Client) signalNAK() {
	c.mu.Lock()
	waiter := c.pending
	c.mu.Unlock()
	if waiter != nil {
		waiter.closeNAK()
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
			// reply slot already filled (duplicate frame?) — fall through.
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

// HexDump formats bytes as space-separated 2-digit lowercase hex — the
// format SW-P-08 spec examples use and the convention most byte-view
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
