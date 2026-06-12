// Integration tests for the ACP1 write + mutate surface and Mode C transport.
//
//go:build integration

// Gated by ACP1_TEST_HOST. These drive the real consumer against the vendor
// oracle (Synapse Simulator / Axon rack), exercising the methods the unit
// tier can only reach with a fake transport: setValue, setInc/setDec/setDef,
// and an AN2 (Mode C) connect.
//
// Run: ACP1_TEST_HOST=10.6.239.113 go test -tags integration ./internal/acp1/smoke/...
package acp1_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"dhs/internal/acp1/consumer"
	cons "dhs/internal/consumer"
)

// acp1Mutator is the ACP1-specific no-arg mutating surface the plugin exposes
// beyond the cross-protocol consumer.Protocol interface.
type acp1Mutator interface {
	SetIncValue(context.Context, cons.ValueRequest) (cons.Value, error)
	SetDecValue(context.Context, cons.ValueRequest) (cons.Value, error)
	SetDefValue(context.Context, cons.ValueRequest) (cons.Value, error)
}

// netwPrefix is the slot-0 control byte (range 0..32, step 1, default 0) used
// as a reversible write target — restored to 0 after each test.
var netwPrefix = cons.ValueRequest{Slot: 0, Group: "control", Label: "NetwPrefix"}

func TestIntegration_SetRoundTrip(t *testing.T) {
	plug := connectPlugin(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := plug.Walk(ctx, 0); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if _, err := plug.SetValue(ctx, netwPrefix, cons.Value{Str: "5"}); err != nil {
		t.Fatalf("SetValue 5: %v", err)
	}
	got, err := plug.GetValue(ctx, netwPrefix)
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if got.Int != 5 && got.Uint != 5 {
		t.Errorf("readback = int:%d uint:%d, want 5", got.Int, got.Uint)
	}
	if _, err := plug.SetValue(ctx, netwPrefix, cons.Value{Str: "0"}); err != nil {
		t.Errorf("restore 0: %v", err)
	}
}

func TestIntegration_IncDecReset(t *testing.T) {
	plug := connectPlugin(t)
	mut, ok := plug.(acp1Mutator)
	if !ok {
		t.Fatal("plugin does not implement the ACP1 mutator surface")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := plug.Walk(ctx, 0); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if _, err := plug.SetValue(ctx, netwPrefix, cons.Value{Str: "5"}); err != nil {
		t.Fatalf("baseline set 5: %v", err)
	}
	defer func() { _, _ = plug.SetValue(ctx, netwPrefix, cons.Value{Str: "0"}) }()

	up, err := mut.SetIncValue(ctx, netwPrefix)
	if err != nil {
		t.Fatalf("SetIncValue: %v", err)
	}
	if up.Int != 6 && up.Uint != 6 {
		t.Errorf("inc = %d/%d, want 6", up.Int, up.Uint)
	}
	down, err := mut.SetDecValue(ctx, netwPrefix)
	if err != nil {
		t.Fatalf("SetDecValue: %v", err)
	}
	if down.Int != 5 && down.Uint != 5 {
		t.Errorf("dec = %d/%d, want 5", down.Int, down.Uint)
	}
	def, err := mut.SetDefValue(ctx, netwPrefix)
	if err != nil {
		t.Fatalf("SetDefValue: %v", err)
	}
	if def.Int != 0 && def.Uint != 0 {
		t.Errorf("reset = %d/%d, want default 0", def.Int, def.Uint)
	}
}

// TestIntegration_AN2Connect exercises Mode C (AN2/TCP :2072). The Synapse
// emulator is UDP-only, so this skips cleanly when 2072 isn't served; against
// a real Axon rack it proves the AN2 consumer path end-to-end.
func TestIntegration_AN2Connect(t *testing.T) {
	skipIfNoHost(t)
	f := &acp1.Factory{}
	plug := f.New(slog.Default())
	tp, ok := plug.(interface{ SetTransport(acp1.TransportKind) })
	if !ok {
		t.Fatal("plugin missing SetTransport")
	}
	tp.SetTransport(acp1.TransportAN2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := plug.Connect(ctx, testHost(), acp1.AN2DefaultPort); err != nil {
		t.Skipf("AN2 (Mode C) not served by %s:2072 (UDP-only host?): %v", testHost(), err)
	}
	t.Cleanup(func() { _ = plug.Disconnect() })
	info, err := plug.GetDeviceInfo(ctx)
	if err != nil {
		t.Fatalf("GetDeviceInfo over AN2: %v", err)
	}
	t.Logf("AN2 connect OK — %d slots", info.NumSlots)
}
