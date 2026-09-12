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

// A device that says no is not a broken device. Refusing a command is how the
// protocol says "not at your user level", "not on this card" or "not while I
// am busy", and every one of those has to reach the caller as an error rather
// than as a zero value.

// TestDeviceRefusals covers a device answering each request with a refusal.
func TestDeviceRefusals(t *testing.T) {
	tests := []struct {
		name string
		typ  codec.PacketType
		// only restricts a case to one generation, because the two use
		// different message types and injecting a fault into one a session
		// never sends proves nothing. Empty means both.
		only string
		call func(*Plugin) error
	}{
		// An inventory is answered from the enumeration and asks no node
		// anything, so these two reach a device through the calls that still
		// read a node directly: an identity probe, and a slot the enumeration
		// never named.
		{"identity", codec.MsgGetID, "", func(p *Plugin) error {
			_, err := p.IdentityProbe(context.Background(), 1)
			return err
		}},
		{"status", codec.MsgGetStat, "", func(p *Plugin) error {
			_, err := p.GetSlotInfo(context.Background(), 5)
			return err
		}},
		{"menu count", codec.MsgGetMenuCount, "32", func(p *Plugin) error {
			_, err := p.Walk(context.Background(), 1)
			return err
		}},
		{"menu item", codec.MsgGetMenuItem, "32", func(p *Plugin) error {
			_, err := p.Walk(context.Background(), 1)
			return err
		}},
		{"menu block, 16-bit", codec.MsgGetFunc, "16", func(p *Plugin) error {
			_, err := p.Walk(context.Background(), 1)
			return err
		}},
		{"value, 16-bit", codec.MsgGetFStat, "16", func(p *Plugin) error {
			_, err := p.GetValue(context.Background(), consumer.ValueRequest{Slot: 1, ID: 0x0113})
			return err
		}},
		{"write, 16-bit", codec.MsgSetParam, "16", func(p *Plugin) error {
			_, err := p.SetValue(context.Background(),
				consumer.ValueRequest{Slot: 1, ID: 0x0113},
				consumer.Value{Kind: consumer.KindInt, Int: 1})
			return err
		}},
		{"default, 16-bit", codec.MsgSetParam, "16", func(p *Plugin) error {
			_, err := p.SetDefault(context.Background(), consumer.ValueRequest{Slot: 1, ID: 0x0113})
			return err
		}},
		{"default", codec.MsgSetValue, "32", func(p *Plugin) error {
			_, err := p.SetDefault(context.Background(), consumer.ValueRequest{Slot: 1, ID: 0x0113})
			return err
		}},
		// Enumeration has two ways to get its answer and falls back from one
		// to the other, so a refusal reaches the caller here rather than
		// through GetDeviceInfo. TestEnumeration* covers the choice itself.
		{"port list", codec.MsgGetDevList, "", func(p *Plugin) error {
			_, err := p.Ports(context.Background(), gatewayAddr.Unit)
			return err
		}},
		{"device map", codec.MsgGetLocDevMap, "", func(p *Plugin) error {
			_, err := p.Devices(context.Background())
			return err
		}},
		{"display", codec.MsgGetDispData, "", func(p *Plugin) error {
			_, err := p.Display(context.Background(), 1)
			return err
		}},
		{"file open", codec.MsgFileOpen, "", func(p *Plugin) error {
			_, err := p.ReadFile(context.Background(), 0, "A.BIN")
			return err
		}},
		{"directory", codec.MsgFileDir, "", func(p *Plugin) error {
			_, err := p.ListDir(context.Background(), 0, "/")
			return err
		}},
	}
	// Both generations, because the 16-bit and 32-bit paths are separate code
	// and a failure handled in one says nothing about the other.
	for _, gen := range generations() {
		t.Run(gen.name, func(t *testing.T) {
			for _, tc := range tests {
				if tc.only != "" && tc.only != gen.name[:2] {
					continue
				}
				t.Run(tc.name, func(t *testing.T) {
					h := newHarness(t, func(d *device) {
						gen.setup(d)
						d.setMenu(1, testMenu())
						d.setFile("A.BIN", []byte("x"))
						d.refuse[tc.typ] = true
					})
					if err := tc.call(h.plugin); err == nil {
						t.Errorf("a refused %s should reach the caller as an error", tc.typ)
					}
				})
			}
		})
	}
}

