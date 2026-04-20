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
		// Full dispatch arrives in Step 2d/2e. For this commit we log
		// the incoming ACP2 request so the handshake path is testable
		// without full ACP2 wiring.
		s.srv.logger.Debug("acp2 frame received (dispatch in Step 2d)",
			slog.String("type", f.Type.String()),
			slog.Int("mtid", int(f.MTID)),
			slog.Int("slot", int(f.Slot)),
			slog.Int("dlen", len(f.Payload)),
		)
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
