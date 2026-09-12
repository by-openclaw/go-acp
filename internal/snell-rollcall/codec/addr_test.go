package codec

import (
	"bytes"
	"errors"
	"testing"
)

func TestAddress_WireRoundTrip(t *testing.T) {
	// Spec 11.1.1: Net, Unit, Port, Index, big-endian, 6 bytes.
	want := []byte{0x10, 0x00, 0x08, 0x01, 0x00, 0x7B}
	a := Address{Net: 0x1000, Unit: 0x08, Port: 0x01, Index: 123}

	got := a.AppendTo(nil)
	if !bytes.Equal(got, want) {
		t.Fatalf("AppendTo = %x, want %x", got, want)
	}

	back, err := DecodeAddress(got)
	if err != nil {
		t.Fatalf("DecodeAddress: %v", err)
	}
	if back != a {
		t.Errorf("round trip = %+v, want %+v", back, a)
	}
}

func TestDecodeAddress_Short(t *testing.T) {
	if _, err := DecodeAddress([]byte{1, 2, 3}); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
}

func TestAddress_String(t *testing.T) {
	tests := []struct {
		addr Address
		want string
	}{
		{Address{Index: IndexUnknown}, "0000-00-00:0FF"},
		{Address{Net: 0x1000, Unit: 0x41, Port: 0x01, Index: 3}, "1000-41-01:003"},
		{Address{Unit: 0x20, Port: 0x02, Index: IndexBlind}, "0000-20-02:000"},
		// A malformed index must remain visible, not be masked to 8 bits.
		{Address{Index: -1}, "0000-00-00:FFFF"},
	}
	for _, tc := range tests {
		if got := tc.addr.String(); got != tc.want {
			t.Errorf("String() = %q, want %q", got, tc.want)
		}
	}
}

func TestParseAddress(t *testing.T) {
	tests := []struct {
		in   string
		want Address
	}{
		{"0000-00-00", Address{Index: IndexUnknown}},
		{"1000-41-01", Address{Net: 0x1000, Unit: 0x41, Port: 0x01, Index: IndexUnknown}},
		{"1000-41-01:03", Address{Net: 0x1000, Unit: 0x41, Port: 0x01, Index: 3}},
		{"0000:20:02:0FF", Address{Unit: 0x20, Port: 0x02, Index: IndexUnknown}},
		{"  0000-FF-01:00  ", Address{Unit: 0xFF, Port: 0x01, Index: 0}},
	}
	for _, tc := range tests {
		got, err := ParseAddress(tc.in)
		if err != nil {
			t.Errorf("ParseAddress(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseAddress(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestParseAddress_Rejects(t *testing.T) {
	for _, in := range []string{
		"", "   ", "1000-41", "1000-41-01-02", "zzzz-41-01",
		"1000-zz-01", "1000-41-zz", "1000-41-01:zz",
		"10000-41-01", // net wider than 16 bits
		"1000-100-01", // unit wider than 8 bits
	} {
		if _, err := ParseAddress(in); !errors.Is(err, ErrBadAddress) {
			t.Errorf("ParseAddress(%q) err = %v, want ErrBadAddress", in, err)
		}
	}
}

func TestParseAddress_RoundTripsString(t *testing.T) {
	for _, a := range []Address{
		{Index: IndexUnknown},
		{Net: 0x2300, Unit: 0x20, Port: 0x00, Index: 5},
		{Net: 0x1000, Unit: 0x81, Port: 0xFF, Index: 0xFE},
	} {
		back, err := ParseAddress(a.String())
		if err != nil {
			t.Fatalf("ParseAddress(%q): %v", a.String(), err)
		}
		if back != a {
			t.Errorf("%s round trips to %+v, want %+v", a, back, a)
		}
	}
}

func TestAddress_Predicates(t *testing.T) {
	if !(Address{Index: IndexUnknown}).IsBroadcast() {
		t.Error("0000-00-00 should be the broadcast address")
	}
	if (Address{Unit: 0x20}).IsBroadcast() {
		t.Error("a unit address is not the broadcast address")
	}
	if !(Address{Unit: 0x01}).IsBridge() || !(Address{Unit: 0x0F}).IsBridge() {
		t.Error("units 0x01..0x0F are bridge addresses (spec 5.1)")
	}
	if (Address{Unit: 0x10}).IsBridge() || (Address{Unit: 0x00}).IsBridge() {
		t.Error("0x00 and 0x10 are not bridge addresses")
	}

	a := Address{Net: 0x1000, Unit: 0x41, Port: 1, Index: 7}
	if got := a.Device(); got.Index != IndexUnknown {
		t.Errorf("Device() kept index %d", got.Index)
	}
	if !a.SameDevice(Address{Net: 0x1000, Unit: 0x41, Port: 1, Index: 99}) {
		t.Error("SameDevice must ignore the session index")
	}
	if a.SameDevice(Address{Net: 0x1000, Unit: 0x42, Port: 1}) {
		t.Error("SameDevice must compare the unit")
	}

	if got := Broadcast(); !got.IsBroadcast() || got.Index != IndexUnknown {
		t.Errorf("Broadcast() = %s", got)
	}
	if got := Loopback(); got.Net != 0xFFFF {
		t.Errorf("Loopback() = %s, want net FFFF (spec 5.6)", got)
	}
}

// TestAddress_Route covers the bridge routing rules of spec 5.3, including the
// worked example: a controller reaching a gateway two bridges away.
func TestAddress_Route(t *testing.T) {
	routeTests := []struct {
		net   uint16
		hops  int
		valid bool
	}{
		{0x0000, 0, true},  // local segment
		{0x2000, 1, true},  // one bridge
		{0x2300, 2, true},  // spec 5.3 worked example
		{0x1234, 4, true},  // the maximum
		{0x0F00, 0, false}, // gap: zero nibble above a non-zero one
		{0x0001, 0, false},
	}
	for _, tc := range routeTests {
		a := Address{Net: tc.net}
		if got := a.ValidRoute(); got != tc.valid {
			t.Errorf("Net %04X ValidRoute = %v, want %v", tc.net, got, tc.valid)
		}
		if !tc.valid {
			continue
		}
		if got := a.HopCount(); got != tc.hops {
			t.Errorf("Net %04X HopCount = %d, want %d", tc.net, got, tc.hops)
		}
	}

	// Spec 5.3: src 0000-10-00 to dst 2300-20-00 crosses bridge 2 then 3.
	dst := Address{Net: 0x2300, Unit: 0x20}
	src := Address{Net: 0x0000, Unit: 0x10}

	hop, ok := dst.NextHop()
	if !ok || hop != 0x02 {
		t.Fatalf("first hop = %02X ok=%v, want 02 true", hop, ok)
	}

	// Bridge 1 forwards: dst shifts left, src shifts right with the bridge's
	// address on the far net inserted at the top.
	dst, src = dst.Forward(), src.ForwardSource(0x01)
	if dst.Net != 0x3000 || src.Net != 0x1000 {
		t.Fatalf("after bridge 1: dst %04X src %04X, want 3000 and 1000", dst.Net, src.Net)
	}

	hop, ok = dst.NextHop()
	if !ok || hop != 0x03 {
		t.Fatalf("second hop = %02X ok=%v, want 03 true", hop, ok)
	}

	dst, src = dst.Forward(), src.ForwardSource(0x03)
	if dst.Net != 0x0000 || src.Net != 0x3100 {
		t.Fatalf("after bridge 2: dst %04X src %04X, want 0000 and 3100", dst.Net, src.Net)
	}
	if _, ok := dst.NextHop(); ok {
		t.Error("a zero top nibble means the destination is on this segment")
	}
}
