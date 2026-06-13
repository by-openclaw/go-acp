package acp2

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/consumer/compliance"
	"dhs/internal/transport"
	"dhs/internal/acp2/codec"
)

// Session manages an AN2/TCP connection to an ACP2 device. It handles:
//   - TCP connect to port 2072
//   - AN2 initialization sequence (GetVersion, GetDeviceInfo, GetSlotInfo, EnableProtocolEvents)
//   - Multiplexing: a reader goroutine routes replies by ACP2 mtid and announces to listeners
//   - mtid pool (1-255) with defer-based release
type Session struct {
	logger *slog.Logger

	mu   sync.Mutex
	conn net.Conn
	host string
	port int

	// AN2-level device info populated during handshake.
	// AN2 GetVersion (spec §3.3.1) returns major + minor; both
	// preserved here so the connect log + public AN2Version() can
	// emit the full "major.minor" — real Neuron reports 1.0.
	an2VersionMajor uint8
	an2VersionMinor uint8
	numSlots        int
	slotStatus      []consumer.SlotStatus
	acp2Version     uint8

	// mtid pool: 1-255 available, 0 reserved for announces.
	mtidMu   sync.Mutex
	mtidPool [255]bool // mtidPool[i] true = mtid (i+1) is in use
	mtidCond *sync.Cond

	// Pending request waiters: keyed by ACP2 mtid.
	waitMu  sync.Mutex
	waiters map[uint8]chan *codec.ACP2Message

	// Announce listeners.
	annMu     sync.Mutex
	annNextID int
	annSubs   map[int]AnnounceFunc

	// Reader goroutine lifecycle.
	done     chan struct{}
	closeErr error

	// closeWait bounds how long closeLocked blocks on the reader goroutine's
	// done channel before giving up. Injectable so tests can exercise the
	// timeout arm without a real 2 s wall-clock wait; defaults to 2 * time.Second
	// in NewSession for production.
	closeWait time.Duration

	// Write serialisation.
	writeMu sync.Mutex

	// Optional traffic capture for unit test data generation.
	recorder *transport.Recorder

	// Optional compliance profile. When non-nil the session fires
	// wire-tolerance events (magic mismatch, short payload, spec-
	// listed stat codes, …) into this counter. Plugin injects it
	// after Connect; nil-safe to leave unset (unit tests that only
	// exercise codec primitives).
	profile *compliance.Profile

	// lastRxNS tracks the wall-clock UnixNano of the most recent frame
	// received on this session — read lock-free by SessionHealth /
	// SessionLive (#365). Updated by readLoop on every frame regardless
	// of type, so announces, replies, and keep-alive responses all
	// refresh liveness.
	lastRxNS atomic.Int64

	// slotLastSeen records the wall-clock time we last had wire evidence
	// of a particular slot's status (handshake AN2 GetSlotInfo or a
	// keep-alive probe of that slot). Used to populate
	// consumer.SlotInfo.LiveAt without inventing a per-slot timestamp
	// elsewhere. Lock around mu (already taken when slotStatus is
	// touched).
	slotLastSeen []time.Time
}

// AnnounceFunc is the callback signature for ACP2 announce subscriptions.
type AnnounceFunc func(slot uint8, msg *codec.ACP2Message)

// SetRecorder attaches a traffic recorder to this session.
// Call before Connect. All sent and received AN2 frames are recorded.
func (s *Session) SetRecorder(rec *transport.Recorder) {
	s.recorder = rec
}

// SetProfile attaches a compliance profile that this session will
// increment on every wire-tolerance event (see compliance_events.go).
// Idempotent; safe to call before or after Connect. Nil-safe: passing
// nil disables event counting for this session.
func (s *Session) SetProfile(p *compliance.Profile) {
	s.profile = p
}

// note is the thin wrapper that fires an event on the attached
// profile. Guards against nil profile so codec-only unit tests (no
// plugin Connect) don't crash.
func (s *Session) note(event string) {
	if s.profile != nil {
		s.profile.Note(event)
	}
}

