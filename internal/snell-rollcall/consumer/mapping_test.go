package rollcall

import (
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/plugin"
	"dhs/internal/snell-rollcall/codec"
)

// TestStyleKind covers every menu style, because the mapping is what makes a
// RollCall menu legible to a caller that has never heard of RollCall, and a
// style left out reads as an object of unknown type rather than failing.
func TestStyleKind(t *testing.T) {
	tests := []struct {
		style codec.Style
		want  consumer.ValueKind
	}{
		{codec.StyleTiled, consumer.KindUnknown},   // a node
		{codec.StyleList, consumer.KindUnknown},    // a node
		{codec.StylePartial, consumer.KindUnknown}, // a node
		{codec.StyleDisplay, consumer.KindString},
		{codec.StyleButton, consumer.KindInt},
		{codec.StyleCheckbox, consumer.KindBool},
		{codec.StyleNumber, consumer.KindInt},
		{codec.StyleVGraph, consumer.KindInt},
		{codec.StyleHGraph, consumer.KindInt},
		{codec.StyleEditString, consumer.KindString},
		{codec.StyleVLevel, consumer.KindInt},
		{codec.StyleHLevel, consumer.KindInt},
		{codec.StyleData, consumer.KindRaw},
		{codec.StyleLink, consumer.KindString},
		{0xE0, consumer.KindUnknown}, // a style the specification does not define
	}
	for _, tc := range tests {
		if got := styleKind(tc.style); got != tc.want {
			t.Errorf("%s maps to %s, want %s", tc.style, got, tc.want)
		}
		// Flags must not change the kind: a hidden number is still a number.
		flagged := tc.style | codec.StyleHidden | codec.StyleCacheable
		if got := styleKind(flagged); got != tc.want {
			t.Errorf("%s with flags maps to %s, want %s", tc.style, got, tc.want)
		}
	}
}

// TestStyleAccess covers which lines a caller may write.
//
// Reporting a line as writable that a device will refuse invites a failure
// the caller could have been spared, so a container, a display and anything
// disabled are all read-only.
func TestStyleAccess(t *testing.T) {
	tests := []struct {
		name  string
		style codec.Style
		write bool
	}{
		{"number", codec.StyleNumber, true},
		{"checkbox", codec.StyleCheckbox, true},
		{"editable string", codec.StyleEditString, true},
		{"button", codec.StyleButton, true},
		{"data", codec.StyleData, true},
		{"tiled container", codec.StyleTiled, false},
		{"list container", codec.StyleList, false},
		{"partial container", codec.StylePartial, false},
		{"display", codec.StyleDisplay, false},
		{"disabled number", codec.StyleNumber | codec.StyleDisabled, false},
		{"disabled checkbox", codec.StyleCheckbox | codec.StyleDisabled, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := styleAccess(tc.style)
			if got&accessRead == 0 {
				t.Error("every line is readable")
			}
			if (got&accessWrite != 0) != tc.write {
				t.Errorf("writable = %v, want %v", got&accessWrite != 0, tc.write)
			}
			if got&accessSetDef != 0 {
				t.Error("the default bit is carried on the value, not the style")
			}
		})
	}
}

