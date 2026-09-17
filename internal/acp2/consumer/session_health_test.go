package acp2

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/plugin"
)

// The health LOGIC is specified once, in internal/consumer. What these
// tests cover is the only part that is ACP2's: that the plugin satisfies the
// interface, and that Connect hands the shared tracker the right stale
// window, the right network and the Session as its time source.

func TestPluginSatisfiesHealthChecker(t *testing.T) {
	var _ consumer.HealthChecker = (*Plugin)(nil)
}

func newHealthPlugin() *Plugin {
	return (&Factory{}).New(plugin.Deps{Logger: slog.Default()}).(*Plugin)
}

func TestSessionHealthBeforeConnect(t *testing.T) {
	got := newHealthPlugin().SessionHealth(context.Background())
	if got.Connected || got.Live || got.Reachable {
		t.Fatalf("nothing is open before Connect, got %+v", got)
	}
	if got.StaleAfter != acp2StaleAfter {
		t.Fatalf("StaleAfter = %v, want the ACP2 window %v", got.StaleAfter, acp2StaleAfter)
	}
}

func TestSessionHealthReadsTheSession(t *testing.T) {
	p := newHealthPlugin()
	s := &Session{logger: slog.Default()}
	p.Opened("tcp", "10.0.0.1", 2072, s)

	if got := p.SessionHealth(context.Background()); !got.Connected || got.Live {
		t.Fatalf("open with nothing received yet, got %+v", got)
	}

	now := time.Now()
	s.lastRxNS.Store(now.UnixNano())
	s.lastTxNS.Store(now.UnixNano())
	got := p.SessionHealth(context.Background())
	if !got.Live || !got.Reachable {
		t.Fatalf("a fresh frame makes the session live and reachable, got %+v", got)
	}
	if !got.LastRx.Equal(now) || !got.LastTx.Equal(now) {
		t.Fatalf("instants must come from the Session, got %v / %v", got.LastRx, got.LastTx)
	}
}

func TestSessionHealthAfterDisconnect(t *testing.T) {
	p := newHealthPlugin()
	p.Opened("tcp", "10.0.0.1", 2072, &Session{logger: slog.Default()})
	p.Closed()
	if got := p.SessionHealth(context.Background()); got.Connected {
		t.Fatalf("Connected must be false once the session is closed, got %+v", got)
	}
}

func TestSessionHealth_IsLiveAt_HelperPure(t *testing.T) {
	now := time.Now()
	h := consumer.SessionHealth{
		LastRx:     now.Add(-30 * time.Second),
		StaleAfter: 90 * time.Second,
	}
	if !h.IsLiveAt(now) {
		t.Fatal("IsLiveAt = false at -30s with 90s window, want true")
	}
	if h.IsLiveAt(now.Add(2 * time.Minute)) {
		t.Fatal("IsLiveAt = true at +2m with 90s window, want false")
	}
}

func TestGetSlotInfo_IsOnlineTruthTable(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name       string
		status     consumer.SlotStatus
		lastRxAgo  time.Duration
		wantOnline bool
		wantState  consumer.SlotState
	}{
		{"present-and-live", consumer.SlotPresent, 1 * time.Second, true, consumer.SlotStatePresent},
		{"present-but-silent", consumer.SlotPresent, 2 * time.Hour, false, consumer.SlotStatePresent},
		{"removed-but-live", consumer.SlotRemoved, 1 * time.Second, false, consumer.SlotStateRemoved},
		{"no-card-but-live", consumer.SlotNoCard, 1 * time.Second, false, consumer.SlotStateNoCard},
		{"error-but-live", consumer.SlotError, 1 * time.Second, false, consumer.SlotStateError},
		{"boot-but-live", consumer.SlotBootMode, 1 * time.Second, false, consumer.SlotStateBoot},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &Plugin{logger: slog.Default()}
			s := &Session{
				logger:       slog.Default(),
				slotStatus:   []consumer.SlotStatus{tc.status, tc.status}, // slot 0 + 1
				slotLastSeen: []time.Time{now, now},
			}
			s.lastRxNS.Store(now.Add(-tc.lastRxAgo).UnixNano())
			p.session = s

			si, err := p.GetSlotInfo(context.Background(), 1)
			if err != nil {
				t.Fatalf("GetSlotInfo: %v", err)
			}
			if si.State != tc.wantState {
				t.Errorf("State = %q, want %q", si.State, tc.wantState)
			}
			if si.IsOnline != tc.wantOnline {
				t.Errorf("IsOnline = %v, want %v (state=%q rxAgo=%v)",
					si.IsOnline, tc.wantOnline, si.State, tc.lastRxAgo)
			}
			if si.LiveAt.IsZero() {
				t.Errorf("LiveAt is zero, want non-zero (handshake should have set it)")
			}
		})
	}
}

func TestSessionMarkSlotProbed_ExtendsTables(t *testing.T) {
	s := &Session{logger: slog.Default()}
	st := consumer.SlotPresent
	s.MarkSlotProbed(5, &st)
	if len(s.slotStatus) < 6 {
		t.Fatalf("slotStatus len = %d, want >= 6 after probing slot 5", len(s.slotStatus))
	}
	if s.slotStatus[5] != consumer.SlotPresent {
		t.Fatalf("slotStatus[5] = %v, want SlotPresent", s.slotStatus[5])
	}
	if s.slotLastSeen[5].IsZero() {
		t.Fatal("slotLastSeen[5] is zero, want recent")
	}
}
