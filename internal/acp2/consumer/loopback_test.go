package acp2

import (
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"dhs/internal/acp2/codec"
)

// loopback_test.go is the in-process AN2/TCP loopback harness shared by
// every consumer I/O test in this package. It mirrors acp1's
// echoAN2Server (an2_client_loopback_test.go) but speaks the AN2 +
// ACP2 two-layer wire format.
//
// The harness starts a real net.Listener on 127.0.0.1:0, accepts one
// connection, decodes incoming AN2 frames with the production codec,
// and writes back canned reply frames built with the same codec. This
// exercises the real Session.Connect → an2Handshake → readLoop →
// DoACP2 stack with zero external dependencies and no real device.
//
// Reply bytes follow the documented AN2 §3.3 reply shapes and the
// ACP2 message header layout (CLAUDE.md). The handshake answers are
// fixed; the ACP2 get_object / get_property / set_property answers are
// supplied per-test via fakeServer.onACP2.

// testLogger returns a logger that discards output so test runs stay
// quiet. slog.Default() floods stderr at Debug on the I/O path.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// fakeServer is a configurable AN2/ACP2 loopback peer.
type fakeServer struct {
	t  *testing.T
	ln net.Listener

	// Handshake knobs. Defaults give a sane 2-slot device.
	an2Major   uint8
	an2Minor   uint8
	slotCount  uint8   // AN2 GetDeviceInfo info byte (card slot count)
	slotStatus []uint8 // per-slot status byte for GetSlotInfo (index = slot)
	slotProtos []byte  // proto list advertised per slot
	acp2Ver    uint8   // ACP2 GetVersion reply pid byte

	// Error injection for the AN2 handshake. When the matching func is
	// requested and the flag is set, the server replies with an AN2
	// error frame (type=3) instead of a normal reply.
	an2ErrOnFunc int // -1 = none

	// onACP2 handles a decoded ACP2 request and returns the reply
	// message to send back. The harness fills in MTID. Return nil to
	// drop the request (no reply — drives ctx-timeout paths).
	onACP2 func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message

	// announceAfterConnect, when non-nil, is sent as an ACP2 announce
	// (proto=2 data frame, ACP2 mtid=0) on the given slot once the
	// handshake completes.
	announceFn func(send func(slot uint8, msg *codec.ACP2Message))

	// onOtherProto handles AN2 frames whose proto is neither internal
	// nor ACP2 (e.g. ACMP=3, vendor=4). Used by the diagnostics test.
	onOtherProto func(c net.Conn, f *codec.AN2Frame)

	mu    sync.Mutex
	conns []net.Conn
	wg    sync.WaitGroup
}

// newFakeServer starts the loopback listener and returns the server,
// its host, and port. The caller must defer srv.stop().
func newFakeServer(t *testing.T) (*fakeServer, string, int) {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeServer{
		t:            t,
		ln:           ln,
		an2Major:     1,
		an2Minor:     0,
		slotCount:    2,
		slotStatus:   []uint8{2, 2, 2}, // slot 0 controller + 2 cards, all present
		slotProtos:   []byte{byte(codec.AN2ProtoACP2)},
		acp2Ver:      1,
		an2ErrOnFunc: -1,
	}
	s.wg.Add(1)
	go s.acceptLoop()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	return s, host, port
}

func (s *fakeServer) stop() {
	_ = s.ln.Close()
	s.mu.Lock()
	for _, c := range s.conns {
		_ = c.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *fakeServer) acceptLoop() {
	defer s.wg.Done()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns = append(s.conns, conn)
		s.mu.Unlock()
		s.wg.Add(1)
		go s.serve(conn)
	}
}

// sendFrame encodes and writes one AN2 frame to the connection.
func (s *fakeServer) sendFrame(c net.Conn, f *codec.AN2Frame) {
	b, err := codec.EncodeAN2Frame(f)
	if err != nil {
		s.t.Errorf("server encode frame: %v", err)
		return
	}
	if _, err := c.Write(b); err != nil {
		return
	}
}

// sendACP2 wraps an ACP2 message in an AN2 data frame and sends it.
//
// Reply / announce messages are encoded as a 4-byte ACP2 header
// followed by the raw Body bytes — the device-side wire shape. We do
// NOT route through codec.EncodeACP2Message here: that encoder is
// request-shaped (the get_object/get_property/set_property/get_version
// branches emit a fixed request body and discard m.Body), so a reply
// carrying property headers in Body would lose them. Hand-building
// header+Body keeps the device replies byte-faithful to the §3.3 /
// ACP2 message-header layout.
func (s *fakeServer) sendACP2(c net.Conn, slot uint8, msg *codec.ACP2Message) {
	payload := make([]byte, codec.ACP2HeaderSize+len(msg.Body))
	payload[0] = byte(msg.Type)
	payload[1] = msg.MTID
	payload[2] = byte(msg.Func)
	payload[3] = msg.PID
	copy(payload[codec.ACP2HeaderSize:], msg.Body)
	s.sendFrame(c, &codec.AN2Frame{
		Proto:   codec.AN2ProtoACP2,
		Slot:    slot,
		MTID:    0, // AN2 data frames always carry AN2 mtid 0
		Type:    codec.AN2TypeData,
		Payload: payload,
	})
}

