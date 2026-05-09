package acp2

import (
	"fmt"
	"log/slog"
	"dhs/internal/acp2/codec"
)

// Version constants advertised by this provider.
//
// an2Version is the AN2 protocol version per spec §3.3.1: a single
// u16 BE field on the wire ("ver(u16)" in the reply table on p.7-8).
// Real EVS Neuron firmware (5.3.5 + 6.7.4) emits 0x00 0x01 = 1.
//
// acp2Version is the ACP2 protocol version, carried in the ACP2
// message header's pid byte of a get_version reply (single u8).
const (
	an2Version  uint16 = 1
	acp2Version uint8  = 1
)

// Slot status codes emitted by GetSlotInfo. Values mirror the consumer's
// protocol.SlotStatus semantics (2 = present / card inserted). Spec
// §3.3.3 p.9.
const (
	slotStatusEmpty   uint8 = 0
	slotStatusPresent uint8 = 2
)

// handleFrame replaces the Step-2a log-and-drop stub with real dispatch.
// AN2 internal frames (proto=0) and ACP2 frames (proto=2) route to
// their own handlers; anything else is logged and dropped.
//
// See CLAUDE.md: ACP2 announces are gated on EnableProtocolEvents([2]),
// and AN2 mtid is always 0 for AN2 data frames (type=4).
func (s *session) dispatch(f *codec.AN2Frame) {
	switch f.Proto {
	case codec.AN2ProtoInternal:
		s.handleAN2Internal(f)
	case codec.AN2ProtoACP2:
		s.handleACP2(f)
	default:
		s.srv.logger.Debug("acp2 frame dropped — unsupported proto",
			slog.String("proto", f.Proto.String()),
		)
	}
}

// handleAN2Internal implements the proto=0 handshake a consumer runs
// before sending any ACP2 traffic. Reply byte layouts per spec:
//
//	1. GetVersion           §3.3.1 -> [0, ver_hi, ver_lo]      ver=u16BE
//	2. GetDeviceInfo        §3.3.2 -> [1, slot_count]          dlen=2
//	3. GetSlotInfo(slot)    §3.3.3 -> [2, status, num, protos] dlen=3+num
//	4. EnableProtocolEvents §3.3.4 -> [3]                      dlen=1
//
// All replies mirror the request's AN2 mtid + slot per spec §3.3 so
// the consumer's waiter table correlates them cleanly.
func (s *session) handleAN2Internal(f *codec.AN2Frame) {
	if f.Type != codec.AN2TypeRequest {
		s.srv.logger.Debug("an2 internal: non-request, dropped",
			slog.String("type", f.Type.String()),
		)
		return
	}
	if len(f.Payload) < 1 {
		s.srv.logger.Warn("an2 internal: empty payload, dropping")
		return
	}

	funcID := f.Payload[0]
	var body []byte
	switch funcID {
	case codec.AN2FuncGetVersion:
		// Spec §3.3.1 (an2_protocol.pdf p.7-8): reply payload is
		// func(u8) + ver(u16), dlen=3. ver is a single u16 BE field,
		// NOT a (major u8, minor u8) split. Real EVS Neuron emits
		// 0x00 0x01 (= ver 1); a spec-strict consumer reading our
		// previous [major=1, minor=0] bytes interpreted ver=0x0100=256.
		body = []byte{funcID, byte(an2Version >> 8), byte(an2Version)}
	case codec.AN2FuncGetDeviceInfo:
		// Spec §1.2.2 + §3.3.2 (p.8): info = total slot count incl
		// slot 0. The example "5(=4+1), 9(=8+1), 19(=18+1), since
		// slot0 is also counted" means the count is (max present
		// slot index) + 1, not just the cardinality of present slots
		// (which would skip empty intermediates). Real Neuron with
		// {RC=slot0, card=slot1} reports info=2.
		s.srv.tree.mu.RLock()
		var maxSlot uint8
		for sl := range s.srv.tree.perSlot {
			if sl > maxSlot {
				maxSlot = sl
			}
		}
		s.srv.tree.mu.RUnlock()
		body = []byte{funcID, maxSlot + 1}
	case codec.AN2FuncGetSlotInfo:
		// AN2 spec §3.3.3: request is dlen=1 (just funcID). The requested
		// slot is carried in the AN2 frame header's Slot field, NOT in the
		// payload. Read it from f.Slot.
		status, protos := s.srv.slotInfo(f.Slot)
		body = append([]byte{funcID, status, uint8(len(protos))}, protos...)
	case codec.AN2FuncEnableProtocolEvents:
		// Payload: funcID, count, proto_ids[count]
		if len(f.Payload) < 2 {
			s.srv.logger.Warn("an2 EnableProtocolEvents: missing count byte")
			return
		}
		count := int(f.Payload[1])
		enabled := make([]int, 0, count)
		for i := 0; i < count && 2+i < len(f.Payload); i++ {
			proto := codec.AN2Proto(f.Payload[2+i])
			s.enable(proto)
			enabled = append(enabled, int(proto))
		}
		s.srv.logger.Info("an2 EnableProtocolEvents",
			slog.Int("slot", int(f.Slot)),
			slog.Any("payload_hex", fmt.Sprintf("%x", f.Payload)),
			slog.Int("count", count),
			slog.Any("enabled_protos", enabled),
		)
		// Spec §3.3.4 (an2_protocol.pdf p.10): reply payload is just
		// func(u8), dlen=1. Real EVS Neuron emits [3]; Cerebrum drops
		// the session at the 5 s timeout if the reply carries any
		// trailing bytes.
		body = []byte{funcID}
	default:
		s.srv.logger.Debug("an2 internal: unknown funcID",
			slog.Int("funcID", int(funcID)),
		)
		return
	}

	reply := &codec.AN2Frame{
		Proto:   codec.AN2ProtoInternal,
		Slot:    f.Slot,
		MTID:    f.MTID,
		Type:    codec.AN2TypeReply,
		Payload: body,
	}
	if err := s.write(reply); err != nil {
		s.srv.logger.Debug("an2 reply write failed",
			slog.String("err", err.Error()),
		)
	}
}

