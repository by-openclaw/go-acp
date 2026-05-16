package acp2

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"dhs/internal/consumer"
)

func TestSessionHealth_NoSession(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	h := p.SessionHealth(context.Background())
	if h.Connected || h.Live || h.Reachable {
		t.Fatalf("SessionHealth with nil session must be all-false, got %+v", h)
	}
	if h.StaleAfter != acp2StaleAfter {
		t.Fatalf("StaleAfter = %v, want %v", h.StaleAfter, acp2StaleAfter)
	}
}

func TestSessionHealth_ConnectedButNoRx(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	p.session = &Session{logger: slog.Default()}
	h := p.SessionHealth(context.Background())
	if !h.Connected {
		t.Fatal("Connected = false with non-nil session, want true")
	}
	if h.Live {
		t.Fatal("Live = true with no rx, want false")
	}
	if !h.LastRx.IsZero() {
		t.Fatalf("LastRx = %v, want zero", h.LastRx)
	}
}

func TestSessionHealth_ConnectedRecentRx(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	s := &Session{logger: slog.Default()}
	now := time.Now()
	s.lastRxNS.Store(now.UnixNano())
	p.session = s
	h := p.SessionHealth(context.Background())
	if !h.Connected || !h.Live {
		t.Fatalf("Connected/Live = (%v,%v) with fresh rx, want (true,true)", h.Connected, h.Live)
	}
}

func TestSessionHealth_ConnectedStaleRx(t *testing.T) {
	p := &Plugin{logger: slog.Default()}
	s := &Session{logger: slog.Default()}
	// 2 hours ago — well past acp2StaleAfter (90 s).
	s.lastRxNS.Store(time.Now().Add(-2 * time.Hour).UnixNano())
	p.session = s
	h := p.SessionHealth(context.Background())
	if !h.Connected {
		t.Fatal("Connected = false with non-nil session, want true")
	}
	if h.Live {
		t.Fatal("Live = true with 2h-old LastRx, want false")
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

func TestProbeReachable_NotListening(t *testing.T) {
	// Pick a definitely-closed port. 127.0.0.1:1 isn't open under any
	// reasonable test environment.
	ok := probeReachable(context.Background(), "127.0.0.1", 1)
	if ok {
		t.Fatal("probeReachable to 127.0.0.1:1 = true, want false")
	}
}

func TestGetSlotInfo_IsOnlineTruthTable(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name        string
		status      consumer.SlotStatus
		lastRxAgo   time.Duration
		wantOnline  bool
		wantState   consumer.SlotState
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
