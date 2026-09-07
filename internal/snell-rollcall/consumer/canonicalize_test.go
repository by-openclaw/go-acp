package rollcall

import (
	"context"
	"testing"

	"dhs/internal/export/canonical"
	"dhs/internal/snell-rollcall/codec"
)

// The canonical export is the reverse of what the provider does, and the two
// are meant to meet in the middle: a tree served as a RollCall device and
// walked back should describe the same objects.

func TestCanonicalizeARollCallDevice(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })
	ctx := context.Background()

	if _, err := h.plugin.Walk(ctx, 1); err != nil {
		t.Fatalf("Walk: %v", err)
	}

	exp, err := h.plugin.Canonicalize(ctx)
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}

	root := exp.Root.Common()
	if root.Identifier != "10.6.250.105" {
		t.Errorf("root = %q, want the host", root.Identifier)
	}
	if len(root.Children) != 1 {
		t.Fatalf("%d slots, want the one that was walked", len(root.Children))
	}

	slot := root.Children[0].Common()
	if slot.Identifier != "slot-1" || slot.Number != 1 {
		t.Errorf("slot = %q number %d", slot.Identifier, slot.Number)
	}

	// The test menu is a container with three lines under it and one beside
	// it, and the nesting is what the device published rather than one
	// imposed on it.
	if len(slot.Children) != 2 {
		t.Fatalf("%d roots under the slot, want the container and the line beside it",
			len(slot.Children))
	}

	video := slot.Children[0].Common()
	if video.Identifier != "Video" || len(video.Children) != 3 {
		t.Fatalf("container = %q with %d children", video.Identifier, len(video.Children))
	}
	if video.Access != canonical.AccessRead {
		t.Errorf("a container is not writable, but its access is %q", video.Access)
	}

	// A real is carried as a scaled integer, and the canonical form says so
	// with a factor rather than by pre-dividing: the wire value stays
	// recoverable.
	gain, ok := video.Children[0].(*canonical.Parameter)
	if !ok {
		t.Fatalf("gain came back as %T", video.Children[0])
	}
	if gain.Type != canonical.ParamReal {
		t.Errorf("gain is %q, want a real", gain.Type)
	}
	if gain.Factor == nil || *gain.Factor != 10 {
		t.Errorf("gain factor = %v, want 10", gain.Factor)
	}
	if gain.Minimum != -6.0 || gain.Maximum != 0.6 {
		t.Errorf("gain range = %v..%v, want the scaled -6..0.6", gain.Minimum, gain.Maximum)
	}
	if gain.Format == nil || *gain.Format != "%0.1f dB" {
		t.Errorf("gain format = %v; the format string is the only place a unit travels", gain.Format)
	}
	if gain.Access != canonical.AccessReadWrite {
		t.Errorf("gain access = %q", gain.Access)
	}

	enable, ok := video.Children[1].(*canonical.Parameter)
	if !ok || enable.Type != canonical.ParamBoolean {
		t.Errorf("enable came back as %T %v", video.Children[1], enable)
	}

	name, ok := video.Children[2].(*canonical.Parameter)
	if !ok || name.Type != canonical.ParamString {
		t.Fatalf("name came back as %T", video.Children[2])
	}
	if name.Maximum != 32.0 {
		t.Errorf("a string's range is a length: %v", name.Maximum)
	}

	// A display line may be read and not written.
	status, ok := slot.Children[1].(*canonical.Parameter)
	if !ok {
		t.Fatalf("status came back as %T", slot.Children[1])
	}
	if status.Access != canonical.AccessRead {
		t.Errorf("a display line is read-only, but its access is %q", status.Access)
	}
}

func TestCanonicalizeADeviceNobodyWalked(t *testing.T) {
	h := newHarness(t, nil)

	exp, err := h.plugin.Canonicalize(context.Background())
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	root := exp.Root.Common()
	if len(root.Children) != 0 {
		t.Errorf("%d slots on a device nothing has walked", len(root.Children))
	}
	// An empty children slice rather than a nil one, so the JSON has a key.
	if root.Children == nil {
		t.Error("children should be present and empty")
	}
}

func TestCanonicalizeWithoutAConnection(t *testing.T) {
	p := New(testDeps())

	exp, err := p.Canonicalize(context.Background())
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	if exp.Root.Common().Identifier != "device" {
		t.Errorf("a plugin with no host names itself %q", exp.Root.Common().Identifier)
	}
}

func TestCanonicalizeStopsWithItsContext(t *testing.T) {
	h := newHarness(t, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := h.plugin.Canonicalize(ctx); err == nil {
		t.Error("a cancelled context should stop the export")
	}
}

func TestExportCanonicalIsTheSameThing(t *testing.T) {
	// The device-model cache and the capture want the same export, and there
	// is no reason to build it twice.
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })
	ctx := context.Background()

	if _, err := h.plugin.Walk(ctx, 1); err != nil {
		t.Fatalf("Walk: %v", err)
	}

	a, err := h.plugin.Canonicalize(ctx)
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	b, err := h.plugin.ExportCanonical(ctx)
	if err != nil {
		t.Fatalf("ExportCanonical: %v", err)
	}
	if len(a.Root.Common().Children) != len(b.Root.Common().Children) {
		t.Error("the two exports describe different devices")
	}
}

