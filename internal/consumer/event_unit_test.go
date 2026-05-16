package consumer

import (
	"encoding/json"
	"testing"
	"time"
)

// TestEvent_Unit_RoundTrip pins the #359 contract: the new Unit field
// on Event survives JSON encoding round-trip. Tolerates non-ASCII
// engineering units (e.g. "°C") so protocols that carry localised
// names (Ember+ Description-derived units, vendor-specific) round-trip
// cleanly.
//
// Asserts behaviour, not the literal JSON shape — Event has no json
// tags today, so the field name is Go-default ("Unit"). Adding tags
// to the entire struct is out of scope for this change.
func TestEvent_Unit_RoundTrip(t *testing.T) {
	cases := []struct{ name, unit string }{
		{"with unit", "dBFS"},
		{"empty unit", ""},
		{"non-ASCII tolerated", "°C"},
		{"per-mille edge case", "‰"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev := Event{
				Slot:      1,
				ID:        17671,
				Label:     "Audio Gain",
				Unit:      c.unit,
				Timestamp: time.Unix(0, 0).UTC(),
			}
			raw, err := json.Marshal(ev)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var rt Event
			if err := json.Unmarshal(raw, &rt); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if rt.Unit != c.unit {
				t.Errorf("round-trip Unit: got %q, want %q", rt.Unit, c.unit)
			}
		})
	}
}

// TestEvent_Unit_DefaultEmpty pins that an Event constructed without
// setting Unit (today's behaviour for protocols without unit semantics
// like Probel SW-P-08/02, OSC, TSL UMD) carries an empty string —
// downstream formatters can use that as the no-unit signal.
func TestEvent_Unit_DefaultEmpty(t *testing.T) {
	ev := Event{Slot: 0, ID: 1}
	if ev.Unit != "" {
		t.Errorf("default Event.Unit = %q, want empty string", ev.Unit)
	}
}
