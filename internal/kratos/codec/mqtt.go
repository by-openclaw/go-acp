package codec

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// MQTT 3.1.1 (OASIS, 29 October 2014) — the packets a subscriber sends
// and receives. The NGC2 document names MQTT (ISO/IEC 20922, which is
// 3.1.1) and leaves the protocol to its specification (§7). QoS 0 only:
// a monitoring client loses nothing it cannot re-read from the CVT.

// Packet types (§2.2.1).
const (
	PacketCONNECT     = 1
	PacketCONNACK     = 2
	PacketPUBLISH     = 3
	PacketPUBACK      = 4
	PacketSUBSCRIBE   = 8
	PacketSUBACK      = 9
	PacketUNSUBSCRIBE = 10
	PacketUNSUBACK    = 11
	PacketPINGREQ     = 12
	PacketPINGRESP    = 13
	PacketDISCONNECT  = 14
)

// MaxRemainingLength is the largest Remaining Length (§2.2.3).
const MaxRemainingLength = 268435455

// Errors of the MQTT layer.
var (
	ErrRemainingLength = errors.New("mqtt: malformed remaining length")
	ErrPacketTooLarge  = errors.New("mqtt: packet larger than the read limit")
	ErrShortPacket     = errors.New("mqtt: packet shorter than its fields")
)

// AppendRemainingLength appends the variable-length encoding of n
// (§2.2.3).
func AppendRemainingLength(b []byte, n int) []byte {
	for {
		d := byte(n % 128)
		n /= 128
		if n > 0 {
			d |= 0x80
		}
		b = append(b, d)
		if n == 0 {
			return b
		}
	}
}

func appendString(b []byte, s string) []byte {
	b = binary.BigEndian.AppendUint16(b, uint16(len(s)))
	return append(b, s...)
}

func packet(header byte, body []byte) []byte {
	out := []byte{header}
	out = AppendRemainingLength(out, len(body))
	return append(out, body...)
}

// Connect builds CONNECT (§3.1): protocol "MQTT" level 4, clean
// session, the username and password flags only when given.
func Connect(clientID, username, password string, keepAliveSec uint16) []byte {
	body := appendString(nil, "MQTT")
	body = append(body, 4)
	flags := byte(0x02) // clean session
	if username != "" {
		flags |= 0x80
		if password != "" {
			flags |= 0x40
		}
	}
	body = append(body, flags)
	body = binary.BigEndian.AppendUint16(body, keepAliveSec)
	body = appendString(body, clientID)
	if username != "" {
		body = appendString(body, username)
		if password != "" {
			body = appendString(body, password)
		}
	}
	return packet(PacketCONNECT<<4, body)
}

// Subscribe builds SUBSCRIBE (§3.8): fixed-header flags 0010, one
// filter per topic, each requested at QoS 0.
func Subscribe(packetID uint16, filters ...string) []byte {
	body := binary.BigEndian.AppendUint16(nil, packetID)
	for _, f := range filters {
		body = appendString(body, f)
		body = append(body, 0)
	}
	return packet(PacketSUBSCRIBE<<4|0x02, body)
}

// Unsubscribe builds UNSUBSCRIBE (§3.10), fixed-header flags 0010.
func Unsubscribe(packetID uint16, filters ...string) []byte {
	body := binary.BigEndian.AppendUint16(nil, packetID)
	for _, f := range filters {
		body = appendString(body, f)
	}
	return packet(PacketUNSUBSCRIBE<<4|0x02, body)
}

// Pingreq and Disconnect (§3.12, §3.14).
func Pingreq() []byte    { return []byte{PacketPINGREQ << 4, 0} }
func Disconnect() []byte { return []byte{PacketDISCONNECT << 4, 0} }

// Puback builds PUBACK (§3.4) for a QoS 1 PUBLISH the broker sent
// although QoS 0 was asked (a broker may not downgrade below what it
// grants, but a client must still acknowledge what it receives).
func Puback(packetID uint16) []byte {
	return packet(PacketPUBACK<<4, binary.BigEndian.AppendUint16(nil, packetID))
}

// Packet is one received control packet.
type Packet struct {
	Type  byte
	Flags byte
	Body  []byte
}

// ReadPacket reads one packet, refusing a body over limit bytes.
func ReadPacket(r io.Reader, limit int) (Packet, error) {
	var h [1]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return Packet{}, err
	}
	n, mult := 0, 1
	for i := 0; ; i++ {
		if i == 4 {
			return Packet{}, ErrRemainingLength
		}
		var d [1]byte
		if _, err := io.ReadFull(r, d[:]); err != nil {
			return Packet{}, err
		}
		n += int(d[0]&0x7F) * mult
		if d[0]&0x80 == 0 {
			break
		}
		mult *= 128
	}
	if limit > 0 && n > limit {
		return Packet{}, ErrPacketTooLarge
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return Packet{}, err
	}
	return Packet{Type: h[0] >> 4, Flags: h[0] & 0x0F, Body: body}, nil
}

// ConnackError is a refused CONNECT (§3.2.2.3).
type ConnackError struct{ Code byte }

func (e ConnackError) Error() string {
	names := map[byte]string{
		1: "unacceptable protocol version",
		2: "identifier rejected",
		3: "server unavailable",
		4: "bad user name or password",
		5: "not authorized",
	}
	if n, ok := names[e.Code]; ok {
		return fmt.Sprintf("mqtt: connection refused (%d %s)", e.Code, n)
	}
	return fmt.Sprintf("mqtt: connection refused (code %d)", e.Code)
}

// ParseConnack checks a CONNACK.
func ParseConnack(p Packet) error {
	if p.Type != PacketCONNACK || len(p.Body) != 2 {
		return fmt.Errorf("mqtt: expected CONNACK, got type %d len %d", p.Type, len(p.Body))
	}
	if p.Body[1] != 0 {
		return ConnackError{Code: p.Body[1]}
	}
	return nil
}

// ParseSuback returns the packet id and one return code per filter
// (§3.9.3): 0..2 the granted QoS, 0x80 failure.
func ParseSuback(p Packet) (uint16, []byte, error) {
	if p.Type != PacketSUBACK || len(p.Body) < 3 {
		return 0, nil, ErrShortPacket
	}
	return binary.BigEndian.Uint16(p.Body), p.Body[2:], nil
}

// Publish is one received PUBLISH (§3.3).
type Publish struct {
	Topic    string
	Payload  []byte
	QoS      byte
	Retain   bool
	Dup      bool
	PacketID uint16 // QoS 1 and 2 only
}

// ParsePublish decodes a PUBLISH.
func ParsePublish(p Packet) (Publish, error) {
	if p.Type != PacketPUBLISH {
		return Publish{}, fmt.Errorf("mqtt: expected PUBLISH, got type %d", p.Type)
	}
	out := Publish{QoS: (p.Flags >> 1) & 0x03, Retain: p.Flags&0x01 != 0, Dup: p.Flags&0x08 != 0}
	b := p.Body
	if len(b) < 2 {
		return Publish{}, ErrShortPacket
	}
	n := int(binary.BigEndian.Uint16(b))
	if len(b) < 2+n {
		return Publish{}, ErrShortPacket
	}
	out.Topic = string(b[2 : 2+n])
	b = b[2+n:]
	if out.QoS > 0 {
		if len(b) < 2 {
			return Publish{}, ErrShortPacket
		}
		out.PacketID = binary.BigEndian.Uint16(b)
		b = b[2:]
	}
	out.Payload = b
	return out, nil
}
