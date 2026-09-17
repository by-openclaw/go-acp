package probelsw02p

import (
	"testing"
	"time"
)

// The dead-man window is 3x the poll spacing, floored so scheduling jitter on
// a loaded host cannot tear down a healthy link.
func TestIdleWindowFor(t *testing.T) {
	tests := []struct {
		name    string
		spacing time.Duration
		want    time.Duration
	}{
		{"below the floor takes the floor", time.Second, minIdleWindow},
		{"default spacing takes the floor", DefaultAppKeepaliveSpacing, minIdleWindow},
		{"above the floor scales 3x", 30 * time.Second, 90 * time.Second},
		{"well above the floor scales 3x", time.Minute, 3 * time.Minute},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := idleWindowFor(tc.spacing); got != tc.want {
				t.Errorf("idleWindowFor(%v) = %v, want %v", tc.spacing, got, tc.want)
			}
		})
	}
}

// Whatever the spacing, the window must leave room for several missed polls.
func TestIdleWindowAllowsMissedPolls(t *testing.T) {
	for _, s := range []time.Duration{time.Second, 10 * time.Second, time.Minute} {
		if w := idleWindowFor(s); w < 3*s {
			t.Errorf("spacing %v -> window %v allows fewer than 3 polls", s, w)
		}
	}
}
