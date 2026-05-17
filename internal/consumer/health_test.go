package consumer

import (
	"testing"
	"time"
)

func TestSessionHealth_IsLiveAt(t *testing.T) {
	now := time.Date(2026, 5, 5, 18, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		h    SessionHealth
		want bool
	}{
		{
			name: "fresh-rx-within-window",
			h: SessionHealth{
				LastRx:     now.Add(-10 * time.Second),
				StaleAfter: 90 * time.Second,
			},
			want: true,
		},
		{
			name: "rx-just-over-window",
			h: SessionHealth{
				LastRx:     now.Add(-91 * time.Second),
				StaleAfter: 90 * time.Second,
			},
			want: false,
		},
		{
			name: "rx-exactly-at-window-boundary",
			h: SessionHealth{
				LastRx:     now.Add(-90 * time.Second),
				StaleAfter: 90 * time.Second,
			},
			want: true,
		},
		{
			name: "no-rx-ever",
			h: SessionHealth{
				StaleAfter: 90 * time.Second,
			},
			want: false,
		},
		{
			name: "zero-stale-after-treated-as-not-live",
			h: SessionHealth{
				LastRx:     now,
				StaleAfter: 0,
			},
			want: false,
		},
		{
			name: "negative-stale-after-treated-as-not-live",
			h: SessionHealth{
				LastRx:     now,
				StaleAfter: -1 * time.Second,
			},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.h.IsLiveAt(now)
			if got != tc.want {
				t.Fatalf("IsLiveAt = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSessionHealth_LiveDecaysOverTime(t *testing.T) {
	t0 := time.Date(2026, 5, 5, 18, 0, 0, 0, time.UTC)
	h := SessionHealth{
		LastRx:     t0,
		StaleAfter: 30 * time.Second,
	}
	// Within window: live.
	if !h.IsLiveAt(t0.Add(15 * time.Second)) {
		t.Fatal("should be live at +15s")
	}
	// At boundary: live.
	if !h.IsLiveAt(t0.Add(30 * time.Second)) {
		t.Fatal("should be live at +30s (boundary)")
	}
	// Past boundary: not live.
	if h.IsLiveAt(t0.Add(31 * time.Second)) {
		t.Fatal("should be stale at +31s")
	}
	// Far past boundary: still not live.
	if h.IsLiveAt(t0.Add(10 * time.Minute)) {
		t.Fatal("should be stale at +10m")
	}
}
