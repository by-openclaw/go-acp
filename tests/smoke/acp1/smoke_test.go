// Integration tests for ACP1 against a real device or emulator.
//
//go:build integration

// Gated by ACP1_TEST_HOST env var. Skip when not set.
//
// Run: ACP1_TEST_HOST=10.6.239.113 go test -tags integration ./tests/integration/acp1/...
package acp1_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"acp/internal/protocol"
	"acp/internal/protocol/acp1"
)

func testHost() string {
	return os.Getenv("ACP1_TEST_HOST")
}

func skipIfNoHost(t *testing.T) {
	t.Helper()
	if testHost() == "" {
		t.Skip("ACP1_TEST_HOST not set — skipping integration test")
	}
}

func connectPlugin(t *testing.T) protocol.Protocol {
	t.Helper()
	skipIfNoHost(t)
	f := &acp1.Factory{}
	plug := f.New(slog.Default())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := plug.Connect(ctx, testHost(), acp1.DefaultPort); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = plug.Disconnect() })
	return plug
}

func TestIntegration_Info(t *testing.T) {
	plug := connectPlugin(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	info, err := plug.GetDeviceInfo(ctx)
	if err != nil {
		t.Fatalf("GetDeviceInfo: %v", err)
	}
	if info.NumSlots <= 0 {
		t.Errorf("NumSlots: %d", info.NumSlots)
	}
	t.Logf("device %s:%d — %d slots", info.IP, info.Port, info.NumSlots)
}

func TestIntegration_WalkSlot0(t *testing.T) {
	plug := connectPlugin(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	objs, err := plug.Walk(ctx, 0)
	if err != nil {
		t.Fatalf("Walk(0): %v", err)
	}
	if len(objs) == 0 {
		t.Error("Walk returned zero objects on slot 0")
	}
	t.Logf("slot 0: %d objects", len(objs))
}

func TestIntegration_GetByLabel(t *testing.T) {
	plug := connectPlugin(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Walk first to populate label map.
	if _, err := plug.Walk(ctx, 0); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	val, err := plug.GetValue(ctx, protocol.ValueRequest{Slot: 0, Label: "Card name"})
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if val.Str == "" {
		t.Error("Card name returned empty string")
	}
	t.Logf("Card name = %q", val.Str)
}