// generations runs a test over both wire generations. A connector that serves
// only the one it was tested on is half a connector, and the read, write and
// menu paths are separate code in each.
func generations() []struct {
	name  string
	long  bool
	setup func(*device)
} {
	return []struct {
		name  string
		long  bool
		setup func(*device)
	}{
		{"32-bit", true, func(*device) {}},
		{"16-bit", false, func(d *device) { d.services &^= codec.SvcLongStr }},
	}
}

// TestDeviceGarbledReplies covers a device answering with the right message
// type and a payload too short to decode, which is what a firmware fault looks
// like from outside. It must be an error rather than a zero value: a gain
// silently read as zero is worse than a read that failed.
func TestDeviceGarbledReplies(t *testing.T) {
	tests := []struct {
		name string
		typ  codec.PacketType
		// only restricts a case to one generation, because the two use
		// different message types and injecting a fault into one a session
		// never sends proves nothing. Empty means both.
		only string
		call func(*Plugin) error
	}{
		// An inventory is answered from the enumeration and asks no node
		// anything, so these two reach a device through the calls that still
		// read a node directly: an identity probe, and a slot the enumeration
		// never named.
		{"identity", codec.MsgGetID, "", func(p *Plugin) error {
			_, err := p.IdentityProbe(context.Background(), 1)
			return err
		}},
		{"status", codec.MsgGetStat, "", func(p *Plugin) error {
			_, err := p.GetSlotInfo(context.Background(), 5)
			return err
		}},
		{"menu count", codec.MsgGetMenuCount, "32", func(p *Plugin) error {
			_, err := p.Walk(context.Background(), 1)
			return err
		}},
		{"menu item", codec.MsgGetMenuItem, "32", func(p *Plugin) error {
			_, err := p.Walk(context.Background(), 1)
			return err
		}},
		{"value", codec.MsgGetValue, "32", func(p *Plugin) error {
			_, err := p.GetValue(context.Background(), consumer.ValueRequest{Slot: 1, ID: 0x0113})
			return err
		}},
		{"value, 16-bit", codec.MsgGetFStat, "16", func(p *Plugin) error {
			_, err := p.GetValue(context.Background(), consumer.ValueRequest{Slot: 1, ID: 0x0113})
			return err
		}},
		{"write reply", codec.MsgSetValue, "32", func(p *Plugin) error {
			_, err := p.SetValue(context.Background(),
				consumer.ValueRequest{Slot: 1, ID: 0x0113},
				consumer.Value{Kind: consumer.KindInt, Int: 1})
			return err
		}},
		{"write reply, 16-bit", codec.MsgSetParam, "16", func(p *Plugin) error {
			_, err := p.SetValue(context.Background(),
				consumer.ValueRequest{Slot: 1, ID: 0x0113},
				consumer.Value{Kind: consumer.KindInt, Int: 1})
			return err
		}},
		{"menu block, 16-bit", codec.MsgGetFunc, "16", func(p *Plugin) error {
			_, err := p.Walk(context.Background(), 1)
			return err
		}},
		{"menu line, 16-bit", codec.MsgGetNextPkt, "16", func(p *Plugin) error {
			_, err := p.Walk(context.Background(), 1)
			return err
		}},
		{"port list", codec.MsgGetDevList, "", func(p *Plugin) error {
			_, err := p.Ports(context.Background(), gatewayAddr.Unit)
			return err
		}},
		{"display", codec.MsgGetDispData, "", func(p *Plugin) error {
			_, err := p.Display(context.Background(), 1)
			return err
		}},
		{"file open", codec.MsgFileOpen, "", func(p *Plugin) error {
			_, err := p.ReadFile(context.Background(), 0, "A.BIN")
			return err
		}},
		{"file read", codec.MsgFileRead, "", func(p *Plugin) error {
			_, err := p.ReadFile(context.Background(), 0, "A.BIN")
			return err
		}},
		{"directory block", codec.MsgFileDir, "", func(p *Plugin) error {
			_, err := p.ListDir(context.Background(), 0, "/")
			return err
		}},
	}
	for _, gen := range generations() {
		t.Run(gen.name, func(t *testing.T) {
			for _, tc := range tests {
				if tc.only != "" && tc.only != gen.name[:2] {
					continue
				}
				t.Run(tc.name, func(t *testing.T) {
					h := newHarness(t, func(d *device) {
						gen.setup(d)
						d.setMenu(1, testMenu())
						d.setFile("A.BIN", []byte("x"))
						d.garble[tc.typ] = true
					})
					if err := tc.call(h.plugin); err == nil {
						t.Errorf("a garbled %s reply should be an error, not a zero value", tc.typ)
					}
				})
			}
		})
	}

	// A garbled entry inside a directory listing needs its own switch: every
	// item of a multi-packet transfer answers the same request type, so the
	// type alone cannot say which transfer to spoil.
	t.Run("directory entry", func(t *testing.T) {
		h := newHarness(t, func(d *device) {
			d.setFile("A.BIN", []byte("x"))
			d.garbleDirEntry = true
		})
		if _, err := h.plugin.ListDir(context.Background(), 0, "/"); err == nil {
			t.Error("a garbled directory entry should be an error")
		}
	})
}

