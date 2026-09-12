package codec

import "testing"

// TestService_Bits pins the service mask of spec 12.1. The mask is what selects
// a generation, so a wrong bit here changes which wire form a session speaks.
func TestService_Bits(t *testing.T) {
	want := map[Service]string{
		0x0001: "Menus", 0x0002: "Control", 0x0004: "Display", 0x0008: "File",
		0x0010: "Logging", 0x0020: "Stream", 0x0040: "Map", 0x0080: "Ports",
		0x0100: "Net", 0x0200: "Exec", 0x0400: "Time", 0x0800: "Res2",
		0x1000: "Thumbnail", 0x2000: "FastMenu", 0x4000: "Loc3", 0x8000: "LongStr",
	}
	for bit, name := range want {
		if got := bit.String(); got != name {
			t.Errorf("service %04X = %q, want %q", uint16(bit), got, name)
		}
	}

	if got := Service(0).String(); got != "-" {
		t.Errorf("empty mask = %q, want %q", got, "-")
	}
	if got := (SvcMenus | SvcControl | SvcLongStr).String(); got != "Menus|Control|LongStr" {
		t.Errorf("combined mask = %q", got)
	}
}

func TestService_Predicates(t *testing.T) {
	m := SvcMenus | SvcControl | SvcLongStr

	if !m.Has(SvcMenus | SvcControl) {
		t.Error("Has must accept a subset")
	}
	if m.Has(SvcMenus | SvcFile) {
		t.Error("Has must require every requested bit, not any")
	}
	if !m.LongStrings() {
		t.Error("LongStrings must be true when bit 15 is set")
	}
	if (SvcMenus | SvcControl).LongStrings() {
		t.Error("LongStrings must be false without bit 15")
	}

	// The regression this guards: SvcLongStr is bit 15, so a signed 16-bit
	// variable makes the whole mask negative. Service is unsigned precisely so
	// this comparison holds.
	if uint16(SvcLongStr|SvcMenus|SvcControl) != 0x8003 {
		t.Error("the long-string mask must be 0x8003, not a negative number")
	}
}

func TestUserLevel(t *testing.T) {
	tests := []struct {
		lvl   UserLevel
		name  string
		valid bool
	}{
		{LevelUser, "user", true},
		{LevelEngineer, "engineer", true},
		{LevelSupervisor, "supervisor", true},
		{LevelFactory, "factory", true},
		{LevelAll, "all", false}, // a mask value, never sent in a Call
		{99, "unknown", false},
	}
	for _, tc := range tests {
		if got := tc.lvl.String(); got != tc.name {
			t.Errorf("level %d = %q, want %q", tc.lvl, got, tc.name)
		}
		if got := tc.lvl.Valid(); got != tc.valid {
			t.Errorf("level %s Valid = %v, want %v", tc.name, got, tc.valid)
		}
	}

	// Ordering matters: gating is a >= comparison in the vendor engine.
	ordered := []UserLevel{LevelUser, LevelEngineer, LevelSupervisor, LevelFactory}
	for i := 1; i < len(ordered); i++ {
		if ordered[i-1] >= ordered[i] {
			t.Errorf("%s must sort below %s", ordered[i-1], ordered[i])
		}
	}
}

func TestStatus(t *testing.T) {
	if got := Status(0).String(); got != "-" {
		t.Errorf("empty status = %q", got)
	}
	if got := (StatusOnline | StatusPresent).String(); got != "Online|Present" {
		t.Errorf("status = %q, want %q", got, "Online|Present")
	}
	full := StatusOnline | StatusMultiLevel | StatusPresent | StatusLocal
	if got := full.String(); got != "Online|MultiLevel|Present|Local" {
		t.Errorf("full status = %q", got)
	}
	if !(StatusOnline | StatusPresent).Has(StatusPresent) {
		t.Error("Has must find a set bit")
	}
	if StatusOnline.Has(StatusPresent) {
		t.Error("Has must not find a clear bit")
	}
}

// TestMode pins the rMode field of spec 12.8, including the Preset bit whose
// behaviour was measured against a live device rather than read from the spec.
func TestMode(t *testing.T) {
	want := map[Mode]string{
		0x01: "VALUE", 0x02: "STRING", 0x04: "DATA",
		0x08: "WRAPPED", 0x10: "PRESET", 0x20: "MATCH_ID",
	}
	for bit, name := range want {
		if got := bit.String(); got != name {
			t.Errorf("mode %02X = %q, want %q", uint16(bit), got, name)
		}
	}
	if got := Mode(0).String(); got != "-" {
		t.Errorf("empty mode = %q", got)
	}
	if got := (ModeValue | ModeString).String(); got != "VALUE|STRING" {
		t.Errorf("mode = %q", got)
	}
	if !(ModeValue | ModePreset).Has(ModePreset) {
		t.Error("Has must find the preset bit")
	}
	if ModeValue.Has(ModePreset) {
		t.Error("Has must not find a clear bit")
	}
}