func TestALineWithNoValueBecomesANode(t *testing.T) {
	// A line with no command and no children is a separator, or the
	// placeholder a server substitutes for something above this user level. It
	// names nothing that can be read.
	h := newHarness(t, func(d *device) {
		d.setMenu(1, []codec.MenuItem{
			{MenuIndex: 0, Style: codec.StyleDisplay | codec.StyleDisabled, Text: "Reserved"},
			{MenuIndex: 1, Style: codec.StyleNumber, Command: 5, Text: "",
				MinRange: 0, MaxRange: 100, Step: 5},
		})
	})
	ctx := context.Background()

	if _, err := h.plugin.Walk(ctx, 1); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	exp, err := h.plugin.Canonicalize(ctx)
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}

	slot := exp.Root.Common().Children[0].Common()
	if len(slot.Children) != 2 {
		t.Fatalf("%d lines, want 2", len(slot.Children))
	}
	if _, ok := slot.Children[0].(*canonical.Node); !ok {
		t.Errorf("a line with no command came back as %T", slot.Children[0])
	}
	// A line with no label is named by its index, because something has to
	// name it and inventing a word would be worse.
	if got := slot.Children[1].Common().Identifier; got != "line-1" {
		t.Errorf("an unnamed line is called %q", got)
	}

	// An integer without a divisor keeps its step as the device gave it.
	step, ok := slot.Children[1].(*canonical.Parameter)
	if !ok {
		t.Fatalf("the numeric line came back as %T", slot.Children[1])
	}
	if step.Type != canonical.ParamInteger || step.Step != 5.0 {
		t.Errorf("the numeric line is %q with step %v", step.Type, step.Step)
	}
}

func TestAccessFollowsTheStyle(t *testing.T) {
	for _, tc := range []struct {
		name  string
		style codec.Style
		want  string
	}{
		{"a number may be written", codec.StyleNumber, canonical.AccessReadWrite},
		{"a checkbox may be written", codec.StyleCheckbox, canonical.AccessReadWrite},
		{"an editable string may be written", codec.StyleEditString, canonical.AccessReadWrite},
		{"a display may not", codec.StyleDisplay, canonical.AccessRead},
		{"a list may not", codec.StyleList, canonical.AccessRead},
		{"a tiled list may not", codec.StyleTiled, canonical.AccessRead},
		{"a partial may not", codec.StylePartial, canonical.AccessRead},
		{"a disabled number may not", codec.StyleNumber | codec.StyleDisabled, canonical.AccessRead},
	} {
		if got := accessOf(tc.style); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestIdentityProbeNamesTheCardTypeNotTheLabel(t *testing.T) {
	// Two cards of one type with different operator labels share a device
	// model. What identifies the model is the vendor's type and the command
	// set the firmware implements, not the name somebody typed into a panel.
	h := newHarness(t, func(d *device) {
		d.identity[1] = codec.ID{
			TypeID:  623,
			Version: codec.Version{Major: 2, Minor: 4, Alpha: ' ', CmdSet: 7},
			Name:    "CAM 1 PROC",
		}
	})

	got, err := h.plugin.IdentityProbe(context.Background(), 1)
	if err != nil {
		t.Fatalf("IdentityProbe: %v", err)
	}
	want := identityToken(codec.UnitTypeName(623)) + "@2.4.cs7"
	if got != want {
		t.Errorf("identity = %q, want %q", got, want)
	}
	if got == "" || got[0] == '@' {
		t.Errorf("identity %q has no product half", got)
	}
}

func TestIdentityProbeOfATypeNobodyHasHeardOf(t *testing.T) {
	h := newHarness(t, func(d *device) {
		d.identity[1] = codec.ID{
			TypeID:  0xFFFE,
			Version: codec.Version{Major: 1, Alpha: ' ', CmdSet: 1},
		}
	})

	got, err := h.plugin.IdentityProbe(context.Background(), 1)
	if err != nil {
		t.Fatalf("IdentityProbe: %v", err)
	}
	// The number still identifies the model just as well as a name would.
	if got != "unit-type-65534@1.0.cs1" {
		t.Errorf("identity = %q", got)
	}
}

func TestIdentityProbeWhenTheDeviceWillNotSay(t *testing.T) {
	h := newHarness(t, func(d *device) { d.refuse[codec.MsgGetID] = true })

	if _, err := h.plugin.IdentityProbe(context.Background(), 1); err == nil {
		t.Error("a refused identity should reach the caller")
	}

	garbled := newHarness(t, func(d *device) { d.garble[codec.MsgGetID] = true })
	if _, err := garbled.plugin.IdentityProbe(context.Background(), 1); err == nil {
		t.Error("an identity that will not decode should be an error")
	}

	p := New(testDeps())
	if _, err := p.IdentityProbe(context.Background(), 1); err == nil {
		t.Error("an identity probe without a connection should fail")
	}
}

func TestIdentityTokenIsSafeInAPath(t *testing.T) {
	// The vendor's own type names carry spaces, dots and slashes — "4929 AES
	// O/P card" is one of them — and the identity becomes a path under
	// .cache/dm.
	for _, tc := range []struct{ in, want string }{
		{"4929 AES O/P card", "4929-AES-O-P-card"},
		{"RC32 Rout. IPSh Cli", "RC32-Rout--IPSh-Cli"},
		{"plain", "plain"},
		{"--edges--", "edges"},
		{"", ""},
	} {
		if got := identityToken(tc.in); got != tc.want {
			t.Errorf("identityToken(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