// handleACP2 dispatches one ACP2 proto=2 request. Decodes the ACP2
// message, routes by Func, and writes the reply inside an AN2 data
// frame back to the consumer.
//
// MVP scope: get_version + get_object. get_property + set_property
// ship in Step 2e.
func (s *session) handleACP2(f *codec.AN2Frame) {
	if f.Type != codec.AN2TypeData {
		s.srv.logger.Debug("acp2 dispatch: ignoring non-data frame",
			slog.String("type", f.Type.String()),
		)
		return
	}
	msg, err := codec.DecodeACP2Message(f.Payload)
	if err != nil {
		s.srv.logger.Warn("acp2 decode failed", slog.String("err", err.Error()))
		return
	}
	if msg.Type != codec.ACP2TypeRequest {
		return
	}
	// Consumer's DecodeACP2Message only parses obj-id / idx for replies
	// and announces (codec.go line 144). For incoming requests we parse
	// them ourselves from msg.Body[0:4]/[4:8] so the handler has the
	// addressed object.
	if (msg.Func == codec.ACP2FuncGetObject ||
		msg.Func == codec.ACP2FuncGetProperty ||
		msg.Func == codec.ACP2FuncSetProperty) && len(msg.Body) >= 8 {
		msg.ObjID = beU32(msg.Body[0:4])
		msg.Idx = beU32(msg.Body[4:8])
		// set_property carries the incoming property header after idx.
		if msg.Func == codec.ACP2FuncSetProperty && len(msg.Body) > 8 {
			props, err := codec.DecodeProperties(msg.Body[8:])
			if err == nil {
				msg.Properties = props
			}
		}
	}

	switch msg.Func {
	case codec.ACP2FuncGetVersion:
		s.replyACP2(f.Slot, &codec.ACP2Message{
			Type: codec.ACP2TypeReply,
			MTID: msg.MTID,
			Func: codec.ACP2FuncGetVersion,
			PID:  acp2Version,
		})
	case codec.ACP2FuncGetObject:
		s.handleGetObject(f.Slot, msg)
	case codec.ACP2FuncGetProperty:
		s.handleGetProperty(f.Slot, msg)
	case codec.ACP2FuncSetProperty:
		s.handleSetProperty(f.Slot, msg)
	default:
		s.replyACP2(f.Slot, errorACP2(msg, codec.ErrProtocol))
	}
}

