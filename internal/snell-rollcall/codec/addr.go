package codec

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

// AddrSize is the wire size of a full address (spec 11.1.1).
const AddrSize = 6

// Reserved session indices (spec 5.4, 12.3).
const (
	// IndexBlind addresses a unit without a session. The server treats such
	// requests as UL_SUPERVISOR (spec 9.17).
	IndexBlind int16 = 0

	// IndexLogging is reserved for the logging service (vendor
	// CoreServiceAPI.h LOGGING_SESSION).
	IndexLogging int16 = 0xFE

	// IndexUnknown is UNKNOWNSESS: unconnected traffic, broadcasts, and the
	// destination of every Call.
	//
	// It is the value 255. Writing "unknown" as -1 is the single easiest
	// mistake to make here, because the field is signed; -1 encodes as
	// 0xFFFF, which a lenient proxy answers anyway and a real device faults
	// on. See internal/snell-rollcall/CLAUDE.md "What NOT to do" #1.
	IndexUnknown int16 = 0xFF
)

// Unit address ranges (spec 5.1).
const (
	UnitBroadcast   uint8 = 0x00 // also "the unit at the other end of this link"
	UnitBridgeFirst uint8 = 0x01
	UnitBridgeLast  uint8 = 0x0F
	UnitFirst       uint8 = 0x10
)

// Address is a RollCall address: a device (Net, Unit, Port) plus a session
// index. Written NNNN-UU-PP:SS.
//
// Spec 11.1.1 FULLADDRESS_STR, 6 bytes:
//
//	UINT16 rNet    4 nibbles of network route
//	UINT8  rUnit   unit address
//	UINT8  rPort   port within the unit
//	INT16  rIndex  session index
type Address struct {
	Net   uint16
	Unit  uint8
	Port  uint8
	Index int16
}

// Broadcast is 0000-00-00:FF, received by every unit on the local segment
// (spec 5.5).
func Broadcast() Address {
	return Address{Index: IndexUnknown}
}

// Loopback is FFFF-00-00, which a unit routes back to itself (spec 5.6).
func Loopback() Address {
	return Address{Net: 0xFFFF, Index: IndexUnknown}
}

// Device returns a copy with the session index cleared to IndexUnknown, which
// is how a device is named when the session is not relevant.
func (a Address) Device() Address {
	a.Index = IndexUnknown
	return a
}

// SameDevice reports whether two addresses name the same Net/Unit/Port,
// ignoring the session index.
func (a Address) SameDevice(b Address) bool {
	return a.Net == b.Net && a.Unit == b.Unit && a.Port == b.Port
}

// IsBroadcast reports whether the unit address is the broadcast address.
// Note that 0000-00-PP is also the "port PP of this unit" internal form
// (spec 5.7), so a gateway must disambiguate by context.
func (a Address) IsBroadcast() bool { return a.Unit == UnitBroadcast && a.Port == 0 }

// IsBridge reports whether the unit address falls in the range reserved for
// network bridges (spec 5.1).
func (a Address) IsBridge() bool {
	return a.Unit >= UnitBridgeFirst && a.Unit <= UnitBridgeLast
}

// HopCount returns how many bridges the route crosses: the number of non-zero
// nibbles in Net, counted from the top.
func (a Address) HopCount() int {
	n := 0
	for shift := 12; shift >= 0; shift -= 4 {
		if (a.Net>>uint(shift))&0xF == 0 {
			break
		}
		n++
	}
	return n
}

// ValidRoute reports whether Net is well formed. Routes fill from the top
// nibble downwards, so a zero nibble may not sit above a non-zero one; 0x0F00
// is malformed while 0xF000 and 0x0000 are not.
//
// The vendor library rejects the malformed form in Core/Transmit.c
// (CheckForRoutingError, RC_ADD_TXQ_BAD_NETADDR).
func (a Address) ValidRoute() bool {
	seenZero := false
	for shift := 12; shift >= 0; shift -= 4 {
		if (a.Net>>uint(shift))&0xF == 0 {
			seenZero = true
			continue
		}
		if seenZero {
			return false
		}
	}
	return true
}

// NextHop returns the bridge unit address a frame must be sent to, and whether
// the frame still needs bridging at all. When the top nibble of Net is zero
// the destination is on this segment and ok is false (spec 5.3).
func (a Address) NextHop() (bridge uint8, ok bool) {
	top := uint8(a.Net >> 12)
	if top == 0 {
		return 0, false
	}
	return top, true
}