// NewSession creates an uninitialised Session. Call Connect to establish
// the TCP connection and run the AN2 handshake.
func NewSession(logger *slog.Logger) *Session {
	s := &Session{
		logger:    logger,
		waiters:   make(map[uint8]chan *codec.ACP2Message),
		annSubs:   make(map[int]AnnounceFunc),
		done:      make(chan struct{}),
		closeWait: 2 * time.Second,
	}
	s.mtidCond = sync.NewCond(&s.mtidMu)
	return s
}

// Connect dials the device, runs the AN2 init sequence, and starts the
// background reader goroutine.
func (s *Session) Connect(ctx context.Context, ip string, port int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conn != nil {
		return fmt.Errorf("acp2: already connected to %s:%d", s.host, s.port)
	}
	if port == 0 {
		port = codec.DefaultPort
	}

	s.logger.Debug("acp2: dialing", "host", ip, "port", port)

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, fmt.Sprintf("%d", port)))
	if err != nil {
		return &consumer.TransportError{Op: "connect", Err: err}
	}
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
	}
	s.conn = conn
	s.host = ip
	s.port = port
	s.done = make(chan struct{})
	s.waiters = make(map[uint8]chan *codec.ACP2Message)

	// Start the reader goroutine before the handshake so replies are routed.
	// Pass conn explicitly: the loop must read from the exact connection this
	// Connect established, never from s.conn (which closeLocked nils).
	go s.readLoop(conn)

	// Run the AN2 init sequence.
	if err := s.an2Handshake(ctx); err != nil {
		_ = s.closeLocked()
		return err
	}

	s.logger.Info("acp2: connected",
		"host", ip, "port", port,
		"an2_version", fmt.Sprintf("%d.%d", s.an2VersionMajor, s.an2VersionMinor),
		"acp2_version", s.acp2Version,
		"slots", s.numSlots)

	return nil
}