// handleGetProperty returns one specific property of an object. The
// request carries the requested pid in msg.PID; the reply echoes the
// same pid with its current value.
func (s *session) handleGetProperty(slot uint8, msg *codec.ACP2Message) {
	e, ok := s.srv.tree.lookup(slot, msg.ObjID)
	if !ok {
		s.replyACP2(slot, errorACP2(msg, codec.ErrInvalidObjID))
		return
	}
	// Spec §4: idx != 0 on a non-preset object is stat=2 invalid_idx.
	// Preset objects carry pid 7 preset_depth; everything else is flat
	// and only accepts idx=0 (ACTIVE INDEX).
	if msg.Idx != 0 && e.objType != codec.ObjTypePreset {
		s.replyACP2(slot, errorACP2(msg, codec.ErrInvalidIdx))
		return
	}
	all, err := buildProperties(e)
	if err != nil {
		s.replyACP2(slot, errorACP2(msg, codec.ErrProtocol))
		return
	}
	for i := range all {
		if all[i].PID == msg.PID {
			body, err := codec.EncodeProperty(&all[i])
			if err != nil {
				s.replyACP2(slot, errorACP2(msg, codec.ErrProtocol))
				return
			}
			s.replyACP2(slot, &codec.ACP2Message{
				Type:  codec.ACP2TypeReply,
				MTID:  msg.MTID,
				Func:  codec.ACP2FuncGetProperty,
				PID:   msg.PID,
				ObjID: msg.ObjID,
				Idx:   msg.Idx,
				Body:  appendObjIDIdx(msg.ObjID, msg.Idx, body),
			})
			return
		}
	}
	s.replyACP2(slot, errorACP2(msg, codec.ErrInvalidPID))
}

// handleSetProperty mutates the tree for the requested (obj-id, pid)
// + incoming property, sends a reply with the confirmed post-state,
// and broadcasts an announce to every session that has
// EnableProtocolEvents([ACP2]) subscribed.
func (s *session) handleSetProperty(slot uint8, msg *codec.ACP2Message) {
	e, ok := s.srv.tree.lookup(slot, msg.ObjID)
	if !ok {
		s.replyACP2(slot, errorACP2(msg, codec.ErrInvalidObjID))
		return
	}
	if len(msg.Properties) == 0 {
		s.replyACP2(slot, errorACP2(msg, codec.ErrProtocol))
		return
	}
	in := &msg.Properties[0]
	post, errStatus, err := s.srv.applySet(e, in)
	if err != nil {
		s.srv.logger.Debug("acp2 applySet",
			slog.Int("obj", int(msg.ObjID)),
			slog.String("err", err.Error()),
		)
		s.replyACP2(slot, errorACP2(msg, errStatus))
		return
	}
	body, err := codec.EncodeProperty(&post)
	if err != nil {
		s.replyACP2(slot, errorACP2(msg, codec.ErrProtocol))
		return
	}
	// Reply with the confirmed post-state.
	reply := &codec.ACP2Message{
		Type:  codec.ACP2TypeReply,
		MTID:  msg.MTID,
		Func:  codec.ACP2FuncSetProperty,
		PID:   msg.PID,
		ObjID: msg.ObjID,
		Idx:   msg.Idx,
		Body:  appendObjIDIdx(msg.ObjID, msg.Idx, body),
	}
	s.replyACP2(slot, reply)

	// Fan out the announce to every session with ACP2 events enabled.
	// Spec §3.2 / §4.2 ACP2 Announce header: [type=2, mtid=0, stat=0, pid].
	// Byte 2 is stat (always 0 for announces) — NOT a second copy of pid.
	// Emitting pid here makes Lawo VSM silently drop the frame; its parser
	// expects byte 2 == 0.
	announce := &codec.ACP2Message{
		Type:  codec.ACP2TypeAnnounce,
		MTID:  0,
		Func:  0, // stat = 0 for announces per spec §3.2
		PID:   codec.PIDValue,
		ObjID: msg.ObjID,
		Idx:   msg.Idx,
		Body:  appendObjIDIdx(msg.ObjID, msg.Idx, body),
	}
	s.srv.broadcastAnnounce(slot, announce)
}

