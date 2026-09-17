package lldp

// Expected behaviour comes from IEEE 802.1AB (TLV framing, the mandatory
// TLVs and their order, the shutdown LLDPDU) and from IS-04 v1.3's
// attached_network_device schema for the MAC rendering — not from working
// code.

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"testing"
	"time"
)

// frame assembles an LLDPDU from TLVs. Kept separate from EncodeTLVs in the
// cases that matter so the decoder is not merely tested against its own
// encoder.
func frame(t *testing.T, tlvs ...TLV) []byte {
	t.Helper()
	b, err := EncodeTLVs(tlvs)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return b
}

func chassisMAC(mac ...byte) TLV {
	return TLV{Type: TypeChassisID, Value: append([]byte{ChassisSubtypeMAC}, mac...)}
}

func portMAC(mac ...byte) TLV {
	return TLV{Type: TypePortID, Value: append([]byte{PortSubtypeMAC}, mac...)}
}

func ttl(sec uint16) TLV {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], sec)
	return TLV{Type: TypeTTL, Value: b[:]}
}

func mandatory(t *testing.T) []TLV {
	t.Helper()
	return []TLV{
		chassisMAC(0x00, 0x11, 0x22, 0x33, 0x44, 0x55),
		portMAC(0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff),
		ttl(120),
	}
}

// ---- TLV framing -----------------------------------------------------------

