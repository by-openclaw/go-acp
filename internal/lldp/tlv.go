package lldp

import (
	"encoding/binary"
	"fmt"
)

// EtherType is the LLDP Ethertype (IEEE 802.1AB §8.1).
const EtherType = 0x88CC

// TLV types (IEEE 802.1AB Table 8-1). Only the ones this package reads are
// named; anything else decodes as a TLV with its number intact, because an
// unknown TLV is normal on the wire and must not fail a frame.
const (
	TypeEnd         uint8 = 0
	TypeChassisID   uint8 = 1
	TypePortID      uint8 = 2
	TypeTTL         uint8 = 3
	TypePortDesc    uint8 = 4
	TypeSysName     uint8 = 5
	TypeSysDesc     uint8 = 6
	TypeSysCaps     uint8 = 7
	TypeMgmtAddr    uint8 = 8
	TypeOrgSpecific uint8 = 127
)

// Chassis ID subtypes (IEEE 802.1AB Table 8-2).
const (
	ChassisSubtypeComponent uint8 = 1
	ChassisSubtypeIfAlias   uint8 = 2
	ChassisSubtypePortComp  uint8 = 3
	ChassisSubtypeMAC       uint8 = 4
	ChassisSubtypeNetAddr   uint8 = 5
	ChassisSubtypeIfName    uint8 = 6
	ChassisSubtypeLocal     uint8 = 7
)

// Port ID subtypes (IEEE 802.1AB Table 8-3). Note the numbering differs from
// chassis subtypes — MAC is 4 for a chassis and 3 for a port. Getting that
// backwards silently turns a MAC into a freeform string, so they are separate
// constant blocks rather than one shared set.
const (
	PortSubtypeIfAlias  uint8 = 1
	PortSubtypePortComp uint8 = 2
	PortSubtypeMAC      uint8 = 3
	PortSubtypeNetAddr  uint8 = 4
	PortSubtypeIfName   uint8 = 5
	PortSubtypeCircuit  uint8 = 6
	PortSubtypeLocal    uint8 = 7
)

// maxTLVLength is the 9-bit ceiling the header can express.
const maxTLVLength = 511

// TLV is one Type-Length-Value unit of an LLDPDU.
type TLV struct {
	Type  uint8
	Value []byte
}

// DecodeTLVs splits an LLDPDU into its TLVs.
//
// It stops at the End TLV and ignores whatever follows, because an Ethernet
// frame is padded to 60 bytes and those zero bytes are not a malformed TLV —
// treating them as one would reject almost every real frame from a switch
// that sends a short LLDPDU.
//
// A truncated TLV IS an error: it means the frame was cut, and continuing
// would report a neighbour assembled from bytes that were never received.
func DecodeTLVs(b []byte) ([]TLV, error) {
	var out []TLV
	for i := 0; i+2 <= len(b); {
		h := binary.BigEndian.Uint16(b[i:])
		typ := uint8(h >> 9)
		n := int(h & 0x01FF)
		i += 2
		if typ == TypeEnd {
			return out, nil
		}
		if i+n > len(b) {
			return nil, fmt.Errorf("lldp: TLV type %d claims %d bytes, %d remain", typ, n, len(b)-i)
		}
		v := make([]byte, n)
		copy(v, b[i:i+n])
		out = append(out, TLV{Type: typ, Value: v})
		i += n
	}
	// Running out of bytes without an End TLV. Real senders emit it; a frame
	// without one was truncated somewhere, and the TLVs already read may be
	// complete — so they are returned WITH the error, letting a caller that
	// only wants chassis/port decide for itself.
	return out, ErrNoEndTLV
}

// EncodeTLVs builds an LLDPDU, appending the End TLV.
//
// Present so the decoder can be tested against bytes this package did not
// itself produce in the same function, and so a future LLDP agent has the
// encoder it will need. dhs does not transmit LLDP today.
func EncodeTLVs(tlvs []TLV) ([]byte, error) {
	var out []byte
	for _, t := range tlvs {
		if t.Type > 127 {
			return nil, fmt.Errorf("lldp: TLV type %d exceeds the 7-bit field", t.Type)
		}
		if len(t.Value) > maxTLVLength {
			return nil, fmt.Errorf("lldp: TLV type %d value is %d bytes, max %d",
				t.Type, len(t.Value), maxTLVLength)
		}
		var h [2]byte
		binary.BigEndian.PutUint16(h[:], uint16(t.Type)<<9|uint16(len(t.Value)))
		out = append(out, h[:]...)
		out = append(out, t.Value...)
	}
	return append(out, 0x00, 0x00), nil
}