// TestStyle_Kinds pins the line kinds of spec 12.6. The kind lives in the high
// nibble and the flags in the low one, so masking is what keeps a flagged
// button from decoding as a different widget.
func TestStyle_Kinds(t *testing.T) {
	tests := []struct {
		style     Style
		name      string
		container bool
	}{
		{StyleTiled, "Tiled", true},
		{StyleList, "List", true},
		{StyleDisplay, "Display", false},
		{StyleButton, "Button", false},
		{StyleCheckbox, "Checkbox", false},
		{StyleNumber, "Number", false},
		{StyleVGraph, "VGraph", false},
		{StyleHGraph, "HGraph", false},
		{StyleEditString, "EditString", false},
		{StyleVLevel, "VLevel", false},
		{StyleHLevel, "HLevel", false},
		{StylePartial, "Partial", false},
		{StyleData, "Data", false},
		{StyleLink, "Link", false},
		{0xE0, "Style?", false}, // undefined kind
	}
	for _, tc := range tests {
		if got := tc.style.String(); got != tc.name {
			t.Errorf("style %04X = %q, want %q", uint16(tc.style), got, tc.name)
		}
		if got := tc.style.Container(); got != tc.container {
			t.Errorf("%s Container = %v, want %v", tc.name, got, tc.container)
		}
		// Flags must not disturb the kind.
		flagged := tc.style | StyleCacheable | StyleDisabled
		if flagged.Kind() != tc.style.Kind() {
			t.Errorf("%s: flags changed the kind to %04X", tc.name, uint16(flagged.Kind()))
		}
		if flagged.Container() != tc.container {
			t.Errorf("%s: flags changed Container", tc.name)
		}
	}
}

func TestStyle_Flags(t *testing.T) {
	s := StyleButton | StyleCacheable | StyleWraps | StyleDisabled | StyleHidden | StyleDeferred
	if got, want := s.String(), "Button+Cacheable+Wraps+Disabled+Hidden+Deferred"; got != want {
		t.Errorf("String = %q, want %q", got, want)
	}
	if !s.Cacheable() || !s.Hidden() || !s.Disabled() || !s.Deferred() {
		t.Error("flag accessors disagree with the bits")
	}

	plain := StyleButton
	if plain.Cacheable() || plain.Hidden() || plain.Disabled() || plain.Deferred() {
		t.Error("a plain button reports a flag it does not carry")
	}
}

// TestAccessGated pins the substitution the vendor menu engine performs for a
// line above the session's user level: a hidden, disabled Data line with
// command 0 and the text "Reserved".
//
// This is the only way to detect level gating, because the engine substitutes
// rather than removes and the line count never changes. Every part of the
// pattern must match, or an ordinary hidden Data line would be misread as
// gated.
func TestAccessGated(t *testing.T) {
	gated := StyleData | StyleHidden | StyleDisabled

	if !AccessGated(gated, 0, "Reserved") {
		t.Error("the exact substitution must be recognised")
	}
	// Flags outside the pattern are allowed; the pattern is a minimum.
	if !AccessGated(gated|StyleCacheable, 0, "Reserved") {
		t.Error("an extra flag must not defeat the match")
	}

	for _, tc := range []struct {
		name  string
		style Style
		cmd   uint32
		text  string
	}{
		{"visible", StyleData | StyleDisabled, 0, "Reserved"},
		{"enabled", StyleData | StyleHidden, 0, "Reserved"},
		{"wrong kind", StyleDisplay | StyleHidden | StyleDisabled, 0, "Reserved"},
		{"has a command", gated, 42, "Reserved"},
		{"different text", gated, 0, "Spare"},
		{"empty text", gated, 0, ""},
	} {
		if AccessGated(tc.style, tc.cmd, tc.text) {
			t.Errorf("%s must not read as access-gated", tc.name)
		}
	}
}

func TestTermCode(t *testing.T) {
	tests := []struct {
		code TermCode
		want string
	}{
		{TermUser, "user"},
		{TermTimeout, "timeout"},
		{TermNetError, "net-error"},
		{TermRemote, "remote"},
		{9, "unknown(9)"},
		{0xFFFF, "unknown(65535)"},
	}
	for _, tc := range tests {
		if got := tc.code.String(); got != tc.want {
			t.Errorf("TermCode(%d) = %q, want %q", tc.code, got, tc.want)
		}
	}
}

// TestDisplayLinePriorities pins spec 11.5.4: a negative display line number is
// a priority, not a position.
func TestDisplayLinePriorities(t *testing.T) {
	if DisplayLineError != -1 || DisplayLineWarning != -2 {
		t.Errorf("error/warning lines are %d/%d, want -1/-2",
			DisplayLineError, DisplayLineWarning)
	}
}

func TestItoa(t *testing.T) {
	for _, tc := range []struct {
		in   uint16
		want string
	}{{0, "0"}, {7, "7"}, {42, "42"}, {1000, "1000"}, {65535, "65535"}} {
		if got := itoa(tc.in); got != tc.want {
			t.Errorf("itoa(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
