package rollcall

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/snell-rollcall/codec"
)

func TestFactory(t *testing.T) {
	f := &Factory{}
	meta := f.Meta()

	if meta.Name != "rollcall" {
		t.Errorf("name = %q, want rollcall", meta.Name)
	}
	if meta.DefaultPort != 2050 {
		t.Errorf("default port = %d, want the IPShare port 2050", meta.DefaultPort)
	}
	if meta.Description == "" {
		t.Error("the connector should describe itself")
	}
	// Both generations are the point of this connector, so the description
	// should say so rather than leaving an operator to find out.
	if !strings.Contains(meta.Description, "16-bit") || !strings.Contains(meta.Description, "32-bit") {
		t.Errorf("description = %q, want it to name both generations", meta.Description)
	}
}

// TestConnect_LearnsTheGateway covers the handshake, which is the first thing
// on a new connection and the only way a TCP client learns either address.
func TestConnect_LearnsTheGateway(t *testing.T) {
	h := newHarness(t, nil)

	info, err := h.plugin.GetDeviceInfo(context.Background())
	if err != nil {
		t.Fatalf("GetDeviceInfo: %v", err)
	}
	if info.IP != "10.6.250.105" || info.Port != DefaultPort {
		t.Errorf("= %s:%d", info.IP, info.Port)
	}
	if info.ProtocolVersion != int(codec.ProtocolVersion) {
		t.Errorf("protocol version = %d", info.ProtocolVersion)
	}
	if info.NumSlots != 2 {
		t.Errorf("slots = %d, want the device's 2 ports", info.NumSlots)
	}
}

// TestConnect_PrefersTheLongStringGeneration pins the negotiation. A peer that
// advertises long strings gets a call asking for them, because that generation
// is the only one that can express a router's command space.
func TestConnect_PrefersTheLongStringGeneration(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })

	long, err := h.plugin.Uses32Bit(context.Background(), 1)
	if err != nil {
		t.Fatalf("Uses32Bit: %v", err)
	}
	if !long {
		t.Error("a peer advertising long strings should be asked for them")
	}
}

// TestConnect_FallsBackAGeneration covers a peer that advertises long strings
// and then refuses a call asking for them.
//
// Services are all-or-nothing, so there is no partial grant to fall back to:
// the client must issue a second call without the bit. The two statements the
// peer made cannot both be true, so the fallback is recorded rather than done
// silently.
func TestConnect_FallsBackAGeneration(t *testing.T) {
	h := newHarness(t, func(d *device) {
		d.refuseLongStrings = true
		d.setMenu(1, testMenu())
	})

	long, err := h.plugin.Uses32Bit(context.Background(), 1)
	if err != nil {
		t.Fatalf("Uses32Bit: %v", err)
	}
	if long {
		t.Error("the session should have fallen back to the 16-bit generation")
	}

	// And the connector said so, because a silent fallback changes which
	// commands are reachable.
	if !hasEvent(h.plugin, EventLongStringsRefused) {
		t.Error("the fallback was not recorded as a compliance event")
	}

	// The fallback session still works.
	objects, err := h.plugin.Walk(context.Background(), 1)
	if err != nil {
		t.Fatalf("Walk on the fallback session: %v", err)
	}
	if len(objects) != len(testMenu()) {
		t.Errorf("walked %d objects, want %d", len(objects), len(testMenu()))
	}
}

