package acp1

import (
	"log/slog"
	"dhs/internal/acp1/codec"
)

// handleRequest dispatches a decoded request to the right handler and
// returns (reply, announce). Reply is what we send back to the caller.
// Announce is non-nil only after a successful mutation, in which case
// it is the MTID=0 broadcast the wire I/O layer should fan out to the
// LAN (spec §"Announcements", p.14 Reply Matrix row "MType=2 MTID=0").
//
// Pure in the I/O sense — this function never touches the socket; the
// caller is responsible for framing and sending both messages.
//
// Returns (nil, nil) for messages that should be silently dropped
// (announcements and replies from other providers, error messages).
func (s *server) handleRequest(msg *codec.Message) (*codec.Message, *codec.Message) {
	if msg.MType != codec.MTypeRequest {
		return nil, nil
	}

	if msg.ObjGroup == codec.GroupRoot && msg.ObjID == 0 {
		return s.handleRoot(msg), nil
	}

	key := objectKey{slot: msg.MAddr, group: msg.ObjGroup, id: msg.ObjID}
	e, ok := s.tree.lookup(key)
	if !ok {
		return errorReply(msg, groupOrInstanceMissing(s, key)), nil
	}

	method := codec.Method(msg.MCode)
	if !methodSupported(e.acpType, method) {
		return errorReply(msg, codec.OErrIllegalForType), nil
	}
	if err := checkAccess(e.access, method); err != 0 {
		return errorReply(msg, err), nil
	}

	switch method {
	case codec.MethodGetValue:
		raw, err := encodeValue(e)
		if err != nil {
			s.logger.Error("getValue encode", slog.String("oid", e.param.OID), slog.String("err", err.Error()))
			return errorReply(msg, codec.OErrIllegalForType), nil
		}
		return reply(msg, raw), nil
	case codec.MethodGetObject:
		raw, err := encodeObject(e)
		if err != nil {
			s.logger.Error("getObject encode", slog.String("oid", e.param.OID), slog.String("err", err.Error()))
			return errorReply(msg, codec.OErrIllegalForType), nil
		}
		return reply(msg, raw), nil
	case codec.MethodSetValue, codec.MethodSetIncValue,
		codec.MethodSetDecValue, codec.MethodSetDefValue:
		raw, err := s.applyMutation(e, method, msg.Value)
		if err != nil {
			s.logger.Warn("acp1 mutation",
				slog.String("oid", e.param.OID),
				slog.String("method", methodName(method)),
				slog.String("err", err.Error()),
			)
			return errorReply(msg, codec.OErrIllegalForType), nil
		}
		return reply(msg, raw), announce(msg, raw)
	}
	return errorReply(msg, codec.OErrIllegalMethod), nil
}

// announce builds the unsolicited value-change announcement that every
// successful set* method must broadcast per spec §"Announcements" p.14.
//
// Shape: MTID=0 (announcement marker), MType=2 (Reply), MCode=set*,
// MAddr=slot, ObjGroup/ObjID=changed object, Value=new stored bytes.
func announce(req *codec.Message, value []byte) *codec.Message {
	return &codec.Message{
		MTID:     0,
		MType:    codec.MTypeReply,
		MAddr:    req.MAddr,
		MCode:    req.MCode,
		ObjGroup: req.ObjGroup,
		ObjID:    req.ObjID,
		Value:    value,
	}
}

// methodName is a small helper so debug logs read "setValue" not "1".
func methodName(m codec.Method) string {
	switch m {
	case codec.MethodGetValue:
		return "getValue"
	case codec.MethodSetValue:
		return "setValue"
	case codec.MethodSetIncValue:
		return "setIncValue"
	case codec.MethodSetDecValue:
		return "setDecValue"
	case codec.MethodSetDefValue:
		return "setDefValue"
	case codec.MethodGetObject:
		return "getObject"
	}
	return "unknown"
}

// handleRoot synthesises the Root (group=0, id=0) reply for the
// requested slot. Real Axon cards answer Root for their own slot with
// the per-slot object counters; we do the same from tree.slots.
func (s *server) handleRoot(msg *codec.Message) *codec.Message {
	s.tree.mu.RLock()
	counts, ok := s.tree.slots[msg.MAddr]
	s.tree.mu.RUnlock()
	if !ok {
		return errorReply(msg, codec.OErrInstanceNoExist)
	}

	switch codec.Method(msg.MCode) {
	case codec.MethodGetValue:
		// Spec p.21: Root.getValue returns the single "boot_mode" byte.
		// We report boot_mode=0 (normal operation). Firmware-upgrade
		// mode (1) is out of scope for the provider.
		return reply(msg, []byte{0})
	case codec.MethodGetObject:
		return reply(msg, encodeRootObject(counts))
	}
	return errorReply(msg, codec.OErrIllegalMethod)
}

