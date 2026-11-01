package mib

import (
	"encoding/binary"
	"net/netip"
	"strconv"
	"strings"
)

// addressTCs are the textual conventions that say an OCTET STRING holds an
// address (RFC 4001, and the names MIBs give the same thing). InetAddressDNS
// is not here: it is already text.
var addressTCs = map[string]bool{
	"inetaddress": true, "inetaddressipv4": true, "inetaddressipv6": true,
	"inetaddressipv4z": true, "inetaddressipv6z": true,
	"ipv4address": true, "ipv6address": true,
}

// IsAddress says the object's syntax is an address carried as octets.
func (o *Object) IsAddress() bool {
	return addressTCs[strings.ToLower(o.TC)] || o.Base == "IpAddress"
}

// Address renders the octets of an address the way RFC 4001's DISPLAY-HINTs
// do, by their length: 4 and 16 bytes, and 8 and 20 with a zone on the end.
// It says no for anything else, and for 8, 16 or 20 printable bytes, which
// is the DNS name an InetAddress may hold, never an address.
func (o *Object) Address(b []byte) (string, bool) {
	if !o.IsAddress() {
		return "", false
	}
	if len(b) != 4 && isPrintable(b) {
		return "", false
	}
	switch len(b) {
	case 4:
		return netip.AddrFrom4([4]byte(b)).String(), true
	case 16:
		return netip.AddrFrom16([16]byte(b)).String(), true
	case 8:
		return netip.AddrFrom4([4]byte(b)).String() + zone(b[4:]), true
	case 20:
		return netip.AddrFrom16([16]byte(b)).String() + zone(b[16:]), true
	}
	return "", false
}

func zone(b []byte) string {
	return "%" + strconv.FormatUint(uint64(binary.BigEndian.Uint32(b)), 10)
}

func isPrintable(b []byte) bool {
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

// AddressBytes is the way back: what an operator typed, as the octets the
// syntax carries. A value that is no address is left for the caller, an
// InetAddress may be a DNS name.
func (o *Object) AddressBytes(s string) ([]byte, bool) {
	if !o.IsAddress() {
		return nil, false
	}
	text, zoneText, zoned := strings.Cut(s, "%")
	a, err := netip.ParseAddr(text)
	if err != nil {
		return nil, false
	}
	out := a.AsSlice()
	if zoned {
		z, err := strconv.ParseUint(zoneText, 10, 32)
		if err != nil {
			return nil, false
		}
		out = binary.BigEndian.AppendUint32(out, uint32(z))
	}
	return out, true
}
