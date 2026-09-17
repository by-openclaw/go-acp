package codec

import (
	"fmt"
	"net"
)

// ValueType names one of SNMP's object syntaxes. The constant IS the BER
// tag, so an encoder never maps and a decoder never guesses: what came
// off the wire is what goes back on it, which is the property a proxy
// needs and a naive re-encode loses.
type ValueType byte

const (
	TypeInteger      ValueType = ValueType(tagInteger)      // Integer32
	TypeOctetString  ValueType = ValueType(tagOctetString)  // OCTET STRING
	TypeNull         ValueType = ValueType(tagNull)         // NULL
	TypeOID          ValueType = ValueType(tagOID)          // OBJECT IDENTIFIER
	TypeIPAddress    ValueType = ValueType(tagIPAddress)    // IpAddress, 4 octets
	TypeCounter32    ValueType = ValueType(tagCounter32)    // Counter32
	TypeGauge32      ValueType = ValueType(tagGauge32)      // Gauge32 / Unsigned32
	TypeTimeTicks    ValueType = ValueType(tagTimeTicks)    // TimeTicks, centiseconds
	TypeOpaque       ValueType = ValueType(tagOpaque)       // Opaque
	TypeCounter64    ValueType = ValueType(tagCounter64)    // Counter64 (v2c+)
	TypeNoSuchObject ValueType = ValueType(tagNoSuchObject) // RFC 3416 §4.1
	TypeNoSuchInst   ValueType = ValueType(tagNoSuchInst)
	TypeEndOfMIBView ValueType = ValueType(tagEndOfMIBView)
)

// String names the type as the SMI does, so a log line or a CLI column
// reads the way the MIB that defined the object reads.
func (t ValueType) String() string {
	switch t {
	case TypeInteger:
		return "INTEGER"
	case TypeOctetString:
		return "OCTET STRING"
	case TypeNull:
		return "NULL"
	case TypeOID:
		return "OBJECT IDENTIFIER"
	case TypeIPAddress:
		return "IpAddress"
	case TypeCounter32:
		return "Counter32"
	case TypeGauge32:
		return "Gauge32"
	case TypeTimeTicks:
		return "TimeTicks"
	case TypeOpaque:
		return "Opaque"
	case TypeCounter64:
		return "Counter64"
	case TypeNoSuchObject:
		return "noSuchObject"
	case TypeNoSuchInst:
		return "noSuchInstance"
	case TypeEndOfMIBView:
		return "endOfMibView"
	default:
		return fmt.Sprintf("unknown(0x%02X)", byte(t))
	}
}

// Value is one SNMP object syntax value.
//
// One struct with a Type discriminant rather than an interface per
// syntax: the set is closed by the SMI and has not grown since RFC 2578,
// every consumer switches on the type anyway, and an interface would put
// an allocation on the decode path — which runs once per varbind, per
// polled OID, per interval, across a fleet.
//
// Exactly one of the payload fields is meaningful, named by Type. The
// zero Value is a NULL, which is what a GET request carries.
type Value struct {
	Type ValueType

	// Int carries Integer32 (signed).
	Int int64
	// Uint carries Counter32, Gauge32, TimeTicks and Counter64.
	Uint uint64
	// Bytes carries OCTET STRING and Opaque.
	Bytes []byte
	// OID carries OBJECT IDENTIFIER.
	OID OID
	// IP carries IpAddress, always as 4 bytes.
	IP net.IP
}

// Constructors. They exist so a call site reads as the type it means and
// cannot leave the discriminant disagreeing with the payload.

func Int(v int64) Value        { return Value{Type: TypeInteger, Int: v} }
func String(s string) Value    { return Value{Type: TypeOctetString, Bytes: []byte(s)} }
func Bytes(b []byte) Value     { return Value{Type: TypeOctetString, Bytes: b} }
func Null() Value              { return Value{Type: TypeNull} }
func ObjectID(o OID) Value     { return Value{Type: TypeOID, OID: o} }
func Counter32(v uint32) Value { return Value{Type: TypeCounter32, Uint: uint64(v)} }
func Gauge32(v uint32) Value   { return Value{Type: TypeGauge32, Uint: uint64(v)} }
func TimeTicks(v uint32) Value { return Value{Type: TypeTimeTicks, Uint: uint64(v)} }
func Counter64(v uint64) Value { return Value{Type: TypeCounter64, Uint: v} }
func Opaque(b []byte) Value    { return Value{Type: TypeOpaque, Bytes: b} }
func NoSuchObject() Value      { return Value{Type: TypeNoSuchObject} }
func NoSuchInstance() Value    { return Value{Type: TypeNoSuchInst} }
func EndOfMIBView() Value      { return Value{Type: TypeEndOfMIBView} }