// TestWalk_BuildsTheTreeFromSpans is the menu model, and the one thing most
// likely to be got wrong.
//
// The array is flat and a container's step is the span of its whole subtree,
// not the count of its immediate children. Reading it as a child count nests
// everything below the second level in the wrong place, and the result still
// looks like a tree.
func TestWalk_BuildsTheTreeFromSpans(t *testing.T) {
	// A tree two levels deep, where the two readings differ:
	//   Root      span 4   the whole subtree
	//     Video   span 2   its own two children
	//       Gain
	//       Offset
	//     Mode
	menu := []codec.MenuItem{
		{MenuIndex: 0, Style: codec.StyleList, Step: 4, Text: "Root"},
		{MenuIndex: 1, Style: codec.StyleList, Step: 2, Text: "Video"},
		{MenuIndex: 2, Style: codec.StyleNumber, Command: 10, Text: "Gain"},
		{MenuIndex: 3, Style: codec.StyleNumber, Command: 11, Text: "Offset"},
		{MenuIndex: 4, Style: codec.StyleNumber, Command: 12, Text: "Mode"},
	}
	h := newHarness(t, func(d *device) { d.setMenu(1, menu) })

	objects, err := h.plugin.Walk(context.Background(), 1)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(objects) != len(menu) {
		t.Fatalf("walked %d objects, want %d", len(objects), len(menu))
	}

	want := map[string][]string{
		"Root":   {"Root"},
		"Video":  {"Root", "Video"},
		"Gain":   {"Root", "Video", "Gain"},
		"Offset": {"Root", "Video", "Offset"},
		"Mode":   {"Root", "Mode"},
	}
	for _, o := range objects {
		got := want[o.Label]
		if got == nil {
			t.Errorf("unexpected object %q", o.Label)
			continue
		}
		if strings.Join(o.Path, ".") != strings.Join(got, ".") {
			t.Errorf("%q is at %v, want %v", o.Label, o.Path, got)
		}
	}

	// "Mode" is the line that separates the two readings. Under a child count
	// it would land inside Video; under a span it is a sibling of it.
	for _, o := range objects {
		if o.Label == "Mode" && len(o.Path) != 2 {
			t.Errorf("Mode is at %v; the span was read as a child count", o.Path)
		}
	}
}

// TestWalk_MapsStylesToKinds covers the translation that makes a RollCall menu
// legible to a caller that has never heard of RollCall.
func TestWalk_MapsStylesToKinds(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })

	objects, err := h.plugin.Walk(context.Background(), 1)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	byLabel := map[string]consumer.Object{}
	for _, o := range objects {
		byLabel[o.Label] = o
	}

	tests := []struct {
		label  string
		kind   consumer.ValueKind
		write  bool
		header bool
	}{
		{"Video", consumer.KindUnknown, false, true},  // a container is a node
		{"Gain", consumer.KindInt, true, false},       // a number
		{"Enable", consumer.KindBool, true, false},    // a checkbox is a boolean
		{"Name", consumer.KindString, true, false},    // an editable string
		{"Status", consumer.KindString, false, false}, // a display is read-only
	}
	for _, tc := range tests {
		o, ok := byLabel[tc.label]
		if !ok {
			t.Errorf("%q is missing from the walk", tc.label)
			continue
		}
		if o.Kind != tc.kind {
			t.Errorf("%q is %s, want %s", tc.label, o.Kind, tc.kind)
		}
		if o.HasWrite() != tc.write {
			t.Errorf("%q writable = %v, want %v", tc.label, o.HasWrite(), tc.write)
		}
		if !o.HasRead() {
			t.Errorf("%q should be readable", tc.label)
		}
		if o.SubGroupMarker != tc.header {
			t.Errorf("%q header = %v, want %v", tc.label, o.SubGroupMarker, tc.header)
		}
	}

	// A number carries its range, and its format string is the closest thing
	// a menu line has to a unit.
	gain := byLabel["Gain"]
	if gain.Min != int64(-60) || gain.Max != int64(6) {
		t.Errorf("Gain range = %v..%v, want -60..6", gain.Min, gain.Max)
	}
	if gain.Unit != "dB" {
		t.Errorf("Gain unit = %q, want dB from the format string", gain.Unit)
	}
	if gain.ID != 0x0113 {
		t.Errorf("Gain id = %d, want its command number", gain.ID)
	}
}

