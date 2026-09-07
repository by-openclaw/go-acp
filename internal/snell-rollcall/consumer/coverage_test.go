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

// The tests here close the last paths a device can take that the ordinary ones
// do not reach: a link that goes away at an awkward moment, a file service that
// gives up half way, a peer that says one thing in a header and another in the
// payload.

// TestSessionOnAClosedLink covers the check a caller meets when the link went
// away before it asked. It is set directly rather than through Disconnect,
// because Disconnect also removes the link from the plugin and the caller
// would then never reach this path.
func TestSessionOnAClosedLink(t *testing.T) {
	h := newHarness(t, nil)

	h.plugin.mu.RLock()
	l := h.plugin.link
	h.plugin.mu.RUnlock()

	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()

	if _, err := h.plugin.session(context.Background(), 1); err == nil {
		t.Error("opening a session on a closed link should fail")
	}
	if _, err := h.plugin.fileSession(context.Background(), 0); err == nil {
		t.Error("opening a file session on a closed link should fail")
	}
}

// TestLinkClosesWhileASessionIsOpening covers the other side of that check: the
// link is finished with between the call going out and its answer arriving.
//
// The session the peer just granted has to be closed rather than handed to the
// caller, or a unit is left holding one that nothing will ever release.
func TestLinkClosesWhileASessionIsOpening(t *testing.T) {
	tests := []struct {
		name string
		open func(*Plugin) error
	}{
		{"control", func(p *Plugin) error {
			_, err := p.session(context.Background(), 1)
			return err
		}},
		{"file", func(p *Plugin) error {
			_, err := p.fileSession(context.Background(), 0)
			return err
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gate := make(chan struct{})
			h := newHarness(t, nil)

			// Enumerate before the gate exists. Resolving a slot number walks
			// the device's node list, and that walk's own call would otherwise
			// be the one the gate holds.
			if _, err := h.plugin.nodes(context.Background()); err != nil {
				t.Fatalf("enumerate: %v", err)
			}
			h.device.mu.Lock()
			h.device.gateCall = gate
			h.device.callsSeen = 0
			h.device.mu.Unlock()

			done := make(chan error, 1)
			go func() { done <- tc.open(h.plugin) }()

			waitForCalls(t, h, 1)

			// Mark the link finished without closing the socket, so the call
			// still completes and the answer still arrives.
			h.plugin.mu.RLock()
			l := h.plugin.link
			h.plugin.mu.RUnlock()
			l.mu.Lock()
			l.closed = true
			l.mu.Unlock()

			close(gate)

			select {
			case err := <-done:
				if err == nil {
					t.Error("a session granted after the link finished must not be handed out")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("the caller was left waiting")
			}
		})
	}
}

func waitForCalls(t *testing.T, h *harness, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		h.device.mu.Lock()
		seen := h.device.callsSeen
		h.device.mu.Unlock()
		if seen >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d calls arrived", seen, n)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestGetValueRefused covers a device refusing a read outright, which is what
// "not at your user level" looks like on the control service.
func TestGetValueRefused(t *testing.T) {
	for _, gen := range generations() {
		t.Run(gen.name, func(t *testing.T) {
			typ := codec.MsgGetValue
			if !gen.long {
				typ = codec.MsgGetFStat
			}
			h := newHarness(t, func(d *device) {
				gen.setup(d)
				d.setMenu(1, testMenu())
				d.refuse[typ] = true
			})

			_, err := h.plugin.GetValue(context.Background(),
				consumer.ValueRequest{Slot: 1, Label: "Gain"})
			if err == nil {
				t.Error("a refused read should reach the caller")
			}
		})
	}
}

// TestSetValueRefusesRawData covers writing bytes to a command. The protocol
// carries raw data on a write, but which bytes mean what belongs to the
// command set rather than to a generic connector, so it is refused rather than
// guessed at.
func TestSetValueRefusesRawData(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })

	_, err := h.plugin.SetValue(context.Background(),
		consumer.ValueRequest{Slot: 1, Label: "Gain"},
		consumer.Value{Kind: consumer.KindRaw, Raw: []byte{1, 2, 3}})
	if err == nil {
		t.Fatal("writing raw data should be refused rather than sent as a number")
	}
	if !strings.Contains(err.Error(), "raw") {
		t.Errorf("err = %v, want it to say what was refused", err)
	}
}

