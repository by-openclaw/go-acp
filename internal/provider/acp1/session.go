package acp1

import (
	"log/slog"

	iacp1 "acp/internal/protocol/acp1"
)

// handleRequest dispatches a decoded request message to the right
// handler and returns the reply message (ready for Encode). The
// function is pure in the sense that it never writes to the network;
// the caller is responsible for socket I/O. Read-only methods
// (getValue, getObject) leave the tree untouched; mutating methods
// lock tree.mu for the duration of their handler.
//
// Returns nil if the message should be silently dropped (non-request
// messages, including announcements from other providers).
func (s *server) handleRequest(msg *iacp1.Message) *iacp1.Message {
	if msg.MType != iacp1.MTypeRequest {
		return nil
	}

	// Spec p.21: Root object is [group=0, id=0]. We synthesise it from
	// per-slot counters rather than storing a canonical Parameter — the
	// canonical tree's slot Nodes carry the shape, not a "root" leaf.
	if msg.ObjGroup == iacp1.GroupRoot && msg.ObjID == 0 {
		return s.handleRoot(msg)
	}

	key := objectKey{slot: msg.MAddr, group: msg.ObjGroup, id: msg.ObjID}
	e, ok := s.tree.lookup(key)
	if !ok {
		// Spec p.29 error codes: distinguish missing group vs missing
		// instance. If the slot/group exists but the id doesn't, it's
		// OErrInstanceNoExist (17); if the group has no objects at all
		// on this slot, OErrGroupNoExist (16).
		return errorReply(msg, groupOrInstanceMissing(s, key))
	}

	method := iacp1.Method(msg.MCode)
	if !methodSupported(e.acpType, method) {
		return errorReply(msg, iacp1.OErrIllegalForType)
	}
	if err := checkAccess(e.access, method); err != 0 {
		return errorReply(msg, err)
	}

	switch method {
	case iacp1.MethodGetValue:
		raw, err := encodeValue(e)
		if err != nil {
			s.logger.Error("getValue encode", slog.String("oid", e.param.OID), slog.String("err", err.Error()))
			return errorReply(msg, iacp1.OErrIllegalForType)
		}
		return reply(msg, raw)
	case iacp1.MethodGetObject:
		raw, err := encodeObject(e)
		if err != nil {
			s.logger.Error("getObject encode", slog.String("oid", e.param.OID), slog.String("err", err.Error()))
			return errorReply(msg, iacp1.OErrIllegalForType)
		}
		return reply(msg, raw)
	case iacp1.MethodSetValue, iacp1.MethodSetIncValue,
		iacp1.MethodSetDecValue, iacp1.MethodSetDefValue:
		raw, err := s.applyMutation(e, method, msg.Value)
		if err != nil {
			s.logger.Warn("acp1 mutation",
				slog.String("oid", e.param.OID),
				slog.String("method", methodName(method)),
				slog.String("err", err.Error()),
			)
			return errorReply(msg, iacp1.OErrIllegalForType)
		}
		return reply(msg, raw)
	}
	return errorReply(msg, iacp1.OErrIllegalMethod)
}

// methodName is a small helper so debug logs read "setValue" not "1".
func methodName(m iacp1.Method) string {
	switch m {
	case iacp1.MethodGetValue:
		return "getValue"
	case iacp1.MethodSetValue:
		return "setValue"
	case iacp1.MethodSetIncValue:
		return "setIncValue"
	case iacp1.MethodSetDecValue:
		return "setDecValue"
	case iacp1.MethodSetDefValue:
		return "setDefValue"
	case iacp1.MethodGetObject:
		return "getObject"
	}
	return "unknown"
}

// handleRoot synthesises the Root (group=0, id=0) reply for the
// requested slot. Real Axon cards answer Root for their own slot with
// the per-slot object counters; we do the same from tree.slots.
func (s *server) handleRoot(msg *iacp1.Message) *iacp1.Message {
	s.tree.mu.RLock()
	counts, ok := s.tree.slots[msg.MAddr]
	s.tree.mu.RUnlock()
	if !ok {
		return errorReply(msg, iacp1.OErrInstanceNoExist)
	}

	switch iacp1.Method(msg.MCode) {
	case iacp1.MethodGetValue:
		// Spec p.21: Root.getValue returns the single "boot_mode" byte.
		// We report boot_mode=0 (normal operation). Firmware-upgrade
		// mode (1) is out of scope for the provider.
		return reply(msg, []byte{0})
	case iacp1.MethodGetObject:
		return reply(msg, encodeRootObject(counts))
	}
	return errorReply(msg, iacp1.OErrIllegalMethod)
}

// encodeRootObject builds the 9-property getObject reply for the Root
// object. Spec p.21 order: access, boot_mode, num_identity,
// num_control, num_status, num_alarm, num_file.
func encodeRootObject(c *slotCounts) []byte {
	return []byte{
		byte(iacp1.TypeRoot),        // object_type
		9,                           // num_properties
		iacp1.AccessRead,            // access — Root is always read-only
		0,                           // boot_mode
		c.numIdentity,               // num_identity
		c.numControl,                // num_control
		c.numStatus,                 // num_status
		c.numAlarm,                  // num_alarm
		c.numFile,                   // num_file
	}
}