// handleGetObject builds the full property list for the requested
// obj-id and writes the reply.
func (s *session) handleGetObject(slot uint8, msg *codec.ACP2Message) {
	e, ok := s.srv.tree.lookup(slot, msg.ObjID)
	if !ok {
		s.replyACP2(slot, errorACP2(msg, codec.ErrInvalidObjID))
		return
	}
	props, err := buildProperties(e)
	if err != nil {
		s.srv.logger.Error("acp2 buildProperties",
			slog.Int("slot", int(slot)),
			slog.Int("obj", int(msg.ObjID)),
			slog.String("err", err.Error()),
		)
		s.replyACP2(slot, errorACP2(msg, codec.ErrProtocol))
		return
	}
	body, err := codec.EncodeProperties(props)
	if err != nil {
		s.replyACP2(slot, errorACP2(msg, codec.ErrProtocol))
		return
	}
	// get_object reply body layout: header(4) + obj-id(4) + idx(4) + props.
	// Use Body to carry the trailing props so EncodeACP2Message's default
	// path serialises it correctly.
	reply := &codec.ACP2Message{
		Type:  codec.ACP2TypeReply,
		MTID:  msg.MTID,
		Func:  codec.ACP2FuncGetObject,
		PID:   msg.PID,
		ObjID: msg.ObjID,
		Idx:   msg.Idx,
		Body:  appendObjIDIdx(msg.ObjID, msg.Idx, body),
	}
	s.replyACP2(slot, reply)
}

// appendObjIDIdx prepends the 8-byte obj-id + idx header to a property
// byte sequence. The ACP2 codec's "default" encode path writes Body
// verbatim after the 4-byte message header, so we stuff the obj-id/idx
// into the body itself.
func appendObjIDIdx(objID, idx uint32, props []byte) []byte {
	out := make([]byte, 8+len(props))
	binaryBigEndianU32(out[0:4], objID)
	binaryBigEndianU32(out[4:8], idx)
	copy(out[8:], props)
	return out
}

func binaryBigEndianU32(dst []byte, v uint32) {
	dst[0] = byte(v >> 24)
	dst[1] = byte(v >> 16)
	dst[2] = byte(v >> 8)
	dst[3] = byte(v)
}

func beU32(src []byte) uint32 {
	return uint32(src[0])<<24 | uint32(src[1])<<16 | uint32(src[2])<<8 | uint32(src[3])
}

// replyACP2 wraps an ACP2 message in an AN2 data frame and sends it.
//
// Consumer's EncodeACP2Message has per-Func cases shaped for REQUESTS
// (get_object writes only obj-id+idx; get_property writes obj-id+idx+
// empty property header). For replies we bypass those cases by
// building the 4-byte header ourselves + concatenating msg.Body —
// which the provider builds as obj-id+idx+encoded-properties. This
// matches the spec reply layout exactly.
func (s *session) replyACP2(slot uint8, msg *codec.ACP2Message) {
	raw := make([]byte, 4+len(msg.Body))
	raw[0] = byte(msg.Type)
	raw[1] = msg.MTID
	raw[2] = byte(msg.Func)
	raw[3] = msg.PID
	copy(raw[4:], msg.Body)
	frame := &codec.AN2Frame{
		Proto:   codec.AN2ProtoACP2,
		Slot:    slot,
		MTID:    0, // AN2 data frames always carry mtid=0
		Type:    codec.AN2TypeData,
		Payload: raw,
	}
	if err := s.write(frame); err != nil {
		s.srv.logger.Debug("acp2 reply write failed",
			slog.String("err", err.Error()),
		)
	}
}