// an2Handshake runs the required AN2 init sequence:
//  1. AN2 GetVersion (proto=0) → an2 version
//  2. AN2 GetDeviceInfo (proto=0) → slot count
//  3. AN2 GetSlotInfo(n) for each slot (proto=0) → per-slot status
//  4. AN2 EnableProtocolEvents([2]) (proto=0) → required for ACP2 announces
//  5. ACP2 GetVersion (proto=2) → acp2 version
func (s *Session) an2Handshake(ctx context.Context) error {
	// 1. AN2 GetVersion
	s.logger.Debug("acp2: AN2 GetVersion")
	reply, err := s.an2Request(ctx, codec.AN2FuncGetVersion, 0, nil)
	if err != nil {
		return fmt.Errorf("an2 GetVersion: %w", err)
	}
	// Reply: func_echo(u8) + major(u8) + minor(u8). Spec §3.3.1.
	// Real Neuron reports 1.0; older firmware that ships only one
	// version byte falls back to {0, reply[0]}.
	if len(reply) >= 3 {
		s.an2VersionMajor = reply[1]
		s.an2VersionMinor = reply[2]
	} else if len(reply) >= 1 {
		s.an2VersionMajor = 0
		s.an2VersionMinor = reply[0]
	}
	s.logger.Debug("acp2: AN2 version", "version",
		fmt.Sprintf("%d.%d", s.an2VersionMajor, s.an2VersionMinor),
		"raw", fmt.Sprintf("%x", reply))

	// 2. AN2 GetDeviceInfo
	s.logger.Debug("acp2: AN2 GetDeviceInfo")
	reply, err = s.an2Request(ctx, codec.AN2FuncGetDeviceInfo, 0, nil)
	if err != nil {
		return fmt.Errorf("an2 GetDeviceInfo: %w", err)
	}
	// Reply payload: func_echo(u8) + info(u8). The func echo byte
	// mirrors the function ID (spec §3.3.2 p. 8). Actual slot count
	// is at reply[1], not reply[0].
	if len(reply) >= 2 {
		s.numSlots = int(reply[1])
	} else if len(reply) >= 1 {
		s.numSlots = int(reply[0]) // fallback for non-standard devices
	}
	s.logger.Debug("acp2: device info", "slots", s.numSlots, "raw", fmt.Sprintf("%x", reply))

	// 3. AN2 GetSlotInfo per slot
	// AN2 GetDeviceInfo returns the number of card slots in the frame.
	// Card slots are numbered 1..N (slot 0 = rack controller, not a card).
	// We query slots 0..N to cover the controller + all cards.
	totalSlots := s.numSlots + 1 // include slot 0 (controller)
	s.slotStatus = make([]consumer.SlotStatus, totalSlots)
	s.slotLastSeen = make([]time.Time, totalSlots)
	for slot := 0; slot < totalSlots; slot++ {
		s.logger.Debug("acp2: AN2 GetSlotInfo", "slot", slot)
		// AN2 spec §3.3.3: dlen=1 (just funcID). Slot is in the AN2 header,
		// NOT duplicated in the payload.
		reply, err = s.an2Request(ctx, codec.AN2FuncGetSlotInfo, byte(slot), nil)
		if err != nil {
			s.logger.Debug("acp2: GetSlotInfo failed", "slot", slot, "err", err)
			continue
		}
		// Reply: func_echo(u8) + stat(u8) + num_protos(u8) + protos(u8[])
		// Spec §3.3.3 p. 9. Status is at reply[1], not reply[0].
		if len(reply) >= 2 {
			s.slotStatus[slot] = consumer.SlotStatus(reply[1])
			s.slotLastSeen[slot] = time.Now()
			s.logger.Debug("acp2: slot info", "slot", slot, "status", reply[1],
				"raw", fmt.Sprintf("%x", reply))
		}
	}

	// 4. AN2 EnableProtocolEvents([2]) — required for ACP2 announces
	s.logger.Debug("acp2: AN2 EnableProtocolEvents")
	enablePayload := []byte{1, byte(codec.AN2ProtoACP2)} // count=1, proto=2
	_, err = s.an2Request(ctx, codec.AN2FuncEnableProtocolEvents, 0, enablePayload)
	if err != nil {
		return fmt.Errorf("an2 EnableProtocolEvents: %w", err)
	}

	// 5. ACP2 GetVersion
	s.logger.Debug("acp2: ACP2 GetVersion")
	acp2Reply, err := s.DoACP2(ctx, 0, &codec.ACP2Message{
		Type: codec.ACP2TypeRequest,
		Func: codec.ACP2FuncGetVersion,
	})
	if err != nil {
		return fmt.Errorf("acp2 GetVersion: %w", err)
	}
	s.acp2Version = acp2Reply.PID // byte 3 = version number
	s.logger.Debug("acp2: ACP2 version", "version", s.acp2Version)

	return nil
}

// an2Request sends an AN2 internal (proto=0) request and waits for the reply.
// Uses AN2 mtid for correlation (the session uses a simple scheme: the AN2
// function byte as mtid for internal requests, since they are sequential).
func (s *Session) an2Request(ctx context.Context, funcID uint8, slot uint8, payload []byte) ([]byte, error) {
	// AN2 internal requests use AN2 mtid = funcID+1 (avoid 0).
	an2MTID := funcID + 1

	// Build the AN2 request payload: funcID byte + optional payload.
	reqPayload := make([]byte, 1+len(payload))
	reqPayload[0] = funcID
	copy(reqPayload[1:], payload)

	frame := &codec.AN2Frame{
		Proto:   codec.AN2ProtoInternal,
		Slot:    slot,
		MTID:    an2MTID,
		Type:    codec.AN2TypeRequest,
		Payload: reqPayload,
	}

	// Register a waiter for this AN2 mtid. We reuse the ACP2 waiter map
	// with a convention: AN2 internal replies come back with proto=0 and
	// AN2 mtid matching. The reader goroutine routes them to a synthetic
	// ACP2Message with MTID=an2MTID.
	ch := make(chan *codec.ACP2Message, 1)
	s.waitMu.Lock()
	s.waiters[an2MTID] = ch
	s.waitMu.Unlock()
	defer func() {
		s.waitMu.Lock()
		delete(s.waiters, an2MTID)
		s.waitMu.Unlock()
	}()

	if err := s.sendFrame(ctx, frame); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.done:
		return nil, fmt.Errorf("acp2: connection closed")
	case msg := <-ch:
		// unreachable nil check: routeReply only ever sends a non-nil
		// *ACP2Message into the waiter channel, so msg is never nil here.
		return msg.Body, nil
	}
}

