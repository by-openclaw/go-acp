package probelsw02p

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"acp/internal/protocol"
)

// TestFactoryMeta verifies the registration contract: the plugin
// announces itself as "probel-sw02p" on the default SW-P-02 TCP port.
func TestFactoryMeta(t *testing.T) {
	f, err := protocol.Get("probel-sw02p")
	if err != nil {
		t.Fatalf("probel-sw02p plugin not registered: %v", err)
	}
	m := f.Meta()
	if m.Name != "probel-sw02p" {
		t.Errorf("meta.Name = %q; want probel-sw02p", m.Name)
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

// TestMetricsNilBeforeConnect pins the contract: Metrics() is nil
// until Connect fires. Wired via Connect, preserved across Disconnect.
func TestMetricsNilBeforeConnect(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := &Plugin{logger: logger}
	if m := p.Metrics(); m != nil {
		t.Errorf("Metrics() = %v before Connect; want nil", m)
	}
}

// TestComplianceProfileNilBeforeConnect: accessing ComplianceProfile
// on a fresh plugin returns nil (the session profile is installed at
// Connect time).
func TestComplianceProfileNilBeforeConnect(t *testing.T) {
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if p.ComplianceProfile() != nil {
		t.Error("ComplianceProfile() = non-nil; want nil before Connect")
	}
}