func TestTLVRoundTrip(t *testing.T) {
	in := []TLV{
		{Type: TypeChassisID, Value: []byte{ChassisSubtypeIfName, 'e', 't', 'h', '0'}},
		{Type: TypeSysName, Value: []byte("switch-1")},
	}
	b, err := EncodeTLVs(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := DecodeTLVs(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("got %d TLVs, want %d", len(out), len(in))
	}
	for i := range in {
		if out[i].Type != in[i].Type || string(out[i].Value) != string(in[i].Value) {
			t.Errorf("TLV %d: got %+v, want %+v", i, out[i], in[i])
		}
	}
}

// An Ethernet frame is padded to 60 bytes. Those zeros follow the End TLV and
// must not be read as another TLV, or nearly every real frame fails.
func TestEndTLVStopsAtPadding(t *testing.T) {
	b := frame(t, TLV{Type: TypeSysName, Value: []byte("sw")})
	padded := append(b, make([]byte, 20)...)
	out, err := DecodeTLVs(padded)
	if err != nil {
		t.Fatalf("padded frame must decode: %v", err)
	}
	if len(out) != 1 {
		t.Errorf("got %d TLVs, want 1 — padding was read as a TLV", len(out))
	}
}

func TestTruncatedTLVIsAnError(t *testing.T) {
	// Header claims 10 bytes, 2 supplied.
	b := []byte{TypeSysName << 1, 10, 'a', 'b'}
	if _, err := DecodeTLVs(b); err == nil {
		t.Error("a TLV claiming more bytes than remain must be an error")
	}
}

// Missing End TLV returns the TLVs read SO FAR alongside the error, so a
// caller that only needs the mandatory ones can still proceed.
func TestMissingEndTLVReturnsPartial(t *testing.T) {
	var b []byte
	for _, tlv := range mandatory(t) {
		var h [2]byte
		binary.BigEndian.PutUint16(h[:], uint16(tlv.Type)<<9|uint16(len(tlv.Value)))
		b = append(b, h[:]...)
		b = append(b, tlv.Value...)
	}
	out, err := DecodeTLVs(b)
	if !errors.Is(err, ErrNoEndTLV) {
		t.Fatalf("err = %v, want ErrNoEndTLV", err)
	}
	if len(out) != 3 {
		t.Errorf("got %d TLVs, want the 3 read before the end", len(out))
	}
}

func TestEncodeRejectsOversizeFields(t *testing.T) {
	if _, err := EncodeTLVs([]TLV{{Type: 128}}); err == nil {
		t.Error("a type above the 7-bit field must be rejected")
	}
	if _, err := EncodeTLVs([]TLV{{Type: TypeSysName, Value: make([]byte, maxTLVLength+1)}}); err == nil {
		t.Error("a value above the 9-bit length must be rejected")
	}
}

// ---- Neighbor decode -------------------------------------------------------

func TestDecodeFullFrame(t *testing.T) {
	tlvs := append(mandatory(t),
		TLV{Type: TypePortDesc, Value: []byte("GigabitEthernet1/0/7")},
		TLV{Type: TypeSysName, Value: []byte("core-sw-1")},
		TLV{Type: TypeSysDesc, Value: []byte("Vendor OS 1.2")},
		// Management address: len=5 (subtype + 4), IPv4, 10.0.0.9,
		// then interface subtype/number/OID which are not read.
		TLV{Type: TypeMgmtAddr, Value: []byte{5, 1, 10, 0, 0, 9, 2, 0, 0, 0, 1, 0}},
		// A vendor TLV every real switch sends; must be skipped, not fatal.
		TLV{Type: TypeOrgSpecific, Value: []byte{0x00, 0x12, 0x0f, 0x01, 0x03}},
	)
	n, err := Decode(frame(t, tlvs...))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if n.ChassisID != "00-11-22-33-44-55" {
		t.Errorf("chassis = %q, want the IS-04 hyphenated MAC form", n.ChassisID)
	}
	if n.PortID != "aa-bb-cc-dd-ee-ff" {
		t.Errorf("port = %q", n.PortID)
	}
	if n.TTL != 120*time.Second {
		t.Errorf("ttl = %v", n.TTL)
	}
	if n.SysName != "core-sw-1" || n.PortDesc != "GigabitEthernet1/0/7" || n.SysDesc != "Vendor OS 1.2" {
		t.Errorf("strings lost: %+v", n)
	}
	if !n.MgmtAddr.Equal(net.IPv4(10, 0, 0, 9)) {
		t.Errorf("mgmt addr = %v", n.MgmtAddr)
	}
	if n.Shutdown() {
		t.Error("TTL 120 is not a shutdown")
	}
	if n.ChassisSubtype != ChassisSubtypeMAC || n.PortSubtype != PortSubtypeMAC {
		t.Errorf("subtypes lost: %d %d", n.ChassisSubtype, n.PortSubtype)
	}
}

// The chassis and port subtype numbering differ — MAC is 4 for a chassis and
// 3 for a port. A shared table would render one of them as a freeform string.
func TestSubtypeNumberingIsNotShared(t *testing.T) {
	// Port subtype 4 is NETWORK ADDRESS, not MAC: must not be hyphenated.
	n, err := Decode(frame(t,
		chassisMAC(0x00, 0x11, 0x22, 0x33, 0x44, 0x55),
		TLV{Type: TypePortID, Value: []byte{PortSubtypeNetAddr, 1, 10, 0, 0, 1}},
		ttl(30),
	))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if n.PortID == "01-0a-00-00-01" {
		t.Error("port subtype 4 is a network address, not a MAC — chassis numbering was applied")
	}
}

func TestNonMACIdentifiersStayFreeform(t *testing.T) {
	n, err := Decode(frame(t,
		TLV{Type: TypeChassisID, Value: append([]byte{ChassisSubtypeLocal}, []byte("chassis-7")...)},
		TLV{Type: TypePortID, Value: append([]byte{PortSubtypeIfName}, []byte("Gi1/0/7")...)},
		ttl(30),
	))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if n.ChassisID != "chassis-7" || n.PortID != "Gi1/0/7" {
		t.Errorf("freeform ids mangled: %q %q", n.ChassisID, n.PortID)
	}
}

// A MAC subtype whose value is not six bytes is malformed. Rendering it as a
// hyphenated MAC anyway would emit a string IS-04's schema rejects.
func TestMACSubtypeWithWrongLengthFallsBack(t *testing.T) {
	n, err := Decode(frame(t,
		TLV{Type: TypeChassisID, Value: []byte{ChassisSubtypeMAC, 0x00, 0x11}},
		portMAC(0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff),
		ttl(30),
	))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if n.ChassisID == "00-11" {
		t.Error("a 2-byte MAC must not be rendered as a hyphenated MAC")
	}
}

func TestShutdownLLDPDU(t *testing.T) {
	n, err := Decode(frame(t,
		chassisMAC(0x00, 0x11, 0x22, 0x33, 0x44, 0x55),
		portMAC(0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff),
		ttl(0),
	))
	if err != nil {
		t.Fatalf("a shutdown LLDPDU is well-formed: %v", err)
	}
	if !n.Shutdown() {
		t.Error("TTL 0 is the shutdown announcement")
	}
}

func TestMandatoryTLVsRequired(t *testing.T) {
	full := mandatory(t)
	for _, tc := range []struct {
		name string
		drop int
	}{
		{"no chassis id", 0},
		{"no port id", 1},
		{"no ttl", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var kept []TLV
			for i, v := range full {
				if i != tc.drop {
					kept = append(kept, v)
				}
			}
			_, err := Decode(frame(t, kept...))
			if !errors.Is(err, ErrIncomplete) {
				t.Errorf("err = %v, want ErrIncomplete", err)
			}
		})
	}
}

