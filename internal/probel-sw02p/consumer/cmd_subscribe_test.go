package probelsw02p

import (
	"testing"
	"time"

	"dhs/internal/probel-sw02p/codec"
)

// TestSubscribeExtendedProtectDisconnectedHappy drives a tx 98 through a
// listener (the whole decode body) — and first feeds an unrelated frame
// the listener must skip (the f.ID-mismatch early return).
func TestSubscribeExtendedProtectDisconnectedHappy(t *testing.T) {
	p, peer := newPipePlugin(t)
	got := make(chan codec.ExtendedProtectDisconnectedParams, 1)
	if err := p.SubscribeExtendedProtectDisconnected(func(params codec.ExtendedProtectDisconnectedParams) {
		got <- params
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Decoy: a tx 04 the listener must ignore (wrong ID).
	writeFrame(t, peer, codec.EncodeConnected(codec.ConnectedParams{Destination: 1, Source: 1}))
	// Real tx 98.
	writeFrame(t, peer, codec.EncodeExtendedProtectDisconnected(codec.ExtendedProtectDisconnectedParams{
		Destination: 321, Device: 12, Protect: codec.ProtectNone,
	}))

	select {
	case params := <-got:
		if params.Destination != 321 || params.Device != 12 {
			t.Errorf("got %+v; want dst=321 dev=12", params)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listener did not fire")
	}
}

// TestSubscribeExtendedProtectTallySkipsWrongCommand feeds a decoy
// frame before the real tx 96 to exercise the listener's ID-filter
// early return.
func TestSubscribeExtendedProtectTallySkipsWrongCommand(t *testing.T) {
	p, peer := newPipePlugin(t)
	got := make(chan codec.ExtendedProtectTallyParams, 1)
	_ = p.SubscribeExtendedProtectTally(func(params codec.ExtendedProtectTallyParams) { got <- params })

	writeFrame(t, peer, codec.EncodeConnected(codec.ConnectedParams{Destination: 1, Source: 1})) // decoy
	writeFrame(t, peer, codec.EncodeExtendedProtectTally(codec.ExtendedProtectTallyParams{Destination: 7, Device: 3, Protect: codec.ProtectProBel}))

	select {
	case params := <-got:
		if params.Destination != 7 {
			t.Errorf("got dst=%d; want 7", params.Destination)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listener did not fire after decoy")
	}
}

// TestSubscribeExtendedProtectConnectedSkipsWrongCommand drives the
// tx 97 listener's ID-filter early return.
func TestSubscribeExtendedProtectConnectedSkipsWrongCommand(t *testing.T) {
	p, peer := newPipePlugin(t)
	got := make(chan codec.ExtendedProtectConnectedParams, 1)
	_ = p.SubscribeExtendedProtectConnected(func(params codec.ExtendedProtectConnectedParams) { got <- params })

	writeFrame(t, peer, codec.EncodeConnected(codec.ConnectedParams{Destination: 1, Source: 1})) // decoy
	writeFrame(t, peer, codec.EncodeExtendedProtectConnected(codec.ExtendedProtectConnectedParams{Destination: 8, Device: 4}))

	select {
	case params := <-got:
		if params.Destination != 8 {
			t.Errorf("got dst=%d; want 8", params.Destination)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listener did not fire after decoy")
	}
}

// TestSubscribeExtendedProtectTallyDumpSkipsWrongCommand drives the
// tx 100 listener's ID-filter early return.
func TestSubscribeExtendedProtectTallyDumpSkipsWrongCommand(t *testing.T) {
	p, peer := newPipePlugin(t)
	got := make(chan codec.ExtendedProtectTallyDumpParams, 1)
	_ = p.SubscribeExtendedProtectTallyDump(func(params codec.ExtendedProtectTallyDumpParams) { got <- params })

	writeFrame(t, peer, codec.EncodeConnected(codec.ConnectedParams{Destination: 1, Source: 1})) // decoy
	writeFrame(t, peer, codec.EncodeExtendedProtectTallyDump(codec.ExtendedProtectTallyDumpParams{Reset: true}))

	select {
	case params := <-got:
		if !params.Reset {
			t.Errorf("got %+v; want Reset=true", params)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listener did not fire after decoy")
	}
}

// TestSubscribeProtectDeviceNameRequestSkipsWrongCommand drives the
// rx 103 listener's ID-filter early return.
func TestSubscribeProtectDeviceNameRequestSkipsWrongCommand(t *testing.T) {
	p, peer := newPipePlugin(t)
	got := make(chan codec.ProtectDeviceNameRequestParams, 1)
	_ = p.SubscribeProtectDeviceNameRequest(func(params codec.ProtectDeviceNameRequestParams) { got <- params })

	writeFrame(t, peer, codec.EncodeConnected(codec.ConnectedParams{Destination: 1, Source: 1})) // decoy
	writeFrame(t, peer, codec.EncodeProtectDeviceNameRequest(codec.ProtectDeviceNameRequestParams{Device: 123}))

	select {
	case params := <-got:
		if params.Device != 123 {
			t.Errorf("got device=%d; want 123", params.Device)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listener did not fire after decoy")
	}
}

// TestSubscribeTallyDumpNotConnected + DeviceName: error paths not
// already pinned elsewhere.
func TestSubscribeTallyDumpAndNameNotConnected(t *testing.T) {
	p := &Plugin{logger: quietLogger()}
	if err := p.SubscribeExtendedProtectTallyDump(func(codec.ExtendedProtectTallyDumpParams) {}); err == nil {
		t.Error("SubscribeExtendedProtectTallyDump on unconnected plugin returned nil")
	}
	if err := p.SubscribeProtectDeviceNameRequest(func(codec.ProtectDeviceNameRequestParams) {}); err == nil {
		t.Error("SubscribeProtectDeviceNameRequest on unconnected plugin returned nil")
	}
}
