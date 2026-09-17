package codec

import (
	"fmt"
	"net"
)

// PDUType is the context-class tag that names what a PDU asks for. The
// constant IS the tag byte, for the same reason [ValueType] is.
type PDUType byte

const (
	PDUTypeGet      PDUType = 0xA0 // GetRequest-PDU
	PDUTypeGetNext  PDUType = 0xA1 // GetNextRequest-PDU
	PDUTypeResponse PDUType = 0xA2 // Response-PDU (GetResponse-PDU in v1)
	PDUTypeSet      PDUType = 0xA3 // SetRequest-PDU
	PDUTypeTrapV1   PDUType = 0xA4 // Trap-PDU — v1 ONLY, and a different shape
	PDUTypeGetBulk  PDUType = 0xA5 // GetBulkRequest-PDU — v2c and later
	PDUTypeInform   PDUType = 0xA6 // InformRequest-PDU
	PDUTypeTrapV2   PDUType = 0xA7 // SNMPv2-Trap-PDU
	PDUTypeReport   PDUType = 0xA8 // Report-PDU — v3
)

func (t PDUType) String() string {
	switch t {
	case PDUTypeGet:
		return "GetRequest"
	case PDUTypeGetNext:
		return "GetNextRequest"
	case PDUTypeResponse:
		return "Response"
	case PDUTypeSet:
		return "SetRequest"
	case PDUTypeTrapV1:
		return "Trap"
	case PDUTypeGetBulk:
		return "GetBulkRequest"
	case PDUTypeInform:
		return "InformRequest"
	case PDUTypeTrapV2:
		return "SNMPv2-Trap"
	case PDUTypeReport:
		return "Report"
	default:
		return fmt.Sprintf("unknown(0x%02X)", byte(t))
	}
}

// ErrorStatus is the error-status field of a response. RFC 1157 defines
// 0..5; RFC 3416 adds 6..18 for v2c, and an agent may send any of them
// to a v2c manager.
type ErrorStatus int

const (
	NoError ErrorStatus = iota
	TooBig
	NoSuchName
	BadValue
	ReadOnly
	GenErr
	NoAccess
	WrongType
	WrongLength
	WrongEncoding
	WrongValue
	NoCreation
	InconsistentValue
	ResourceUnavailable
	CommitFailed
	UndoFailed
	AuthorizationError
	NotWritable
	InconsistentName
)

var errorStatusNames = [...]string{
	"noError", "tooBig", "noSuchName", "badValue", "readOnly", "genErr",
	"noAccess", "wrongType", "wrongLength", "wrongEncoding", "wrongValue",
	"noCreation", "inconsistentValue", "resourceUnavailable", "commitFailed",
	"undoFailed", "authorizationError", "notWritable", "inconsistentName",
}

func (e ErrorStatus) String() string {
	if e >= 0 && int(e) < len(errorStatusNames) {
		return errorStatusNames[e]
	}
	return fmt.Sprintf("errorStatus(%d)", int(e))
}

// VarBind is one (name, value) pair. In a request the value is a NULL
// placeholder; in a response it is the answer, or one of the RFC 3416
// §4.1 exceptions.
type VarBind struct {
	Name  OID
	Value Value
}

// PDU is every SNMP PDU except the v1 trap.
//
// GetBulk reuses the error-status and error-index SLOTS for
// non-repeaters and max-repetitions — the same two integers in the same
// two positions, meaning something else entirely. They are separate
// fields here rather than one pair with a comment, because a GetBulk
// whose max-repetitions was read as an error index is a bug that looks
// like a working poll returning one row.
type PDU struct {
	Type      PDUType
	RequestID int32

	// Request/response bookkeeping. Meaningless on a GetBulk.
	ErrorStatus ErrorStatus
	ErrorIndex  int

	// GetBulk only.
	NonRepeaters   int
	MaxRepetitions int

	VarBinds []VarBind
}