func TestMalformedMandatoryTLVs(t *testing.T) {
	for _, tc := range []struct {
		name string
		tlvs []TLV
	}{
		{"chassis with subtype only", []TLV{
			{Type: TypeChassisID, Value: []byte{ChassisSubtypeMAC}},
			portMAC(1, 2, 3, 4, 5, 6), ttl(30)}},
		{"port with subtype only", []TLV{
			chassisMAC(1, 2, 3, 4, 5, 6),
			{Type: TypePortID, Value: []byte{PortSubtypeMAC}}, ttl(30)}},
		{"ttl not two bytes", []TLV{
			chassisMAC(1, 2, 3, 4, 5, 6), portMAC(1, 2, 3, 4, 5, 6),
			{Type: TypeTTL, Value: []byte{0x01}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Decode(frame(t, tc.tlvs...)); err == nil {
				t.Error("must be rejected")
			}
		})
	}
}

// A truncated frame that nonetheless carried every mandatory TLV is usable —
// they come first by requirement, so the useful half survived.
func TestTruncatedButCompleteDecodes(t *testing.T) {
	var b []byte
	for _, tlv := range mandatory(t) {
		var h [2]byte
		binary.BigEndian.PutUint16(h[:], uint16(tlv.Type)<<9|uint16(len(tlv.Value)))
		b = append(b, h[:]...)
		b = append(b, tlv.Value...)
	}
	n, err := Decode(b)
	if err != nil {
		t.Fatalf("a truncated frame with all mandatory TLVs must decode: %v", err)
	}
	if n.ChassisID != "00-11-22-33-44-55" {
		t.Errorf("chassis = %q", n.ChassisID)
	}
}

func TestTruncatedAndIncompleteReportsBoth(t *testing.T) {
	// Chassis only, no End TLV.
	c := chassisMAC(0x00, 0x11, 0x22, 0x33, 0x44, 0x55)
	var h [2]byte
	binary.BigEndian.PutUint16(h[:], uint16(c.Type)<<9|uint16(len(c.Value)))
	b := append(h[:], c.Value...)
	_, err := Decode(b)
	if !errors.Is(err, ErrIncomplete) || !errors.Is(err, ErrNoEndTLV) {
		t.Errorf("err = %v, want both ErrIncomplete and ErrNoEndTLV", err)
	}
}

func TestDecodePropagatesFramingError(t *testing.T) {
	if _, err := Decode([]byte{TypeSysName << 1, 10, 'a'}); err == nil {
		t.Error("a truncated TLV must fail the whole decode")
	}
}

func TestStringHygiene(t *testing.T) {
	n, err := Decode(frame(t, append(mandatory(t),
		// NUL-padded fixed-width field, and an invalid UTF-8 byte.
		TLV{Type: TypeSysName, Value: []byte("sw-1\x00\x00\x00")},
		TLV{Type: TypeSysDesc, Value: []byte{'a', 0xff, 'b'}},
	)...))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if n.SysName != "sw-1" {
		t.Errorf("sysName = %q — NUL padding not trimmed", n.SysName)
	}
	if n.SysDesc == "a\xffb" {
		t.Error("invalid UTF-8 must not be passed through to JSON")
	}
}

func TestManagementAddress(t *testing.T) {
	v6 := []byte{17, 2}
	v6 = append(v6, net.ParseIP("2001:db8::1").To16()...)
	for _, tc := range []struct {
		name  string
		value []byte
		want  net.IP
	}{
		{"ipv4", []byte{5, 1, 192, 168, 1, 1}, net.IPv4(192, 168, 1, 1)},
		{"ipv6", v6, net.ParseIP("2001:db8::1")},
		{"unknown family", []byte{5, 99, 1, 2, 3, 4}, nil},
		{"too short", []byte{1}, nil},
		{"length overruns", []byte{200, 1, 10, 0, 0, 1}, nil},
		{"zero length", []byte{0, 1}, nil},
		{"ipv4 wrong size", []byte{3, 1, 10, 0}, nil},
		{"ipv6 wrong size", []byte{3, 2, 10, 0}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := Decode(frame(t, append(mandatory(t),
				TLV{Type: TypeMgmtAddr, Value: tc.value})...))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			switch {
			case tc.want == nil && n.MgmtAddr != nil:
				t.Errorf("got %v, want none", n.MgmtAddr)
			case tc.want != nil && !n.MgmtAddr.Equal(tc.want):
				t.Errorf("got %v, want %v", n.MgmtAddr, tc.want)
			}
		})
	}
}

// Only the FIRST management address is kept; a switch may advertise several.
func TestFirstManagementAddressWins(t *testing.T) {
	n, err := Decode(frame(t, append(mandatory(t),
		TLV{Type: TypeMgmtAddr, Value: []byte{5, 1, 10, 0, 0, 1}},
		TLV{Type: TypeMgmtAddr, Value: []byte{5, 1, 10, 0, 0, 2}},
	)...))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !n.MgmtAddr.Equal(net.IPv4(10, 0, 0, 1)) {
		t.Errorf("got %v, want the first advertised address", n.MgmtAddr)
	}
}