// TestSetValueStringTooLongForTheWire covers a caller writing a string longer
// than even the long-string generation carries. It fails locally rather than
// being cut without the caller knowing.
func TestSetValueStringTooLongForTheWire(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })

	long := strings.Repeat("x", codec.MaxLongString+10)
	_, err := h.plugin.SetValue(context.Background(),
		consumer.ValueRequest{Slot: 1, Label: "Name"},
		consumer.Value{Kind: consumer.KindString, Str: long})
	if err == nil {
		t.Fatal("a string past the long-string ceiling should be refused")
	}
}

// TestSetValueTruncatesForTheOlderGeneration covers the same string on a
// 16-bit session, where truncation is what the protocol requires: the field is
// twenty bytes and there is no other way to carry a label at all.
func TestSetValueTruncatesForTheOlderGeneration(t *testing.T) {
	h := newHarness(t, func(d *device) {
		d.services &^= codec.SvcLongStr
		d.setMenu(1, testMenu())
	})

	long := strings.Repeat("x", 100)
	got, err := h.plugin.SetValue(context.Background(),
		consumer.ValueRequest{Slot: 1, Label: "Name"},
		consumer.Value{Kind: consumer.KindString, Str: long})
	if err != nil {
		t.Fatalf("a long string must be cut to the field, not refused: %v", err)
	}
	if len(got.Str) > codec.MaxTextSize-1 {
		t.Errorf("the device stored %d bytes, more than the field holds", len(got.Str))
	}
}

// TestSubscribeByLabelWithoutAMenu covers subscribing to a named object on a
// device whose menu cannot be read. There is no way to turn the name into a
// command, so it fails rather than subscribing to nothing.
func TestSubscribeByLabelWithoutAMenu(t *testing.T) {
	h := newHarness(t, func(d *device) { d.refuse[codec.MsgGetMenuCount] = true })
	req := consumer.ValueRequest{Slot: 1, Label: "Gain"}

	if err := h.plugin.Subscribe(req, func(consumer.Event) {}); err == nil {
		t.Error("subscribing by name with no menu should fail")
	}
	if err := h.plugin.Unsubscribe(req); err == nil {
		t.Error("unsubscribing by name with no menu should fail")
	}
}

// TestDeliveryStopsWhenTheSessionCloses covers the push pump ending when its
// session goes, rather than spinning on a closed channel.
func TestDeliveryStopsWhenTheSessionCloses(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })

	if err := h.plugin.Subscribe(consumer.ValueRequest{Slot: 1},
		func(consumer.Event) {}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Close the session out from under the pump. Its push channel closes,
	// which is the pump's signal to stop.
	s, err := h.plugin.session(context.Background(), 1)
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Nothing hangs and nothing panics; a later push simply goes nowhere.
	h.device.push(1, codec.MsgDispData, nil)
	time.Sleep(50 * time.Millisecond)
}

// TestAcknowledgementFailureStopsDelivery covers the link dying between a push
// being taken and its acknowledgement being sent. There is nothing left to
// acknowledge on, so the pump stops rather than looping on a dead socket.
func TestAcknowledgementFailureStopsDelivery(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })

	delivered := make(chan struct{}, 4)
	if err := h.plugin.Subscribe(consumer.ValueRequest{Slot: 1},
		func(consumer.Event) {
			// The device goes away while the callback runs, so the
			// acknowledgement that follows has nowhere to go.
			h.device.close()
			delivered <- struct{}{}
		}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	value, err := codec.Value{Command: 0x0113, Mode: codec.ModeValue, Val: 1}.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	h.device.push(1, codec.MsgRetValue, value)

	select {
	case <-delivered:
	case <-time.After(2 * time.Second):
		t.Fatal("the push was never delivered")
	}
}