// DoACP2 sends an ACP2 request (inside an AN2 data frame) and waits for
// the corresponding reply. Allocates and releases an ACP2 mtid.
func (s *Session) DoACP2(ctx context.Context, slot uint8, req *codec.ACP2Message) (*codec.ACP2Message, error) {
	// Allocate a mtid.
	mtid, err := s.allocMTID(ctx)
	if err != nil {
		return nil, err
	}
	defer s.releaseMTID(mtid)

	req.MTID = mtid
	if req.Type == 0 {
		req.Type = codec.ACP2TypeRequest
	}

	payload, err := codec.EncodeACP2Message(req)
	if err != nil {
		return nil, err
	}

	// ACP2 messages are carried in AN2 data frames (type=4, AN2 mtid=0).
	frame := &codec.AN2Frame{
		Proto:   codec.AN2ProtoACP2,
		Slot:    slot,
		MTID:    0, // AN2 mtid always 0 for data frames
		Type:    codec.AN2TypeData,
		Payload: payload,
	}

	ch := make(chan *codec.ACP2Message, 1)
	s.waitMu.Lock()
	s.waiters[mtid] = ch
	s.waitMu.Unlock()
	defer func() {
		s.waitMu.Lock()
		delete(s.waiters, mtid)
		s.waitMu.Unlock()
	}()

	s.logger.Debug("acp2: sending request",
		"slot", slot, "mtid", mtid, "func", req.Func,
		"obj_id", req.ObjID, "idx", req.Idx,
		"payload_hex", fmt.Sprintf("%x", payload))

	if err := s.sendFrame(ctx, frame); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.done:
		return nil, fmt.Errorf("acp2: connection closed while waiting for reply mtid=%d", mtid)
	case reply := <-ch:
		// unreachable nil check: routeReply only ever sends a non-nil
		// *ACP2Message into the waiter channel, so reply is never nil here.
		if reply.Type == codec.ACP2TypeError {
			// Fire the per-stat-code compliance event so the session
			// profile reflects spec-listed error frequencies. Status
			// codes 0..5 defined in acp2_protocol.pdf p.5; error
			// replies carry the code in the Func slot (codec.go
			// ACP2Message.Func comment). Switch lives in the pure
			// helper EventForErrStatus so replay tests can assert it.
			if label := EventForErrStatus(codec.ACP2ErrStatus(reply.Func)); label != "" {
				s.note(label)
			}
			return reply, reply.ToACP2Error()
		}
		s.logger.Debug("acp2: received reply",
			"mtid", mtid, "func", reply.Func,
			"obj_id", reply.ObjID, "props", len(reply.Properties))
		return reply, nil
	}
}