// errorACP2 builds an ACP2 error reply per spec §"Error". The error
// message is exactly the 4-byte ACP2 header — NO body.
//
//	| Offset | Field | Width | Notes                                 |
//	|--------|-------|-------|---------------------------------------|
//	| 0      | type  | u8    | 3 = error                             |
//	| 1      | mtid  | u8    | matches the request mtid              |
//	| 2      | stat  | u8    | error status (§3.1: 0..5)             |
//	| 3      | pid   | u8    | 0                                     |
//
// The func slot carries the stat byte for error messages; the codec
// picks this back up in ToACP2Error. ObjID is preserved on the
// in-memory message struct purely for caller-side diagnostics — it
// is NOT serialised onto the wire.
//
// Spec reference: acp2_protocol.docx §"Error" (line 1207-1250) — the
// error table lists only type/mtid/stat/pid; no body row.
func errorACP2(req *codec.ACP2Message, stat codec.ACP2ErrStatus) *codec.ACP2Message {
	return &codec.ACP2Message{
		Type:  codec.ACP2TypeError,
		MTID:  req.MTID,
		Func:  codec.ACP2Func(stat),
		PID:   0,
		ObjID: req.ObjID,
		Body:  nil,
	}
}

// slotInfo answers GetSlotInfo for a given slot per AN2 spec §3.3.3.
// A slot is "present" iff we hold canonical ACP2 data for it in the
// provider's tree; every other slot (0-254) is "empty".
//
// The reply's protos array enumerates *payload* protocols supported by
// the slot (§3.2.1 codes 1=ACP1, 2=ACP2, 3=ACMP). proto=0 is reserved
// for AN2-internal signalling per §1.2.3 ("Proto 0 is reserved as
// 'signalling' protocol for internal (Axonnet2) use") and is never
// listed here. Real EVS Neuron slot 0 advertises [2,3,4]; slot 1
// advertises [2,3] — never includes 0.
//
// Present ACP2 slots advertise [ACP2] only; ACMP and vendor-internal
// protos (like Neuron's proto=4) are not implemented here.
//
// The slot number of the rack-controller endpoint is fixture-defined
// (typically slot 0 on real Synapse frames, but any number is valid per
// spec). Callers that want a frame-controller view for slot 0 must
// include a slot-0 subtree in the canonical export.
func (s *server) slotInfo(slot uint8) (status uint8, protos []uint8) {
	s.tree.mu.RLock()
	defer s.tree.mu.RUnlock()
	if _, ok := s.tree.perSlot[slot]; ok {
		return slotStatusPresent, []uint8{uint8(codec.AN2ProtoACP2)}
	}
	return slotStatusEmpty, nil
}

// -----------------------------------------------------------------
// session helpers

// enable records a consumer's EnableProtocolEvents subscription.
// Announces (type=2 messages) only fan out to sessions where the
// target protocol is enabled — this is the spec-required gate.
func (s *session) enable(p codec.AN2Proto) {
	if s.enabled == nil {
		s.enabled = map[codec.AN2Proto]bool{}
	}
	s.enabled[p] = true
}

// write serialises an AN2 frame and writes it under the per-session
// write lock.
func (s *session) write(f *codec.AN2Frame) error {
	raw, err := codec.EncodeAN2Frame(f)
	if err != nil {
		return fmt.Errorf("encode frame: %w", err)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err = s.conn.Write(raw)
	return err
}
