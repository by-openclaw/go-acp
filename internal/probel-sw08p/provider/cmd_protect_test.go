package probelsw08p

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"acp/internal/probel-sw08p/codec"
	probelproto "acp/internal/probel-sw08p/consumer"
)

// TestProtectRoundTripLoopback exercises the full protect life-cycle
// across the wire: Interrogate (empty) → Connect → Interrogate (held)
// → Disconnect (owner) → Interrogate (empty), plus the Device Name and
// Tally Dump surfaces.
func TestProtectRoundTripLoopback(t *testing.T) {
	exp := demoMatrixExport(16, 16)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := newServer(logger, exp)
	srv.tree.setDeviceName(42, "PANEL01")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx, "127.0.0.1:0") }()
	addr := waitBound(t, srv)
	host, port := splitAddr(t, addr)

	f := &probelproto.Factory{}
	plugin := f.New(logger).(*probelproto.Plugin)
	dc, cancelDC := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelDC()
	if err := plugin.Connect(dc, host, port); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = plugin.Disconnect() }()

	// 1. Initially no protect on dst=3.
	tally, err := plugin.ProtectInterrogate(dc, 0, 0, 3, 0)
	if err != nil {
		t.Fatalf("interrogate empty: %v", err)
	}
	if tally.State != codec.ProtectNone {
		t.Errorf("empty state = %d; want ProtectNone(0)", tally.State)
	}

	// 2. Device 42 claims it.
	connected, err := plugin.ProtectConnect(dc, 0, 0, 3, 42)
	if err != nil {
		t.Fatalf("protect-connect: %v", err)
	}
	if connected.DeviceID != 42 || connected.State != codec.ProtectProbel {
		t.Errorf("connected = %+v; want device=42 state=Probel", connected)
	}

	// 3. Interrogate reports it held by 42.
	tally, err = plugin.ProtectInterrogate(dc, 0, 0, 3, 0)
	if err != nil {
		t.Fatalf("interrogate held: %v", err)
	}
	if tally.DeviceID != 42 || tally.State != codec.ProtectProbel {
		t.Errorf("held = %+v; want device=42 state=Probel", tally)
	}

	// 4. Device name resolves to PANEL01.
	name, err := plugin.ProtectDeviceName(dc, 42)
	if err != nil {
		t.Fatalf("device-name: %v", err)
	}
	if name != "PANEL01" {
		t.Errorf("name = %q; want PANEL01", name)
	}

	// 5. Tally dump shows device 42 at dst=3.
	dump, err := plugin.ProtectTallyDump(dc, 0, 0, 0)
	if err != nil {
		t.Fatalf("tally-dump: %v", err)
	}
	if len(dump.Items) < 4 {
		t.Fatalf("dump items = %d; want ≥4", len(dump.Items))
	}
	if dump.Items[3].DeviceID != 42 || dump.Items[3].State != codec.ProtectProbel {
		t.Errorf("dump[3] = %+v; want device=42 state=Probel", dump.Items[3])
	}

	// 6. Disconnect releases.
	disc, err := plugin.ProtectDisconnect(dc, 0, 0, 3, 42)
	if err != nil {
		t.Fatalf("protect-disconnect: %v", err)
	}
	if disc.State != codec.ProtectNone {
		t.Errorf("disc.state = %d; want None", disc.State)
	}
}

// TestMasterProtectOverrideLoopback verifies rx 029 seizes an existing
// override protect — it bypasses the owner check applied by plain
// Protect Connect.
func TestMasterProtectOverrideLoopback(t *testing.T) {
	exp := demoMatrixExport(16, 16)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := newServer(logger, exp)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx, "127.0.0.1:0") }()
	addr := waitBound(t, srv)
	host, port := splitAddr(t, addr)

	// Seed a regular Protect Probel owned by 42.
	if err := srv.tree.applyProtectConnect(0, 0, 7, 42, uint8(codec.ProtectProbel), false); err != nil {
		t.Fatalf("seed protect: %v", err)
	}

	f := &probelproto.Factory{}
	plugin := f.New(logger).(*probelproto.Plugin)
	dc, cancelDC := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelDC()
	if err := plugin.Connect(dc, host, port); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = plugin.Disconnect() }()

	// Master panel (device=99) seizes it.
	reply, err := plugin.MasterProtectConnect(dc, 0, 0, 7, 99)
	if err != nil {
		t.Fatalf("master-protect-connect: %v", err)
	}
	if reply.DeviceID != 99 || reply.State != codec.ProtectProbelOver {
		t.Errorf("master reply = %+v; want device=99 state=ProbelOver", reply)
	}
	// Tree should now reflect device=99, override state.
	rec := srv.tree.protectAt(0, 0, 7)
	if rec.deviceID != 99 || rec.state != uint8(codec.ProtectProbelOver) {
		t.Errorf("tree = %+v; want device=99 state=ProbelOver", rec)
	}
}