// TestWalk_DisabledLineIsReadOnly covers the flag that decides whether a
// caller may write. A greyed line in the vendor's own panel is one a device
// will refuse, so reporting it as writable would invite a failure.
func TestWalk_DisabledLineIsReadOnly(t *testing.T) {
	menu := []codec.MenuItem{
		{MenuIndex: 0, Style: codec.StyleNumber, Command: 1, Text: "Open"},
		{MenuIndex: 1, Style: codec.StyleNumber | codec.StyleDisabled, Command: 2, Text: "Locked"},
	}
	h := newHarness(t, func(d *device) { d.setMenu(1, menu) })

	objects, err := h.plugin.Walk(context.Background(), 1)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, o := range objects {
		switch o.Label {
		case "Open":
			if !o.HasWrite() {
				t.Error("an enabled number should be writable")
			}
		case "Locked":
			if o.HasWrite() {
				t.Error("a disabled line must not be reported as writable")
			}
			if !o.HasRead() {
				t.Error("a disabled line is still readable")
			}
		}
	}
}

// TestWalk_ReportsAccessGating covers the placeholder a server substitutes for
// a line above the session's user level.
//
// It is not a removal: line counts are identical at every level, so a client
// comparing counts sees no difference. Only the flags reveal it, which is why
// it is detected and reported.
func TestWalk_ReportsAccessGating(t *testing.T) {
	menu := []codec.MenuItem{
		{MenuIndex: 0, Style: codec.StyleNumber, Command: 1, Text: "Visible"},
		{MenuIndex: 1, Style: codec.StyleData | codec.StyleHidden | codec.StyleDisabled,
			Command: 0, Text: "Reserved"},
	}
	h := newHarness(t, func(d *device) { d.setMenu(1, menu) })

	objects, err := h.plugin.Walk(context.Background(), 1)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	// The line is still there, which is the whole point: it is substituted,
	// not removed.
	if len(objects) != 2 {
		t.Errorf("walked %d objects, want 2; gating substitutes rather than removes", len(objects))
	}
	if !hasEvent(h.plugin, EventAccessGated) {
		t.Error("the gated line was not reported")
	}
}

// TestGetValue_ResolvesColdByLabel covers the promise that a caller may name an
// object without knowing its command number, and without having walked first.
func TestGetValue_ResolvesColdByLabel(t *testing.T) {
	h := newHarness(t, func(d *device) {
		d.setMenu(1, testMenu())
		d.setValue(1, codec.Value{Command: 0x0113, Mode: codec.ModeValue, Val: -12})
	})

	// No walk first. The read has to do it.
	v, err := h.plugin.GetValue(context.Background(), consumer.ValueRequest{Slot: 1, Label: "Gain"})
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if v.Kind != consumer.KindInt || v.Int != -12 {
		t.Errorf("= %s %d, want int -12", v.Kind, v.Int)
	}
}

func TestGetValue_ResolvesByPathAndID(t *testing.T) {
	h := newHarness(t, func(d *device) {
		d.setMenu(1, testMenu())
		d.setValue(1, codec.Value{Command: 0x0113, Mode: codec.ModeValue, Val: 3})
	})
	ctx := context.Background()

	for _, req := range []consumer.ValueRequest{
		{Slot: 1, Path: "Video.Gain"},
		{Slot: 1, Label: "gain"}, // matching is case-insensitive
		{Slot: 1, ID: 0x0113},
	} {
		v, err := h.plugin.GetValue(ctx, req)
		if err != nil {
			t.Errorf("%+v: %v", req, err)
			continue
		}
		if v.Int != 3 {
			t.Errorf("%+v = %d, want 3", req, v.Int)
		}
	}
}

func TestGetValue_Unresolvable(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })
	ctx := context.Background()

	for _, req := range []consumer.ValueRequest{
		{Slot: 1, Label: "no such object"},
		{Slot: 1, Path: "nowhere.at.all"},
		{Slot: 1}, // names nothing at all
	} {
		if _, err := h.plugin.GetValue(ctx, req); err == nil {
			t.Errorf("%+v should not have resolved", req)
		}
	}
}

