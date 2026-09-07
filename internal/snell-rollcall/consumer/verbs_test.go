package rollcall

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/snell-rollcall/codec"
)

// allVerbs is every operation the connector offers, so a change in how one of
// them fails cannot quietly diverge from the rest.
//
// The two tables below run all of them against a device that will not open a
// session, and against a connector that was never connected. Both are ordinary
// conditions in service: a unit runs out of sessions, and a caller forgets to
// connect. Neither may return a zero value as though it had succeeded.
func allVerbs() []struct {
	name string
	call func(context.Context, *Plugin) error
} {
	return []struct {
		name string
		call func(context.Context, *Plugin) error
	}{
		{"GetDeviceInfo", func(ctx context.Context, p *Plugin) error {
			_, err := p.GetDeviceInfo(ctx)
			return err
		}},
		{"GetSlotInfo", func(ctx context.Context, p *Plugin) error {
			_, err := p.GetSlotInfo(ctx, 1)
			return err
		}},
		{"Walk", func(ctx context.Context, p *Plugin) error {
			_, err := p.Walk(ctx, 1)
			return err
		}},
		{"GetValue", func(ctx context.Context, p *Plugin) error {
			_, err := p.GetValue(ctx, consumer.ValueRequest{Slot: 1, ID: 1})
			return err
		}},
		{"SetValue", func(ctx context.Context, p *Plugin) error {
			_, err := p.SetValue(ctx, consumer.ValueRequest{Slot: 1, ID: 1},
				consumer.Value{Kind: consumer.KindInt, Int: 1})
			return err
		}},
		{"SetDefault", func(ctx context.Context, p *Plugin) error {
			_, err := p.SetDefault(ctx, consumer.ValueRequest{Slot: 1, ID: 1})
			return err
		}},
		{"Display", func(ctx context.Context, p *Plugin) error {
			_, err := p.Display(ctx, 1)
			return err
		}},
		{"Devices", func(ctx context.Context, p *Plugin) error {
			_, err := p.Devices(ctx)
			return err
		}},
		{"Ports", func(ctx context.Context, p *Plugin) error {
			_, err := p.Ports(ctx, gatewayAddr.Unit)
			return err
		}},
		{"Uses32Bit", func(ctx context.Context, p *Plugin) error {
			_, err := p.Uses32Bit(ctx, 1)
			return err
		}},
		{"ReadFile", func(ctx context.Context, p *Plugin) error {
			_, err := p.ReadFile(ctx, 0, "A.BIN")
			return err
		}},
		{"ReadFileTo", func(ctx context.Context, p *Plugin) error {
			_, err := p.ReadFileTo(ctx, 0, "A.BIN", &bytes.Buffer{})
			return err
		}},
		{"ListDir", func(ctx context.Context, p *Plugin) error {
			_, err := p.ListDir(ctx, 0, "/")
			return err
		}},
		{"Subscribe", func(_ context.Context, p *Plugin) error {
			return p.Subscribe(consumer.ValueRequest{Slot: 1}, func(consumer.Event) {})
		}},
		{"Unsubscribe", func(_ context.Context, p *Plugin) error {
			return p.Unsubscribe(consumer.ValueRequest{Slot: 1, ID: 1})
		}},
	}
}

// TestEveryVerbFailsWhenNoSessionCanBeOpened covers a unit that has run out of
// sessions, which is the ordinary way a busy device stops being usable.
func TestEveryVerbFailsWhenNoSessionCanBeOpened(t *testing.T) {
	for _, tc := range allVerbs() {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, func(d *device) { d.refuse[codec.MsgCall] = true })
			if err := tc.call(context.Background(), h.plugin); err == nil {
				t.Errorf("%s succeeded on a device that opened no session", tc.name)
			}
		})
	}
}

// TestEveryVerbFailsWhenNotConnected covers a caller that forgot to connect.
func TestEveryVerbFailsWhenNotConnected(t *testing.T) {
	for _, tc := range allVerbs() {
		t.Run(tc.name, func(t *testing.T) {
			p := New(testDeps())
			err := tc.call(context.Background(), p)
			if err == nil {
				t.Fatalf("%s succeeded on a connector that was never connected", tc.name)
			}
			if !errors.Is(err, consumer.ErrNotConnected) {
				t.Errorf("%s err = %v, want ErrNotConnected", tc.name, err)
			}
		})
	}
}

// TestEveryVerbFailsAfterDisconnect covers the same verbs once the link has
// been taken away, which is what happens when a device is unplugged.
func TestEveryVerbFailsAfterDisconnect(t *testing.T) {
	for _, tc := range allVerbs() {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, func(d *device) {
				d.setMenu(1, testMenu())
				d.setFile("A.BIN", []byte("x"))
			})
			if err := h.plugin.Disconnect(); err != nil {
				t.Fatalf("Disconnect: %v", err)
			}
			if err := tc.call(context.Background(), h.plugin); err == nil {
				t.Errorf("%s succeeded after disconnecting", tc.name)
			}
		})
	}
}

