package main

import (
	"testing"

	rccodec "dhs/internal/snell-rollcall/codec"
)

// fakeLevelled stands in for a plugin that has a user level and a client name.
type fakeLevelled struct {
	level rccodec.UserLevel
	name  string
	set   bool
}

func (f *fakeLevelled) SetUserLevel(l rccodec.UserLevel) error { f.level, f.set = l, true; return nil }
func (f *fakeLevelled) SetName(n string)                       { f.name = n }

func TestTheUserLevelAndClientNameReachThePlugin(t *testing.T) {
	// The factory level is the one the panel's preferences hide and the one a
	// factory-gated command needs.
	f := &fakeLevelled{}
	if err := applyRollCallIdentity(f, "factory", "desk-03"); err != nil {
		t.Fatalf("applyRollCallIdentity: %v", err)
	}
	if !f.set || f.level != rccodec.LevelFactory || f.name != "desk-03" {
		t.Errorf("applied level %s name %q (set %v)", f.level, f.name, f.set)
	}
}

func TestNothingGivenIsNothingDone(t *testing.T) {
	f := &fakeLevelled{}
	if err := applyRollCallIdentity(f, "", ""); err != nil {
		t.Fatalf("applyRollCallIdentity: %v", err)
	}
	if f.set || f.name != "" {
		t.Error("something was set though nothing was asked for")
	}
	// And a protocol with neither is left alone when nothing is asked.
	if err := applyRollCallIdentity(struct{}{}, "", ""); err != nil {
		t.Errorf("a protocol without levels was refused with nothing asked: %v", err)
	}
}

func TestALevelTheProtocolCannotTakeIsRefused(t *testing.T) {
	if err := applyRollCallIdentity(struct{}{}, "factory", ""); err == nil {
		t.Error("--user-level was accepted on a protocol with no levels")
	}
	if err := applyRollCallIdentity(struct{}{}, "", "x"); err == nil {
		t.Error("--client-name was accepted on a protocol with no client name")
	}
	if err := applyRollCallIdentity(&fakeLevelled{}, "root", ""); err == nil {
		t.Error("a level that is not one of the four was accepted")
	}
}
