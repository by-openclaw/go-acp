package probelsw08p

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"acp/internal/protocol"
)

// TestFactoryMeta verifies the registration contract: the plugin
// announces itself as "probel-sw08p" on the default SW-P-08 TCP port.
func TestFactoryMeta(t *testing.T) {
	f, err := protocol.Get("probel-sw08p")
	if err != nil {
		t.Fatalf("probel plugin not registered: %v", err)
	}
	m := f.Meta()
	if m.Name != "probel-sw08p" {
		t.Errorf("meta.Name = %q; want probel", m.Name)
	}
	if m.DefaultPort != DefaultPort {
		t.Errorf("meta.DefaultPort = %d; want %d", m.DefaultPort, DefaultPort)
	}
}

// TestStubsReturnNotImplemented locks down the scaffold behaviour —
// Walk / GetValue / SetValue / Subscribe return ErrNotImplemented until
// their per-command PRs land. Keeps future refactors honest.
func TestStubsReturnNotImplemented(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := &Plugin{logger: logger}
	ctx := context.Background()

	if _, err := p.Walk(ctx, 0); err != protocol.ErrNotImplemented {
		t.Errorf("Walk err = %v; want ErrNotImplemented", err)
	}
	if _, err := p.GetValue(ctx, protocol.ValueRequest{}); err != protocol.ErrNotImplemented {
		t.Errorf("GetValue err = %v; want ErrNotImplemented", err)
	}
	if _, err := p.SetValue(ctx, protocol.ValueRequest{}, protocol.Value{}); err != protocol.ErrNotImplemented {
		t.Errorf("SetValue err = %v; want ErrNotImplemented", err)
	}
	if err := p.Subscribe(protocol.ValueRequest{}, func(protocol.Event) {}); err != protocol.ErrNotImplemented {
		t.Errorf("Subscribe err = %v; want ErrNotImplemented", err)
	}
}

// TestDisconnectBeforeConnect is a safety net — Disconnect on an
// unconnected plugin must be a no-op.
func TestDisconnectBeforeConnect(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := &Plugin{logger: logger}
	if err := p.Disconnect(); err != nil {
		t.Errorf("Disconnect on fresh plugin = %v; want nil", err)
	}
}
