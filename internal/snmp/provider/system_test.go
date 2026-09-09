package provider

// The system group is the first thing any manager reads and how an NMS
// decides what a device IS. Every object in it is mandatory, three of
// them are writable, and one of them is a clock in units that are easy
// to get wrong.

import (
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/mib"
)

func systemTree(t *testing.T, info SystemInfo, clk clock.Clock) *MIB {
	t.Helper()
	m := NewMIB()
	if err := m.Register(SystemGroup(info, clk)...); err != nil {
		t.Fatal(err)
	}
	return m
}

// All seven, because all seven are mandatory: an agent that omits
// sysObjectID.0 is a device an NMS cannot classify however much else it
// serves.
func TestTheSystemGroupIsComplete(t *testing.T) {
	m := systemTree(t, SystemInfo{
		Descr:    "dhs SNMP agent",
		ObjectID: mib.SnellWilcox,
		Contact:  "ops@example.invalid",
		Name:     "frame-12",
		Location: "TEC RACK 23",
	}, clock.NewFake(time.Unix(1_700_000_000, 0)))

	for _, want := range []codec.OID{
		mib.SysDescr, mib.SysObjectID, mib.SysUpTime,
		mib.SysContact, mib.SysName, mib.SysLocation, mib.SysServices,
	} {
		if _, ok := m.Get(want); !ok {
			t.Errorf("%s is missing", mib.Name(want))
		}
	}
	if m.Len() != 7 {
		t.Errorf("%d objects, want the group's 7", m.Len())
	}

	obj, _ := m.Get(mib.SysObjectID)
	if obj.Get().OID.Compare(mib.SnellWilcox) != 0 {
		t.Errorf("sysObjectID.0 = %s", obj.Get())
	}
	obj, _ = m.Get(mib.SysName)
	if obj.Get().String() != "frame-12" {
		t.Errorf("sysName.0 = %s", obj.Get())
	}
}

// sysUpTime is TimeTicks: HUNDREDTHS of a second. An agent reporting
// seconds looks a hundred times younger than it is, and every trap it
// stamps is wrong — which is what a receiver correlates on.
func TestUptimeIsInHundredthsOfASecond(t *testing.T) {
	clk := clock.NewFake(time.Unix(1_700_000_000, 0))
	m := systemTree(t, SystemInfo{}, clk)

	obj, _ := m.Get(mib.SysUpTime)
	if obj.Get().Type != codec.TypeTimeTicks {
		t.Fatalf("sysUpTime.0 is a %s", obj.Get().Type)
	}
	if got := obj.Get().Uint; got != 0 {
		t.Errorf("a fresh agent = %d ticks", got)
	}

	clk.Advance(90 * time.Second)
	if got := obj.Get().Uint; got != 9000 {
		t.Errorf("after 90s = %d ticks, want 9000", got)
	}

	// And the helper a trap uses reads the same clock, so an event and a
	// poll a second apart cannot disagree about when they happened.
	if got := Uptime(m); got != 9000 {
		t.Errorf("Uptime = %d", got)
	}
}

// A tree with no system group has no uptime to report, and says zero
// rather than panicking on a trap.
func TestUptimeOfATreeWithoutIt(t *testing.T) {
	if got := Uptime(NewMIB()); got != 0 {
		t.Errorf("= %d", got)
	}
}

// Three of the seven are read-write per RFC 1213, and this agent honours
// that — an operator setting sysLocation from their NMS is the ordinary
// case.
func TestTheThreeWritableObjects(t *testing.T) {
	m := systemTree(t, SystemInfo{Contact: "before", Name: "before", Location: "before"}, nil)

	for _, o := range []codec.OID{mib.SysContact, mib.SysName, mib.SysLocation} {
		obj, ok := m.Get(o)
		if !ok {
			t.Fatalf("%s is missing", mib.Name(o))
		}
		if obj.Access != ReadWrite {
			t.Errorf("%s is %s, want read-write", mib.Name(o), obj.Access)
		}
		if err := obj.Set(codec.String("after")); err != nil {
			t.Fatalf("%s: %v", mib.Name(o), err)
		}
		if got := obj.Get().String(); got != "after" {
			t.Errorf("%s = %q after a write", mib.Name(o), got)
		}
	}

	// And the rest are not.
	for _, o := range []codec.OID{mib.SysDescr, mib.SysObjectID, mib.SysUpTime, mib.SysServices} {
		obj, _ := m.Get(o)
		if obj.Access != ReadOnly {
			t.Errorf("%s is %s, want read-only", mib.Name(o), obj.Access)
		}
	}
}

// The defaults are what a device with nothing configured reports, and
// they have to be valid rather than empty: an NMS classifying on
// sysObjectID.0 needs a value there.
func TestTheDefaults(t *testing.T) {
	m := systemTree(t, SystemInfo{}, nil)

	obj, _ := m.Get(mib.SysServices)
	if obj.Get().Int != mib.SysServicesApplication {
		t.Errorf("sysServices.0 = %s, want the application-layer default", obj.Get())
	}
	obj, _ = m.Get(mib.SysObjectID)
	if obj.Get().OID.Compare(mib.DHS) != 0 {
		t.Errorf("sysObjectID.0 = %s, want our own sub-tree", obj.Get())
	}

	// A nil clock is the system clock, not a panic on the first read.
	obj, _ = m.Get(mib.SysUpTime)
	_ = obj.Get()
}

// An explicit sysServices survives, because a device that is more than a
// gateway has to be able to say so.
func TestAnExplicitSysServices(t *testing.T) {
	m := systemTree(t, SystemInfo{Services: 72}, nil)
	obj, _ := m.Get(mib.SysServices)
	if obj.Get().Int != 72 {
		t.Errorf("= %s", obj.Get())
	}
}

// A SET arrives on the read loop and a GET may be answered from
// elsewhere, so the writable strings are behind a lock. Run under -race,
// this is the test that says so.
func TestTheWritableObjectsAreSafeUnderConcurrency(t *testing.T) {
	m := systemTree(t, SystemInfo{Contact: "start"}, nil)
	obj, _ := m.Get(mib.SysContact)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			_ = obj.Set(codec.String("written"))
		}
	}()
	for i := 0; i < 200; i++ {
		_ = obj.Get()
	}
	<-done
}