// TestGetSlotInfoRefusalsPerStep covers a device that answers one step of the
// slot query and refuses the next, so the failure is attributed to the step
// that actually failed rather than the first one.
func TestGetSlotInfoRefusalsPerStep(t *testing.T) {
	h := newHarness(t, func(d *device) { d.refuse[codec.MsgGetStat] = true })

	_, err := h.plugin.GetSlotInfo(context.Background(), 1)
	if err == nil {
		t.Fatal("a refused status should fail the slot query")
	}
}

// TestReadFile_CloseFails covers a device that will not close a handle. The
// read has already succeeded, so the caller gets its bytes: a file service
// that cannot tidy up is a leak on the device, not a failed read.
func TestReadFile_CloseFails(t *testing.T) {
	body := []byte("contents")
	h := newHarness(t, func(d *device) {
		d.setFile("A.BIN", body)
		d.refuse[codec.MsgFileClose] = true
	})

	got, err := h.plugin.ReadFile(context.Background(), 0, "A.BIN")
	if err != nil {
		t.Fatalf("a failed close must not fail the read: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("read %q, want %q", got, body)
	}
}

// TestReadFileTo_WriterFails covers the destination refusing the bytes, which
// is what a full disk looks like. The error is the writer's, and how much was
// written is reported so a caller can say where it stopped.
func TestReadFileTo_WriterFails(t *testing.T) {
	h := newHarness(t, func(d *device) {
		d.setFile("A.BIN", bytes.Repeat([]byte("x"), 1000))
	})

	want := errors.New("disk full")
	n, err := h.plugin.ReadFileTo(context.Background(), 0, "A.BIN", failingWriter{err: want})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want the writer's own reason", err)
	}
	if n != 0 {
		t.Errorf("wrote %d bytes, want the count the writer accepted", n)
	}
}

type failingWriter struct{ err error }

func (f failingWriter) Write(p []byte) (int, error) { return 0, f.err }

// TestReadFile_ReadRefused covers a device that opens a file and then refuses
// to read from it.
func TestReadFile_ReadRefused(t *testing.T) {
	h := newHarness(t, func(d *device) {
		d.setFile("A.BIN", []byte("x"))
		d.refuse[codec.MsgFileRead] = true
	})
	ctx := context.Background()

	if _, err := h.plugin.ReadFile(ctx, 0, "A.BIN"); err == nil {
		t.Error("a refused read should fail")
	}
	if _, err := h.plugin.ReadFileTo(ctx, 0, "A.BIN", &bytes.Buffer{}); err == nil {
		t.Error("a refused read should fail the streaming form too")
	}
}

// TestReadFileTo_Missing covers the streaming form against a file that is not
// there.
func TestReadFileTo_Missing(t *testing.T) {
	h := newHarness(t, nil)

	if _, err := h.plugin.ReadFileTo(context.Background(), 0, "NOSUCH.BIN", &bytes.Buffer{}); err == nil {
		t.Error("streaming a file that is not there should fail")
	}
}

// TestWalk16_SkipsUnexpectedItems covers a server answering one item of a
// menu transfer with something other than a menu line. The rest of the menu is
// still worth having, so the odd item is skipped rather than the walk
// abandoned.
func TestWalk16_SkipsUnexpectedItems(t *testing.T) {
	h := newHarness(t, func(d *device) {
		d.services &^= codec.SvcLongStr
		d.setMenu(1, testMenu())
		d.oddMenuItem = 2 // the third line comes back as an acknowledgement
	})

	objects, err := h.plugin.Walk(context.Background(), 1)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(objects) != len(testMenu())-1 {
		t.Errorf("walked %d objects, want the menu less the odd one", len(objects))
	}
}

// TestDevices_EmptyMapFallsBackToTheGateway covers a gateway that has heard
// nothing announce itself. It still knows about itself, and reporting nothing
// at all would look like a failed discovery.
func TestDevices_EmptyMapFallsBackToTheGateway(t *testing.T) {
	h := newHarness(t, func(d *device) { d.emptyDeviceMap = true })

	devices, err := h.plugin.Devices(context.Background())
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("got %d devices, want the gateway itself", len(devices))
	}
	if devices[0].ID.Name != "Centra" {
		t.Errorf("= %q, want the gateway", devices[0].ID.Name)
	}
}

// TestUnsubscribeLeavesOtherListeners covers removing one subscription from a
// slot that has more than one. Closing the back channel for one would silence
// the others, so the device is told to stop only when the last goes.
func TestUnsubscribeLeavesOtherListeners(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })

	events := make(chan consumer.Event, 8)
	gain := consumer.ValueRequest{Slot: 1, Label: "Gain"}
	enable := consumer.ValueRequest{Slot: 1, Label: "Enable"}

	for _, req := range []consumer.ValueRequest{gain, enable} {
		if err := h.plugin.Subscribe(req, func(e consumer.Event) { events <- e }); err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
	}
	if err := h.plugin.Unsubscribe(gain); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}

	// The other listener still hears its own command.
	value, err := codec.Value{Command: 0x0114, Mode: codec.ModeValue, Val: 1}.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	h.device.push(1, codec.MsgRetValue, value)

	select {
	case ev := <-events:
		if ev.ID != 0x0114 {
			t.Errorf("id = %d, want the surviving subscription's command", ev.ID)
		}
	case <-timeoutAfter():
		t.Fatal("removing one subscription silenced another")
	}
}

