package acp1

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"acp/internal/transport"
)

// TCPClient is the ACP1 session layer for TCP direct mode (spec v1.4
// §"ACP Header" p. 10). Unlike UDP — where Client and Listener each own
// a separate socket — TCP direct carries requests, replies, AND
// announcements on one long-lived connection. The client multiplexes:
//
//   - A dedicated reader goroutine pulls MLEN-framed messages off the
//     socket, decodes each one, and routes based on MTID.
//   - Replies (non-zero MTID) go to a pending[mtid] channel, waking the
//     goroutine blocked inside Do.
//   - Announcements (MTID==0) fan out to every registered RawEventFunc
//     listener, plus are dropped if none are registered.
//
// Retry semantics differ from UDP: TCP is reliable, so per-message
// retransmits aren't needed. Do still honours a per-transaction receive
// timeout so a stuck peer doesn't block forever, and returns
// context.DeadlineExceeded on timeout. The caller can retry at the
// application level if appropriate.
//
// Thread safety:
//   - Do is safe for concurrent callers (writes are serialised by
//     TCPConn.Send's internal mutex; pending map entries are
//     independent).
//   - AddListener / RemoveListener are safe to call from any goroutine.
//   - Close is safe to call more than once.
type TCPClient struct {
	conn   *transport.TCPConn
	logger *slog.Logger
	cfg    ClientConfig

	// mtidMu guards nextMTID only. Kept separate from pendingMu so that
	// allocating an MTID under load doesn't block listener registration.
	mtidMu   sync.Mutex
	nextMTID uint32

	// pendingMu guards pending + listeners + closed.
	pendingMu sync.Mutex
	pending   map[uint32]chan *Message
	listeners []RawEventFunc
	closed    bool

	readerDone chan struct{}
}

// NewTCPClient takes an already-connected TCPConn and starts the
// multiplexing reader goroutine. The caller retains ownership of the
// conn until Close is called.
func NewTCPClient(conn *transport.TCPConn, logger *slog.Logger, cfg ClientConfig) *TCPClient {
	if logger == nil {
		logger = slog.Default()
	}
	dc := defaultConfig()
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = dc.MaxRetries
	}
	if cfg.ReceiveTimeout <= 0 {
		cfg.ReceiveTimeout = dc.ReceiveTimeout
	}

	//nolint:gosec // non-crypto MTID seed
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	seed := r.Uint32()
	if seed == 0 {
		seed = 1
	}

	c := &TCPClient{
		conn:       conn,
		logger:     logger,
		cfg:        cfg,
		nextMTID:   seed,
		pending:    map[uint32]chan *Message{},
		readerDone: make(chan struct{}),
	}
	go c.readerLoop()
	return c
}

// Close tears down the client. The reader goroutine exits as soon as
// the TCP socket closes. Pending transactions get an ErrClientClosed
// on their reply channels so Do unblocks cleanly.
func (c *TCPClient) Close() error {
	c.pendingMu.Lock()
	if c.closed {
		c.pendingMu.Unlock()
		return nil
	}
	c.closed = true
	// Wake any in-flight Do() calls.
	for mtid, ch := range c.pending {
		close(ch)
		delete(c.pending, mtid)
	}
	c.pendingMu.Unlock()

	var err error
	if c.conn != nil {
		err = c.conn.Close()
	}
	// Wait for reader to exit so Close is synchronous — caller can
	// assume no more listener callbacks after Close returns.
	<-c.readerDone
	return err
}

// Do is the ACP1 Client contract: send a request, return the matching
// reply. Implements the clientIface interface the Plugin uses, so it's
// a drop-in replacement for UDP Client at the Plugin layer.
func (c *TCPClient) Do(ctx context.Context, req *Message) (*Message, error) {
	if req == nil {
		return nil, errors.New("acp1 tcp: Do nil request")
	}
	c.pendingMu.Lock()
	if c.closed || c.conn == nil {
		c.pendingMu.Unlock()
		return nil, errors.New("acp1 tcp: Do on closed client")
	}
	c.pendingMu.Unlock()

	// Allocate a fresh MTID and register its reply channel BEFORE
	// sending, so a fast device can't beat us with a reply we haven't
	// subscribed to yet.
	req.MTID = c.allocMTID()
	replyCh := make(chan *Message, 1)

	c.pendingMu.Lock()
	c.pending[req.MTID] = replyCh
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, req.MTID)
		c.pendingMu.Unlock()
	}()

	payload, err := req.Encode()
	if err != nil {
		return nil, fmt.Errorf("acp1 tcp: encode: %w", err)
	}
	// Per-attempt deadline. TCP is reliable so no retry loop, but we
	// still bound the transaction so a hung peer can't block forever.
	sendCtx, cancel := context.WithTimeout(ctx, c.cfg.ReceiveTimeout)
	defer cancel()
	if err := c.conn.Send(sendCtx, payload); err != nil {
		return nil, fmt.Errorf("acp1 tcp send: %w", err)
	}

	// Wait for the reader goroutine to route the matching reply.
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(c.cfg.ReceiveTimeout):
		return nil, context.DeadlineExceeded
	case reply, ok := <-replyCh:
		if !ok {
			return nil, errors.New("acp1 tcp: connection closed while waiting for reply")
		}
		return reply, nil
	}
}

