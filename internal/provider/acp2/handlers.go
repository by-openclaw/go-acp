package acp2

import (
	"fmt"
	"log/slog"

	iacp2 "acp/internal/protocol/acp2"
)

// Version constants advertised by this provider.
//
// AN2 v1.0 matches what real Axon firmware emits today. ACP2 v1 is the
// only ACP2 major in the wild; consumers use it to gate feature flags.
const (
	an2VersionMajor uint8 = 1
	an2VersionMinor uint8 = 0
	acp2Version     uint8 = 1
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
func (s *session) dispatch(f *iacp2.AN2Frame) {
	switch f.Proto {
	case iacp2.AN2ProtoInternal:
		s.handleAN2Internal(f)
	case iacp2.AN2ProtoACP2:
		s.handleACP2(f)
	default:
		s.srv.logger.Debug("acp2 frame dropped — unsupported proto",
			slog.String("proto", f.Proto.String()),
		)
	}
}

// handleAN2Internal implements the proto=0 handshake a consumer runs
// before sending any ACP2 traffic:
//
//	1. GetVersion           (funcID=0) -> [0, major, minor]
//	2. GetDeviceInfo        (funcID=1) -> [1, slot_count]
//	3. GetSlotInfo(slot)    (funcID=2) -> [2, status, num_protos, protos…]
//	4. EnableProtocolEvents (funcID=3) -> [3, 0]  (ack)
//
// All replies mirror the request's AN2 mtid + slot per spec §3.3 so
// the consumer's waiter table correlates them cleanly.
func (s *session) handleAN2Internal(f *iacp2.AN2Frame) {
	if f.Type != iacp2.AN2TypeRequest {
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
	case iacp2.AN2FuncGetVersion:
		body = []byte{funcID, an2VersionMajor, an2VersionMinor}
	case iacp2.AN2FuncGetDeviceInfo:
		body = []byte{funcID, s.srv.tree.slotN}
	case iacp2.AN2FuncGetSlotInfo:
		if len(f.Payload) < 2 {
			s.srv.logger.Warn("an2 GetSlotInfo: missing slot byte")
			return
		}
		slot := f.Payload[1]
		status, protos := s.srv.slotInfo(slot)
		body = append([]byte{funcID, status, uint8(len(protos))}, protos...)
	case iacp2.AN2FuncEnableProtocolEvents:
		// Payload: funcID, count, proto_ids[count]
		if len(f.Payload) < 2 {
			s.srv.logger.Warn("an2 EnableProtocolEvents: missing count byte")
			return
		}
		count := int(f.Payload[1])
		for i := 0; i < count && 2+i < len(f.Payload); i++ {
			s.enable(iacp2.AN2Proto(f.Payload[2+i]))
		}
		body = []byte{funcID, 0} // ack
	default:
		s.srv.logger.Debug("an2 internal: unknown funcID",
			slog.Int("funcID", int(funcID)),
		)
		return
	}

	reply := &iacp2.AN2Frame{
		Proto:   iacp2.AN2ProtoInternal,
		Slot:    f.Slot,
		MTID:    f.MTID,
		Type:    iacp2.AN2TypeReply,
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
func (s *session) handleACP2(f *iacp2.AN2Frame) {
	if f.Type != iacp2.AN2TypeData {
		s.srv.logger.Debug("acp2 dispatch: ignoring non-data frame",
			slog.String("type", f.Type.String()),
		)
		return
	}
	msg, err := iacp2.DecodeACP2Message(f.Payload)
	if err != nil {
		s.srv.logger.Warn("acp2 decode failed", slog.String("err", err.Error()))
		return
	}
	if msg.Type != iacp2.ACP2TypeRequest {
		return
	}

	switch msg.Func {
	case iacp2.ACP2FuncGetVersion:
		s.replyACP2(f.Slot, &iacp2.ACP2Message{
			Type: iacp2.ACP2TypeReply,
			MTID: msg.MTID,
			Func: iacp2.ACP2FuncGetVersion,
			PID:  acp2Version,
		})
	case iacp2.ACP2FuncGetObject:
		s.handleGetObject(f.Slot, msg)
	case iacp2.ACP2FuncGetProperty:
		s.handleGetProperty(f.Slot, msg)
	case iacp2.ACP2FuncSetProperty:
		s.handleSetProperty(f.Slot, msg)
	default:
		s.replyACP2(f.Slot, errorACP2(msg, iacp2.ErrProtocol))
	}
}

// handleGetProperty returns one specific property of an object. The
// request carries the requested pid in msg.PID; the reply echoes the
// same pid with its current value.
func (s *session) handleGetProperty(slot uint8, msg *iacp2.ACP2Message) {
	e, ok := s.srv.tree.lookup(slot, msg.ObjID)
	if !ok {
		s.replyACP2(slot, errorACP2(msg, iacp2.ErrInvalidObjID))
		return
	}
	all, err := buildProperties(e)
	if err != nil {
		s.replyACP2(slot, errorACP2(msg, iacp2.ErrProtocol))
		return
	}
	for i := range all {
		if all[i].PID == msg.PID {
			body, err := iacp2.EncodeProperty(&all[i])
			if err != nil {
				s.replyACP2(slot, errorACP2(msg, iacp2.ErrProtocol))
				return
			}
			s.replyACP2(slot, &iacp2.ACP2Message{
				Type:  iacp2.ACP2TypeReply,
				MTID:  msg.MTID,
				Func:  iacp2.ACP2FuncGetProperty,
				PID:   msg.PID,
				ObjID: msg.ObjID,
				Idx:   msg.Idx,
				Body:  appendObjIDIdx(msg.ObjID, msg.Idx, body),
			})
			return
		}
	}
	s.replyACP2(slot, errorACP2(msg, iacp2.ErrInvalidPID))
}

// handleSetProperty mutates the tree for the requested (obj-id, pid)
// + incoming property, sends a reply with the confirmed post-state,
// and broadcasts an announce to every session that has
// EnableProtocolEvents([ACP2]) subscribed.
func (s *session) handleSetProperty(slot uint8, msg *iacp2.ACP2Message) {
	e, ok := s.srv.tree.lookup(slot, msg.ObjID)
	if !ok {
		s.replyACP2(slot, errorACP2(msg, iacp2.ErrInvalidObjID))
		return
	}
	if len(msg.Properties) == 0 {
		s.replyACP2(slot, errorACP2(msg, iacp2.ErrProtocol))
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
	body, err := iacp2.EncodeProperty(&post)
	if err != nil {
		s.replyACP2(slot, errorACP2(msg, iacp2.ErrProtocol))
		return
	}
	// Reply with the confirmed post-state.
	reply := &iacp2.ACP2Message{
		Type:  iacp2.ACP2TypeReply,
		MTID:  msg.MTID,
		Func:  iacp2.ACP2FuncSetProperty,
		PID:   msg.PID,
		ObjID: msg.ObjID,
		Idx:   msg.Idx,
		Body:  appendObjIDIdx(msg.ObjID, msg.Idx, body),
	}
	s.replyACP2(slot, reply)

	// Fan out the announce to every session with ACP2 events enabled.
	announce := &iacp2.ACP2Message{
		Type:  iacp2.ACP2TypeAnnounce,
		MTID:  0,
		Func:  iacp2.ACP2Func(iacp2.PIDValue), // announce carries the pid in the func slot per spec
		PID:   iacp2.PIDValue,
		ObjID: msg.ObjID,
		Idx:   msg.Idx,
		Body:  appendObjIDIdx(msg.ObjID, msg.Idx, body),
	}
	s.srv.broadcastAnnounce(slot, announce)
}

// handleGetObject builds the full property list for the requested
// obj-id and writes the reply.
func (s *session) handleGetObject(slot uint8, msg *iacp2.ACP2Message) {
	e, ok := s.srv.tree.lookup(slot, msg.ObjID)
	if !ok {
		s.replyACP2(slot, errorACP2(msg, iacp2.ErrInvalidObjID))
		return
	}
	props, err := buildProperties(e)
	if err != nil {
		s.srv.logger.Error("acp2 buildProperties",
			slog.Int("slot", int(slot)),
			slog.Int("obj", int(msg.ObjID)),
			slog.String("err", err.Error()),
		)
		s.replyACP2(slot, errorACP2(msg, iacp2.ErrProtocol))
		return
	}
	body, err := iacp2.EncodeProperties(props)
	if err != nil {
		s.replyACP2(slot, errorACP2(msg, iacp2.ErrProtocol))
		return
	}
	// get_object reply body layout: header(4) + obj-id(4) + idx(4) + props.
	// Use Body to carry the trailing props so EncodeACP2Message's default
	// path serialises it correctly.
	reply := &iacp2.ACP2Message{
		Type:  iacp2.ACP2TypeReply,
		MTID:  msg.MTID,
		Func:  iacp2.ACP2FuncGetObject,
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

// replyACP2 wraps an ACP2 message in an AN2 data frame and sends it
// via the session's write socket.
func (s *session) replyACP2(slot uint8, msg *iacp2.ACP2Message) {
	raw, err := iacp2.EncodeACP2Message(msg)
	if err != nil {
		s.srv.logger.Warn("acp2 encode reply", slog.String("err", err.Error()))
		return
	}
	frame := &iacp2.AN2Frame{
		Proto:   iacp2.AN2ProtoACP2,
		Slot:    slot,
		MTID:    0, // AN2 data frames always carry mtid=0
		Type:    iacp2.AN2TypeData,
		Payload: raw,
	}
	if err := s.write(frame); err != nil {
		s.srv.logger.Debug("acp2 reply write failed",
			slog.String("err", err.Error()),
		)
	}
}

// errorACP2 builds an ACP2 error reply per spec §"Error Codes". The
// func field of an error message holds the stat byte (not a function
// ID); codec.go picks this back up in ToACP2Error.
func errorACP2(req *iacp2.ACP2Message, stat iacp2.ACP2ErrStatus) *iacp2.ACP2Message {
	body := make([]byte, 4)
	binaryBigEndianU32(body, req.ObjID)
	return &iacp2.ACP2Message{
		Type:  iacp2.ACP2TypeError,
		MTID:  req.MTID,
		Func:  iacp2.ACP2Func(stat),
		PID:   0,
		ObjID: req.ObjID,
		Body:  body,
	}
}

// slotInfo answers GetSlotInfo for a given slot. Slot 0 is the rack
// controller (no ACP2 payload protocols); card slots expose ACP2 plus
// AN2 internal. Subclasses of this reply (ACMP, etc.) are out of scope.
func (s *server) slotInfo(slot uint8) (status uint8, protos []uint8) {
	s.tree.mu.RLock()
	defer s.tree.mu.RUnlock()
	if slot == 0 {
		// Controller slot exposes AN2 internal only.
		return slotStatusPresent, []uint8{uint8(iacp2.AN2ProtoInternal)}
	}
	if slot > s.tree.slotN {
		return slotStatusEmpty, nil
	}
	return slotStatusPresent, []uint8{uint8(iacp2.AN2ProtoInternal), uint8(iacp2.AN2ProtoACP2)}
}

// -----------------------------------------------------------------
// session helpers

// enable records a consumer's EnableProtocolEvents subscription.
// Announces (type=2 messages) only fan out to sessions where the
// target protocol is enabled — this is the spec-required gate.
func (s *session) enable(p iacp2.AN2Proto) {
	if s.enabled == nil {
		s.enabled = map[iacp2.AN2Proto]bool{}
	}
	s.enabled[p] = true
}

// write serialises an AN2 frame and writes it under the per-session
// write lock.
func (s *session) write(f *iacp2.AN2Frame) error {
	raw, err := iacp2.EncodeAN2Frame(f)
	if err != nil {
		return fmt.Errorf("encode frame: %w", err)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err = s.conn.Write(raw)
	return err
}