func (s *fakeServer) serve(c net.Conn) {
	defer s.wg.Done()
	defer func() { _ = c.Close() }()

	for {
		frame, err := codec.ReadAN2Frame(c)
		if err != nil {
			return
		}
		switch frame.Proto {
		case codec.AN2ProtoInternal:
			s.handleAN2(c, frame)
		case codec.AN2ProtoACP2:
			s.handleACP2(c, frame)
		default:
			// Non-ACP2 payload protocols (ACMP=3, vendor=4). The diag
			// harness answers these so RunDiagnostics doesn't block on
			// its 3s per-probe timeout.
			if s.onOtherProto != nil {
				s.onOtherProto(c, frame)
			}
		}
	}
}

// handleAN2 answers an AN2 internal (proto=0) request. The request
// payload is funcID byte + optional args; the reply echoes the AN2
// mtid and follows the §3.3 reply shapes.
func (s *fakeServer) handleAN2(c net.Conn, f *codec.AN2Frame) {
	if f.Type != codec.AN2TypeRequest || len(f.Payload) < 1 {
		return
	}
	funcID := f.Payload[0]

	if s.an2ErrOnFunc >= 0 && int(funcID) == s.an2ErrOnFunc {
		// AN2 error frame: type=3, echo mtid. Empty payload.
		s.sendFrame(c, &codec.AN2Frame{
			Proto: codec.AN2ProtoInternal,
			Slot:  f.Slot,
			MTID:  f.MTID,
			Type:  codec.AN2TypeError,
		})
		return
	}

	var payload []byte
	switch funcID {
	case codec.AN2FuncGetVersion:
		// §3.3.1: func(u8) major(u8) minor(u8)
		payload = []byte{funcID, s.an2Major, s.an2Minor}
	case codec.AN2FuncGetDeviceInfo:
		// §3.3.2: func(u8) info(u8) — info = card slot count
		payload = []byte{funcID, s.slotCount}
	case codec.AN2FuncGetSlotInfo:
		// §3.3.3: func(u8) stat(u8) num(u8) protos(u8*num).
		// Slot is in the AN2 header (f.Slot).
		stat := uint8(0)
		if int(f.Slot) < len(s.slotStatus) {
			stat = s.slotStatus[f.Slot]
		}
		payload = append([]byte{funcID, stat, byte(len(s.slotProtos))}, s.slotProtos...)
	case codec.AN2FuncEnableProtocolEvents:
		// §3.3.4: func(u8)
		payload = []byte{funcID}
	default:
		return
	}

	s.sendFrame(c, &codec.AN2Frame{
		Proto:   codec.AN2ProtoInternal,
		Slot:    f.Slot,
		MTID:    f.MTID,
		Type:    codec.AN2TypeReply,
		Payload: payload,
	})
}

// handleACP2 answers an ACP2 data frame. get_version is answered by the
// harness directly (handshake step 5); everything else is delegated to
// onACP2 so each test can shape the object/property replies. The reply
// MTID always echoes the request so the session's waiter map matches.
func (s *fakeServer) handleACP2(c net.Conn, f *codec.AN2Frame) {
	if f.Type != codec.AN2TypeData || len(f.Payload) < codec.ACP2HeaderSize {
		return
	}
	req, err := codec.DecodeACP2Message(f.Payload)
	if err != nil {
		return
	}
	// DecodeACP2Message only populates ObjID/Idx/Properties for replies
	// and announces — request bodies are left raw in req.Body. Parse the
	// request body here so onACP2 handlers can dispatch on ObjID and
	// inspect the set_property value the client sent.
	parseRequestBody(req)

	// get_version is part of the handshake — answer it unconditionally.
	if req.Type == codec.ACP2TypeRequest && req.Func == codec.ACP2FuncGetVersion {
		s.sendACP2(c, f.Slot, &codec.ACP2Message{
			Type: codec.ACP2TypeReply,
			MTID: req.MTID,
			Func: codec.ACP2FuncGetVersion,
			PID:  s.acp2Ver,
		})
		// Fire the optional announce once the handshake is done.
		if s.announceFn != nil {
			fn := s.announceFn
			s.announceFn = nil
			fn(func(slot uint8, msg *codec.ACP2Message) {
				msg.MTID = 0
				msg.Type = codec.ACP2TypeAnnounce
				s.sendACP2(c, slot, msg)
			})
		}
		return
	}

	if s.onACP2 == nil {
		return
	}
	reply := s.onACP2(f.Slot, req)
	if reply == nil {
		return // drop — drives a context-timeout path
	}
	reply.MTID = req.MTID
	s.sendACP2(c, f.Slot, reply)
}

