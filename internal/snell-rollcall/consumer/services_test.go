package rollcall

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/snell-rollcall/codec"
)

// TestReadFile covers the service two things depend on: a router's names come
// from a file, and the vendor's Control Panel will not render a device at all
// without reading TEMPLATE.ZIP from it.
func TestReadFile(t *testing.T) {
	body := bytes.Repeat([]byte("RollCall template archive. "), 200)

	h := newHarness(t, func(d *device) { d.setFile("TEMPLATE.ZIP", body) })

	got, err := h.plugin.ReadFile(context.Background(), 0, "TEMPLATE.ZIP")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("read %d bytes, want %d, and they differ", len(got), len(body))
	}
	// The file is longer than one frame, so this also proves the block loop.
	if len(body) <= codec.MaxPayload {
		t.Fatal("the test file must be longer than a single frame")
	}
}

// TestReadFile_HonoursTheDeviceBlockSize covers the figure an open reply
// carries.
//
// Both numeric fields change meaning between the request and the reply: on the
// way out the offset says where to read and the extra says how many bytes; on
// the way back the offset is how many were read and the extra is the error.
// The block size arrives in the open reply's offset, and honouring it is what
// keeps a read within what the device is willing to hand over.
func TestReadFile_HonoursTheDeviceBlockSize(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 500)

	h := newHarness(t, func(d *device) {
		d.blockSize = 64 // the device wants small blocks
		d.setFile("NAMES.DAT", body)
	})

	got, err := h.plugin.ReadFile(context.Background(), 0, "NAMES.DAT")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("read %d bytes, want %d", len(got), len(body))
	}
}

func TestReadFile_Missing(t *testing.T) {
	h := newHarness(t, nil)

	_, err := h.plugin.ReadFile(context.Background(), 0, "NOSUCH.BIN")
	if err == nil {
		t.Fatal("reading a file that is not there should fail")
	}
	if !strings.Contains(err.Error(), "no such file") {
		t.Errorf("err = %v, want the device's own reason", err)
	}
}

// TestReadFileTo covers the streaming form, for a caller that would rather not
// hold a whole archive in memory.
func TestReadFileTo(t *testing.T) {
	body := bytes.Repeat([]byte("ab"), 1000)
	h := newHarness(t, func(d *device) { d.setFile("BIG.BIN", body) })

	var buf bytes.Buffer
	n, err := h.plugin.ReadFileTo(context.Background(), 0, "BIG.BIN", &buf)
	if err != nil {
		t.Fatalf("ReadFileTo: %v", err)
	}
	if n != int64(len(body)) {
		t.Errorf("wrote %d bytes, want %d", n, len(body))
	}
	if !bytes.Equal(buf.Bytes(), body) {
		t.Error("the streamed bytes differ from the file")
	}
}

// TestSubscribe_DeliversPushes covers the back channel end to end, and the two
// messages it takes to open. Opening the channel is not enough on its own:
// value pushes additionally need change reporting, and with only the first a
// subscription looks live and nothing arrives.
func TestSubscribe_DeliversPushes(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })

	events := make(chan consumer.Event, 8)
	err := h.plugin.Subscribe(consumer.ValueRequest{Slot: 1},
		func(e consumer.Event) { events <- e })
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	value, err := codec.Value{
		Command: 0x0113,
		Mode:    codec.ModeValue,
		Val:     -24,
	}.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	h.device.push(1, codec.MsgRetValue, value)

	select {
	case ev := <-events:
		if ev.Slot != 1 {
			t.Errorf("slot = %d", ev.Slot)
		}
		if ev.ID != 0x0113 {
			t.Errorf("id = %d, want the command number", ev.ID)
		}
		if ev.Label != "Gain" {
			t.Errorf("label = %q, want the menu's own label", ev.Label)
		}
		if ev.Value.Int != -24 {
			t.Errorf("value = %d, want -24", ev.Value.Int)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the push was never delivered")
	}
}

// TestSubscribe_AcknowledgesAfterDelivery pins the flow control. The
// acknowledgement is what asks the device for the next push, so sending it
// before the listener has taken this one throws that away.
func TestSubscribe_AcknowledgesAfterDelivery(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })

	events := make(chan consumer.Event, 8)
	if err := h.plugin.Subscribe(consumer.ValueRequest{Slot: 1},
		func(e consumer.Event) { events <- e }); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	for i := range 3 {
		value, err := codec.Value{
			Command: 0x0113, Mode: codec.ModeValue, Val: int32(i),
		}.AppendTo(nil)
		if err != nil {
			t.Fatalf("AppendTo: %v", err)
		}
		h.device.push(1, codec.MsgRetValue, value)
	}

	// All three arrive, which can only happen if each was acknowledged: the
	// session layer holds the next push until the previous one is answered.
	for i := range 3 {
		select {
		case ev := <-events:
			if ev.Value.Int != int64(i) {
				t.Errorf("push %d carried %d", i, ev.Value.Int)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of 3 pushes arrived; acknowledgements are not flowing", i)
		}
	}
}

