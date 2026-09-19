package usm

import (
	"testing"
	"time"

	"dhs/internal/clock"
)

func TestNewRemoteEngineTracksPeerClock(t *testing.T) {
	fk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	id, err := NewEngineID(Enterprise, "agent")
	if err != nil {
		t.Fatal(err)
	}
	e, err := NewRemoteEngine(id, 7, 5000, fk)
	if err != nil {
		t.Fatal(err)
	}
	if e.Boots() != 7 {
		t.Errorf("boots = %d, want 7", e.Boots())
	}
	if got := e.Time(); got != 5000 {
		t.Errorf("time at discovery = %d, want 5000", got)
	}
	fk.Advance(30 * time.Second)
	if got := e.Time(); got != 5030 {
		t.Errorf("time after 30s = %d, want 5030", got)
	}
}

func TestNewRemoteEngineRejectsNegativeTime(t *testing.T) {
	id, _ := NewEngineID(Enterprise, "agent")
	if _, err := NewRemoteEngine(id, 0, -1, clock.NewFake(time.Time{})); err == nil {
		t.Error("negative engine time should error")
	}
}

func TestNewRemoteEngineRejectsBadEngineID(t *testing.T) {
	// A too-short engine ID fails inside NewEngine and the error surfaces.
	if _, err := NewRemoteEngine([]byte{1, 2}, 0, 0, clock.NewFake(time.Time{})); err == nil {
		t.Error("a 2-byte engine ID should be rejected")
	}
}
