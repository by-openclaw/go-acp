package query

// Liveness defaults for a 24/7 Query WS subscription.
//
// The regression: the CLI passed WatchOptions{}, and a zero ReadTimeout meant
// "wait indefinitely". A quiet plant is normal, so no grain traffic proves
// nothing — and "indefinitely" also covers a half-open socket, which left the
// watch connected to nothing and reporting nothing, with no error.

import (
	"testing"
	"time"
)

func TestWatchOptionsResolve(t *testing.T) {
	tests := []struct {
		name         string
		opts         WatchOptions
		wantRead     time.Duration
		wantKeepAliv time.Duration
	}{
		{
			name:         "zero value takes the 24/7 defaults — this is the bug fix",
			opts:         WatchOptions{},
			wantRead:     DefaultReadTimeout,
			wantKeepAliv: DefaultKeepAlive,
		},
		{
			name:         "explicit values are honoured",
			opts:         WatchOptions{ReadTimeout: 5 * time.Second, KeepAlive: 2 * time.Second},
			wantRead:     5 * time.Second,
			wantKeepAliv: 2 * time.Second,
		},
		{
			name:         "negative ReadTimeout disables the deadline (deliberate opt-out)",
			opts:         WatchOptions{ReadTimeout: -1},
			wantRead:     0,
			wantKeepAliv: DefaultKeepAlive,
		},
		{
			name:         "negative KeepAlive disables pinging",
			opts:         WatchOptions{KeepAlive: -1},
			wantRead:     DefaultReadTimeout,
			wantKeepAliv: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotRead, gotKA := tc.opts.resolve()
			if gotRead != tc.wantRead {
				t.Errorf("ReadTimeout = %v, want %v", gotRead, tc.wantRead)
			}
			if gotKA != tc.wantKeepAliv {
				t.Errorf("KeepAlive = %v, want %v", gotKA, tc.wantKeepAliv)
			}
		})
	}
}

// The dead-man window must be comfortably larger than the ping cadence, or a
// single delayed pong would tear down a healthy subscription.
func TestWatchDefaultsGiveSeveralPingsPerWindow(t *testing.T) {
	if DefaultReadTimeout < 3*DefaultKeepAlive {
		t.Fatalf("DefaultReadTimeout (%v) must allow at least 3 pings of %v",
			DefaultReadTimeout, DefaultKeepAlive)
	}
}
