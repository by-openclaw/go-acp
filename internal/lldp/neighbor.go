package lldp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrNoEndTLV reports an LLDPDU that ran out of bytes before its End TLV.
var ErrNoEndTLV = errors.New("lldp: LLDPDU ended without an End TLV")

// ErrIncomplete reports an LLDPDU missing one of the three TLVs 802.1AB
// makes mandatory (chassis ID, port ID, TTL).
var ErrIncomplete = errors.New("lldp: LLDPDU is missing a mandatory TLV")

// Neighbor is what one LLDP frame says about the device at the other end of
// a link.
//
// ChassisID and PortID are formatted for direct use: a MAC-subtype value
// becomes the lowercase hyphenated form ("00-11-22-33-44-55"), which is both
// the IEEE display convention and exactly the pattern IS-04's
// attached_network_device schema requires. Every other subtype stays the
// freeform string the sender chose, which that schema also permits.
type Neighbor struct {
	ChassisID string
	PortID    string

	// Subtypes are kept because they are the only way to know whether an ID
	// is a MAC, an interface name or something locally invented. Two switches
	// can send the same string with different meanings.
	ChassisSubtype uint8
	PortSubtype    uint8

	PortDesc string
	SysName  string
	SysDesc  string

	// MgmtAddr is the first management address advertised, or nil.
	MgmtAddr net.IP

	// TTL is how long this neighbour stays valid. Zero is meaningful: it is
	// the shutdown LLDPDU, the sender saying it is going away.
	TTL time.Duration
}

// Shutdown reports whether this frame was a shutdown announcement (TTL 0),
// which IEEE 802.1AB §8.4 requires a sender to emit when it stops. Callers
// must drop the neighbour rather than record it with a zero lifetime.
func (n Neighbor) Shutdown() bool { return n.TTL == 0 }

// Decode parses one LLDPDU — the Ethernet PAYLOAD, with no Ethernet header —
// into a Neighbor.
//
// Unknown and organisationally-specific TLVs are skipped rather than
// rejected: every real switch sends vendor TLVs, and a decoder that failed on
// them would report nothing on most networks.
func Decode(pdu []byte) (Neighbor, error) {
	tlvs, err := DecodeTLVs(pdu)
	// A truncated LLDPDU may still carry every mandatory TLV, since those
	// come first by requirement. Keep going and let the completeness check
	// below decide; only report the truncation if something is actually
	// missing.
	if err != nil && !errors.Is(err, ErrNoEndTLV) {
		return Neighbor{}, err
	}
	truncated := errors.Is(err, ErrNoEndTLV)

	var (
		n                              Neighbor
		haveChassis, havePort, haveTTL bool
	)
	for _, t := range tlvs {
		switch t.Type {
		case TypeChassisID:
			if len(t.Value) < 2 {
				return Neighbor{}, fmt.Errorf("lldp: chassis ID TLV is %d bytes, need subtype + value", len(t.Value))
			}
			n.ChassisSubtype = t.Value[0]
			n.ChassisID = formatID(t.Value[0] == ChassisSubtypeMAC, t.Value[1:])
			haveChassis = true
		case TypePortID:
			if len(t.Value) < 2 {
				return Neighbor{}, fmt.Errorf("lldp: port ID TLV is %d bytes, need subtype + value", len(t.Value))
			}
			n.PortSubtype = t.Value[0]
			n.PortID = formatID(t.Value[0] == PortSubtypeMAC, t.Value[1:])
			havePort = true
		case TypeTTL:
			if len(t.Value) != 2 {
				return Neighbor{}, fmt.Errorf("lldp: TTL TLV is %d bytes, want 2", len(t.Value))
			}
			n.TTL = time.Duration(binary.BigEndian.Uint16(t.Value)) * time.Second
			haveTTL = true
		case TypePortDesc:
			n.PortDesc = cleanString(t.Value)
		case TypeSysName:
			n.SysName = cleanString(t.Value)
		case TypeSysDesc:
			n.SysDesc = cleanString(t.Value)
		case TypeMgmtAddr:
			if ip := decodeMgmtAddr(t.Value); ip != nil && n.MgmtAddr == nil {
				n.MgmtAddr = ip
			}
		}
	}
	if !haveChassis || !havePort || !haveTTL {
		if truncated {
			return Neighbor{}, fmt.Errorf("%w: %w", ErrIncomplete, ErrNoEndTLV)
		}
		return Neighbor{}, fmt.Errorf("%w (chassis=%t port=%t ttl=%t)",
			ErrIncomplete, haveChassis, havePort, haveTTL)
	}
	return n, nil
}

// formatID renders an ID value: MACs in the lowercase hyphenated form IS-04
// requires, everything else as the sender's own string.
func formatID(isMAC bool, v []byte) string {
	if isMAC && len(v) == 6 {
		const hex = "0123456789abcdef"
		b := make([]byte, 0, 17)
		for i, c := range v {
			if i > 0 {
				b = append(b, '-')
			}
			b = append(b, hex[c>>4], hex[c&0x0f])
		}
		return string(b)
	}
	return cleanString(v)
}

// cleanString trims the trailing NULs switches pad fixed-width fields with,
// and refuses to hand back invalid UTF-8.
//
// The UTF-8 check is not fussiness: these strings end up in JSON, and
// encoding/json silently replaces invalid bytes with U+FFFD. A sysName that
// arrived as Latin-1 would then be published as mojibake with no trace of
// where it happened, so it is replaced here where the reason is visible.
func cleanString(v []byte) string {
	s := strings.TrimRight(string(v), "\x00")
	s = strings.TrimSpace(s)
	if !utf8.ValidString(s) {
		return strings.ToValidUTF8(s, "�")
	}
	return s
}

// decodeMgmtAddr reads the address out of a Management Address TLV
// (IEEE 802.1AB §8.5.9). Layout: length byte covering subtype+address, then
// an IANA address-family subtype, then the address.
//
// Returns nil for anything that is not IPv4 or IPv6 — the TLV can carry any
// IANA family, and guessing at one we cannot render is worse than omitting it.
func decodeMgmtAddr(v []byte) net.IP {
	if len(v) < 2 {
		return nil
	}
	n := int(v[0]) // subtype + address
	if n < 1 || 1+n > len(v) {
		return nil
	}
	subtype, addr := v[1], v[2:1+n]
	switch subtype {
	case 1: // IANA: IPv4
		if len(addr) == net.IPv4len {
			return net.IP(append([]byte(nil), addr...))
		}
	case 2: // IANA: IPv6
		if len(addr) == net.IPv6len {
			return net.IP(append([]byte(nil), addr...))
		}
	}
	return nil
}
