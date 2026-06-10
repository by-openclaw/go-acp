package acp1

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"sync"
	"time"

	"dhs/internal/acp1/codec"
	an2 "dhs/internal/acp2/codec"
)

// AN2DefaultPort is the ACP1 Mode C (AN2/TCP) port per spec p.10.
const AN2DefaultPort = 2072

// AN2Client is the ACP1 session layer for Mode C (AN2 over TCP, spec v1.4
// §"ACP Header" p.10). Like TCPClient it multiplexes requests, replies, and
// announcements over one long-lived connection — but every ACP1 PDU is
// wrapped in an AN2 frame (proto=ACP1, type=Data), and the client sends an
// AN2 EnableProtocolEvents([ACP1]) control frame at startup so the device
// fans spontaneous announces back over the same socket.
//
// Routing is by the ACP1 payload MTID (decoded from the frame payload), not
// the AN2 frame MTID — the provider preserves the ACP1 MTID on the reply and
// sets the AN2 wrapper MTID to 0 (see provider handleAN2ACP1Frame). A drop-in
// clientIface for the Plugin, with the same AddListener/RemoveListener
// announce fan-out shape as TCPClient.
type AN2Client struct {
	conn   *net.TCPConn
	logger *slog.Logger
	cfg    ClientConfig

	mtidMu   sync.Mutex
	nextMTID uint32

	pendingMu sync.Mutex
	pending   map[uint32]chan *codec.Message
	listeners []RawEventFunc
	closed    bool

	readerDone chan struct{}
}

// NewAN2Client wraps an already-connected raw TCP socket, starts the reader
// goroutine, and sends EnableProtocolEvents([ACP1]) so announces flow.
func NewAN2Client(conn *net.TCPConn, logger *slog.Logger, cfg ClientConfig) *AN2Client {
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

	c := &AN2Client{
		conn:       conn,
		logger:     logger,
		cfg:        cfg,
		nextMTID:   seed,
		pending:    map[uint32]chan *codec.Message{},
		readerDone: make(chan struct{}),
	}
	go c.readerLoop()
	c.enableProtocolEvents()
	return c
}

// enableProtocolEvents sends the AN2 control frame that opts this session in
// to ACP1 announcements. Best-effort — a device that ignores it simply won't
// push announces, which degrades Subscribe but not request/reply.
func (c *AN2Client) enableProtocolEvents() {
	frame := &an2.AN2Frame{
		Proto:   an2.AN2ProtoInternal,
		Slot:    0,
		MTID:    0,
		Type:    an2.AN2TypeRequest,
		Payload: []byte{an2.AN2FuncEnableProtocolEvents, byte(an2.AN2ProtoACP1)},
	}
	b, err := an2.EncodeAN2Frame(frame)
	if err != nil {
		return
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(c.cfg.ReceiveTimeout))
	_, _ = c.conn.Write(b)
}

// Do sends one ACP1 request inside an AN2 data frame and returns the matching
// reply. Implements clientIface.
func (c *AN2Client) Do(ctx context.Context, req *codec.Message) (*codec.Message, error) {
	if req == nil {
		return nil, errors.New("acp1 an2: Do nil request")
	}
	c.pendingMu.Lock()
	if c.closed || c.conn == nil {
		c.pendingMu.Unlock()
		return nil, errors.New("acp1 an2: Do on closed client")
	}
	c.pendingMu.Unlock()

	req.MTID = c.allocMTID()
	replyCh := make(chan *codec.Message, 1)
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
		return nil, fmt.Errorf("acp1 an2: encode: %w", err)
	}
	frame := &an2.AN2Frame{
		Proto:   an2.AN2ProtoACP1,
		Slot:    req.MAddr,
		MTID:    0,
		Type:    an2.AN2TypeData,
		Payload: payload,
	}
	wire, err := an2.EncodeAN2Frame(frame)
	if err != nil {
		return nil, fmt.Errorf("acp1 an2: frame encode: %w", err)
	}

	_ = c.conn.SetWriteDeadline(time.Now().Add(c.cfg.ReceiveTimeout))
	if _, err := c.conn.Write(wire); err != nil {
		return nil, fmt.Errorf("acp1 an2 send: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(c.cfg.ReceiveTimeout):
		return nil, context.DeadlineExceeded
	case reply, ok := <-replyCh:
		if !ok {
			return nil, errors.New("acp1 an2: connection closed while waiting for reply")
		}
		return reply, nil
	}
}

// AddListener registers a raw-event callback for announcements.
func (c *AN2Client) AddListener(fn RawEventFunc) int {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	c.listeners = append(c.listeners, fn)
	return len(c.listeners) - 1
}

// RemoveListener drops a previously-registered callback.
func (c *AN2Client) RemoveListener(h int) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	if h < 0 || h >= len(c.listeners) {
		return
	}
	c.listeners[h] = nil
}

// Close tears down the client. Idempotent.
func (c *AN2Client) Close() error {
	c.pendingMu.Lock()
	if c.closed {
		c.pendingMu.Unlock()
		return nil
	}
	c.closed = true
	for mtid, ch := range c.pending {
		close(ch)
		delete(c.pending, mtid)
	}
	c.pendingMu.Unlock()

	var err error
	if c.conn != nil {
		err = c.conn.Close()
	}
	<-c.readerDone
	return err
}

// allocMTID returns the next non-zero ACP1 MTID (spec p.11).
func (c *AN2Client) allocMTID() uint32 {
	c.mtidMu.Lock()
	defer c.mtidMu.Unlock()
	c.nextMTID++
	if c.nextMTID == 0 {
		c.nextMTID = 1
	}
	return c.nextMTID
}

// readerLoop reads AN2 frames, unwraps ACP1 payloads, and routes them.
func (c *AN2Client) readerLoop() {
	defer close(c.readerDone)
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error("acp1 an2 reader panic", "err", r)
		}
	}()

	for {
		frame, err := an2.ReadAN2Frame(c.conn)
		if err != nil {
			c.pendingMu.Lock()
			closed := c.closed
			c.pendingMu.Unlock()
			if !closed {
				c.logger.Debug("acp1 an2 reader: exiting on error", "err", err)
			}
			return
		}
		if c.cfg.OnRx != nil {
			c.cfg.OnRx()
		}
		// Only ACP1-proto data frames carry ACP1 PDUs. Internal frames
		// (e.g. the EnableProtocolEvents reply) are control acks — ignore.
		if frame.Proto != an2.AN2ProtoACP1 || len(frame.Payload) == 0 {
			continue
		}
		msg, derr := codec.Decode(frame.Payload)
		if derr != nil {
			c.logger.Debug("acp1 an2 reader: malformed payload", "err", derr)
			continue
		}

		if msg.MTID != 0 {
			c.pendingMu.Lock()
			ch, ok := c.pending[msg.MTID]
			c.pendingMu.Unlock()
			if !ok {
				continue
			}
			select {
			case ch <- msg:
			default:
			}
			continue
		}

		if !msg.IsAnnouncement() {
			continue
		}
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
						c.logger.Error("acp1 an2 listener panic", "err", r)
					}
				}()
				fn(msg)
			}()
		}
	}
}