// AddListener registers a raw-event callback. Returns a numeric handle
// suitable for passing to RemoveListener. Multiple listeners are allowed;
// each receives every announcement in registration order. Callbacks run
// in the reader goroutine and must not block.
func (c *TCPClient) AddListener(fn RawEventFunc) int {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	c.listeners = append(c.listeners, fn)
	return len(c.listeners) - 1
}

// RemoveListener drops a previously-registered callback. A handle that
// was already invalidated (via a prior RemoveListener) is a no-op.
func (c *TCPClient) RemoveListener(h int) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	if h < 0 || h >= len(c.listeners) {
		return
	}
	c.listeners[h] = nil
}

// allocMTID returns the next non-zero MTID. Spec p. 11: never zero,
// skip when the counter wraps.
func (c *TCPClient) allocMTID() uint32 {
	c.mtidMu.Lock()
	defer c.mtidMu.Unlock()
	c.nextMTID++
	if c.nextMTID == 0 {
		c.nextMTID = 1
	}
	return c.nextMTID
}

// readerLoop is the single reader goroutine. Pulls one framed message
// per iteration, decodes, and routes. Exits when the socket is closed.
func (c *TCPClient) readerLoop() {
	defer close(c.readerDone)
	// Panic isolation: a buggy listener must not kill the reader.
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error("acp1 tcp reader panic", "err", r)
		}
	}()

	for {
		// Blocking read with no deadline — closing the socket (via
		// Close) unblocks it with an error.
		raw, err := c.conn.Receive(context.Background(), MaxPacket)
		if err != nil {
			// Close → exit. Any other error → also exit, since a
			// framing error on a TCP stream means we can't resync.
			c.pendingMu.Lock()
			closed := c.closed
			c.pendingMu.Unlock()
			if closed {
				return
			}
			c.logger.Debug("acp1 tcp reader: exiting on error", "err", err)
			return
		}

		msg, derr := Decode(raw)
		if derr != nil {
			c.logger.Debug("acp1 tcp reader: malformed frame", "err", derr, "bytes", len(raw))
			continue
		}

		// Route by MTID. Non-zero → look up pending transaction.
		// Zero → announcement fan-out.
		if msg.MTID != 0 {
			c.pendingMu.Lock()
			ch, ok := c.pending[msg.MTID]
			c.pendingMu.Unlock()
			if !ok {
				c.logger.Debug("acp1 tcp reader: no pending for MTID",
					"mtid", msg.MTID, "mtype", msg.MType)
				continue
			}
			select {
			case ch <- msg:
			default:
				c.logger.Debug("acp1 tcp reader: reply channel full, dropping",
					"mtid", msg.MTID)
			}
			continue
		}

		// MTID == 0: announcement. Fan out.
		if !msg.IsAnnouncement() {
			c.logger.Debug("acp1 tcp reader: MTID=0 but not announcement",
				"mtype", msg.MType)
			continue
		}
		c.logger.Debug("acp1 tcp reader: announcement",
			"slot", msg.MAddr, "grp", msg.ObjGroup, "id", msg.ObjID,
			"mtype", msg.MType, "mcode", msg.MCode)

		c.pendingMu.Lock()
		lst := make([]RawEventFunc, 0, len(c.listeners))
		for _, fn := range c.listeners {
			if fn != nil {
				lst = append(lst, fn)
			}
		}
		c.pendingMu.Unlock()

		for _, fn := range lst {
			func() {
				defer func() {
					if r := recover(); r != nil {
						c.logger.Error("acp1 tcp listener panic", "err", r)
					}
				}()
				fn(msg)
			}()
		}
	}
}