// TestSessionRefused covers a device that will not open a session at all,
// which is what a unit with none left does.
func TestSessionRefused(t *testing.T) {
	h := newHarness(t, func(d *device) { d.refuse[codec.MsgCall] = true })
	ctx := context.Background()

	if _, err := h.plugin.Walk(ctx, 1); err == nil {
		t.Error("a refused session should fail the walk")
	}
	if _, err := h.plugin.GetSlotInfo(ctx, 1); err == nil {
		t.Error("a refused session should fail the slot query")
	}
	if _, err := h.plugin.Uses32Bit(ctx, 1); err == nil {
		t.Error("a refused session should fail the generation query")
	}
	if _, err := h.plugin.ReadFile(ctx, 0, "A.BIN"); err == nil {
		t.Error("a refused session should fail the file read")
	}
	if err := h.plugin.Subscribe(consumer.ValueRequest{Slot: 1}, func(consumer.Event) {}); err == nil {
		t.Error("a refused session should fail the subscription")
	}
	if _, err := h.plugin.ListDir(ctx, 0, "/"); err == nil {
		t.Error("a refused session should fail the directory listing")
	}
}

// TestListDir covers a directory listing, which is how a client finds what a
// device has before asking for it.
func TestListDir(t *testing.T) {
	h := newHarness(t, func(d *device) {
		d.setFile("TEMPLATE.ZIP", make([]byte, 4096))
		d.setFile("NAMES.DAT", make([]byte, 128))
	})

	entries, err := h.plugin.ListDir(context.Background(), 0, "/")
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("listed %d entries, want 2", len(entries))
	}
	byName := map[string]codec.DirEntry{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	if got := byName["TEMPLATE.ZIP"].Info.Length; got != 4096 {
		t.Errorf("TEMPLATE.ZIP is %d bytes, want 4096", got)
	}
	if _, ok := byName["NAMES.DAT"]; !ok {
		t.Error("NAMES.DAT is missing from the listing")
	}
}

// TestReadFile_SessionIsReused covers the file session being opened once. A
// unit has few sessions and running out is what stops it answering anybody, so
// a second read must not cost another.
func TestReadFile_SessionIsReused(t *testing.T) {
	h := newHarness(t, func(d *device) {
		d.setFile("A.BIN", []byte("first"))
		d.setFile("B.BIN", []byte("second"))
	})
	ctx := context.Background()

	if _, err := h.plugin.ReadFile(ctx, 0, "A.BIN"); err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	h.device.mu.Lock()
	after1 := len(h.device.sessions)
	h.device.mu.Unlock()

	if _, err := h.plugin.ReadFile(ctx, 0, "B.BIN"); err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	h.device.mu.Lock()
	after2 := len(h.device.sessions)
	h.device.mu.Unlock()

	if after2 != after1 {
		t.Errorf("a second read opened another session: %d then %d", after1, after2)
	}
}