// TrapV1 is the RFC 1157 Trap-PDU, which is not a [PDU] at all: it has
// no request-id, no error fields, and five fields of its own. Encoding a
// v1 trap as a v2 notification with a version byte flipped produces a
// datagram every v1 manager silently drops.
type TrapV1 struct {
	// Enterprise is the sysObjectID of the object issuing the trap.
	Enterprise OID
	// AgentAddr is the address of the object issuing the trap, as an
	// IpAddress — four octets, whatever the transport underneath.
	AgentAddr net.IP
	// Generic is one of the seven RFC 1157 generic traps.
	Generic GenericTrap
	// Specific is the enterprise-specific trap number, meaningful only
	// when Generic is EnterpriseSpecific.
	Specific int
	// Timestamp is sysUpTime at the moment of the event, in
	// centiseconds.
	Timestamp uint32
	VarBinds  []VarBind
}

// GenericTrap is the RFC 1157 generic-trap field.
type GenericTrap int

const (
	ColdStart GenericTrap = iota
	WarmStart
	LinkDown
	LinkUp
	AuthenticationFailure
	EGPNeighborLoss
	EnterpriseSpecific
)

var genericTrapNames = [...]string{
	"coldStart", "warmStart", "linkDown", "linkUp",
	"authenticationFailure", "egpNeighborLoss", "enterpriseSpecific",
}

func (g GenericTrap) String() string {
	if g >= 0 && int(g) < len(genericTrapNames) {
		return genericTrapNames[g]
	}
	return fmt.Sprintf("genericTrap(%d)", int(g))
}

// appendPDU writes an ordinary PDU.
func appendPDU(dst []byte, p PDU) []byte {
	var body []byte
	body = appendInt(body, tagInteger, int64(p.RequestID))
	if p.Type == PDUTypeGetBulk {
		body = appendInt(body, tagInteger, int64(p.NonRepeaters))
		body = appendInt(body, tagInteger, int64(p.MaxRepetitions))
	} else {
		body = appendInt(body, tagInteger, int64(p.ErrorStatus))
		body = appendInt(body, tagInteger, int64(p.ErrorIndex))
	}
	body = appendVarBinds(body, p.VarBinds)
	return appendTLV(dst, byte(p.Type), body)
}

// appendTrapV1 writes the Trap-PDU.
func appendTrapV1(dst []byte, t TrapV1) []byte {
	var body []byte
	body = appendOID(body, t.Enterprise)
	addr := t.AgentAddr.To4()
	if addr == nil {
		addr = net.IPv4zero.To4()
	}
	body = appendTLV(body, tagIPAddress, addr)
	body = appendInt(body, tagInteger, int64(t.Generic))
	body = appendInt(body, tagInteger, int64(t.Specific))
	body = appendUint(body, tagTimeTicks, uint64(t.Timestamp))
	body = appendVarBinds(body, t.VarBinds)
	return appendTLV(dst, byte(PDUTypeTrapV1), body)
}

func appendVarBinds(dst []byte, vbs []VarBind) []byte {
	var list []byte
	for _, vb := range vbs {
		var one []byte
		one = appendOID(one, vb.Name)
		one = appendValue(one, vb.Value)
		list = appendTLV(list, tagSequence, one)
	}
	return appendTLV(dst, tagSequence, list)
}

// parsePDU reads an ordinary PDU from an already-delimited element.
func parsePDU(e tlv) (PDU, error) {
	p := PDU{Type: PDUType(e.tag)}
	rest := e.value

	id, rest, err := takeInt(rest, "request-id")
	if err != nil {
		return PDU{}, err
	}
	// request-id is an Integer32 on the wire and agents do use the full
	// signed range; narrowing here rather than at every comparison keeps
	// the correlation key one type.
	p.RequestID = int32(id)

	first, rest, err := takeInt(rest, "error-status")
	if err != nil {
		return PDU{}, err
	}
	second, rest, err := takeInt(rest, "error-index")
	if err != nil {
		return PDU{}, err
	}
	if p.Type == PDUTypeGetBulk {
		p.NonRepeaters, p.MaxRepetitions = int(first), int(second)
	} else {
		p.ErrorStatus, p.ErrorIndex = ErrorStatus(first), int(second)
	}

	p.VarBinds, err = parseVarBinds(rest)
	if err != nil {
		return PDU{}, err
	}
	return p, nil
}