// TestSlotState covers the states a unit's status flags can produce.
//
// A unit reports whether it is present and whether it is under local control.
// It has no notion of a card that is booting or has just been removed, so
// those states are never produced rather than guessed at.
func TestSlotState(t *testing.T) {
	tests := []struct {
		name   string
		status codec.Status
		want   consumer.SlotState
	}{
		{"empty", 0, consumer.SlotStateNoCard},
		{"present", codec.StatusPresent, consumer.SlotStatePresent},
		{"present and online", codec.StatusPresent | codec.StatusOnline, consumer.SlotStatePresent},
		{"under local control", codec.StatusPresent | codec.StatusLocal, consumer.SlotStatePresent},
		{"online but absent", codec.StatusOnline, consumer.SlotStateNoCard},
	}
	for _, tc := range tests {
		if got := slotState(tc.status); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestSessionServices covers what a control session asks for, and the one bit
// that changes the answer.
func TestSessionServices(t *testing.T) {
	short := sessionServices(false)
	long := sessionServices(true)

	for _, want := range []codec.Service{codec.SvcMenus, codec.SvcControl, codec.SvcDisplay} {
		if !short.Has(want) {
			t.Errorf("a session should ask for %s", want)
		}
	}
	// The file service is opened separately, because services are
	// all-or-nothing and a unit without one must still be controllable.
	if short.Has(codec.SvcFile) {
		t.Error("the control session must not ask for the file service")
	}

	if short.LongStrings() {
		t.Error("the 16-bit request must not carry the long-string bit")
	}
	if !long.LongStrings() {
		t.Error("the 32-bit request must carry it")
	}
	if long&^codec.SvcLongStr != short {
		t.Error("the two requests should differ in exactly that one bit")
	}
}

// TestClientIdentity covers what we tell a peer we are. The type id is the
// vendor's own for a routing client, so their tools show us as something they
// recognise.
func TestClientIdentity(t *testing.T) {
	p := New(testDeps())
	id := p.identity()

	if id.ID.TypeID != codec.TypeIDRoutingIPShareClient {
		t.Errorf("type id = %d, want the routing client id", id.ID.TypeID)
	}
	if id.ID.Name == "" {
		t.Error("we should say what we are")
	}
	if _, err := id.AppendTo(nil); err != nil {
		t.Errorf("our own identity must encode: %v", err)
	}
}

func TestGenerationName(t *testing.T) {
	if got := generationName(codec.SvcMenus); got != "16-bit" {
		t.Errorf("= %q, want 16-bit", got)
	}
	if got := generationName(codec.SvcMenus | codec.SvcLongStr); got != "32-bit" {
		t.Errorf("= %q, want 32-bit", got)
	}
}

func TestDisplayLabel(t *testing.T) {
	tests := []struct {
		line int16
		want string
	}{
		{codec.DisplayLineError, "error"},
		{codec.DisplayLineWarning, "warning"},
		{0, "display0"},
		{3, "display3"},
	}
	for _, tc := range tests {
		if got := displayLabel(tc.line); got != tc.want {
			t.Errorf("displayLabel(%d) = %q, want %q", tc.line, got, tc.want)
		}
	}
}

func TestPathString(t *testing.T) {
	tests := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"a"}, "a"},
		{[]string{"a", "b", "c"}, "a.b.c"},
	}
	for _, tc := range tests {
		if got := pathString(tc.in); got != tc.want {
			t.Errorf("pathString(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestFactoryNew covers construction through the registry's own path, which is
// how the connector is actually built in service.
func TestFactoryNew(t *testing.T) {
	f := &Factory{}

	p := f.New(plugin.Deps{})
	if p == nil {
		t.Fatal("the factory returned nothing")
	}
	// An empty dependency set is filled in rather than panicking.
	if err := p.Disconnect(); err != nil {
		t.Errorf("Disconnect on a fresh connector: %v", err)
	}
}

// TestMenuLineScale covers the divisor's zero-means-one rule. Devices publish
// a zero routinely, and dividing by it directly faults.
func TestMenuLineScale(t *testing.T) {
	if got := (menuLine{}).Scale(); got != 1 {
		t.Errorf("Scale = %d, want 1 for a zero divisor", got)
	}
	if got := (menuLine{DivScale: 10}).Scale(); got != 10 {
		t.Errorf("Scale = %d, want 10", got)
	}
}

// TestRegistered covers the init that makes the connector reachable by name.
// Without it the package compiles, the tests pass, and the CLI has never heard
// of the protocol.
func TestRegistered(t *testing.T) {
	f, err := consumer.Get("rollcall")
	if err != nil {
		t.Fatalf("the connector is not registered: %v", err)
	}
	if f.Meta().Name != "rollcall" {
		t.Errorf("registered as %q", f.Meta().Name)
	}

	var found bool
	for _, name := range consumer.List() {
		if name == "rollcall" {
			found = true
		}
	}
	if !found {
		t.Errorf("rollcall is missing from %v", consumer.List())
	}
}