// IPAddress builds an IpAddress. A v4-mapped v6 address is narrowed,
// because SNMP's IpAddress is four octets and nothing else; an address
// that is not v4 produces a NULL rather than four bytes of somebody
// else's address.
func IPAddress(ip net.IP) Value {
	v4 := ip.To4()
	if v4 == nil {
		return Null()
	}
	return Value{Type: TypeIPAddress, IP: v4}
}

// IsException reports whether this value is one of the three RFC 3416
// §4.1 markers an agent returns INSTEAD of a value. They travel in the
// varbind where a value would be, so a caller that does not check treats
// "this object does not exist" as data.
func (v Value) IsException() bool {
	switch v.Type {
	case TypeNoSuchObject, TypeNoSuchInst, TypeEndOfMIBView:
		return true
	default:
		return false
	}
}

// String renders the value for a log line or a CLI column. OCTET STRING
// is rendered as text when it is printable and as hex when it is not —
// a MAC address or a binary status field pasted raw into a terminal is
// how a log line eats somebody's scrollback.
func (v Value) String() string {
	switch v.Type {
	case TypeInteger:
		return fmt.Sprintf("%d", v.Int)
	case TypeCounter32, TypeGauge32, TypeTimeTicks, TypeCounter64:
		return fmt.Sprintf("%d", v.Uint)
	case TypeOctetString, TypeOpaque:
		if printable(v.Bytes) {
			return string(v.Bytes)
		}
		return fmt.Sprintf("%X", v.Bytes)
	case TypeOID:
		return v.OID.String()
	case TypeIPAddress:
		return v.IP.String()
	default:
		return v.Type.String()
	}
}

func printable(b []byte) bool {
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

// appendValue writes the value as its own BER element.
func appendValue(dst []byte, v Value) []byte {
	switch v.Type {
	case TypeInteger:
		return appendInt(dst, tagInteger, v.Int)
	case TypeCounter32, TypeGauge32, TypeTimeTicks:
		// The SMI caps these at 32 bits; a caller that overflowed one
		// gets the low word rather than a five-byte quantity no manager
		// would accept.
		return appendUint(dst, byte(v.Type), uint64(uint32(v.Uint)))
	case TypeCounter64:
		return appendUint(dst, tagCounter64, v.Uint)
	case TypeOctetString, TypeOpaque:
		return appendTLV(dst, byte(v.Type), v.Bytes)
	case TypeOID:
		return appendOID(dst, v.OID)
	case TypeIPAddress:
		ip := v.IP.To4()
		if ip == nil {
			ip = net.IPv4zero.To4()
		}
		return appendTLV(dst, tagIPAddress, ip)
	case TypeNoSuchObject, TypeNoSuchInst, TypeEndOfMIBView:
		// The exceptions are tag-only: they carry no contents at all.
		return appendTLV(dst, byte(v.Type), nil)
	default:
		// Anything else — including the zero Value — is a NULL, which is
		// what a request carries in the value slot.
		return appendTLV(dst, tagNull, nil)
	}
}

// parseValue reads one already-delimited element as a value.
func parseValue(e tlv) (Value, error) {
	switch e.tag {
	case tagInteger:
		n, err := parseInt(e.value, "INTEGER")
		if err != nil {
			return Value{}, err
		}
		return Value{Type: TypeInteger, Int: n}, nil

	case tagCounter32, tagGauge32, tagTimeTicks, tagCounter64:
		n, err := parseUint(e.value, ValueType(e.tag).String())
		if err != nil {
			return Value{}, err
		}
		return Value{Type: ValueType(e.tag), Uint: n}, nil

	case tagOctetString, tagOpaque:
		// Copied, not aliased: the caller's datagram buffer is reused by
		// the read loop, and a varbind that points into it changes under
		// the caller between one packet and the next.
		return Value{Type: ValueType(e.tag), Bytes: append([]byte(nil), e.value...)}, nil

	case tagOID:
		o, err := parseOID(e.value)
		if err != nil {
			return Value{}, err
		}
		return Value{Type: TypeOID, OID: o}, nil

	case tagIPAddress:
		if len(e.value) != 4 {
			return Value{}, malformed("IpAddress of %d bytes, want 4", len(e.value))
		}
		return Value{Type: TypeIPAddress, IP: net.IPv4(e.value[0], e.value[1], e.value[2], e.value[3]).To4()}, nil

	case tagNull:
		if len(e.value) != 0 {
			return Value{}, malformed("NULL with %d bytes of contents", len(e.value))
		}
		return Value{Type: TypeNull}, nil

	case tagNoSuchObject, tagNoSuchInst, tagEndOfMIBView:
		return Value{Type: ValueType(e.tag)}, nil

	default:
		return Value{}, malformed("unknown value tag 0x%02X", e.tag)
	}
}