// TestSubscribe_ByLabel covers a subscription to one object rather than a
// whole slot.
func TestSubscribe_ByLabel(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })

	events := make(chan consumer.Event, 8)
	if err := h.plugin.Subscribe(consumer.ValueRequest{Slot: 1, Label: "Gain"},
		func(e consumer.Event) { events <- e }); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// A push for the subscribed command arrives.
	wanted, _ := codec.Value{Command: 0x0113, Mode: codec.ModeValue, Val: 1}.AppendTo(nil)
	h.device.push(1, codec.MsgRetValue, wanted)

	select {
	case ev := <-events:
		if ev.ID != 0x0113 {
			t.Errorf("id = %d", ev.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the subscribed command was not delivered")
	}

	// One for a different command does not.
	other, _ := codec.Value{Command: 0x0114, Mode: codec.ModeValue, Val: 1}.AppendTo(nil)
	h.device.push(1, codec.MsgRetValue, other)

	select {
	case ev := <-events:
		t.Errorf("a push for command %d reached a listener for another", ev.ID)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestUnsubscribe(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })

	events := make(chan consumer.Event, 8)
	req := consumer.ValueRequest{Slot: 1, Label: "Gain"}
	if err := h.plugin.Subscribe(req, func(e consumer.Event) { events <- e }); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := h.plugin.Unsubscribe(req); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}

	value, _ := codec.Value{Command: 0x0113, Mode: codec.ModeValue, Val: 5}.AppendTo(nil)
	h.device.push(1, codec.MsgRetValue, value)

	select {
	case ev := <-events:
		t.Errorf("a push arrived after unsubscribing: %+v", ev)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestSubscribe_NeedsACallback(t *testing.T) {
	h := newHarness(t, nil)

	if err := h.plugin.Subscribe(consumer.ValueRequest{Slot: 0}, nil); err == nil {
		t.Error("subscribing with no callback should be refused")
	}
}

// TestSubscribe_MenuChangeInvalidatesTheWalk covers a device reconfiguring
// itself. A cached menu is then wrong, so it is dropped rather than used to
// report an access the card no longer has.
func TestSubscribe_MenuChangeInvalidatesTheWalk(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })
	ctx := context.Background()

	if _, err := h.plugin.Walk(ctx, 1); err != nil {
		t.Fatalf("Walk: %v", err)
	}

	events := make(chan consumer.Event, 4)
	if err := h.plugin.Subscribe(consumer.ValueRequest{Slot: 1},
		func(e consumer.Event) { events <- e }); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	h.plugin.mu.RLock()
	cached := h.plugin.trees[1] != nil
	h.plugin.mu.RUnlock()
	if !cached {
		t.Fatal("the walk was not cached")
	}

	h.device.push(1, codec.MsgFuncListChg, nil)

	deadline := time.Now().Add(2 * time.Second)
	for {
		h.plugin.mu.RLock()
		still := h.plugin.trees[1] != nil
		h.plugin.mu.RUnlock()
		if !still {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a menu change did not invalidate the cached walk")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestDisplay covers a unit's status lines, which are a separate service from
// the menu: numbered rather than named, with two negative numbers reserved for
// an error and a warning.
func TestDisplay(t *testing.T) {
	h := newHarness(t, nil)

	lines, err := h.plugin.Display(context.Background(), 1)
	if err != nil {
		t.Fatalf("Display: %v", err)
	}
	if len(lines) != 2 {
		t.Errorf("got %d lines, want the device's 2", len(lines))
	}
	if lines[0] != "line 0" {
		t.Errorf("line 0 = %q", lines[0])
	}
}

// TestDevices covers discovery: a gateway keeps a map of what it has heard
// announce itself, and walking that map is how a fleet is found.
func TestDevices(t *testing.T) {
	h := newHarness(t, nil)

	devices, err := h.plugin.Devices(context.Background())
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}
	if len(devices) == 0 {
		t.Fatal("no devices found")
	}
	if devices[0].ID.Name != "Centra" {
		t.Errorf("first device = %q", devices[0].ID.Name)
	}
	if got := codec.UnitTypeName(devices[0].ID.TypeID); got != "Router Matrix" {
		t.Errorf("type = %q, want the product name", got)
	}
}

func TestPorts(t *testing.T) {
	h := newHarness(t, func(d *device) { d.ports = 4 })

	ports, err := h.plugin.Ports(context.Background(), gatewayAddr.Unit)
	if err != nil {
		t.Fatalf("Ports: %v", err)
	}
	if len(ports) != 4 {
		t.Errorf("got %d ports, want 4", len(ports))
	}
}

// TestBothGenerations runs the same reads and writes over each wire
// generation, because a connector that serves only the one it was tested on
// is half a connector.
func TestBothGenerations(t *testing.T) {
	tests := []struct {
		name        string
		longStrings bool
	}{
		{"32-bit", true},
		{"16-bit", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, func(d *device) {
				if !tc.longStrings {
					d.services &^= codec.SvcLongStr
				}
				d.setMenu(1, testMenu())
			})
			ctx := context.Background()

			long, err := h.plugin.Uses32Bit(ctx, 1)
			if err != nil {
				t.Fatalf("Uses32Bit: %v", err)
			}
			if long != tc.longStrings {
				t.Fatalf("session is 32-bit = %v, want %v", long, tc.longStrings)
			}

			// Walk.
			objects, err := h.plugin.Walk(ctx, 1)
			if err != nil {
				t.Fatalf("Walk: %v", err)
			}
			if len(objects) != len(testMenu()) {
				t.Errorf("walked %d objects, want %d", len(objects), len(testMenu()))
			}

			// Read.
			v, err := h.plugin.GetValue(ctx, consumer.ValueRequest{Slot: 1, Label: "Gain"})
			if err != nil {
				t.Fatalf("GetValue: %v", err)
			}
			if v.Kind != consumer.KindInt {
				t.Errorf("kind = %s, want int", v.Kind)
			}

			// Write, and read back what the device stored.
			got, err := h.plugin.SetValue(ctx,
				consumer.ValueRequest{Slot: 1, Label: "Gain"},
				consumer.Value{Kind: consumer.KindInt, Int: -30})
			if err != nil {
				t.Fatalf("SetValue: %v", err)
			}
			if got.Int != -30 {
				t.Errorf("= %d, want -30", got.Int)
			}
		})
	}
}