// TestPushOnTheOlderGeneration covers a value push in the 16-bit form, which
// is a different message from the 32-bit one and takes a different path.
func TestPushOnTheOlderGeneration(t *testing.T) {
	h := newHarness(t, func(d *device) {
		d.services &^= codec.SvcLongStr
		d.setMenu(1, testMenu())
	})

	events := make(chan consumer.Event, 4)
	if err := h.plugin.Subscribe(consumer.ValueRequest{Slot: 1},
		func(e consumer.Event) { events <- e }); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	payload, err := codec.FuncStatus{
		Command: 0x0113,
		Mode:    codec.ModeValue,
		Value:   -18,
	}.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	h.device.push(1, codec.MsgRetFStat, payload)

	select {
	case ev := <-events:
		if ev.ID != 0x0113 || ev.Value.Int != -18 {
			t.Errorf("= id %d value %d, want 275 and -18", ev.ID, ev.Value.Int)
		}
		if ev.Label != "Gain" {
			t.Errorf("label = %q", ev.Label)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a 16-bit value push was not delivered")
	}
}

// TestDisplayPushWithNoSlotListener covers a status line arriving when only a
// single object is subscribed. A display line belongs to the unit rather than
// to any command, so it goes to a listener for the whole slot or nowhere.
func TestDisplayPushWithNoSlotListener(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })

	events := make(chan consumer.Event, 4)
	if err := h.plugin.Subscribe(consumer.ValueRequest{Slot: 1, Label: "Gain"},
		func(e consumer.Event) { events <- e }); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	line, err := codec.Disp{Line: 0, Text: "OK"}.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	h.device.push(1, codec.MsgDispData, line)

	select {
	case ev := <-events:
		t.Errorf("a display line reached a listener for one command: %+v", ev)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestDeviceMapEntryUndecodable covers a gateway answering its own map with a
// device record too short to read.
func TestDeviceMapEntryUndecodable(t *testing.T) {
	h := newHarness(t, func(d *device) { d.badMapEntry = true })

	if _, err := h.plugin.Devices(context.Background()); err == nil {
		t.Error("an undecodable map entry should be an error")
	}
}

// TestDirectoryListingSkipsUnexpectedItems covers a device answering one entry
// of a listing with something that is not a directory record.
func TestDirectoryListingSkipsUnexpectedItems(t *testing.T) {
	h := newHarness(t, func(d *device) {
		d.setFile("A.BIN", []byte("x"))
		d.setFile("B.BIN", []byte("y"))
		d.oddDirItem = 0
	})

	entries, err := h.plugin.ListDir(context.Background(), 0, "/")
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("listed %d entries, want the listing less the odd one", len(entries))
	}
}

// TestReadFileFailsPartWay covers a device that opens a file, hands over some
// of it and then reports an error. What arrived is discarded: half a template
// archive is worse than none, because it looks like a file.
func TestReadFileFailsPartWay(t *testing.T) {
	h := newHarness(t, func(d *device) {
		d.blockSize = 8
		d.setFile("A.BIN", bytes.Repeat([]byte("x"), 64))
		d.failReadAfter = 2
	})
	ctx := context.Background()

	if _, err := h.plugin.ReadFile(ctx, 0, "A.BIN"); err == nil {
		t.Error("a read that failed part way should be an error")
	}

	h2 := newHarness(t, func(d *device) {
		d.blockSize = 8
		d.setFile("A.BIN", bytes.Repeat([]byte("x"), 64))
		d.failReadAfter = 2
	})
	if _, err := h2.plugin.ReadFileTo(ctx, 0, "A.BIN", &bytes.Buffer{}); err == nil {
		t.Error("the streaming form should fail too")
	}
}

// TestReadHonoursTheReplyCount covers a device whose reply carries more bytes
// than it says it read.
//
// The count is what a client must believe. Taking the whole payload instead
// would append bytes the device did not mean to send, and a template archive
// with extra bytes in the middle of it fails to open with no clue why.
func TestReadHonoursTheReplyCount(t *testing.T) {
	body := bytes.Repeat([]byte("abcdefgh"), 4)
	h := newHarness(t, func(d *device) {
		d.blockSize = 8
		d.shortReadCount = true
		d.setFile("A.BIN", body)
	})

	got, err := h.plugin.ReadFile(context.Background(), 0, "A.BIN")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// The file still arrives whole, and that is the proof. Advancing by the
	// count the device gave means the byte it declined to count is simply
	// asked for again on the next block. Taking the whole payload while
	// advancing by the count would duplicate a byte per block, and the result
	// would be longer than the file and wrong in the middle.
	if !bytes.Equal(got, body) {
		t.Errorf("read %d bytes, want the file's %d, and they differ", len(got), len(body))
	}
}