// reply builds a successful reply message mirroring the request's
// MTID, slot (MAddr), method (MCode), and object addressing. Per spec
// the reply MUST carry the same MTID so the client can correlate.
func reply(req *iacp1.Message, value []byte) *iacp1.Message {
	return &iacp1.Message{
		MTID:     req.MTID,
		MType:    iacp1.MTypeReply,
		MAddr:    req.MAddr,
		MCode:    req.MCode,
		ObjGroup: req.ObjGroup,
		ObjID:    req.ObjID,
		Value:    value,
	}
}

// errorReply builds an MType=Error message with the given MCODE. Spec
// p.29 says the device MAY echo ObjGroup/ObjID on errors; we do so
// since the C# reference driver relies on that echo for context.
// Encode() writes only the MCode byte for Error messages — ObjGroup/
// ObjID are ignored on the wire — but keeping them set helps any
// middleware that inspects the struct.
func errorReply(req *iacp1.Message, code iacp1.ObjectErrCode) *iacp1.Message {
	return &iacp1.Message{
		MTID:     req.MTID,
		MType:    iacp1.MTypeError,
		MAddr:    req.MAddr,
		MCode:    byte(code),
		ObjGroup: req.ObjGroup,
		ObjID:    req.ObjID,
	}
}

// methodSupported implements the spec "Method Support Matrix" (CLAUDE.md
// under §ACP1 Method Support Matrix). Ensures we emit OErrIllegalForType
// rather than crashing on e.g. setIncValue of an Alarm.
func methodSupported(t iacp1.ObjectType, m iacp1.Method) bool {
	switch t {
	case iacp1.TypeRoot:
		return m == iacp1.MethodGetValue || m == iacp1.MethodGetObject
	case iacp1.TypeInteger, iacp1.TypeLong, iacp1.TypeFloat, iacp1.TypeByte, iacp1.TypeIPAddr:
		// Numeric-with-step types support all six methods.
		return true
	case iacp1.TypeEnum:
		// No inc/dec on enums (no step).
		switch m {
		case iacp1.MethodGetValue, iacp1.MethodSetValue,
			iacp1.MethodSetDefValue, iacp1.MethodGetObject:
			return true
		}
		return false
	case iacp1.TypeString:
		// No inc/dec/setDef on strings.
		switch m {
		case iacp1.MethodGetValue, iacp1.MethodSetValue, iacp1.MethodGetObject:
			return true
		}
		return false
	case iacp1.TypeAlarm, iacp1.TypeFile, iacp1.TypeFrame:
		return m == iacp1.MethodGetValue || m == iacp1.MethodGetObject
	}
	return false
}

// checkAccess maps the requested method to the access bit required by
// spec p.20 and returns 0 if the entry grants it, or the appropriate
// OErrNo*Access code otherwise.
func checkAccess(access uint8, m iacp1.Method) iacp1.ObjectErrCode {
	switch m {
	case iacp1.MethodGetValue, iacp1.MethodGetObject:
		if access&iacp1.AccessRead == 0 {
			return iacp1.OErrNoReadAccess
		}
	case iacp1.MethodSetValue, iacp1.MethodSetIncValue, iacp1.MethodSetDecValue:
		if access&iacp1.AccessWrite == 0 {
			return iacp1.OErrNoWriteAccess
		}
	case iacp1.MethodSetDefValue:
		if access&iacp1.AccessSetDef == 0 {
			return iacp1.OErrNoSetDefAccess
		}
	default:
		return iacp1.OErrIllegalMethod
	}
	return 0
}

// groupOrInstanceMissing distinguishes between a totally absent group
// on this slot (spec code 16) and an unknown id within an existing
// group (spec code 17). Walk the flat index once looking for any entry
// matching slot+group — cheap at the 2-3k-object scale a real frame
// carries.
func groupOrInstanceMissing(s *server, k objectKey) iacp1.ObjectErrCode {
	s.tree.mu.RLock()
	defer s.tree.mu.RUnlock()
	for key := range s.tree.entries {
		if key.slot == k.slot && key.group == k.group {
			return iacp1.OErrInstanceNoExist
		}
	}
	return iacp1.OErrGroupNoExist
}

// -----------------------------------------------------------------
// UDP plumbing — handleDatagram is called by server.readLoop.

// handleDatagram decodes the incoming bytes, dispatches via
// handleRequest, and writes the reply back to src. Decode failures are
// logged and dropped — the spec has no "bad-framing" reply.
func (s *server) handleDatagram2(data []byte, srcStr string, send func([]byte) error) {
	msg, err := iacp1.Decode(data)
	if err != nil {
		s.logger.Debug("acp1 provider: decode failed",
			slog.String("src", srcStr),
			slog.String("err", err.Error()),
		)
		return
	}
	rep := s.handleRequest(msg)
	if rep == nil {
		return
	}
	out, err := rep.Encode()
	if err != nil {
		s.logger.Error("acp1 provider: reply encode",
			slog.String("err", err.Error()),
			slog.String("src", srcStr),
		)
		return
	}
	if err := send(out); err != nil {
		s.logger.Warn("acp1 provider: reply send",
			slog.String("err", err.Error()),
			slog.String("src", srcStr),
		)
	}
}

