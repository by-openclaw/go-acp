package acp1

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"
)

// Transport is the minimal send/receive contract the ACP1 client needs.
// Production uses *transport.UDPConn; tests use an in-memory fake.
//
// Keeping the contract tiny makes it trivial to back the client with a
// different transport later (TCP direct mode, AN2 framer, a recorded
// capture player) without touching client logic.
type Transport interface {
	Send(ctx context.Context, payload []byte) error
	Receive(ctx context.Context, maxSize int) ([]byte, error)
	Close() error
}

// ClientConfig tunes the retry loop. Defaults come from spec §"Timeouts"
// p. 12 and the appendix recommended practices: 5 retries, 10-second
// per-attempt receive timeout, exponential backoff to avoid congestion.
//
// Zero-valued fields are replaced with defaults by the client constructor,
// so callers can override only what they care about.
type ClientConfig struct {
	MaxRetries     int           // default 5
	ReceiveTimeout time.Duration // default 10s — per attempt
	InitialBackoff time.Duration // default 100ms — first retry delay
	MaxBackoff     time.Duration // default 2s — cap per-retry delay
}

// defaultConfig returns a ClientConfig with all fields populated.
func defaultConfig() ClientConfig {
	return ClientConfig{
		MaxRetries:     5,
		ReceiveTimeout: 10 * time.Second,
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     2 * time.Second,
	}
}

// Client is the ACP1 transactional layer. It owns a Transport and a
// monotonically-increasing MTID counter. Do() is the single entry point
// for request/reply — it handles retries, MTID allocation, and reply
// filtering (announcements and mismatched MTIDs are skipped silently).
//
// Transactions are serialised by a mutex per spec §"ACP Message Types"
// p. 8: "Flow control is achieved by serializing the transactions." Only
// one in-flight request at a time per Client instance.
type Client struct {
	tr     Transport
	logger *slog.Logger
	cfg    ClientConfig

	mu       sync.Mutex // serialises Do() calls
	nextMTID uint32     // monotonic; wraps skip 0
}

// NewClient builds a Client around a live Transport. The initial MTID is
// chosen at random per spec §"ACP Header" p. 11: "A client randomly
// generates an initial MTID at power-up. An MTID must not be zero."
func NewClient(tr Transport, logger *slog.Logger, cfg ClientConfig) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	// Populate missing fields with defaults.
	dc := defaultConfig()
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = dc.MaxRetries
	}
	if cfg.ReceiveTimeout <= 0 {
		cfg.ReceiveTimeout = dc.ReceiveTimeout
	}
	if cfg.InitialBackoff <= 0 {
		cfg.InitialBackoff = dc.InitialBackoff
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = dc.MaxBackoff
	}
	// Seed nextMTID from a cryptographically-unimportant but non-zero
	// random source. The spec only requires "randomly generated at
	// power-up" — not cryptographic randomness.
	//nolint:gosec // not a security boundary
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	seed := r.Uint32()
	if seed == 0 {
		seed = 1
	}
	return &Client{
		tr:       tr,
		logger:   logger,
		cfg:      cfg,
		nextMTID: seed,
	}
}

// Close releases the transport. Safe to call multiple times.
func (c *Client) Close() error {
	if c.tr == nil {
		return nil
	}
	err := c.tr.Close()
	c.tr = nil
	return err
}