// encodeRootObject builds the 9-property getObject reply for the Root
// object. Spec p.21 order: access, boot_mode, num_identity,
// num_control, num_status, num_alarm, num_file.
func encodeRootObject(c *slotCounts) []byte {
	return []byte{
		byte(codec.TypeRoot),        // object_type
		9,                           // num_properties
		codec.AccessRead,            // access — Root is always read-only
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
func reply(req *codec.Message, value []byte) *codec.Message {
	return &codec.Message{
		MTID:     req.MTID,
		MType:    codec.MTypeReply,
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
func errorReply(req *codec.Message, code codec.ObjectErrCode) *codec.Message {
	return &codec.Message{
		MTID:     req.MTID,
		MType:    codec.MTypeError,
		MAddr:    req.MAddr,
		MCode:    byte(code),
		ObjGroup: req.ObjGroup,
		ObjID:    req.ObjID,
	}
}

// methodSupported implements the spec "Method Support Matrix" (CLAUDE.md
// under §ACP1 Method Support Matrix). Ensures we emit OErrIllegalForType
// rather than crashing on e.g. setIncValue of an Alarm.
func methodSupported(t codec.ObjectType, m codec.Method) bool {
	switch t {
	case codec.TypeRoot:
		return m == codec.MethodGetValue || m == codec.MethodGetObject
	case codec.TypeInteger, codec.TypeLong, codec.TypeFloat, codec.TypeByte, codec.TypeIPAddr:
		// Numeric-with-step types support all six methods.
		return true
	case codec.TypeEnum:
		// No inc/dec on enums (no step).
		switch m {
		case codec.MethodGetValue, codec.MethodSetValue,
			codec.MethodSetDefValue, codec.MethodGetObject:
			return true
		}
		return false
	case codec.TypeString:
		// No inc/dec/setDef on strings.
		switch m {
		case codec.MethodGetValue, codec.MethodSetValue, codec.MethodGetObject:
			return true
		}
		return false
	case codec.TypeAlarm, codec.TypeFile, codec.TypeFrame:
		return m == codec.MethodGetValue || m == codec.MethodGetObject
	}
	return false
}

// checkAccess maps the requested method to the access bit required by
// spec p.20 and returns 0 if the entry grants it, or the appropriate
// OErrNo*Access code otherwise.
func checkAccess(access uint8, m codec.Method) codec.ObjectErrCode {
	switch m {
	case codec.MethodGetValue, codec.MethodGetObject:
		if access&codec.AccessRead == 0 {
			return codec.OErrNoReadAccess
		}
	case codec.MethodSetValue, codec.MethodSetIncValue, codec.MethodSetDecValue:
		if access&codec.AccessWrite == 0 {
			return codec.OErrNoWriteAccess
		}
	case codec.MethodSetDefValue:
		if access&codec.AccessSetDef == 0 {
			return codec.OErrNoSetDefAccess
		}
	}
	// Unknown methods are not rejected here: method validity is owned by
	// methodSupported() + the handleRequest dispatch switch, whose final
	// arm emits OErrIllegalMethod. checkAccess only gates access on the
	// known methods above; returning 0 for anything else lets the
	// dispatcher be the single illegal-method authority (avoids a dead
	// redundant guard).
	return 0
}

// groupOrInstanceMissing distinguishes between a totally absent group
// on this slot (spec code 16) and an unknown id within an existing
// group (spec code 17). Walk the flat index once looking for any entry
// matching slot+group — cheap at the 2-3k-object scale a real frame
// carries.
func groupOrInstanceMissing(s *server, k objectKey) codec.ObjectErrCode {
	s.tree.mu.RLock()
	defer s.tree.mu.RUnlock()
	for key := range s.tree.entries {
		if key.slot == k.slot && key.group == k.group {
			return codec.OErrInstanceNoExist
		}
	}
	return codec.OErrGroupNoExist
}

// -----------------------------------------------------------------
// UDP plumbing — handleDatagram is called by server.readLoop.

// handleDatagram decodes the incoming bytes, dispatches via
// handleRequest, writes the reply back to src, and — if a mutation
// happened — broadcasts the announcement via broadcastFn. Decode
// failures are logged and dropped (no "bad-framing" reply in spec).
func (s *server) handleDatagram2(data []byte, srcStr string, send func([]byte) error) {
	s.logger.Info("acp1 datagram recv",
		slog.String("src", srcStr),
		slog.Int("bytes", len(data)),
	)
	msg, err := codec.Decode(data)
	if err != nil {
		s.logger.Warn("acp1 provider: decode failed",
			slog.String("src", srcStr),
			slog.Int("bytes", len(data)),
			slog.String("err", err.Error()),
		)
		return
	}
	s.logger.Info("acp1 request",
		slog.String("src", srcStr),
		slog.Uint64("mtid", uint64(msg.MTID)),
		slog.Int("mtype", int(msg.MType)),
		slog.Int("mcode", int(msg.MCode)),
		slog.Int("objgroup", int(msg.ObjGroup)),
		slog.Int("objid", int(msg.ObjID)),
		slog.Int("maddr", int(msg.MAddr)),
		slog.Int("value_len", len(msg.Value)),
	)
	rep, ann := s.handleRequest(msg)
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
	if ann != nil {
		s.broadcastAnnounce(ann)
	}
}

