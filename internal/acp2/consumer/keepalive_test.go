package acp2

import (
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"dhs/internal/consumer"
)

func TestResolvedKeepAlive_Defaults(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	interval, timeout := p.resolvedKeepAlive()
	if interval != defaultKeepAliveInterval {
		t.Fatalf("default interval = %v, want %v", interval, defaultKeepAliveInterval)
	}
	if timeout != 3*defaultKeepAliveInterval {
		t.Fatalf("default timeout = %v, want 3x interval = %v", timeout, 3*defaultKeepAliveInterval)
	}
}

func TestResolvedKeepAlive_CustomInterval(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	p.kaCfg = consumer.KeepAliveConfig{Interval: 2 * time.Second}
	interval, timeout := p.resolvedKeepAlive()
	if interval != 2*time.Second {
		t.Fatalf("interval = %v, want 2s", interval)
	}
	if timeout != 6*time.Second {
		t.Fatalf("derived timeout = %v, want 3x = 6s", timeout)
	}
}

func TestResolvedKeepAlive_DisableInterval(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	p.kaCfg = consumer.KeepAliveConfig{Interval: consumer.DisableInterval}
	interval, timeout := p.resolvedKeepAlive()
	if interval != 0 {
		t.Fatalf("disabled interval = %v, want 0", interval)
	}
	if timeout != defaultKeepAliveTimeout {
		t.Fatalf("timeout with disabled interval = %v, want %v", timeout, defaultKeepAliveTimeout)
	}
}

func TestResolvedKeepAlive_DisableTimeout(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	p.kaCfg = consumer.KeepAliveConfig{Interval: 5 * time.Second, Timeout: consumer.DisableTimeout}
	_, timeout := p.resolvedKeepAlive()
	if timeout != 0 {
		t.Fatalf("disabled timeout = %v, want 0", timeout)
	}
}

func TestSessionLive_NoSession(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	if p.SessionLive() {
		t.Fatal("SessionLive() = true with nil session, want false")
	}
}

func TestSessionLive_DisabledTimeout(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	p.session = &Session{logger: slog.Default()}
	p.kaCfg = consumer.KeepAliveConfig{Timeout: consumer.DisableTimeout}
	if !p.SessionLive() {
		t.Fatal("SessionLive() = false with disabled timeout, want true (transport-up = live)")
	}
}

func TestSessionLive_NoRx(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	p.session = &Session{logger: slog.Default()}
	if p.SessionLive() {
		t.Fatal("SessionLive() = true with no LastRx, want false")
	}
}

func TestSessionLive_RecentRx(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	s := &Session{logger: slog.Default()}
	s.lastRxNS.Store(time.Now().UnixNano())
	p.session = s
	if !p.SessionLive() {
		t.Fatal("SessionLive() = false with fresh LastRx, want true")
	}
}

func TestSessionLive_StaleRx(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	s := &Session{logger: slog.Default()}
	// LastRx 1 hour ago — well past the default 15s timeout.
	s.lastRxNS.Store(time.Now().Add(-1 * time.Hour).UnixNano())
	p.session = s
	if p.SessionLive() {
		t.Fatal("SessionLive() = true with 1h-old LastRx, want false")
	}
}

func TestSetKeepAlive_StoresCfg(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	cfg := consumer.KeepAliveConfig{Interval: 7 * time.Second, Timeout: 30 * time.Second}
	p.SetKeepAlive(cfg)
	if p.kaCfg != cfg {
		t.Fatalf("kaCfg = %+v, want %+v", p.kaCfg, cfg)
	}
}

// Ensure the tests above don't accidentally alias atomic.Int64 the wrong way.
var _ = atomic.Int64{}
