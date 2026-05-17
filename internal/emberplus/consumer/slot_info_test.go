package emberplus

import (
	"context"
	"testing"

	"dhs/internal/consumer"
)

// TestGetSlotInfo_OnlineFollowsSession pins #458: the dhs-internal
// SlotInfo.IsOnline must track p.sessionConnected. Pre-#458
// GetSlotInfo returned the Go zero value (`false`) regardless of
// liveness, so `dhs consumer emberplus info <host>` always printed
// `online=false` even against a healthy connected provider.
//
// Ember+ has no slot hot-plug concept — the single virtual slot 0
// follows the session bool 1:1.
func TestGetSlotInfo_OnlineFollowsSession(t *testing.T) {
	cases := []struct {
		name      string
		connected bool
		want      bool
	}{
		{"disconnected -> online=false", false, false},
		{"connected -> online=true", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &Plugin{}
			p.sessionConnected = tc.connected

			si, err := p.GetSlotInfo(context.Background(), 0)
			if err != nil {
				t.Fatalf("GetSlotInfo: %v", err)
			}
			if si.IsOnline != tc.want {
				t.Errorf("IsOnline = %v, want %v (sessionConnected=%v)", si.IsOnline, tc.want, tc.connected)
			}
			if si.Status != consumer.SlotPresent {
				t.Errorf("Status = %v, want SlotPresent", si.Status)
			}
			if si.State != consumer.SlotStatePresent {
				t.Errorf("State = %q, want SlotStatePresent", si.State)
			}
			if si.Slot != 0 {
				t.Errorf("Slot = %d, want 0", si.Slot)
			}
		})
	}
}

// TestGetSlotInfo_RejectsNonZeroSlot keeps the existing only-slot-0
// guard from regressing when the IsOnline bit is added.
func TestGetSlotInfo_RejectsNonZeroSlot(t *testing.T) {
	p := &Plugin{}
	p.sessionConnected = true

	if _, err := p.GetSlotInfo(context.Background(), 1); err == nil {
		t.Fatal("GetSlotInfo(slot=1): expected error, got nil")
	}
}