// sendFrame encodes and sends one AN2 frame on the TCP connection.
func (s *Session) sendFrame(ctx context.Context, f *codec.AN2Frame) error {
	data, err := codec.EncodeAN2Frame(f)
	if err != nil {
		return err
	}
	s.logger.Debug("acp2: tx",
		"proto", f.Proto, "slot", f.Slot, "mtid", f.MTID, "type", f.Type,
		"frame_hex", fmt.Sprintf("%x", data))

	if s.recorder != nil {
		s.recorder.Record("acp2", "tx", data)
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if s.conn == nil {
		return consumer.ErrNotConnected
	}

	if dl, ok := ctx.Deadline(); ok {
		_ = s.conn.SetWriteDeadline(dl)
	} else {
		_ = s.conn.SetWriteDeadline(time.Time{})
	}

	if _, err := s.conn.Write(data); err != nil {
		return &consumer.TransportError{Op: "send", Err: err}
	}
	return nil
}

// readLoop runs in a goroutine, reading AN2 frames from the TCP connection
// and routing them to the appropriate waiter or announce subscriber.
//
// conn is captured by the caller (Connect) and passed in explicitly so the
// loop never races on s.conn while closeLocked nils it out. When closeLocked
// closes conn, the in-flight ReadAN2Frame errors (io.EOF / net.ErrClosed) and
// the loop exits cleanly via the error arm below — matching the acp1 transport
// pattern of capturing the conn locally and tolerating a closed socket.
func (s *Session) readLoop(conn net.Conn) {
	defer close(s.done)

	for {
		frame, err := codec.ReadAN2Frame(conn)
		if err != nil {
			// ReadAN2Frame wraps the underlying I/O error with %w, so a bare
			// `err == io.EOF` / type-assert never matches. Unwrap with
			// errors.Is / errors.As to correctly detect EOF and a closed conn.
			if errors.Is(err, io.EOF) || isClosedErr(err) {
				s.logger.Debug("acp2: rx: connection closed")
			} else {
				s.logger.Debug("acp2: rx: connection closed", "err", err)
			}
			s.closeErr = err
			return
		}

		// Touch lastRx on every frame so SessionLive / dead-man see
		// announces, replies, AND keep-alive probe answers (#365).
		s.lastRxNS.Store(time.Now().UnixNano())

		// Record raw frame for capture (includes announces — tests need them).
		if s.recorder != nil {
			if raw, encErr := codec.EncodeAN2Frame(frame); encErr == nil {
				s.recorder.Record("acp2", "rx", raw)
			}
		}

		// Log full frame hex for requests/replies; skip for ACP2 announces
		// (they flood the log with large SDP payloads every ~2s).
		isAnnounce := frame.Proto == codec.AN2ProtoACP2 &&
			len(frame.Payload) >= 1 &&
			frame.Payload[0] == byte(codec.ACP2TypeAnnounce)
		if !isAnnounce {
			s.logger.Debug("acp2: rx",
				"proto", frame.Proto, "slot", frame.Slot,
				"mtid", frame.MTID, "type", frame.Type,
				"dlen", len(frame.Payload),
				"payload_hex", fmt.Sprintf("%x", frame.Payload))
		}

		switch frame.Proto {
		case codec.AN2ProtoInternal:
			s.handleAN2Internal(frame)
		case codec.AN2ProtoACP2:
			s.handleACP2Frame(frame)
		default:
			s.logger.Debug("acp2: rx: ignoring frame with proto", "proto", frame.Proto)
		}
	}
}

// handleAN2Internal routes AN2 internal (proto=0) replies and events.
func (s *Session) handleAN2Internal(f *codec.AN2Frame) {
	switch f.Type {
	case codec.AN2TypeReply:
		// Route to waiter by AN2 mtid.
		synth := &codec.ACP2Message{
			Type: codec.ACP2TypeReply,
			MTID: f.MTID,
			Body: f.Payload,
		}
		s.routeReply(f.MTID, synth)

	case codec.AN2TypeEvent:
		// AN2 slot events (e.g. card insertion/removal).
		s.logger.Debug("acp2: AN2 slot event", "slot", f.Slot, "payload_len", len(f.Payload))
		if len(f.Payload) >= 1 {
			status := consumer.SlotStatus(f.Payload[0])
			s.mu.Lock()
			if int(f.Slot) < len(s.slotStatus) {
				s.slotStatus[f.Slot] = status
			}
			s.mu.Unlock()
		}

	case codec.AN2TypeError:
		s.logger.Warn("acp2: AN2 error", "slot", f.Slot, "mtid", f.MTID)
		synth := &codec.ACP2Message{
			Type: codec.ACP2TypeError,
			MTID: f.MTID,
			Body: f.Payload,
		}
		s.routeReply(f.MTID, synth)

	default:
		s.logger.Debug("acp2: AN2 unhandled type", "type", f.Type)
	}
}

// handleACP2Frame routes ACP2 data/event frames.
func (s *Session) handleACP2Frame(f *codec.AN2Frame) {
	if f.Type != codec.AN2TypeData {
		s.logger.Debug("acp2: non-data ACP2 frame", "type", f.Type)
		return
	}
	if len(f.Payload) < codec.ACP2HeaderSize {
		s.logger.Warn("acp2: ACP2 payload too short", "len", len(f.Payload))
		return
	}

	msg, err := codec.DecodeACP2Message(f.Payload)
	if err != nil {
		s.logger.Warn("acp2: decode ACP2 message", "err", err)
		return
	}

	if msg.Type == codec.ACP2TypeAnnounce {
		// Announces fan out silently — per docs/logging.md they're
		// the high-volume hot path and the watch verb already prints
		// every dispatched event with full decoding. Logging here
		// would double-print and truncate the wire bytes besides.
		s.annMu.Lock()
		subs := make([]AnnounceFunc, 0, len(s.annSubs))
		for _, fn := range s.annSubs {
			subs = append(subs, fn)
		}
		s.annMu.Unlock()
		for _, fn := range subs {
			fn(f.Slot, msg)
		}
		return
	}

	// Non-announce: log full details.
	s.logger.Debug("acp2: ACP2 message",
		"type", msg.Type, "mtid", msg.MTID, "func", msg.Func,
		"obj_id", msg.ObjID)

	// Route replies and errors by ACP2 mtid.
	if msg.MTID != 0 {
		s.routeReply(msg.MTID, msg)
	}
}

// routeReply sends a message to the waiter registered for the given mtid.
//
// When no waiter is registered the reply is dropped and a compliance
// event (OrphanReplyMtid) fires. This catches both peer bugs (the
// device emitted a stale mtid) and our own pool regressions (mtid
// released before reply arrived). Per spec p.4 §"Mtid", replies must
// correlate to in-flight requests; an orphan reply is a wire violation.
func (s *Session) routeReply(mtid uint8, msg *codec.ACP2Message) {
	s.waitMu.Lock()
	ch, ok := s.waiters[mtid]
	s.waitMu.Unlock()
	if ok {
		select {
		case ch <- msg:
		default:
			s.logger.Warn("acp2: waiter channel full", "mtid", mtid)
		}
	} else {
		s.logger.Debug("acp2: no waiter for mtid", "mtid", mtid)
		s.note(OrphanReplyMtid)
	}
}

// allocMTID allocates a free ACP2 mtid (1-255). Blocks when the pool
// is exhausted until a release signals the cond, or the caller's
// context cancels — whichever comes first.
//
// Spec p.4 §"Mtid": ACP2 mtid space is 1..255 with 0 reserved for
// announces. The pool MUST never wrap (which would alias an in-flight
// request) and MUST be cancellable so a stuck pool doesn't leak
// goroutines on session teardown.
func (s *Session) allocMTID(ctx context.Context) (uint8, error) {
	s.mtidMu.Lock()
	defer s.mtidMu.Unlock()

	// Cancellation watcher: wakes the cond when ctx fires so a
	// blocked Wait() observes the cancellation. The watcher exits
	// when the alloc returns (ctx.Done() fires from cancel-on-defer
	// in the caller, or via a `done` channel we close on success).
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			s.mtidCond.Broadcast()
		case <-done:
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}

		for i := 0; i < 255; i++ {
			if !s.mtidPool[i] {
				s.mtidPool[i] = true
				return uint8(i + 1), nil
			}
		}
		// All mtids in use — wait for a release or for ctx to cancel.
		s.mtidCond.Wait()
	}
}