// parseTrapV1 reads a Trap-PDU.
func parseTrapV1(e tlv) (TrapV1, error) {
	var t TrapV1
	rest := e.value

	ent, err := expect(rest, tagOID, "trap enterprise")
	if err != nil {
		return TrapV1{}, err
	}
	if t.Enterprise, err = parseOID(ent.value); err != nil {
		return TrapV1{}, err
	}
	rest = rest[ent.size:]

	addr, err := expect(rest, tagIPAddress, "trap agent-addr")
	if err != nil {
		return TrapV1{}, err
	}
	if len(addr.value) != 4 {
		return TrapV1{}, malformed("trap agent-addr of %d bytes, want 4", len(addr.value))
	}
	t.AgentAddr = net.IPv4(addr.value[0], addr.value[1], addr.value[2], addr.value[3]).To4()
	rest = rest[addr.size:]

	generic, rest, err := takeInt(rest, "generic-trap")
	if err != nil {
		return TrapV1{}, err
	}
	specific, rest, err := takeInt(rest, "specific-trap")
	if err != nil {
		return TrapV1{}, err
	}
	t.Generic, t.Specific = GenericTrap(generic), int(specific)

	ts, err := expect(rest, tagTimeTicks, "trap time-stamp")
	if err != nil {
		return TrapV1{}, err
	}
	ticks, err := parseUint(ts.value, "trap time-stamp")
	if err != nil {
		return TrapV1{}, err
	}
	t.Timestamp = uint32(ticks)
	rest = rest[ts.size:]

	if t.VarBinds, err = parseVarBinds(rest); err != nil {
		return TrapV1{}, err
	}
	return t, nil
}

// takeInt reads one INTEGER off the front and returns what is left, the
// shape every fixed-order PDU field decode wants.
func takeInt(b []byte, what string) (int64, []byte, error) {
	e, err := expect(b, tagInteger, what)
	if err != nil {
		return 0, nil, err
	}
	v, err := parseInt(e.value, what)
	if err != nil {
		return 0, nil, err
	}
	return v, b[e.size:], nil
}

func parseVarBinds(b []byte) ([]VarBind, error) {
	list, err := expect(b, tagSequence, "variable-bindings")
	if err != nil {
		return nil, err
	}
	// Trailing bytes after the varbind list are a truncated or padded
	// datagram; either way this is not the message it claims to be.
	if list.size != len(b) {
		return nil, malformed("%d trailing byte(s) after the variable-bindings",
			len(b)-list.size)
	}

	var out []VarBind
	rest := list.value
	for len(rest) > 0 {
		one, err := expect(rest, tagSequence, "variable-binding")
		if err != nil {
			return nil, err
		}
		name, err := expect(one.value, tagOID, "variable-binding name")
		if err != nil {
			return nil, err
		}
		oid, err := parseOID(name.value)
		if err != nil {
			return nil, err
		}
		valElem, err := readTLV(one.value[name.size:], "variable-binding value")
		if err != nil {
			return nil, err
		}
		if name.size+valElem.size != len(one.value) {
			return nil, malformed("variable-binding has %d byte(s) after its value",
				len(one.value)-name.size-valElem.size)
		}
		val, err := parseValue(valElem)
		if err != nil {
			return nil, err
		}
		out = append(out, VarBind{Name: oid, Value: val})
		rest = rest[one.size:]
	}
	return out, nil
}
