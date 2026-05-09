package acp2

import (
	"log/slog"
	"testing"
	"time"

	"dhs/internal/protocol"
)

func TestResolvedReconnect_Defaults(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	initial, cap, max := p.resolvedReconnect()
	if initial != defaultReconnectInitial {
		t.Errorf("default initial = %v, want %v", initial, defaultReconnectInitial)
	}
	if cap != defaultReconnectCap {
		t.Errorf("default cap = %v, want %v", cap, defaultReconnectCap)
	}
	if max != 0 {
		t.Errorf("default max = %d, want 0 (unlimited)", max)
	}
}

func TestResolvedReconnect_Disabled(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	p.reconnectCfg = protocol.ReconnectConfig{Disabled: true}
	initial, _, _ := p.resolvedReconnect()
	if initial != 0 {
		t.Errorf("disabled initial = %v, want 0", initial)
	}
}

func TestResolvedReconnect_Custom(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	p.reconnectCfg = protocol.ReconnectConfig{
		Initial:     500 * time.Millisecond,
		Cap:         5 * time.Second,
		MaxAttempts: 10,
	}
	initial, cap, max := p.resolvedReconnect()
	if initial != 500*time.Millisecond {
		t.Errorf("initial = %v, want 500ms", initial)
	}
	if cap != 5*time.Second {
		t.Errorf("cap = %v, want 5s", cap)
	}
	if max != 10 {
		t.Errorf("max = %d, want 10", max)
	}
}

func TestResolvedReconnect_CapFloorsToInitial(t *testing.T) {
	// Sanity: if user supplies cap < initial, cap is bumped up so
	// the doubling never goes below the floor.
	p := &Plugin{logger: slog.Default()}
	p.reconnectCfg = protocol.ReconnectConfig{
		Initial: 10 * time.Second,
		Cap:     2 * time.Second,
	}
	initial, cap, _ := p.resolvedReconnect()
	if cap != initial {
		t.Errorf("cap = %v, want = initial %v", cap, initial)
	}
}

func TestSetReconnect_StoresCfg(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	cfg := protocol.ReconnectConfig{Initial: 2 * time.Second, Cap: 30 * time.Second, MaxAttempts: 5}
	p.SetReconnect(cfg)
	if p.reconnectCfg != cfg {
		t.Errorf("reconnectCfg = %+v, want %+v", p.reconnectCfg, cfg)
	}
}
