package main

import "testing"

// fakeUnit stands in for a gateway that has a unit address.
type fakeUnit struct {
	got uint8
	set bool
}

func (f *fakeUnit) SetUnit(u uint8) { f.got, f.set = u, true }

func TestTheUnitIsSetWhenOneIsAskedFor(t *testing.T) {
	// The IQ frame is unit 12. An emulation of it answers there too.
	f := &fakeUnit{}
	if err := applyUnit(f, 12); err != nil {
		t.Fatalf("applyUnit: %v", err)
	}
	if !f.set || f.got != 12 {
		t.Errorf("unit = %d (set %v), want 12", f.got, f.set)
	}
}

func TestTheUnitIsLeftAloneWhenNoneIsAskedFor(t *testing.T) {
	f := &fakeUnit{}
	if err := applyUnit(f, -1); err != nil {
		t.Fatalf("applyUnit: %v", err)
	}
	if f.set {
		t.Error("the unit was changed though none was asked for")
	}
}

func TestAUnitNoGatewayCanAnswerAsIsRefused(t *testing.T) {
	// Zero is the broadcast address; the rest are wider than the byte a unit
	// travels in.
	for _, u := range []int{0, 256, -2} {
		if err := applyUnit(&fakeUnit{}, u); err == nil {
			t.Errorf("unit %d was accepted", u)
		}
	}
}

func TestAUnitOnAProtocolThatHasNone(t *testing.T) {
	if err := applyUnit(struct{}{}, 12); err == nil {
		t.Error("a unit was set on a protocol with no unit address")
	}
}