// Forward applies the bridge transform of spec 5.3 to a destination address:
// the route shifts left by one nibble, dropping the hop just taken.
func (a Address) Forward() Address {
	a.Net <<= 4
	return a
}

// ForwardSource applies the matching transform to a source address: the route
// shifts right and the bridge's own address on the far net is inserted at the
// top, so the recipient can reverse the path.
func (a Address) ForwardSource(bridgeUnit uint8) Address {
	a.Net = (a.Net >> 4) | (uint16(bridgeUnit&0xF) << 12)
	return a
}

// AppendTo appends the 6-byte wire form of a to dst.
func (a Address) AppendTo(dst []byte) []byte {
	var b [AddrSize]byte
	binary.BigEndian.PutUint16(b[0:2], a.Net)
	b[2] = a.Unit
	b[3] = a.Port
	binary.BigEndian.PutUint16(b[4:6], uint16(a.Index))
	return append(dst, b[:]...)
}

// DecodeAddress reads a 6-byte address from the front of b.
func DecodeAddress(b []byte) (Address, error) {
	if err := need(b, AddrSize, "Address", ""); err != nil {
		return Address{}, err
	}
	return addressAt(b), nil
}

// addressAt reads an address from a slice the caller has already sized. Every
// structure that embeds an address validates its own length first, so this
// cannot fail and the callers stay free of error paths that can never run.
func addressAt(b []byte) Address {
	return Address{
		Net:   binary.BigEndian.Uint16(b[0:2]),
		Unit:  b[2],
		Port:  b[3],
		Index: int16(binary.BigEndian.Uint16(b[4:6])),
	}
}

// String renders the address as NNNN-UU-PP:SSS.
//
// The index is printed from the full 16-bit field with a minimum of three
// digits, matching the vendor tools ("0000:00:00/0FF"). It is deliberately not
// masked to 8 bits: an out-of-range index such as 0xFFFF is a defect worth
// seeing, and masking it would print a plausible "0FF" and hide it.
func (a Address) String() string {
	return fmt.Sprintf("%04X-%02X-%02X:%03X", a.Net, a.Unit, a.Port, uint16(a.Index))
}

// ParseAddress accepts NNNN-UU-PP and NNNN-UU-PP:SS, with either "-" or ":"
// between the device fields so that both spellings seen in vendor documents
// are understood. A missing session index means IndexUnknown.
func ParseAddress(s string) (Address, error) {
	text := strings.TrimSpace(s)
	if text == "" {
		return Address{}, fmt.Errorf("%w: empty", ErrBadAddress)
	}

	idx := IndexUnknown
	if i := strings.LastIndexByte(text, ':'); i >= 0 {
		// Only treat the last colon as a session separator when what
		// follows parses and what precedes still has three fields.
		head, tail := text[:i], text[i+1:]
		if strings.Count(strings.ReplaceAll(head, ":", "-"), "-") == 2 {
			v, err := strconv.ParseUint(tail, 16, 16)
			if err != nil {
				return Address{}, fmt.Errorf("%w: index %q", ErrBadAddress, tail)
			}
			idx = int16(v)
			text = head
		}
	}

	parts := strings.Split(strings.ReplaceAll(text, ":", "-"), "-")
	if len(parts) != 3 {
		return Address{}, fmt.Errorf("%w: want NNNN-UU-PP[:SS], got %q", ErrBadAddress, s)
	}

	net, err := strconv.ParseUint(parts[0], 16, 16)
	if err != nil {
		return Address{}, fmt.Errorf("%w: net %q", ErrBadAddress, parts[0])
	}
	unit, err := strconv.ParseUint(parts[1], 16, 8)
	if err != nil {
		return Address{}, fmt.Errorf("%w: unit %q", ErrBadAddress, parts[1])
	}
	port, err := strconv.ParseUint(parts[2], 16, 8)
	if err != nil {
		return Address{}, fmt.Errorf("%w: port %q", ErrBadAddress, parts[2])
	}

	return Address{
		Net:   uint16(net),
		Unit:  uint8(unit),
		Port:  uint8(port),
		Index: idx,
	}, nil
}