// releaseMTID returns a mtid to the pool.
func (s *Session) releaseMTID(mtid uint8) {
	if mtid == 0 {
		return
	}
	s.mtidMu.Lock()
	s.mtidPool[mtid-1] = false
	s.mtidMu.Unlock()
	s.mtidCond.Signal()
}

// SubscribeAnnounces registers a callback for ACP2 announces. Returns an
// ID for later unsubscribe.
func (s *Session) SubscribeAnnounces(fn AnnounceFunc) int {
	s.annMu.Lock()
	defer s.annMu.Unlock()
	s.annNextID++
	id := s.annNextID
	s.annSubs[id] = fn
	return id
}

// UnsubscribeAnnounces removes a previously registered announce callback.
func (s *Session) UnsubscribeAnnounces(id int) {
	s.annMu.Lock()
	defer s.annMu.Unlock()
	delete(s.annSubs, id)
}

// NumSlots returns the slot count discovered during the AN2 handshake.
func (s *Session) NumSlots() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.numSlots
}

// AN2Version returns the AN2 protocol version as "major.minor"
// per spec §3.3.1 GetVersion. Real Neuron firmware reports "1.0".
func (s *Session) AN2Version() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fmt.Sprintf("%d.%d", s.an2VersionMajor, s.an2VersionMinor)
}