// TestSetValue_ReturnsWhatTheDeviceStored pins the property that makes a write
// worth reading back. The reply carries the device's own value, so a caller
// that asked for something out of range learns what it actually got without a
// second read.
func TestSetValue_ReturnsWhatTheDeviceStored(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })
	ctx := context.Background()

	// In range: stored as asked.
	got, err := h.plugin.SetValue(ctx,
		consumer.ValueRequest{Slot: 1, Label: "Gain"},
		consumer.Value{Kind: consumer.KindInt, Int: -20})
	if err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	if got.Int != -20 {
		t.Errorf("= %d, want -20", got.Int)
	}

	// Above the range: the device clamps, and only the reply says so.
	got, err = h.plugin.SetValue(ctx,
		consumer.ValueRequest{Slot: 1, Label: "Gain"},
		consumer.Value{Kind: consumer.KindInt, Int: 9999})
	if err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	if got.Int != 6 {
		t.Errorf("= %d, want the device's ceiling of 6", got.Int)
	}

	// And a read agrees, so the reply was the truth rather than an echo.
	back, err := h.plugin.GetValue(ctx, consumer.ValueRequest{Slot: 1, Label: "Gain"})
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if back.Int != 6 {
		t.Errorf("read back %d, want 6", back.Int)
	}
}

func TestSetValue_Bool(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })
	ctx := context.Background()

	got, err := h.plugin.SetValue(ctx,
		consumer.ValueRequest{Slot: 1, Label: "Enable"},
		consumer.Value{Kind: consumer.KindBool, Bool: true})
	if err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	if got.Kind != consumer.KindBool || !got.Bool {
		t.Errorf("= %s %v, want a true boolean", got.Kind, got.Bool)
	}

	got, err = h.plugin.SetValue(ctx,
		consumer.ValueRequest{Slot: 1, Label: "Enable"},
		consumer.Value{Kind: consumer.KindBool, Bool: false})
	if err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	if got.Bool {
		t.Error("= true, want false")
	}
}

func TestSetValue_String(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })

	got, err := h.plugin.SetValue(context.Background(),
		consumer.ValueRequest{Slot: 1, Label: "Name"},
		consumer.Value{Kind: consumer.KindString, Str: "Camera 1"})
	if err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	if got.Kind != consumer.KindString || got.Str != "Camera 1" {
		t.Errorf("= %s %q", got.Kind, got.Str)
	}
}

// TestSetDefault covers restoring a device's own default, which is a distinct
// operation rather than a write of a known value: only the device knows what
// its default is. The preset flag asks for it, and the numeric field is
// ignored.
func TestSetDefault(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })
	ctx := context.Background()

	if _, err := h.plugin.SetValue(ctx,
		consumer.ValueRequest{Slot: 1, Label: "Gain"},
		consumer.Value{Kind: consumer.KindInt, Int: 5}); err != nil {
		t.Fatalf("SetValue: %v", err)
	}

	got, err := h.plugin.SetDefault(ctx, consumer.ValueRequest{Slot: 1, Label: "Gain"})
	if err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	if got.Int != -60 {
		t.Errorf("= %d, want the device's default of -60", got.Int)
	}
}

// TestGetSlotInfo covers a port's identity, which is the card in it.
func TestGetSlotInfo(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })

	info, err := h.plugin.GetSlotInfo(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetSlotInfo: %v", err)
	}
	if info.Slot != 1 {
		t.Errorf("slot = %d", info.Slot)
	}
	if info.State != consumer.SlotStatePresent || !info.IsOnline {
		t.Errorf("= %v online=%v, want a present card", info.State, info.IsOnline)
	}

	// The type id names the product, which is what makes an inventory
	// legible without an operator knowing the numbers.
	if info.Identity["type"] != "5915 Embedded Audio" {
		t.Errorf("type = %q, want the product name for id 623", info.Identity["type"])
	}
	if info.Identity["name"] != "5915 Card" {
		t.Errorf("name = %q", info.Identity["name"])
	}
	// The version and the id together are what identify a device model, so
	// both are reported rather than only the name.
	if info.Identity["version"] != "2.1a.cs3" {
		t.Errorf("version = %q", info.Identity["version"])
	}
	if info.Identity["category"] == "" {
		t.Error("the product category should be reported")
	}
}

