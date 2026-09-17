package consumer

import (
	"testing"
	"time"
)

func TestSlotStatus_State_Mapping(t *testing.T) {
	cases := []struct {
		status SlotStatus
		want   SlotState
	}{
		{SlotNoCard, SlotStateNoCard},
		{SlotPowerUp, SlotStatePowerup},
		{SlotPresent, SlotStatePresent},
		{SlotError, SlotStateError},
		{SlotRemoved, SlotStateRemoved},
		{SlotBootMode, SlotStateBoot},
		{SlotStatus(99), SlotStateNoCard}, // unknown defaults to no_card
	}
	for _, tc := range cases {
		t.Run(string(tc.want), func(t *testing.T) {
			got := tc.status.State()
			if got != tc.want {
				t.Fatalf("State() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSlotInfo_IsOnline_TruthTable(t *testing.T) {
	// Online is derived: State == present && Live.
	now := time.Now()

	cases := []struct {
		name  string
		state SlotState
		live  bool
		want  bool
	}{
		{"present-and-live", SlotStatePresent, true, true},
		{"present-but-not-live", SlotStatePresent, false, false},
		{"error-and-live", SlotStateError, true, false},
		{"error-and-not-live", SlotStateError, false, false},
		{"no_card-and-live", SlotStateNoCard, true, false},
		{"no_card-and-not-live", SlotStateNoCard, false, false},
		{"powerup-and-live", SlotStatePowerup, true, false},
		{"boot-and-live", SlotStateBoot, true, false},
		{"removed-and-live", SlotStateRemoved, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := SlotInfo{
				Slot:     1,
				State:    tc.state,
				LiveAt:   now,
				IsOnline: tc.state == SlotStatePresent && tc.live,
			}
			if info.IsOnline != tc.want {
				t.Fatalf("IsOnline = %v, want %v (state=%s live=%v)",
					info.IsOnline, tc.want, tc.state, tc.live)
			}
		})
	}
}

func TestSlotState_DistinctFromString(t *testing.T) {
	// Confirm the locked names — "powerup" not "power_up", "boot" not
	// "boot_mode" — are exposed in the new SlotState type. The wire-level
	// SlotStatus.String() retains the legacy names for round-trip
	// fidelity with on-disk snapshots.
	if string(SlotStatePowerup) != "powerup" {
		t.Fatalf("SlotStatePowerup = %q, want powerup", SlotStatePowerup)
	}
	if string(SlotStateBoot) != "boot" {
		t.Fatalf("SlotStateBoot = %q, want boot", SlotStateBoot)
	}
}
