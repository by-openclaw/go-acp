package acp1

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"dhs/internal/acp1/codec"
	an2 "dhs/internal/acp2/codec"
)

// AN2 codec re-exports — the transport framer is shared with ACP2 today.
// A future refactor moves it to internal/transport/an2 so neither protocol
// has the architectural awkwardness; for now we import.
const (
	an2DefaultPort = 2072
	an2MaxFrameLen = an2.AN2HeaderSize + an2.MaxPayload
	an2RxTimeout   = 30 * time.Second
)

// ServeAN2 runs the ACP1 Mode C (AN2/TCP) listener on the given address.
// AN2 frames carry either AN2-internal (proto=0) control messages or
// ACP1 payloads (proto=1). Mode C clients MUST send AN2
// EnableProtocolEvents([1]) before receiving announces — the per-
// session flag is honoured during fan-out.
func (s *server) ServeAN2(ctx context.Context, addr string) error {
	tcpAddr, err := net.ResolveTCPAddr("tcp4", addr)
	if err != nil {
		return fmt.Errorf("acp1 provider an2: resolve %q: %w", addr, err)
	}
	ln, err := net.ListenTCP("tcp4", tcpAddr)
	if err != nil {
		return fmt.Errorf("acp1 provider an2: listen %q: %w", addr, err)
	}
	defer func() { _ = ln.Close() }()

	s.logger.Info("acp1 provider an2 listening", slog.String("addr", ln.Addr().String()))

	reg := newAN2SessionRegistry(s.logger)
	s.an2Registry = reg

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.AcceptTCP()
		if err != nil {
			if isClosed(err) || errors.Is(ctx.Err(), context.Canceled) {
				return nil
			}
			s.logger.Warn("acp1 an2 accept failed", slog.String("err", err.Error()))
			continue
		}
		_ = conn.SetNoDelay(true)
		ip := remoteIP(conn.RemoteAddr())
		if !reg.tryAdd(ip) {
			s.logger.Warn("acp1 an2 refusing session: per-ip cap exceeded",
				slog.String("ip", ip),
				slog.Int("max", tcpMaxSessionsPerIP))
			_ = conn.Close()
			continue
		}
		go s.serveAN2Session(ctx, conn, ip, reg)
	}
}

// serveAN2Session reads AN2 frames, dispatches by proto, and writes
// AN2-framed replies back. EnableProtocolEvents toggles the per-
// session announce-enable flag so untrusted clients don't receive
// traffic by default.
func (s *server) serveAN2Session(ctx context.Context, conn *net.TCPConn, ip string, reg *an2SessionRegistry) {
	send := make(chan []byte, tcpSendChanCap)
	sess := reg.register(ip, conn, send)
	defer func() {
		_ = conn.Close()
		reg.remove(ip, sess.id)
	}()

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			select {
			case <-ctx.Done():
				return
			case payload, ok := <-send:
				if !ok {
					return
				}
				_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if _, err := conn.Write(payload); err != nil {
					if !isClosed(err) {
						s.logger.Warn("acp1 an2 write", slog.String("err", err.Error()))
					}
					return
				}
			}
		}
	}()

	for {
		_ = conn.SetReadDeadline(time.Now().Add(an2RxTimeout))
		f, err := readAN2Frame(conn)
		if err != nil {
			if isClosed(err) || errors.Is(err, io.EOF) {
				break
			}
			s.logger.Warn("acp1 an2 read", slog.String("err", err.Error()))
			break
		}

		switch f.Proto {
		case an2.AN2ProtoInternal:
			if reply := s.handleAN2Internal(f, sess); reply != nil {
				if b, err := an2.EncodeAN2Frame(reply); err == nil {
					enqueue(sess.send, b, "an2-internal-reply", s.logger)
				}
			}
		case an2.AN2ProtoACP1:
			s.handleAN2ACP1Frame(f, sess)
		default:
			s.logger.Debug("acp1 an2: unsupported proto",
				slog.String("proto", f.Proto.String()),
				slog.String("ip", ip))
		}
	}

	close(send)
	<-writerDone
}

// handleAN2Internal answers AN2 control messages. Minimal subset: we
// implement GetVersion + EnableProtocolEvents. GetDeviceInfo and
// GetSlotInfo return zero-shaped replies — Mode C consumers that walk
// ACP1 don't strictly need them since the ACP1 Frame object on the
// rack controller carries the same slot-status data.
func (s *server) handleAN2Internal(f *an2.AN2Frame, sess *an2Session) *an2.AN2Frame {
	if f.Type != an2.AN2TypeRequest || len(f.Payload) < 1 {
		return nil
	}
	funcID := f.Payload[0]
	reply := &an2.AN2Frame{
		Proto: an2.AN2ProtoInternal,
		Slot:  f.Slot,
		MTID:  f.MTID,
		Type:  an2.AN2TypeReply,
	}
	switch funcID {
	case an2.AN2FuncGetVersion:
		// Reply: [func, version_major, version_minor]. We support v1.
		reply.Payload = []byte{funcID, 0x01, 0x00}
	case an2.AN2FuncGetDeviceInfo:
		// Reply: [func, num_slots]. Use the tree's slot count. Real
		// devices add more fields; for ACP1 Mode C the consumer can
		// fall through to the ACP1 Frame object for full status.
		reply.Payload = []byte{funcID, byte(s.tree.numSlots())}
	case an2.AN2FuncGetSlotInfo:
		// Reply: [func, slot, present_flag, ...]. Synthesise a minimal
		// shape — present=1 if the tree has any object on that slot.
		var slot uint8
		if len(f.Payload) >= 2 {
			slot = f.Payload[1]
		}
		present := byte(0)
		if s.tree.slotPresent(slot) {
			present = 1
		}
		reply.Payload = []byte{funcID, slot, present}
	case an2.AN2FuncEnableProtocolEvents:
		// Payload: [func, proto1, proto2, ...]. Toggle the session
		// flag for each requested proto.
		sess.enableProtos(f.Payload[1:])
		reply.Payload = []byte{funcID, 0x00} // 0 = ok
	default:
		// Echo the func ID back as an error; consumers ignore it.
		reply.Type = an2.AN2TypeError
		reply.Payload = []byte{funcID}
	}
	return reply
}

