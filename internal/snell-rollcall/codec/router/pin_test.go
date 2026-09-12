package router

import "testing"

// TestSourcePin pins the packing of spec §CMD_ROUTED_SRC: matrix in the top
// byte, level in the next, source in the low sixteen bits.
func TestSourcePin(t *testing.T) {
	tests := []struct {
		name   string
		pin    SourcePin
		packed uint32
	}{
		{"unrouted", SourcePin{}, 0},
		{"local source 1", SourcePin{Matrix: 1, Level: 1, Source: 1}, 0x0101_0001},
		{"the document's shape", SourcePin{Matrix: 2, Level: 3, Source: 0x1234}, 0x0203_1234},
		{"largest", SourcePin{Matrix: 0xFF, Level: 0xFF, Source: 0xFFFF}, 0xFFFF_FFFF},
		{"a full level", SourcePin{Matrix: 1, Level: 1, Source: 65535}, 0x0101_FFFF},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.pin.Pack(); got != tc.packed {
				t.Errorf("Pack() = %#08x, want %#08x", got, tc.packed)
			}
			if got := UnpackSourcePin(tc.packed); got != tc.pin {
				t.Errorf("Unpack(%#08x) = %+v, want %+v", tc.packed, got, tc.pin)
			}
		})
	}
}

// TestSourcePin_LocalVersusTieline pins the rule that decides how a controller
// makes a route: matching matrix and level is a local crosspoint, anything else
// needs a tieline found for it.
func TestSourcePin_LocalVersusTieline(t *testing.T) {
	dest := SourcePin{Matrix: 1, Level: 2, Source: 10}

	local := SourcePin{Matrix: 1, Level: 2, Source: 99}
	if !dest.SameLevel(local) {
		t.Error("same matrix and level is a local route")
	}

	for _, remote := range []SourcePin{
		{Matrix: 2, Level: 2, Source: 99}, // another matrix
		{Matrix: 1, Level: 3, Source: 99}, // another level
	} {
		if dest.SameLevel(remote) {
			t.Errorf("%s must not read as local to %s", remote, dest)
		}
	}
}

func TestSourcePin_Unrouted(t *testing.T) {
	// Source numbers are one-based, so zero is the only way to say "nothing".
	if !(SourcePin{Matrix: 1, Level: 1}).IsUnrouted() {
		t.Error("source 0 means unrouted")
	}
	if (SourcePin{Matrix: 1, Level: 1, Source: 1}).IsUnrouted() {
		t.Error("source 1 is a real source")
	}
	if got := (SourcePin{Matrix: 1, Level: 1}).String(); got != "unrouted" {
		t.Errorf("String() = %q", got)
	}
	if got := (SourcePin{Matrix: 2, Level: 3, Source: 4}).String(); got != "m2/l3/s4" {
		t.Errorf("String() = %q", got)
	}
}

func TestPinLimits(t *testing.T) {
	if MaxPinMatrix != 0xFF || MaxPinLevel != 0xFF || MaxPinSource != 0xFFFF {
		t.Error("the pin field widths do not match the 8/8/16 split")
	}
	// The limits are what the packing can hold, so the largest pin round trips.
	max := SourcePin{Matrix: MaxPinMatrix, Level: MaxPinLevel, Source: MaxPinSource}
	if UnpackSourcePin(max.Pack()) != max {
		t.Error("the largest pin does not round trip")
	}
}

// TestProtectState pins the word of spec §CMD_PROTECT_STATE, revision 4, where
// the id became 16 bits and the master flag moved to bit 24.
func TestProtectState(t *testing.T) {
	tests := []struct {
		name   string
		state  ProtectState
		packed uint32
	}{
		{"unprotected", ProtectState{}, 0},
		{"protected by 1", ProtectState{Protected: true, DeviceID: 1}, 0x0000_0101},
		{"protected by 1023", ProtectState{Protected: true, DeviceID: 1023}, 0x0003_FF01},
		{"master", ProtectState{Protected: true, DeviceID: 5, Master: true}, 0x0100_0501},
		{"master release", ProtectState{DeviceID: 5, Master: true}, 0x0100_0500},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.state.Pack(); got != tc.packed {
				t.Errorf("Pack() = %#08x, want %#08x", got, tc.packed)
			}
			if got := UnpackProtectState(tc.packed); got != tc.state {
				t.Errorf("Unpack(%#08x) = %+v, want %+v", tc.packed, got, tc.state)
			}
		})
	}

	// The id occupies bits 8..23, so it must not collide with the master bit
	// even at its widest.
	wide := ProtectState{Protected: true, DeviceID: 0xFFFF}
	if wide.Pack()&(1<<masterBit) != 0 {
		t.Error("a full-width device id spilled into the master bit")
	}
	if got := UnpackProtectState(wide.Pack()); got.DeviceID != 0xFFFF || got.Master {
		t.Errorf("= %+v, want the id preserved and no master flag", got)
	}
}