// TestOverlongPath covers a caller naming a path longer than the service
// accepts. It fails locally rather than sending something a device will reject.
func TestOverlongPath(t *testing.T) {
	h := newHarness(t, nil)
	long := strings.Repeat("x", codec.MaxFileName+10)
	ctx := context.Background()

	if _, err := h.plugin.ReadFile(ctx, 0, long); !errors.Is(err, codec.ErrStringTooLong) {
		t.Errorf("read err = %v, want ErrStringTooLong", err)
	}
	if _, err := h.plugin.ListDir(ctx, 0, long); !errors.Is(err, codec.ErrStringTooLong) {
		t.Errorf("list err = %v, want ErrStringTooLong", err)
	}
	var sink discardWriter
	if _, err := h.plugin.ReadFileTo(ctx, 0, long, sink); !errors.Is(err, codec.ErrStringTooLong) {
		t.Errorf("stream err = %v, want ErrStringTooLong", err)
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// TestCommandTooWideForTheSession covers a router command on a 16-bit session.
// Its number cannot be expressed at all in that generation, so the read is
// refused rather than truncated into a different command.
func TestCommandTooWideForTheSession(t *testing.T) {
	h := newHarness(t, func(d *device) {
		d.services &^= codec.SvcLongStr
		d.setMenu(1, testMenu())
	})
	ctx := context.Background()

	req := consumer.ValueRequest{Slot: 1, ID: 0x0010_4EEF}

	_, err := h.plugin.GetValue(ctx, req)
	if err == nil {
		t.Fatal("a 32-bit command must not be readable on a 16-bit session")
	}
	if !strings.Contains(err.Error(), "32-bit") {
		t.Errorf("err = %v, want it to name the generation", err)
	}

	_, err = h.plugin.SetValue(ctx, req, consumer.Value{Kind: consumer.KindInt, Int: 1})
	if err == nil {
		t.Fatal("a 32-bit command must not be writable on a 16-bit session")
	}
}

// TestEncodeValueKinds covers turning a neutral value into a write.
func TestEncodeValueKinds(t *testing.T) {
	p := New(testDeps())
	line := &menuLine{Command: 1, Style: codec.StyleNumber, DivScale: 10}

	tests := []struct {
		name string
		in   consumer.Value
		mode codec.Mode
		num  int32
		text string
		fail bool
	}{
		{"bool true", consumer.Value{Kind: consumer.KindBool, Bool: true}, codec.ModeValue, 1, "", false},
		{"bool false", consumer.Value{Kind: consumer.KindBool}, codec.ModeValue, 0, "", false},
		{"int", consumer.Value{Kind: consumer.KindInt, Int: -42}, codec.ModeValue, -42, "", false},
		{"uint", consumer.Value{Kind: consumer.KindUint, Uint: 42}, codec.ModeValue, 42, "", false},
		// A float is in the displayed units, so it is scaled back into wire
		// units by the line's own divisor.
		{"float", consumer.Value{Kind: consumer.KindFloat, Float: -6.0}, codec.ModeValue, -60, "", false},
		{"string", consumer.Value{Kind: consumer.KindString, Str: "hi"}, codec.ModeString, 0, "hi", false},
		{"bare number", consumer.Value{Int: 7}, codec.ModeValue, 7, "", false},
		{"bare string", consumer.Value{Str: "hi"}, codec.ModeString, 0, "hi", false},
		{"raw", consumer.Value{Kind: consumer.KindRaw, Raw: []byte{1}}, 0, 0, "", true},
		{"frame", consumer.Value{Kind: consumer.KindFrame}, 0, 0, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mode, num, text, err := p.encodeValue(line, tc.in)
			if tc.fail {
				if err == nil {
					t.Errorf("writing a %s should be refused", tc.in.Kind)
				}
				return
			}
			if err != nil {
				t.Fatalf("encodeValue: %v", err)
			}
			if mode != tc.mode || num != tc.num || text != tc.text {
				t.Errorf("= %s %d %q, want %s %d %q", mode, num, text, tc.mode, tc.num, tc.text)
			}
		})
	}
}

// TestValueFromRawData covers a command whose value is opaque bytes, which is
// what the router command set carries its parameters in.
func TestValueFromRawData(t *testing.T) {
	p := New(testDeps())
	line := &menuLine{Command: 1, Style: codec.StyleData}

	v := p.valueFrom(line, codec.ModeData, 3, "", []byte{1, 2, 3})
	if v.Kind != consumer.KindRaw {
		t.Errorf("kind = %s, want raw", v.Kind)
	}
	if len(v.Raw) != 3 {
		t.Errorf("raw = %x, want three bytes", v.Raw)
	}

	// The bytes are copied rather than aliased, because the frame they came
	// from is reused by the next read.
	v.Raw[0] = 9
	again := p.valueFrom(line, codec.ModeData, 3, "", []byte{1, 2, 3})
	if again.Raw[0] != 1 {
		t.Error("the raw value aliases the frame it was decoded from")
	}
}

// TestValueFromStringOnly covers a reply that carries only a string on a line
// the menu called numeric, which happens on a device whose menu and values
// disagree.
func TestValueFromStringOnly(t *testing.T) {
	p := New(testDeps())
	line := &menuLine{Command: 1, Style: codec.StyleNumber}

	v := p.valueFrom(line, codec.ModeString, 0, "text", nil)
	if v.Kind != consumer.KindString || v.Str != "text" {
		t.Errorf("= %s %q, want the string the device sent", v.Kind, v.Str)
	}
}

// TestDispatchIgnoresUnknownPushes covers a device pushing something the
// connector has no use for. It must not crash and must not be delivered as a
// value.
func TestDispatchIgnoresUnknownPushes(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })

	events := make(chan consumer.Event, 8)
	if err := h.plugin.Subscribe(consumer.ValueRequest{Slot: 1},
		func(e consumer.Event) { events <- e }); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// A message type that carries no value, and a value push whose payload
	// will not decode. Neither should reach a listener.
	h.device.push(1, codec.MsgTime, nil)
	h.device.push(1, codec.MsgRetValue, []byte{0x01})
	h.device.push(1, codec.MsgRetFStat, []byte{0x01})
	h.device.push(1, codec.MsgDispData, []byte{0x01})

	// A good one afterwards proves the pump survived all four.
	good, err := codec.Value{Command: 0x0113, Mode: codec.ModeValue, Val: 7}.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	h.device.push(1, codec.MsgRetValue, good)

	select {
	case ev := <-events:
		if ev.Value.Int != 7 {
			t.Errorf("first delivered event carried %d, want 7", ev.Value.Int)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the pump stopped on an undecodable push")
	}
}

