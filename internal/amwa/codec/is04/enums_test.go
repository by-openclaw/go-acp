package is04

import "testing"

// TestFormatURNRules pins the rule IS-04 states for `format`: an
// x-nmos URN must be one AMWA defines; anything outside the x-nmos
// namespace is a vendor extension and passes.
func TestFormatURNRules(t *testing.T) {
	cases := map[string]bool{
		"":                              false,
		FormatAudio:                     true,
		FormatVideo:                     true,
		FormatData:                      true,
		FormatMux:                       true,
		"urn:x-nmos:format:hologram":    false,
		"urn:x-nmos:transport:rtp":      false, // x-nmos, but the wrong register
		"urn:x-vendor:format:foo":       true,
		"https://vendor.example/format": true,
	}
	for u, want := range cases {
		if got := IsValidFormatURN(u); got != want {
			t.Errorf("IsValidFormatURN(%q) = %v, want %v", u, got, want)
		}
	}
	if IsNMOSFormat("urn:x-vendor:format:foo") {
		t.Error("a vendor URN is not an NMOS-defined format")
	}
}

// TestTransportAndDeviceURNsRejectEmptyAndForeignNamespaces mirrors
// the same shape rule on `transport` and device `type`.
func TestTransportAndDeviceURNsRejectEmptyAndForeignNamespaces(t *testing.T) {
	if IsValidTransportURN("") {
		t.Error("empty transport must be invalid")
	}
	if IsValidTransportURN("urn:x-nmos:format:video") {
		t.Error("a format URN is not a transport")
	}
	if IsValidDeviceTypeURN("") {
		t.Error("empty device type must be invalid")
	}
	if IsValidDeviceTypeURN("urn:x-nmos:format:video") {
		t.Error("a format URN is not a device type")
	}
}

// TestIsTransportAtIS04 is the IS-04 side of the version gate: the
// sender.json enum at each tag decides, and a vendor transport is
// permitted on every minor.
func TestIsTransportAtIS04(t *testing.T) {
	cases := []struct {
		name string
		u    string
		ver  string
		want bool
	}{
		{"empty transport", "", "v1.3", false},
		{"unregistered x-nmos transport", "urn:x-nmos:transport:teleport", "v1.3", false},
		{"vendor transport on the oldest minor", "https://vendor.example/transport", "v1.0", true},
		{"rtp on v1.0", TransportRTP, "v1.0", true},
		{"websocket is not in the v1.2 sender.json enum", TransportWebSocket, "v1.2", false},
		{"websocket arrives in v1.3", TransportWebSocket, "v1.3", true},
		{"mxl on a future major", TransportMXL, "v2.0", true},
		{"an unparseable minor is treated as the newest", TransportMXL, "garbage", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsTransportAtIS04(tc.u, tc.ver); got != tc.want {
				t.Fatalf("IsTransportAtIS04(%q, %q) = %v, want %v", tc.u, tc.ver, got, tc.want)
			}
		})
	}
}

// TestCompareAPIVerOrdersMajorThenMinor: an unparseable side sorts
// ABOVE everything, so the gate degrades to "allow", never to a
// silent drop.
func TestCompareAPIVerOrdersMajorThenMinor(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.3", "v1.3", 0},
		{"v1.2", "v1.3", -1},
		{"v1.3", "v1.2", 1},
		{"v1.9", "v2.0", -1},
		{"v2.0", "v1.9", 1},
		{"garbage", "v1.0", 1},
		{"v1.0", "garbage", -1},
	}
	for _, tc := range cases {
		if got := compareAPIVer(tc.a, tc.b); got != tc.want {
			t.Errorf("compareAPIVer(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestSplitAPIVerRejectsEveryMalformation: only the exact `vMAJOR.MINOR`
// shape parses; each way of getting it wrong reports !ok.
func TestSplitAPIVerRejectsEveryMalformation(t *testing.T) {
	for _, bad := range []string{"", "1.3", "v13", "vx.3", "v1.x", "v"} {
		if _, _, ok := splitAPIVer(bad); ok {
			t.Errorf("splitAPIVer(%q) must not parse", bad)
		}
	}
	maj, min, ok := splitAPIVer("v1.3")
	if !ok || maj != 1 || min != 3 {
		t.Fatalf("splitAPIVer(v1.3) = %d, %d, %v", maj, min, ok)
	}
}