// TestValueWithBothNumberAndString covers the combination that is normal
// rather than exceptional. A device answers a checksum with the number
// -1686180113 and the string "0x9B7EEEEF", and a caller wants both: the number
// to compare and the string to show.
func TestValueWithBothNumberAndString(t *testing.T) {
	h := newHarness(t, func(d *device) {
		d.setMenu(1, testMenu())
		d.setValue(1, codec.Value{
			Command: 0x0113,
			Mode:    codec.ModeValue | codec.ModeString,
			Val:     -1686180113,
			Text:    "0x9B7EEEEF",
		})
	})

	v, err := h.plugin.GetValue(context.Background(), consumer.ValueRequest{Slot: 1, Label: "Gain"})
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if v.Int != -1686180113 {
		t.Errorf("number = %d", v.Int)
	}
	if v.Str != "0x9B7EEEEF" {
		t.Errorf("string = %q, want the device's own rendering", v.Str)
	}
}

// TestValueWithNoMode covers a reply that sets no flag and so carries nothing.
// The object exists and the device simply said nothing about it, so an empty
// value of the right kind is returned rather than an error, and the oddity is
// recorded.
func TestValueWithNoMode(t *testing.T) {
	h := newHarness(t, func(d *device) {
		d.setMenu(1, testMenu())
		d.setValue(1, codec.Value{Command: 0x0113})
	})

	v, err := h.plugin.GetValue(context.Background(), consumer.ValueRequest{Slot: 1, Label: "Gain"})
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if v.Kind != consumer.KindInt {
		t.Errorf("kind = %s, want the object's own kind", v.Kind)
	}
	if !hasEvent(h.plugin, EventValueModeEmpty) {
		t.Error("a reply carrying nothing was not recorded")
	}
}

// TestComplianceEventsAreCounted covers the catalogue's shape. A device that
// misbehaves on every one of sixty-five thousand destinations must not fill
// memory with the evidence, so repeats are counted rather than accumulated.
func TestComplianceEventsAreCounted(t *testing.T) {
	h := newHarness(t, nil)

	for range 10 {
		h.plugin.fire(EventValueModeEmpty, "detail")
	}

	events := h.plugin.ComplianceEvents()
	if len(events) != 1 {
		t.Fatalf("%d entries, want one per kind", len(events))
	}
	if events[0].Count != 10 {
		t.Errorf("count = %d, want 10", events[0].Count)
	}
	if events[0].Name != EventValueModeEmpty {
		t.Errorf("name = %q", events[0].Name)
	}
	if events[0].At.IsZero() {
		t.Error("the event carries no time")
	}
}

func TestUnitFromFormat(t *testing.T) {
	tests := []struct {
		format string
		want   string
	}{
		{"%0.1f dB", "dB"},
		{"%d", ""},
		{"%d %%", "%%"},
		{"%s", ""},
		{"", ""},
		{"no conversion here", ""},
		{"%d frames", "frames"},
	}
	for _, tc := range tests {
		if got := unitFromFormat(tc.format); got != tc.want {
			t.Errorf("unitFromFormat(%q) = %q, want %q", tc.format, got, tc.want)
		}
	}
}
