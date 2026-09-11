package rollcall

import (
	"context"
	"testing"

	"dhs/internal/snell-rollcall/codec"
)

// A device may page its menu into separately loadable partials, and a walk
// follows the CM_PARTIAL links to load them all. Measured on the real IQ
// gateway (unit 0x0C): its home menu is seven partial links, so a walk that
// loaded only the home partial returned seven objects and nothing behind them.
//
// The home menu here is the same shape — a hidden RETURN backlink, then two
// links to partials at bases 100 and 200 — and each partial carries a control
// the home does not. A full walk finds all three levels.
func pagedHome() []codec.MenuItem {
	return []codec.MenuItem{
		{MenuIndex: 0, Style: codec.StyleList, Step: 3, Text: "Menu"},
		{MenuIndex: 1, Style: codec.StylePartial | codec.StyleHidden, Command: 0, Text: "RETURN"},
		{MenuIndex: 2, Style: codec.StylePartial, Command: 100, Text: "Ethernet"},
		{MenuIndex: 3, Style: codec.StylePartial, Command: 200, Text: "Gateway"},
	}
}

func ethernetPartial() []codec.MenuItem {
	return []codec.MenuItem{
		{MenuIndex: 100, Style: codec.StylePartial | codec.StyleHidden, Command: 0, Text: "RETURN"},
		{MenuIndex: 101, Style: codec.StyleEditString, Command: 0x4001, MaxRange: 63, Text: "IP Address"},
		{MenuIndex: 102, Style: codec.StyleNumber, Command: 0x4002, MaxRange: 65535, Text: "Port"},
	}
}

func gatewayPartial() []codec.MenuItem {
	return []codec.MenuItem{
		{MenuIndex: 200, Style: codec.StylePartial | codec.StyleHidden, Command: 0, Text: "RETURN"},
		{MenuIndex: 201, Style: codec.StyleButton, Command: 16706, MinRange: 1, Text: "Restart Unit"},
	}
}

func pagedDevice(d *device) {
	d.setMenu(1, pagedHome())
	d.setPartial(1, 100, ethernetPartial())
	d.setPartial(1, 200, gatewayPartial())
}

func walkLabels(t *testing.T, setup func(*device)) map[string]bool {
	t.Helper()
	h := newHarness(t, setup)
	objs, err := h.plugin.Walk(context.Background(), 1)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	got := map[string]bool{}
	for _, o := range objs {
		got[o.Label] = true
	}
	return got
}

func TestWalkFollowsPartials32Bit(t *testing.T) {
	got := walkLabels(t, pagedDevice)
	for _, want := range []string{"IP Address", "Port", "Restart Unit"} {
		if !got[want] {
			t.Errorf("a walk that follows partials did not reach %q; it found %v", want, keys(got))
		}
	}
}

func TestWalkFollowsPartials16Bit(t *testing.T) {
	// The real gateway is 16-bit — it advertises no long strings — so this is
	// the path that matters. Same menu, older generation.
	got := walkLabels(t, func(d *device) {
		d.services &^= codec.SvcLongStr
		pagedDevice(d)
	})
	for _, want := range []string{"IP Address", "Port", "Restart Unit"} {
		if !got[want] {
			t.Errorf("a 16-bit walk did not reach %q; it found %v", want, keys(got))
		}
	}
}

func TestWalkFollowsPartialsTerminatesOnACycle(t *testing.T) {
	// A partial links back to one already loaded — its RETURN to the home, and
	// a spurious link to itself. A base is loaded once, so the walk ends
	// rather than looping, and each line appears once.
	got := walkLabels(t, func(d *device) {
		d.setMenu(1, []codec.MenuItem{
			{MenuIndex: 0, Style: codec.StyleList, Step: 1, Text: "Menu"},
			{MenuIndex: 1, Style: codec.StylePartial, Command: 100, Text: "Sub"},
		})
		d.setPartial(1, 100, []codec.MenuItem{
			{MenuIndex: 100, Style: codec.StylePartial | codec.StyleHidden, Command: 0, Text: "RETURN"},
			{MenuIndex: 101, Style: codec.StylePartial, Command: 100, Text: "Self"},
			{MenuIndex: 102, Style: codec.StyleNumber, Command: 42, Text: "Depth"},
		})
	})
	if !got["Depth"] {
		t.Errorf("the walk did not reach the sub-partial: %v", keys(got))
	}
}

func TestWalkSkipsDisabledPartials(t *testing.T) {
	// A disabled partial cannot be navigated to, so it is not loaded. Its link
	// still appears as an object; what is behind it does not.
	got := walkLabels(t, func(d *device) {
		d.setMenu(1, []codec.MenuItem{
			{MenuIndex: 0, Style: codec.StyleList, Step: 1, Text: "Menu"},
			{MenuIndex: 1, Style: codec.StylePartial | codec.StyleDisabled, Command: 100, Text: "Locked"},
		})
		d.setPartial(1, 100, []codec.MenuItem{
			{MenuIndex: 100, Style: codec.StyleNumber, Command: 7, Text: "Hidden Thing"},
		})
	})
	if !got["Locked"] {
		t.Error("the disabled partial's own link should still be an object")
	}
	if got["Hidden Thing"] {
		t.Error("a disabled partial was loaded anyway")
	}
}

func TestWalkLoadsASharedPartialOnce(t *testing.T) {
	// Two links to the same partial, so it is queued twice before it is
	// loaded. It is loaded once and its lines appear once.
	h := newHarness(t, func(d *device) {
		d.setMenu(1, []codec.MenuItem{
			{MenuIndex: 0, Style: codec.StyleList, Step: 2, Text: "Menu"},
			{MenuIndex: 1, Style: codec.StylePartial, Command: 100, Text: "Here"},
			{MenuIndex: 2, Style: codec.StylePartial, Command: 100, Text: "There"},
		})
		d.setPartial(1, 100, []codec.MenuItem{
			{MenuIndex: 100, Style: codec.StyleNumber, Command: 9, Text: "Shared"},
		})
	})

	objs, err := h.plugin.Walk(context.Background(), 1)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	n := 0
	for _, o := range objs {
		if o.Label == "Shared" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the shared partial's line appears %d times, want once", n)
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
