package s101

import "testing"

// TestFrame_DTDAppBytes_Captured pins the R6 #470 requirement: every
// EmBER frame carries the DTD minor/major in the 9-byte header
// (offsets 7+8). Decode must surface them on the Frame so the session
// can latch the device's wire-level DTD revision.
func TestFrame_DTDAppBytes_Captured(t *testing.T) {
	orig := NewEmBERFrame([]byte{0x60, 0x03, 0x01, 0x01, 0xFF})
	// Encoder defaults to the canonical 2.60 advertised by the
	// shipping Ember+ ecosystem (libember-cpp, EmberPlusView,
	// TinyEmber+, Lawo VSM). Decode round-trip must read them back.
	data := Encode(orig)
	got, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.DTDMinor != DTDMinorVersion {
		t.Errorf("DTDMinor: got 0x%02X, want 0x%02X", got.DTDMinor, DTDMinorVersion)
	}
	if got.DTDMajor != DTDMajorVersion {
		t.Errorf("DTDMajor: got 0x%02X, want 0x%02X", got.DTDMajor, DTDMajorVersion)
	}
}

// TestFrame_DTDAppBytes_AltVersions exercises the codec against a
// provider that advertises an older DTD (2.10 = 0x0A / 0x02) — every
// legit minor/major byte must round-trip without coercion.
func TestFrame_DTDAppBytes_AltVersions(t *testing.T) {
	cases := []struct{ minor, major byte }{
		{0x0A, 0x02}, // 2.10
		{0x1E, 0x02}, // 2.30
		{0x32, 0x02}, // 2.50
		{0x3C, 0x02}, // 2.60
	}
	for _, c := range cases {
		f := NewEmBERFrame([]byte{0x60, 0x01, 0x00})
		f.DTDMinor = c.minor
		f.DTDMajor = c.major
		got, err := Decode(Encode(f))
		if err != nil {
			t.Fatalf("Decode (%d.%d): %v", c.major, c.minor, err)
		}
		if got.DTDMinor != c.minor || got.DTDMajor != c.major {
			t.Errorf("round-trip %d.%d: got %d.%d",
				c.major, c.minor, got.DTDMajor, got.DTDMinor)
		}
	}
}

// TestFrame_KeepAlive_NoDTDBytes confirms keep-alive frames carry no
// app-bytes (their header is only 4 bytes per spec p.94). The session
// must not latch DTD from keep-alive traffic — otherwise the dead-man
// loop would surface a spurious 0.0 version on quiet sessions.
func TestFrame_KeepAlive_NoDTDBytes(t *testing.T) {
	got, err := Decode(Encode(NewKeepAliveRequest()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.DTDMinor != 0 || got.DTDMajor != 0 {
		t.Errorf("keep-alive carries DTD bytes: minor=0x%02X major=0x%02X (want 0/0)",
			got.DTDMinor, got.DTDMajor)
	}
}