// TestProtectState_ReadHasNoMasterBit records what a read returns: the same
// layout with bit 24 always clear, and a zero id meaning not protected. A
// client must not conclude from a cleared master bit that the panel is not a
// master.
func TestProtectState_ReadHasNoMasterBit(t *testing.T) {
	read := UnpackProtectState(0x0000_0501)
	if !read.Protected || read.DeviceID != 5 {
		t.Errorf("= %+v, want protected by 5", read)
	}
	if read.Master {
		t.Error("a read never sets the master bit")
	}

	clear := UnpackProtectState(0)
	if clear.Protected || clear.DeviceID != 0 {
		t.Errorf("= %+v, want unprotected", clear)
	}
}

// TestProtectState_CanRelease pins the authority rule: a normal panel releases
// only what it protected, a master releases anything.
func TestProtectState_CanRelease(t *testing.T) {
	held := ProtectState{Protected: true, DeviceID: 7}

	tests := []struct {
		name  string
		panel ProtectState
		want  bool
	}{
		{"the panel that set it", ProtectState{DeviceID: 7}, true},
		{"another panel", ProtectState{DeviceID: 8}, false},
		{"a master", ProtectState{DeviceID: 8, Master: true}, true},
		{"a master with the same id", ProtectState{DeviceID: 7, Master: true}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.panel.CanRelease(held); got != tc.want {
				t.Errorf("CanRelease = %v, want %v", got, tc.want)
			}
		})
	}

	// An unprotected destination is releasable by anyone, which is what makes
	// a redundant release harmless.
	if !(ProtectState{DeviceID: 99}).CanRelease(ProtectState{}) {
		t.Error("an unprotected destination needs no authority to release")
	}
}

func TestProtectState_String(t *testing.T) {
	tests := []struct {
		in   ProtectState
		want string
	}{
		{ProtectState{}, "unprotected"},
		{ProtectState{DeviceID: 5}, "unprotected"},
		{ProtectState{Protected: true, DeviceID: 5}, "protected by 5"},
		{ProtectState{Protected: true, DeviceID: 5, Master: true}, "protected by 5 (master)"},
	}
	for _, tc := range tests {
		if got := tc.in.String(); got != tc.want {
			t.Errorf("String() = %q, want %q", got, tc.want)
		}
	}
	if MaxProtectID != 1023 {
		t.Errorf("MaxProtectID = %d, want the configured range ceiling 1023", MaxProtectID)
	}
}

// TestRouteResult pins the result codes as they stand at revision 12, which is
// the revision that settled them.
func TestRouteResult(t *testing.T) {
	tests := []struct {
		code RouteResult
		want string
	}{
		{RouteOK, "ok"},
		{RouteIdle, "controller idle"},
		{RouteNotInstalled, "source or destination not installed"},
		{RouteInhibited, "route inhibited"},
		{RouteProtected, "destination protected"},
		{RouteInUse, "source or destination in use"},
		{RouteNoTieline, "no tieline available"},
		{RouteConfiguration, "configuration error"},
		{RouteBadParameters, "invalid parameters"},
		{RouteNotConnected, "not connected"},
		{99, "result(99)"},
	}
	for _, tc := range tests {
		if got := tc.code.String(); got != tc.want {
			t.Errorf("RouteResult(%d) = %q, want %q", tc.code, got, tc.want)
		}
	}

	if !RouteOK.OK() {
		t.Error("code 0 is success")
	}
	for _, c := range []RouteResult{1, 2, 3, 4, 5, 6, 7, 8, 9} {
		if c.OK() {
			t.Errorf("code %d must not read as success", c)
		}
	}
}

// TestRouteResult_Retryable separates the failures worth repeating from the
// ones that will always fail the same way. Retrying a protected destination
// forever is how a panel ends up hammering a controller.
func TestRouteResult_Retryable(t *testing.T) {
	retryable := map[RouteResult]bool{RouteIdle: true, RouteNotConnected: true}
	for c := RouteResult(0); c <= 9; c++ {
		if got := c.Retryable(); got != retryable[c] {
			t.Errorf("%s Retryable = %v, want %v", c, got, retryable[c])
		}
	}
}