// handleAN2ACP1Frame routes an ACP1 PDU carried inside an AN2 frame
// through the existing handleRequest dispatcher and emits the reply
// (and, for mutating methods, a value-change announce) back.
func (s *server) handleAN2ACP1Frame(f *an2.AN2Frame, sess *an2Session) {
	if f.Type != an2.AN2TypeData || len(f.Payload) == 0 {
		return
	}
	req, err := codec.Decode(f.Payload)
	if err != nil {
		s.logger.Warn("acp1 an2: decode payload",
			slog.String("err", err.Error()),
			slog.String("ip", sess.ip))
		return
	}
	reply, ann := s.handleRequest(req)
	if reply != nil {
		body, err := reply.Encode()
		if err == nil {
			out := &an2.AN2Frame{
				Proto:   an2.AN2ProtoACP1,
				Slot:    f.Slot,
				MTID:    0,
				Type:    an2.AN2TypeData,
				Payload: body,
			}
			if b, err := an2.EncodeAN2Frame(out); err == nil {
				enqueue(sess.send, b, "an2-acp1-reply", s.logger)
			}
		}
	}
	if ann != nil {
		// Route through broadcastAnnounce for the Broadcasts gate
		// (#257) plus the UDP+TCP bridges.
		s.broadcastAnnounce(ann)
	}
}

// broadcastAN2Announce wraps an already-encoded ACP1 message in an AN2
// frame and fans it to every AN2 session that has enabled ACP1 events.
// Called from the UDP-side broadcastAnnounce bridge so an announce
// produced via UDP set still reaches AN2 consumers.
func (s *server) broadcastAN2Announce(acp1Body []byte) {
	if s.an2Registry == nil {
		return
	}
	out := &an2.AN2Frame{
		Proto:   an2.AN2ProtoACP1,
		Slot:    0,
		MTID:    0,
		Type:    an2.AN2TypeData,
		Payload: acp1Body,
	}
	b, err := an2.EncodeAN2Frame(out)
	if err != nil {
		return
	}
	s.an2Registry.broadcastACP1(b)
}

// ---- helpers -----------------------------------------------------------

func readAN2Frame(conn *net.TCPConn) (*an2.AN2Frame, error) {
	var hdr [an2.AN2HeaderSize]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return nil, err
	}
	if binary.BigEndian.Uint16(hdr[0:2]) != an2.AN2Magic {
		return nil, an2.ErrBadMagic
	}
	dlen := int(binary.BigEndian.Uint16(hdr[6:8]))
	if dlen > an2.MaxPayload {
		return nil, an2.ErrFrameTooBig
	}
	body := make([]byte, dlen)
	if dlen > 0 {
		if _, err := io.ReadFull(conn, body); err != nil {
			return nil, err
		}
	}
	return &an2.AN2Frame{
		Proto:   an2.AN2Proto(hdr[2]),
		Slot:    hdr[3],
		MTID:    hdr[4],
		Type:    an2.AN2Type(hdr[5]),
		Payload: body,
	}, nil
}

// ---- session registry --------------------------------------------------

type an2SessionRegistry struct {
	logger *slog.Logger

	mu       sync.RWMutex
	sessions map[uint64]*an2Session
	perIP    map[string]int
}

type an2Session struct {
	id   uint64
	ip   string
	conn *net.TCPConn
	send chan []byte

	mu             sync.Mutex
	eventsEnabled  map[an2.AN2Proto]bool
}

func newAN2SessionRegistry(logger *slog.Logger) *an2SessionRegistry {
	if logger == nil {
		logger = slog.Default()
	}
	return &an2SessionRegistry{
		logger:   logger,
		sessions: map[uint64]*an2Session{},
		perIP:    map[string]int{},
	}
}

func (r *an2SessionRegistry) tryAdd(ip string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.perIP[ip] >= tcpMaxSessionsPerIP {
		return false
	}
	r.perIP[ip]++
	return true
}

func (r *an2SessionRegistry) register(ip string, conn *net.TCPConn, send chan []byte) *an2Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := sessionID(conn)
	sess := &an2Session{
		id: id, ip: ip, conn: conn, send: send,
		eventsEnabled: map[an2.AN2Proto]bool{},
	}
	r.sessions[id] = sess
	return sess
}

func (r *an2SessionRegistry) remove(ip string, id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.perIP[ip] > 0 {
		r.perIP[ip]--
	}
	delete(r.sessions, id)
}

// broadcastACP1 fans an AN2-encoded ACP1 announce to every session
// that has enabled events for proto=1. Drop-on-full per session.
func (r *an2SessionRegistry) broadcastACP1(payload []byte) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, sess := range r.sessions {
		if !sess.acp1EventsEnabled() {
			continue
		}
		select {
		case sess.send <- payload:
		default:
			r.logger.Warn("acp1 an2 broadcast drop (slow consumer)",
				slog.String("ip", sess.ip))
		}
	}
}

func (s *an2Session) enableProtos(protos []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range protos {
		s.eventsEnabled[an2.AN2Proto(p)] = true
	}
}

func (s *an2Session) acp1EventsEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.eventsEnabled[an2.AN2ProtoACP1]
}