// ACP2Version returns the ACP2 protocol version.
func (s *Session) ACP2Version() uint8 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.acp2Version
}

// SlotStatus returns the status of a given slot.
func (s *Session) SlotStatus(slot int) consumer.SlotStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slot < 0 || slot >= len(s.slotStatus) {
		return consumer.SlotNoCard
	}
	return s.slotStatus[slot]
}

// Disconnect tears down the TCP connection and stops the reader goroutine.
func (s *Session) Disconnect() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeLocked()
}

func (s *Session) closeLocked() error {
	if s.conn == nil {
		return nil
	}
	err := s.conn.Close()
	s.conn = nil
	// Wait for reader goroutine to exit.
	select {
	case <-s.done:
	case <-time.After(s.closeWait):
	}
	// Reset mtid pool.
	s.mtidMu.Lock()
	s.mtidPool = [255]bool{}
	s.mtidMu.Unlock()
	s.mtidCond.Broadcast()
	return err
}

// isClosedErr checks if an error indicates a closed connection.
func isClosedErr(err error) bool {
	if err == nil {
		return false
	}
	// errors.As unwraps the %w chain that ReadAN2Frame adds, so a wrapped
	// *net.OpError ("use of closed network connection") is still detected.
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return netErr.Err.Error() == "use of closed network connection"
	}
	// net.ErrClosed is the modern sentinel for a closed connection; match it
	// directly too in case the OpError shape isn't present.
	return errors.Is(err, net.ErrClosed)
}

// SlotInfoFromAN2 returns the SlotInfo as known from the AN2 handshake.
// Populates Status (raw byte), State (semantic enum), and LiveAt
// (timestamp of the last GetSlotInfo reply that touched this slot).
// IsOnline is left for the Plugin to derive — it depends on
// SessionLive() which the Session itself doesn't expose.
func (s *Session) SlotInfoFromAN2(slot int) consumer.SlotInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	si := consumer.SlotInfo{Slot: slot}
	if slot >= 0 && slot < len(s.slotStatus) {
		si.Status = s.slotStatus[slot]
		si.State = si.Status.State()
	}
	if slot >= 0 && slot < len(s.slotLastSeen) {
		si.LiveAt = s.slotLastSeen[slot]
	}
	return si
}

// LastRx is the wall-clock time of the last frame received on this
// session. Lock-free atomic load; zero when nothing has been received
// yet.
func (s *Session) LastRx() time.Time {
	ns := s.lastRxNS.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// MarkSlotProbed updates the per-slot lastSeen timestamp and (when
// status is non-nil) the slot's wire-level status byte. Called by the
// keep-alive prober after a successful AN2 GetSlotInfo. Allocates the
// slot tables on first call if the handshake didn't run yet
// (defensive — handshake should always run before keep-alive starts).
func (s *Session) MarkSlotProbed(slot int, status *consumer.SlotStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slot < 0 {
		return
	}
	if slot >= len(s.slotStatus) {
		extended := make([]consumer.SlotStatus, slot+1)
		copy(extended, s.slotStatus)
		s.slotStatus = extended
	}
	if slot >= len(s.slotLastSeen) {
		extended := make([]time.Time, slot+1)
		copy(extended, s.slotLastSeen)
		s.slotLastSeen = extended
	}
	if status != nil {
		s.slotStatus[slot] = *status
	}
	s.slotLastSeen[slot] = time.Now()
}

// Host returns the connected host IP.
func (s *Session) Host() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.host
}

// Port returns the connected port.
func (s *Session) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

// Done returns a channel that is closed when the session is disconnected.
func (s *Session) Done() <-chan struct{} {
	return s.done
}
