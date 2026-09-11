package codec

import "fmt"

// Version is the value of the message's version field. The constant IS
// the wire value, which is why v2c is 1 and not 2 — the field numbers
// protocol versions from zero and SNMPv2c was the second.
type Version int

const (
	Version1  Version = 0
	Version2c Version = 1
	Version3  Version = 3
)

func (v Version) String() string {
	switch v {
	case Version1:
		return "v1"
	case Version2c:
		return "v2c"
	case Version3:
		return "v3"
	default:
		return fmt.Sprintf("version(%d)", int(v))
	}
}

// Message is one SNMP datagram.
//
// Exactly one of PDU and TrapV1 is set, named by the PDU type: a v1 trap
// is a different structure from every other PDU (see [TrapV1]), so it
// gets its own field rather than a [PDU] with five of its seven fields
// meaningless.
type Message struct {
	Version   Version
	Community string

	// PDU is set for every PDU type except PDUTypeTrapV1. On a v3
	// message it is the PDU from inside the scoped PDU, so a caller
	// that only cares what was asked reads one field whatever version
	// carried it — and is nil when the scoped PDU arrived encrypted.
	PDU *PDU
	// TrapV1 is set only for PDUTypeTrapV1, and only on a v1 message.
	TrapV1 *TrapV1
	// V3 is the v3-only envelope: the header, the security model's
	// opaque parameters, and the scope. Nil on v1 and v2c.
	V3 *V3
}

// Type reports which PDU this message carries, without the caller having
// to know which of the two fields is set.
func (m Message) Type() PDUType {
	switch {
	case m.TrapV1 != nil:
		return PDUTypeTrapV1
	case m.PDU != nil:
		return m.PDU.Type
	default:
		return 0
	}
}

// MaxMessageSize is the largest datagram this package will build or
// read. RFC 3416 §3 requires every implementation to accept 484 octets
// and permits more; 64 KiB is the UDP payload ceiling, so a message
// claiming more than this is claiming something no datagram can carry.
const MaxMessageSize = 65507

// Encode renders the message as the bytes of one UDP datagram.
//
// A message whose PDU field does not match its type is a programming
// error rather than a wire condition, and is refused here rather than
// put on the wire as something a peer would have to make sense of.
func Encode(m Message) ([]byte, error) {
	switch m.Version {
	case Version1, Version2c:
	case Version3:
		// The security model needs to know where the parameters landed
		// (see EncodeV3); a caller that does not can ignore it.
		raw, _, err := EncodeV3(m)
		return raw, err
	default:
		return nil, fmt.Errorf("snmp: unknown version %d", int(m.Version))
	}

	var body []byte
	switch {
	case m.TrapV1 != nil:
		if m.PDU != nil {
			return nil, fmt.Errorf("snmp: message carries both a PDU and a v1 trap")
		}
		if m.Version != Version1 {
			// A Trap-PDU in a v2c message is the single most common way
			// a notification silently reaches nobody: v2c managers do
			// not decode tag 0xA4, and drop it without a log line.
			return nil, fmt.Errorf("snmp: a v1 Trap-PDU cannot travel in a %s message "+
				"(use PDUTypeTrapV2)", m.Version)
		}
		body = appendInt(body, tagInteger, int64(m.Version))
		body = appendTLV(body, tagOctetString, []byte(m.Community))
		body = appendTrapV1(body, *m.TrapV1)

	case m.PDU != nil:
		if err := checkPDUForVersion(m.PDU.Type, m.Version); err != nil {
			return nil, err
		}
		body = appendInt(body, tagInteger, int64(m.Version))
		body = appendTLV(body, tagOctetString, []byte(m.Community))
		body = appendPDU(body, *m.PDU)

	default:
		return nil, fmt.Errorf("snmp: message carries no PDU")
	}

	out := appendTLV(nil, tagSequence, body)
	if len(out) > MaxMessageSize {
		return nil, fmt.Errorf("snmp: message of %d bytes exceeds the %d-byte datagram limit",
			len(out), MaxMessageSize)
	}
	return out, nil
}

// checkPDUForVersion refuses the combinations a peer cannot decode.
// GetBulk, Inform, SNMPv2-Trap and Report were all introduced with v2 and
// are tags a v1 agent has never heard of.
func checkPDUForVersion(t PDUType, v Version) error {
	switch t {
	case PDUTypeGet, PDUTypeGetNext, PDUTypeResponse, PDUTypeSet:
		return nil
	case PDUTypeGetBulk, PDUTypeInform, PDUTypeTrapV2, PDUTypeReport:
		if v == Version1 {
			return fmt.Errorf("snmp: %s is not a v1 PDU", t)
		}
		return nil
	case PDUTypeTrapV1:
		return fmt.Errorf("snmp: a v1 trap belongs in Message.TrapV1, not Message.PDU")
	default:
		return fmt.Errorf("snmp: unknown PDU type 0x%02X", byte(t))
	}
}

// Decode reads one datagram.
//
// Trailing bytes after the outer SEQUENCE are refused: a datagram with
// something appended is either truncated framing or a second message
// somebody hoped would be ignored, and neither is what it claims to be.
func Decode(b []byte) (Message, error) {
	outer, err := expect(b, tagSequence, "message")
	if err != nil {
		return Message{}, err
	}
	if outer.size != len(b) {
		return Message{}, malformed("%d trailing byte(s) after the message", len(b)-outer.size)
	}

	rest := outer.value
	version, rest, err := takeInt(rest, "version")
	if err != nil {
		return Message{}, err
	}
	m := Message{Version: Version(version)}

	switch m.Version {
	case Version1, Version2c:
	case Version3:
		// The whole datagram travels alongside what is left of it, so
		// the v3 decoder can report absolute offsets into it.
		return decodeV3(b, rest)
	default:
		return Message{}, malformed("unknown version %d", version)
	}

	community, err := expect(rest, tagOctetString, "community")
	if err != nil {
		return Message{}, err
	}
	m.Community = string(community.value)
	rest = rest[community.size:]

	pdu, err := readTLV(rest, "PDU")
	if err != nil {
		return Message{}, err
	}
	if pdu.size != len(rest) {
		return Message{}, malformed("%d trailing byte(s) after the PDU", len(rest)-pdu.size)
	}

	if PDUType(pdu.tag) == PDUTypeTrapV1 {
		if m.Version != Version1 {
			return Message{}, malformed("a v1 Trap-PDU in a %s message", m.Version)
		}
		t, err := parseTrapV1(pdu)
		if err != nil {
			return Message{}, err
		}
		m.TrapV1 = &t
		return m, nil
	}

	if err := checkPDUForVersion(PDUType(pdu.tag), m.Version); err != nil {
		return Message{}, malformed("%s", err)
	}
	p, err := parsePDU(pdu)
	if err != nil {
		return Message{}, err
	}
	m.PDU = &p
	return m, nil
}
