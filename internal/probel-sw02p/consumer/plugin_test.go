package probelsw02p

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"dhs/internal/consumer"
)

// TestFactoryMeta verifies the registration contract: the plugin
// announces itself as "probel-sw02p" on the default SW-P-02 TCP port.
func TestFactoryMeta(t *testing.T) {
	f, err := consumer.Get("probel-sw02p")
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

	if _, err := p.Walk(ctx, 0); err != consumer.ErrNotImplemented {
		t.Errorf("Walk err = %v; want ErrNotImplemented", err)
	}
	if _, err := p.GetValue(ctx, consumer.ValueRequest{}); err != consumer.ErrNotImplemented {
		t.Errorf("GetValue err = %v; want ErrNotImplemented", err)
	}
	if _, err := p.SetValue(ctx, consumer.ValueRequest{}, consumer.Value{}); err != consumer.ErrNotImplemented {
		t.Errorf("SetValue err = %v; want ErrNotImplemented", err)
	}
	if err := p.Subscribe(consumer.ValueRequest{}, func(consumer.Event) {}); err != consumer.ErrNotImplemented {
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

// TestMetricsExistsBeforeConnect pins the contract: Metrics() is never nil.
// It used to be, until Connect installed a copy of the injected connector,
// which meant --metrics-addr on a plugin that had not connected scraped
// nothing and every caller carried a nil check.
func TestMetricsExistsBeforeConnect(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := &Plugin{logger: logger}
	if p.Metrics() == nil {
		t.Error("Metrics() = nil; it must never be")
	}
}

// TestComplianceProfileExistsBeforeConnect: the profile is connector-scoped
// and available immediately, so a deviation is never dropped for want of a
// Connect.
func TestComplianceProfileExistsBeforeConnect(t *testing.T) {
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if p.ComplianceProfile() == nil {
		t.Error("ComplianceProfile() = nil; it must never be")
	}
}