// TestReadFileStopsRunningAway covers a device that never reaches the end of a
// file. Without a ceiling the read would consume memory until the process
// died, which is a worse failure than saying the device is not stopping.
func TestReadFileStopsRunningAway(t *testing.T) {
	h := newHarness(t, func(d *device) {
		d.setFile("A.BIN", bytes.Repeat([]byte("x"), 64))
		d.endlessFile = true
	})
	h.plugin.fileCeiling = 4096

	_, err := h.plugin.ReadFile(context.Background(), 0, "A.BIN")
	if err == nil {
		t.Fatal("a file that never ends should be refused")
	}
	if !strings.Contains(err.Error(), "not stopping") {
		t.Errorf("err = %v, want it to name the device's behaviour", err)
	}
}

func TestMaxFileBytesDefault(t *testing.T) {
	p := New(testDeps())
	if p.maxFileBytes() != DefaultMaxFileBytes {
		t.Errorf("= %d, want the default %d", p.maxFileBytes(), DefaultMaxFileBytes)
	}
	p.fileCeiling = 1024
	if p.maxFileBytes() != 1024 {
		t.Errorf("= %d, want the ceiling a caller set", p.maxFileBytes())
	}
}

// TestResolveAPathThatIsALabel covers a caller writing a label where a path is
// expected, which is what happens whenever an object sits at the top of a menu
// and its path and its label are the same word.
func TestResolveAPathThatIsALabel(t *testing.T) {
	h := newHarness(t, func(d *device) {
		d.setMenu(1, testMenu())
		d.setValue(1, codec.Value{Command: 0x0113, Mode: codec.ModeValue, Val: 11})
	})

	v, err := h.plugin.GetValue(context.Background(),
		consumer.ValueRequest{Slot: 1, Path: "Gain"})
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if v.Int != 11 {
		t.Errorf("= %d, want 11", v.Int)
	}
}

// TestUnitFromFormatWithNoConversion covers a format string that opens a
// conversion and never finishes it, which a device with a truncated template
// produces.
func TestUnitFromFormatWithNoConversion(t *testing.T) {
	for _, in := range []string{"%", "%-", "%0."} {
		if got := unitFromFormat(in); got != "" {
			t.Errorf("unitFromFormat(%q) = %q, want no unit", in, got)
		}
	}
}

// TestGetValueByLabelWithoutAMenu covers naming an object on a device whose
// menu cannot be read. The session opens, so the failure is the menu's, and a
// name cannot be turned into a command without one.
func TestGetValueByLabelWithoutAMenu(t *testing.T) {
	h := newHarness(t, func(d *device) { d.refuse[codec.MsgGetMenuCount] = true })
	ctx := context.Background()

	if _, err := h.plugin.GetValue(ctx, consumer.ValueRequest{Slot: 1, Label: "Gain"}); err == nil {
		t.Error("reading by name with no menu should fail")
	}
	if _, err := h.plugin.SetValue(ctx, consumer.ValueRequest{Slot: 1, Label: "Gain"},
		consumer.Value{Kind: consumer.KindInt, Int: 1}); err == nil {
		t.Error("writing by name with no menu should fail")
	}
}

// TestUnsubscribeAfterTheDeviceWentAway covers removing the last subscription
// from a slot we can no longer reach.
//
// There is nothing left to tell the device, and saying so would be noise: the
// subscription is gone from our side either way, and a device that is not
// there is not still pushing.
func TestUnsubscribeAfterTheDeviceWentAway(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })
	req := consumer.ValueRequest{Slot: 1}

	if err := h.plugin.Subscribe(req, func(consumer.Event) {}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := h.plugin.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	if err := h.plugin.Unsubscribe(req); err != nil {
		t.Errorf("unsubscribing from a device that has gone: %v", err)
	}

	h.plugin.mu.RLock()
	left := len(h.plugin.subs)
	h.plugin.mu.RUnlock()
	if left != 0 {
		t.Errorf("%d subscriptions were left behind", left)
	}
}