// ---- Source ----------------------------------------------------------------

func TestSourceFunc(t *testing.T) {
	want := map[string]Neighbor{"eth0": {SysName: "sw"}}
	var s Source = SourceFunc(func(context.Context) (map[string]Neighbor, error) {
		return want, nil
	})
	got, err := s.Neighbors(context.Background())
	if err != nil || got["eth0"].SysName != "sw" {
		t.Errorf("got %v, %v", got, err)
	}
}

// A caller must not be able to mutate the source's own map through the
// returned one.
func TestStaticSourceReturnsCopy(t *testing.T) {
	s := StaticSource{"eth0": {SysName: "sw"}}
	got, err := s.Neighbors(context.Background())
	if err != nil {
		t.Fatalf("neighbors: %v", err)
	}
	got["eth0"] = Neighbor{SysName: "tampered"}
	delete(got, "eth0")
	if s["eth0"].SysName != "sw" {
		t.Error("the source's own map was mutated through the returned one")
	}
}

func TestCacheServesWithinTTL(t *testing.T) {
	var calls int
	now := time.Unix(1000, 0)
	c := NewCache(SourceFunc(func(context.Context) (map[string]Neighbor, error) {
		calls++
		return map[string]Neighbor{"eth0": {SysName: "sw"}}, nil
	}), time.Minute)
	c.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if _, err := c.Neighbors(context.Background()); err != nil {
			t.Fatalf("neighbors: %v", err)
		}
	}
	if calls != 1 {
		t.Errorf("source called %d times within the TTL, want 1", calls)
	}
	now = now.Add(2 * time.Minute)
	if _, err := c.Neighbors(context.Background()); err != nil {
		t.Fatalf("neighbors: %v", err)
	}
	if calls != 2 {
		t.Errorf("source called %d times after expiry, want 2", calls)
	}
}

func TestCacheZeroTTLAlwaysFetches(t *testing.T) {
	var calls int
	c := NewCache(SourceFunc(func(context.Context) (map[string]Neighbor, error) {
		calls++
		return map[string]Neighbor{}, nil
	}), 0)
	for i := 0; i < 3; i++ {
		if _, err := c.Neighbors(context.Background()); err != nil {
			t.Fatalf("neighbors: %v", err)
		}
	}
	if calls != 3 {
		t.Errorf("source called %d times with no TTL, want 3", calls)
	}
}

// A momentarily unreachable source must not look like a device that
// unplugged itself: the stale view is served WITH the error.
func TestCacheServesStaleOnError(t *testing.T) {
	fail := false
	boom := errors.New("switch unreachable")
	now := time.Unix(1000, 0)
	c := NewCache(SourceFunc(func(context.Context) (map[string]Neighbor, error) {
		if fail {
			return nil, boom
		}
		return map[string]Neighbor{"eth0": {SysName: "sw"}}, nil
	}), time.Minute)
	c.now = func() time.Time { return now }

	if _, err := c.Neighbors(context.Background()); err != nil {
		t.Fatalf("warm: %v", err)
	}
	fail = true
	now = now.Add(2 * time.Minute)
	got, err := c.Neighbors(context.Background())
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want the source error", err)
	}
	if got["eth0"].SysName != "sw" {
		t.Error("stale neighbours must survive a failed refresh")
	}
	// The error is remembered and reported again while the stale entry is
	// still being served from cache.
	now = now.Add(time.Second)
	if _, err := c.Neighbors(context.Background()); !errors.Is(err, boom) {
		t.Errorf("cached err = %v, want the source error", err)
	}
}

func TestCacheColdFailureReturnsNothing(t *testing.T) {
	boom := errors.New("no source")
	c := NewCache(SourceFunc(func(context.Context) (map[string]Neighbor, error) {
		return nil, boom
	}), time.Minute)
	got, err := c.Neighbors(context.Background())
	if !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nothing — there is no stale view to serve", got)
	}
}

func TestCacheReturnsCopy(t *testing.T) {
	c := NewCache(StaticSource{"eth0": {SysName: "sw"}}, time.Minute)
	got, err := c.Neighbors(context.Background())
	if err != nil {
		t.Fatalf("neighbors: %v", err)
	}
	delete(got, "eth0")
	again, err := c.Neighbors(context.Background())
	if err != nil {
		t.Fatalf("neighbors: %v", err)
	}
	if again["eth0"].SysName != "sw" {
		t.Error("the cached map was mutated through a returned copy")
	}
}
