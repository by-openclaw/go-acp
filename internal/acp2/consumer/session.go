package acp2

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"acp/internal/protocol"
	"acp/internal/protocol/compliance"
	"acp/internal/transport"
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
	an2Version  uint8
	numSlots    int
	slotStatus  []protocol.SlotStatus
	acp2Version uint8

	// mtid pool: 1-255 available, 0 reserved for announces.
	mtidMu   sync.Mutex
	mtidPool [255]bool // mtidPool[i] true = mtid (i+1) is in use
	mtidCond *sync.Cond

	// Pending request waiters: keyed by ACP2 mtid.
	waitMu  sync.Mutex
	waiters map[uint8]chan *ACP2Message

	// Announce listeners.
	annMu     sync.Mutex
	annNextID int
	annSubs   map[int]AnnounceFunc

	// Reader goroutine lifecycle.
	done     chan struct{}
	closeErr error

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
}

// AnnounceFunc is the callback signature for ACP2 announce subscriptions.
type AnnounceFunc func(slot uint8, msg *ACP2Message)

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
		logger:  logger,
		waiters: make(map[uint8]chan *ACP2Message),
		annSubs: make(map[int]AnnounceFunc),
		done:    make(chan struct{}),
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
		port = DefaultPort
	}

	s.logger.Debug("acp2: dialing", "host", ip, "port", port)

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, fmt.Sprintf("%d", port)))
	if err != nil {
		return &protocol.TransportError{Op: "connect", Err: err}
	}
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
	}
	s.conn = conn
	s.host = ip
	s.port = port
	s.done = make(chan struct{})
	s.waiters = make(map[uint8]chan *ACP2Message)

	// Start the reader goroutine before the handshake so replies are routed.
	go s.readLoop()

	// Run the AN2 init sequence.
	if err := s.an2Handshake(ctx); err != nil {
		_ = s.closeLocked()
		return err
	}

	s.logger.Info("acp2: connected",
		"host", ip, "port", port,
		"an2_version", s.an2Version,
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
	reply, err := s.an2Request(ctx, AN2FuncGetVersion, 0, nil)
	if err != nil {
		return fmt.Errorf("an2 GetVersion: %w", err)
	}
	// Reply: func_echo(u8) + major(u8) + minor(u8). Spec §3.3.1.
	// Version is at reply[2] (minor), not reply[0].
	if len(reply) >= 3 {
		s.an2Version = reply[2]
	} else if len(reply) >= 1 {
		s.an2Version = reply[0]
	}
	s.logger.Debug("acp2: AN2 version", "version", s.an2Version, "raw", fmt.Sprintf("%x", reply))

	// 2. AN2 GetDeviceInfo
	s.logger.Debug("acp2: AN2 GetDeviceInfo")
	reply, err = s.an2Request(ctx, AN2FuncGetDeviceInfo, 0, nil)
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
	s.slotStatus = make([]protocol.SlotStatus, totalSlots)
	for slot := 0; slot < totalSlots; slot++ {
		s.logger.Debug("acp2: AN2 GetSlotInfo", "slot", slot)
		// AN2 spec §3.3.3: dlen=1 (just funcID). Slot is in the AN2 header,
		// NOT duplicated in the payload.
		reply, err = s.an2Request(ctx, AN2FuncGetSlotInfo, byte(slot), nil)
		if err != nil {
			s.logger.Debug("acp2: GetSlotInfo failed", "slot", slot, "err", err)
			continue
		}
		// Reply: func_echo(u8) + stat(u8) + num_protos(u8) + protos(u8[])
		// Spec §3.3.3 p. 9. Status is at reply[1], not reply[0].
		if len(reply) >= 2 {
			s.slotStatus[slot] = protocol.SlotStatus(reply[1])
			s.logger.Debug("acp2: slot info", "slot", slot, "status", reply[1],
				"raw", fmt.Sprintf("%x", reply))
		}
	}

	// 4. AN2 EnableProtocolEvents([2]) — required for ACP2 announces
	s.logger.Debug("acp2: AN2 EnableProtocolEvents")
	enablePayload := []byte{1, byte(AN2ProtoACP2)} // count=1, proto=2
	_, err = s.an2Request(ctx, AN2FuncEnableProtocolEvents, 0, enablePayload)
	if err != nil {
		return fmt.Errorf("an2 EnableProtocolEvents: %w", err)
	}

	// 5. ACP2 GetVersion
	s.logger.Debug("acp2: ACP2 GetVersion")
	acp2Reply, err := s.DoACP2(ctx, 0, &ACP2Message{
		Type: ACP2TypeRequest,
		Func: ACP2FuncGetVersion,
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

	frame := &AN2Frame{
		Proto:   AN2ProtoInternal,
		Slot:    slot,
		MTID:    an2MTID,
		Type:    AN2TypeRequest,
		Payload: reqPayload,
	}

	// Register a waiter for this AN2 mtid. We reuse the ACP2 waiter map
	// with a convention: AN2 internal replies come back with proto=0 and
	// AN2 mtid matching. The reader goroutine routes them to a synthetic
	// ACP2Message with MTID=an2MTID.
	ch := make(chan *ACP2Message, 1)
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
		if msg == nil {
			return nil, fmt.Errorf("acp2: nil reply for AN2 func %d", funcID)
		}
		return msg.Body, nil
	}
}

// DoACP2 sends an ACP2 request (inside an AN2 data frame) and waits for
// the corresponding reply. Allocates and releases an ACP2 mtid.
func (s *Session) DoACP2(ctx context.Context, slot uint8, req *ACP2Message) (*ACP2Message, error) {
	// Allocate a mtid.
	mtid, err := s.allocMTID(ctx)
	if err != nil {
		return nil, err
	}
	defer s.releaseMTID(mtid)

	req.MTID = mtid
	if req.Type == 0 {
		req.Type = ACP2TypeRequest
	}

	payload, err := EncodeACP2Message(req)
	if err != nil {
		return nil, err
	}

	// ACP2 messages are carried in AN2 data frames (type=4, AN2 mtid=0).
	frame := &AN2Frame{
		Proto:   AN2ProtoACP2,
		Slot:    slot,
		MTID:    0, // AN2 mtid always 0 for data frames
		Type:    AN2TypeData,
		Payload: payload,
	}

	ch := make(chan *ACP2Message, 1)
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
		if reply == nil {
			return nil, fmt.Errorf("acp2: nil reply for mtid=%d", mtid)
		}
		if reply.Type == ACP2TypeError {
			// Fire the per-stat-code compliance event so the session
			// profile reflects spec-listed error frequencies. Status
			// codes 0..5 defined in acp2_protocol.pdf p.5; error
			// replies carry the code in the Func slot (codec.go
			// ACP2Message.Func comment). Switch lives in the pure
			// helper EventForErrStatus so replay tests can assert it.
			if label := EventForErrStatus(ACP2ErrStatus(reply.Func)); label != "" {
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
func (s *Session) sendFrame(ctx context.Context, f *AN2Frame) error {
	data, err := EncodeAN2Frame(f)
	if err != nil {
		return err
	}
	s.logger.Debug("acp2: sendFrame",
		"proto", f.Proto, "slot", f.Slot, "mtid", f.MTID, "type", f.Type,
		"frame_hex", fmt.Sprintf("%x", data))

	if s.recorder != nil {
		s.recorder.Record("acp2", "tx", data)
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if s.conn == nil {
		return protocol.ErrNotConnected
	}

	if dl, ok := ctx.Deadline(); ok {
		_ = s.conn.SetWriteDeadline(dl)
	} else {
		_ = s.conn.SetWriteDeadline(time.Time{})
	}

	if _, err := s.conn.Write(data); err != nil {
		return &protocol.TransportError{Op: "send", Err: err}
	}
	return nil
}

// readLoop runs in a goroutine, reading AN2 frames from the TCP connection
// and routing them to the appropriate waiter or announce subscriber.
func (s *Session) readLoop() {
	defer close(s.done)

	for {
		conn := s.conn
		if conn == nil {
			return
		}
		frame, err := ReadAN2Frame(conn)
		if err != nil {
			if err == io.EOF || isClosedErr(err) {
				s.logger.Debug("acp2: reader: connection closed")
			} else {
				s.logger.Debug("acp2: reader: connection closed", "err", err)
			}
			s.closeErr = err
			return
		}

		// Record raw frame for capture (includes announces — tests need them).
		if s.recorder != nil {
			if raw, encErr := EncodeAN2Frame(frame); encErr == nil {
				s.recorder.Record("acp2", "rx", raw)
			}
		}

		// Log full frame hex for requests/replies; skip for ACP2 announces
		// (they flood the log with large SDP payloads every ~2s).
		isAnnounce := frame.Proto == AN2ProtoACP2 &&
			len(frame.Payload) >= 1 &&
			frame.Payload[0] == byte(ACP2TypeAnnounce)
		if !isAnnounce {
			s.logger.Debug("acp2: reader: frame",
				"proto", frame.Proto, "slot", frame.Slot,
				"mtid", frame.MTID, "type", frame.Type,
				"dlen", len(frame.Payload),
				"payload_hex", fmt.Sprintf("%x", frame.Payload))
		}

		switch frame.Proto {
		case AN2ProtoInternal:
			s.handleAN2Internal(frame)
		case AN2ProtoACP2:
			s.handleACP2Frame(frame)
		default:
			s.logger.Debug("acp2: reader: ignoring frame with proto", "proto", frame.Proto)
		}
	}
}

// handleAN2Internal routes AN2 internal (proto=0) replies and events.
func (s *Session) handleAN2Internal(f *AN2Frame) {
	switch f.Type {
	case AN2TypeReply:
		// Route to waiter by AN2 mtid.
		synth := &ACP2Message{
			Type: ACP2TypeReply,
			MTID: f.MTID,
			Body: f.Payload,
		}
		s.routeReply(f.MTID, synth)

	case AN2TypeEvent:
		// AN2 slot events (e.g. card insertion/removal).
		s.logger.Debug("acp2: AN2 slot event", "slot", f.Slot, "payload_len", len(f.Payload))
		if len(f.Payload) >= 1 {
			status := protocol.SlotStatus(f.Payload[0])
			s.mu.Lock()
			if int(f.Slot) < len(s.slotStatus) {
				s.slotStatus[f.Slot] = status
			}
			s.mu.Unlock()
		}

	case AN2TypeError:
		s.logger.Warn("acp2: AN2 error", "slot", f.Slot, "mtid", f.MTID)
		synth := &ACP2Message{
			Type: ACP2TypeError,
			MTID: f.MTID,
			Body: f.Payload,
		}
		s.routeReply(f.MTID, synth)

	default:
		s.logger.Debug("acp2: AN2 unhandled type", "type", f.Type)
	}
}

// handleACP2Frame routes ACP2 data/event frames.
func (s *Session) handleACP2Frame(f *AN2Frame) {
	if f.Type != AN2TypeData {
		s.logger.Debug("acp2: non-data ACP2 frame", "type", f.Type)
		return
	}
	if len(f.Payload) < ACP2HeaderSize {
		s.logger.Warn("acp2: ACP2 payload too short", "len", len(f.Payload))
		return
	}

	msg, err := DecodeACP2Message(f.Payload)
	if err != nil {
		s.logger.Warn("acp2: decode ACP2 message", "err", err)
		return
	}

	if msg.Type == ACP2TypeAnnounce {
		// Announce debug: include first 20 bytes hex for diagnosis.
		hexDump := fmt.Sprintf("%x", f.Payload)
		if len(hexDump) > 40 {
			hexDump = hexDump[:40] + "..."
		}
		s.logger.Debug("acp2: announce",
			"slot", f.Slot, "obj_id", msg.ObjID, "pid", msg.PID,
			"props", len(msg.Properties), "dlen", len(f.Payload), "hex", hexDump)
		// Fan out to all subscribers.
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
func (s *Session) routeReply(mtid uint8, msg *ACP2Message) {
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
	}
}

// allocMTID allocates a free ACP2 mtid (1-255). Blocks if all are in use.
func (s *Session) allocMTID(ctx context.Context) (uint8, error) {
	s.mtidMu.Lock()
	defer s.mtidMu.Unlock()

	for {
		// Check context first.
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
		// All mtids in use — wait for a release.
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

// AN2Version returns the AN2 protocol version.
func (s *Session) AN2Version() uint8 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.an2Version
}

// ACP2Version returns the ACP2 protocol version.
func (s *Session) ACP2Version() uint8 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.acp2Version
}

// SlotStatus returns the status of a given slot.
func (s *Session) SlotStatus(slot int) protocol.SlotStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slot < 0 || slot >= len(s.slotStatus) {
		return protocol.SlotNoCard
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
	case <-time.After(2 * time.Second):
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
	if netErr, ok := err.(*net.OpError); ok {
		return netErr.Err.Error() == "use of closed network connection"
	}
	return false
}

// SlotInfoFromAN2 returns the SlotInfo as known from the AN2 handshake.
func (s *Session) SlotInfoFromAN2(slot int) protocol.SlotInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	si := protocol.SlotInfo{Slot: slot}
	if slot >= 0 && slot < len(s.slotStatus) {
		si.Status = s.slotStatus[slot]
	}
	return si
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