// TestGetSlotInfo_EmptySlot covers a port with nothing fitted, which a unit
// reports rather than refuses.
func TestGetSlotInfo_EmptySlot(t *testing.T) {
	h := newHarness(t, func(d *device) { d.emptySlots[1] = true })

	info, err := h.plugin.GetSlotInfo(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetSlotInfo: %v", err)
	}
	if info.State != consumer.SlotStateNoCard {
		t.Errorf("state = %v, want no_card", info.State)
	}
	if info.Status != consumer.SlotNoCard {
		t.Errorf("status = %v, want no_card", info.Status)
	}
	if info.IsOnline {
		t.Error("an empty slot is not online")
	}
}

func TestGetSlotInfo_OutOfRange(t *testing.T) {
	h := newHarness(t, nil)

	if _, err := h.plugin.GetSlotInfo(context.Background(), 999); err == nil {
		t.Error("a slot outside the port range should be refused")
	}
	if _, err := h.plugin.Walk(context.Background(), -1); err == nil {
		t.Error("a negative slot should be refused")
	}
}

// TestNotConnected covers every verb before Connect, because a caller that
// forgot to connect should be told so rather than see a nil dereference.
func TestNotConnected(t *testing.T) {
	p := New(testDeps())
	ctx := context.Background()

	if _, err := p.GetDeviceInfo(ctx); !errors.Is(err, consumer.ErrNotConnected) {
		t.Errorf("GetDeviceInfo err = %v, want ErrNotConnected", err)
	}
	if _, err := p.GetSlotInfo(ctx, 0); !errors.Is(err, consumer.ErrNotConnected) {
		t.Errorf("GetSlotInfo err = %v, want ErrNotConnected", err)
	}
	if _, err := p.Walk(ctx, 0); !errors.Is(err, consumer.ErrNotConnected) {
		t.Errorf("Walk err = %v, want ErrNotConnected", err)
	}
	if _, err := p.GetValue(ctx, consumer.ValueRequest{Label: "x"}); !errors.Is(err, consumer.ErrNotConnected) {
		t.Errorf("GetValue err = %v, want ErrNotConnected", err)
	}
	if _, err := p.SetValue(ctx, consumer.ValueRequest{Label: "x"}, consumer.Value{}); !errors.Is(err, consumer.ErrNotConnected) {
		t.Errorf("SetValue err = %v, want ErrNotConnected", err)
	}
	if err := p.Subscribe(consumer.ValueRequest{}, func(consumer.Event) {}); !errors.Is(err, consumer.ErrNotConnected) {
		t.Errorf("Subscribe err = %v, want ErrNotConnected", err)
	}
	// Disconnecting something that was never connected is not an error.
	if err := p.Disconnect(); err != nil {
		t.Errorf("Disconnect on a fresh plugin: %v", err)
	}
}

// TestDisconnect_TerminatesSessions covers the rule that keeps units usable.
// Servers do not time idle sessions out, so one we abandon is held until the
// unit reboots, and a unit that runs out stops answering anybody.
func TestDisconnect_TerminatesSessions(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })

	if _, err := h.plugin.Walk(context.Background(), 1); err != nil {
		t.Fatalf("Walk: %v", err)
	}

	h.device.mu.Lock()
	before := len(h.device.sessions)
	h.device.mu.Unlock()
	if before == 0 {
		t.Fatal("no sessions were opened")
	}

	if err := h.plugin.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		h.device.mu.Lock()
		left := len(h.device.sessions)
		h.device.mu.Unlock()
		if left == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d sessions were left open on the device", left)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestReconnect covers Connect being called twice, which the neutral contract
// requires. The old link and its sessions go first.
func TestReconnect(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })
	ctx := context.Background()

	if _, err := h.plugin.Walk(ctx, 1); err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if err := h.plugin.Connect(ctx, "10.6.250.105", DefaultPort); err != nil {
		t.Fatalf("reconnect: %v", err)
	}

	// The new connection works, and the cached menu from the old one is gone
	// rather than being reported for a device that may have changed.
	if _, err := h.plugin.GetDeviceInfo(ctx); err != nil {
		t.Fatalf("GetDeviceInfo after reconnect: %v", err)
	}
}

func hasEvent(p *Plugin, name string) bool {
	for _, e := range p.ComplianceEvents() {
		if e.Name == name {
			return true
		}
	}
	return false
}