// TestDisplayPush covers a status line arriving on the back channel, and the
// line numbers the display service does not define.
func TestDisplayPush(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })

	events := make(chan consumer.Event, 8)
	if err := h.plugin.Subscribe(consumer.ValueRequest{Slot: 1},
		func(e consumer.Event) { events <- e }); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// An error line, which is a priority rather than a position.
	errLine, err := codec.Disp{Line: codec.DisplayLineError, Text: "no input"}.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	h.device.push(1, codec.MsgDispData, errLine)

	select {
	case ev := <-events:
		if ev.Label != "error" {
			t.Errorf("label = %q, want the priority named", ev.Label)
		}
		if ev.Value.Str != "no input" {
			t.Errorf("text = %q", ev.Value.Str)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the display push was not delivered")
	}

	// A line outside the four the service defines is kept under its own
	// number and reported, because the device is using the service in a way
	// the specification does not describe.
	odd, err := codec.Disp{Line: 42, Text: "extra"}.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	h.device.push(1, codec.MsgDispData, odd)

	select {
	case ev := <-events:
		if ev.ID != 42 {
			t.Errorf("id = %d, want the line number it arrived under", ev.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("an out-of-range display line was dropped")
	}
	if !hasEvent(h.plugin, EventDisplayLineOutOfRange) {
		t.Error("an out-of-range display line was not reported")
	}
}

// TestStyleChangeInvalidatesTheWalk covers a line becoming hidden or disabled.
// The cached menu is then wrong about what a caller may write, so it is
// dropped rather than used.
func TestStyleChangeInvalidatesTheWalk(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })
	ctx := context.Background()

	if _, err := h.plugin.Walk(ctx, 1); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if err := h.plugin.Subscribe(consumer.ValueRequest{Slot: 1},
		func(consumer.Event) {}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	change := codec.FuncStyle{MenuIndex: 1, Style: codec.StyleNumber | codec.StyleDisabled}
	h.device.push(1, codec.MsgFuncStyleChg, change.AppendTo(nil))

	deadline := time.Now().Add(2 * time.Second)
	for {
		h.plugin.mu.RLock()
		cached := h.plugin.trees[1] != nil
		h.plugin.mu.RUnlock()
		if !cached {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("a style change did not invalidate the cached walk")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestUnsubscribeUnknown covers removing a subscription that was never made,
// and one for an object that cannot be resolved.
func TestUnsubscribeUnknown(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })

	if err := h.plugin.Unsubscribe(consumer.ValueRequest{Slot: 1}); err != nil {
		t.Errorf("unsubscribing something never subscribed: %v", err)
	}
	if err := h.plugin.Unsubscribe(consumer.ValueRequest{Slot: 1, Label: "nothing"}); err == nil {
		t.Error("unsubscribing an unresolvable object should say so")
	}
}

// TestSubscribeWithoutAMenu covers a device with no menu service. Values can
// still be pushed and delivered; they simply arrive without labels.
func TestSubscribeWithoutAMenu(t *testing.T) {
	h := newHarness(t, func(d *device) { d.refuse[codec.MsgGetMenuCount] = true })

	events := make(chan consumer.Event, 4)
	if err := h.plugin.Subscribe(consumer.ValueRequest{Slot: 1},
		func(e consumer.Event) { events <- e }); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	value, err := codec.Value{Command: 99, Mode: codec.ModeValue, Val: 1}.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	h.device.push(1, codec.MsgRetValue, value)

	select {
	case ev := <-events:
		if ev.ID != 99 {
			t.Errorf("id = %d", ev.ID)
		}
		if ev.Label != "" {
			t.Errorf("label = %q, want none without a menu", ev.Label)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a push without a menu was not delivered")
	}

	// A command the menu does not list is reported, because the device knows
	// its own command set better than a cached walk does.
	if !hasEvent(h.plugin, EventUnsolicitedCommand) {
		t.Error("an unlisted command was not reported")
	}
}

// TestDuplicateCommandsAreReported covers two menu lines claiming the same
// command number. Both are kept, because a write to either reaches the same
// place, and the first keeps the label.
func TestDuplicateCommandsAreReported(t *testing.T) {
	menu := []codec.MenuItem{
		{MenuIndex: 0, Style: codec.StyleNumber, Command: 5, Text: "First"},
		{MenuIndex: 1, Style: codec.StyleNumber, Command: 5, Text: "Second"},
	}
	h := newHarness(t, func(d *device) { d.setMenu(1, menu) })

	objects, err := h.plugin.Walk(context.Background(), 1)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(objects) != 2 {
		t.Errorf("walked %d objects, want both kept", len(objects))
	}
	if !hasEvent(h.plugin, EventDuplicateCommand) {
		t.Error("the duplicate command was not reported")
	}
}

// TestMenuSpanOverrunIsClamped covers a container claiming a subtree longer
// than the menu that contains it. The tree is still built, from what is
// actually there, and the claim is reported.
func TestMenuSpanOverrunIsClamped(t *testing.T) {
	menu := []codec.MenuItem{
		{MenuIndex: 0, Style: codec.StyleList, Step: 99, Text: "Root"},
		{MenuIndex: 1, Style: codec.StyleNumber, Command: 1, Text: "Only"},
	}
	h := newHarness(t, func(d *device) { d.setMenu(1, menu) })

	objects, err := h.plugin.Walk(context.Background(), 1)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(objects) != 2 {
		t.Fatalf("walked %d objects, want 2", len(objects))
	}
	if !hasEvent(h.plugin, EventMenuSpanOverruns) {
		t.Error("the overrunning span was not reported")
	}
	// The line that was there is still nested under the container.
	for _, o := range objects {
		if o.Label == "Only" && len(o.Path) != 2 {
			t.Errorf("Only is at %v, want it under Root", o.Path)
		}
	}
}