// Do sends one request message and returns the matching reply. The
// request's MTID field is overwritten with a freshly allocated non-zero
// value — callers should leave it zero. MAddr, MType, MCode, ObjGroup,
// ObjID, and Value are used as-is.
//
// Retry semantics (spec p. 12):
//   - On receive timeout, retransmit with the SAME MTID (critical — the
//     C# reference driver gets this wrong by incrementing; we follow spec).
//   - Exponential backoff between attempts to avoid congestion.
//   - Up to MaxRetries total attempts.
//   - Announcements and replies with mismatched MTIDs are silently skipped
//     and we keep waiting within the same attempt window.
//
// Error semantics:
//   - Transport send/receive failures return immediately wrapped in a
//     descriptive error — no retry (only receive-timeout retries).
//   - Error-reply messages (MType=3) are returned as successful replies;
//     the caller inspects msg.IsError() / msg.ErrCode().
//   - Exhaustion returns ErrMaxRetries.
func (c *Client) Do(ctx context.Context, req *Message) (*Message, error) {
	if req == nil {
		return nil, errors.New("acp1: Do nil request")
	}
	if c.tr == nil {
		return nil, errors.New("acp1: Do on closed client")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Allocate a fresh MTID. If the caller accidentally set one, overwrite
	// — the spec says the CLIENT owns MTID allocation. Keeping the same
	// MTID across retries is handled below by NOT re-allocating inside the
	// retry loop.
	req.MTID = c.allocMTID()

	payload, err := req.Encode()
	if err != nil {
		return nil, fmt.Errorf("acp1: encode: %w", err)
	}

	var lastErr error
	backoff := c.cfg.InitialBackoff

	for attempt := 0; attempt < c.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			// Spec p. 12: "To avoid congestion, an exponential back-off
			// algorithm can be used." We sleep between attempts, honouring
			// caller cancellation.
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > c.cfg.MaxBackoff {
				backoff = c.cfg.MaxBackoff
			}
		}

		reply, err := c.doOneAttempt(ctx, payload, req.MTID)
		if err == nil {
			return reply, nil
		}

		// Timeout → retry with the SAME MTID (payload bytes unchanged).
		if errors.Is(err, context.DeadlineExceeded) {
			lastErr = err
			c.logger.Debug("acp1 attempt timed out",
				"attempt", attempt+1,
				"max", c.cfg.MaxRetries,
				"mtid", req.MTID)
			continue
		}
		// Any other error is fatal — don't retry a permanently-broken
		// socket or a malformed reply from the client-side encoder.
		return nil, err
	}

	return nil, fmt.Errorf("acp1: %w after %d attempts: %v",
		ErrMaxRetries, c.cfg.MaxRetries, lastErr)
}

// doOneAttempt performs one send + filtered-receive cycle within a single
// ReceiveTimeout window. It sends the payload once, then drains the
// receive pipe, discarding announcements and MTID mismatches, until
// either the matching reply arrives or the attempt window expires.
func (c *Client) doOneAttempt(parent context.Context, payload []byte, wantMTID uint32) (*Message, error) {
	attemptCtx, cancel := context.WithTimeout(parent, c.cfg.ReceiveTimeout)
	defer cancel()

	if err := c.tr.Send(attemptCtx, payload); err != nil {
		return nil, fmt.Errorf("acp1 send: %w", err)
	}

	for {
		raw, err := c.tr.Receive(attemptCtx, MaxPacket)
		if err != nil {
			return nil, err
		}
		msg, err := Decode(raw)
		if err != nil {
			c.logger.Debug("acp1 malformed reply, skipping", "err", err, "bytes", len(raw))
			continue
		}
		if msg.IsAnnouncement() {
			// Announcements are handled by the Listener goroutine
			// (Commit 5). The transactional client just drops them.
			c.logger.Debug("acp1 skipping announcement inside transaction",
				"group", msg.ObjGroup, "id", msg.ObjID)
			continue
		}
		if msg.MTID != wantMTID {
			// Belongs to a previous timed-out transaction or a different
			// client on the wire. Drop and keep waiting.
			c.logger.Debug("acp1 MTID mismatch, skipping",
				"got", msg.MTID, "want", wantMTID)
			continue
		}
		return msg, nil
	}
}

// ErrMaxRetries is returned when Do exhausts all retry attempts without
// receiving a matching reply. Call sites wrap this in a TransportError
// when surfacing to higher layers.
var ErrMaxRetries = errors.New("acp1: max retries exceeded")

// allocMTID returns the next non-zero MTID. Must be called with c.mu held.
// Spec: MTID must never be zero. When the counter wraps from 0xFFFFFFFF
// to 0, skip to 1.
func (c *Client) allocMTID() uint32 {
	c.nextMTID++
	if c.nextMTID == 0 {
		c.nextMTID = 1
	}
	return c.nextMTID
}