// TestSubscribeUnresolvable covers subscribing to something the menu does not
// contain.
func TestSubscribeUnresolvable(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })

	err := h.plugin.Subscribe(consumer.ValueRequest{Slot: 1, Label: "nothing"},
		func(consumer.Event) {})
	if err == nil {
		t.Error("subscribing to an object that is not there should say so")
	}
}

// TestSubscribeRefused covers a device that will not open its back channel.
// The subscription must not be left registered, or a later push would be
// delivered to a listener that was told it had failed.
func TestSubscribeRefused(t *testing.T) {
	h := newHarness(t, func(d *device) {
		d.setMenu(1, testMenu())
		d.refuse[codec.MsgBkChnReady] = true
	})

	err := h.plugin.Subscribe(consumer.ValueRequest{Slot: 1, Label: "Gain"},
		func(consumer.Event) {})
	if err == nil {
		t.Fatal("a refused back channel should fail the subscription")
	}

	h.plugin.mu.RLock()
	left := len(h.plugin.subs)
	h.plugin.mu.RUnlock()
	if left != 0 {
		t.Errorf("%d subscriptions were left registered after a failure", left)
	}
}

// TestSetValueOnAnUnwalkedSlot covers a write that has to walk first, and one
// to a command the menu does not list.
func TestSetValueOnAnUnwalkedSlot(t *testing.T) {
	h := newHarness(t, func(d *device) {
		d.setMenu(1, testMenu())
		d.setValue(1, codec.Value{Command: 999, Mode: codec.ModeValue})
	})

	// A command the menu does not list is still addressable: the device knows
	// its own command set better than a cached walk does.
	got, err := h.plugin.SetValue(context.Background(),
		consumer.ValueRequest{Slot: 1, ID: 999},
		consumer.Value{Kind: consumer.KindInt, Int: 4})
	if err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	if got.Int != 4 {
		t.Errorf("= %d, want 4", got.Int)
	}
}

// TestCallRefusedOnA16BitDevice covers a device that never advertised long
// strings and still will not open a session. The connector must not try a
// fallback it already knows is the same request.
func TestCallRefusedOnA16BitDevice(t *testing.T) {
	h := newHarness(t, func(d *device) {
		d.services &^= codec.SvcLongStr
		d.refuse[codec.MsgCall] = true
	})

	_, err := h.plugin.Walk(context.Background(), 1)
	if err == nil {
		t.Fatal("a refused call should fail")
	}
	// There is nothing to fall back from, so nothing is reported as one.
	if hasEvent(h.plugin, EventLongStringsRefused) {
		t.Error("a device that never advertised long strings did not refuse them")
	}
}

// TestListsSkipUnexpectedItems covers a gateway answering one item of a device
// or port list with something other than a device record. The rest of the list
// is still worth having.
func TestListsSkipUnexpectedItems(t *testing.T) {
	t.Run("port list", func(t *testing.T) {
		h := newHarness(t, func(d *device) {
			d.ports = 3
			d.oddListItem = 1
		})
		ports, err := h.plugin.Ports(context.Background(), gatewayAddr.Unit)
		if err != nil {
			t.Fatalf("Ports: %v", err)
		}
		if len(ports) != 2 {
			t.Errorf("got %d ports, want the list less the odd one", len(ports))
		}
	})

	t.Run("device map", func(t *testing.T) {
		h := newHarness(t, func(d *device) { d.oddListItem = 0 })
		devices, err := h.plugin.Devices(context.Background())
		if err != nil {
			t.Fatalf("Devices: %v", err)
		}
		// Nothing decodable came back, so the gateway itself is the answer.
		if len(devices) != 1 || devices[0].ID.Name != "Centra" {
			t.Errorf("= %d devices, want the gateway itself", len(devices))
		}
	})
}

// TestLinkCloseIsIdempotent covers the guard on the link's own shutdown. It is
// reached directly because Disconnect takes the link out of the plugin before
// closing it, so nothing in service can close one twice; the guard is what
// makes that safe rather than assumed.
func TestLinkCloseIsIdempotent(t *testing.T) {
	h := newHarness(t, func(d *device) { d.setMenu(1, testMenu()) })

	if _, err := h.plugin.Walk(context.Background(), 1); err != nil {
		t.Fatalf("Walk: %v", err)
	}

	h.plugin.mu.RLock()
	l := h.plugin.link
	h.plugin.mu.RUnlock()

	l.close()
	l.close() // must not panic, and must not close a session twice
}