// parseRequestBody fills req.ObjID / req.Idx / req.Properties from the
// raw request body for funcs 1-3 (get_object, get_property,
// set_property). The production decoder intentionally leaves these
// fields zero for request-type messages; the loopback server needs
// them to dispatch and to echo set values back.
func parseRequestBody(req *codec.ACP2Message) {
	switch req.Func {
	case codec.ACP2FuncGetObject, codec.ACP2FuncGetProperty, codec.ACP2FuncSetProperty:
	default:
		return
	}
	if len(req.Body) < 8 {
		return
	}
	req.ObjID = uint32(req.Body[0])<<24 | uint32(req.Body[1])<<16 | uint32(req.Body[2])<<8 | uint32(req.Body[3])
	req.Idx = uint32(req.Body[4])<<24 | uint32(req.Body[5])<<16 | uint32(req.Body[6])<<8 | uint32(req.Body[7])
	if len(req.Body) > 8 {
		if props, err := codec.DecodeProperties(req.Body[8:]); err == nil {
			req.Properties = props
		}
	}
}

// ---- reply builders ----

// objectReply builds a get_object reply message carrying the supplied
// properties for obj-id. Encodes the properties with the production
// codec so the wire bytes match what a real device emits.
func objectReply(objID uint32, props []codec.Property) *codec.ACP2Message {
	return &codec.ACP2Message{
		Type:       codec.ACP2TypeReply,
		Func:       codec.ACP2FuncGetObject,
		ObjID:      objID,
		Idx:        0,
		Properties: props,
		Body:       objectBody(objID, props),
	}
}

// propertyReply builds a get_property / set_property reply carrying a
// single property for obj-id.
func propertyReply(fn codec.ACP2Func, objID uint32, prop codec.Property) *codec.ACP2Message {
	return &codec.ACP2Message{
		Type:       codec.ACP2TypeReply,
		Func:       fn,
		ObjID:      objID,
		Idx:        0,
		Properties: []codec.Property{prop},
		Body:       objectBody(objID, []codec.Property{prop}),
	}
}

// errorReply builds an ACP2 error message with the given stat code. Per
// spec the error message is the 4-byte header only; func carries stat.
func errorReply(stat codec.ACP2ErrStatus) *codec.ACP2Message {
	return &codec.ACP2Message{
		Type: codec.ACP2TypeError,
		Func: codec.ACP2Func(stat),
	}
}

// objectBody encodes obj-id(u32) + idx(u32) + properties into the raw
// Body bytes. The reply goes back out through EncodeACP2Message's
// default branch (Func != get_object/property/set), which emits the
// header followed by Body verbatim — so the device-side encoding stays
// byte-faithful regardless of the func.
func objectBody(objID uint32, props []codec.Property) []byte {
	body := make([]byte, 8)
	body[0] = byte(objID >> 24)
	body[1] = byte(objID >> 16)
	body[2] = byte(objID >> 8)
	body[3] = byte(objID)
	// idx stays 0
	pb, _ := codec.EncodeProperties(props)
	return append(body, pb...)
}

// u32Prop builds a property whose value is a single big-endian u32 —
// the wire shape for object_type, access, number_type, etc. (pid data
// stored as u32, low byte significant).
func u32Prop(pid uint8, vtype uint8, v uint32) codec.Property {
	data := []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
	return codec.Property{PID: pid, VType: vtype, PLen: uint16(4 + len(data)), Data: data}
}

// childrenProp builds a pid=14 children property (u32[] child obj-ids).
func childrenProp(ids ...uint32) codec.Property {
	data := make([]byte, 0, len(ids)*4)
	for _, id := range ids {
		data = append(data, byte(id>>24), byte(id>>16), byte(id>>8), byte(id))
	}
	return codec.Property{PID: codec.PIDChildren, PLen: uint16(4 + len(data)), Data: data}
}

// waitFor polls cond until it is true or the deadline elapses. Keeps
// announce-delivery assertions deterministic without a fixed sleep.
func waitFor(t *testing.T, d time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return cond()
}
