package router

import "testing"

// The numbers below are the ones a vendor Centra publishes on Level 1 of its
// Matrix 1, read off its menu rather than taken from a document: the level
// interface predates the tables in commands.go and is not written down
// anywhere we have.

func TestTheLevelCommandsAreTheNumbersTheVendorUses(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  Command
		want Command
	}{
		{"selected destination", LvlDestSelect, 100},
		{"source name index", LvlSrcNameIndex, 101},
		{"dest name index", LvlDstNameIndex, 102},
		{"selected source", LvlSrcSelect, 110},
		{"source name", LvlSrcName, 111},
		{"dest name", LvlDstName, 112},
		{"dest protect", LvlDestProtect, 113},
		{"take mode", LvlTakeMode, 120},
		{"take", LvlTake, 121},
		{"cancel", LvlCancel, 122},
		{"source count", LvlSourceCount, 130},
		{"dest count", LvlDestCount, 131},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}

func TestTheLevelSetOverlapsTheTablesAndMeansSomethingElse(t *testing.T) {
	// This is the whole reason a level is served on a node of its own. Command
	// 100 is the interface version to a controller and the selected
	// destination to a level, and nothing on the wire tells them apart.
	if LvlDestSelect != CmdInterfaceVersion {
		t.Error("the two sets no longer collide, which would make this note stale")
	}
	if LvlSrcSelect != CmdGetTrackTemplate {
		t.Error("110 was the track template in the tables and the source list on a level")
	}
	if LvlDestProtect != CmdNumSalvos {
		t.Error("113 was the salvo count in the tables and the protect on a level")
	}
}

func TestDirectRoutingIsOneCommandPerDestination(t *testing.T) {
	if got := LvlRoute(1); got != 10001 {
		t.Errorf("destination 1 routes on %d, want 10001", got)
	}
	if got := LvlRoute(1450); got != 11450 {
		t.Errorf("destination 1450 routes on %d, want 11450", got)
	}
	if got := LvlProtect(1); got != 20001 {
		t.Errorf("destination 1 protects on %d, want 20001", got)
	}
	if got := LvlProtect(1450); got != 21450 {
		t.Errorf("destination 1450 protects on %d, want 21450", got)
	}
}

func TestTellingTheDirectBlocksApart(t *testing.T) {
	for _, tc := range []struct {
		name string
		cmd  Command
		dest int
		ok   bool
	}{
		{"the first route", 10001, 1, true},
		{"a route far up the block", 11450, 1450, true},
		{"the base itself is no destination", LvlRouteBase, 0, false},
		{"a command below the block", 999, 0, false},
		{"a protect is not a route", 20001, 0, false},
	} {
		dest, ok := IsLevelRoute(tc.cmd)
		if ok != tc.ok || dest != tc.dest {
			t.Errorf("%s: IsLevelRoute(%d) = %d, %v; want %d, %v",
				tc.name, tc.cmd, dest, ok, tc.dest, tc.ok)
		}
	}

	for _, tc := range []struct {
		name string
		cmd  Command
		dest int
		ok   bool
	}{
		{"the first protect", 20001, 1, true},
		{"a protect far up the block", 21450, 1450, true},
		{"the base itself is no destination", LvlProtectBase, 0, false},
		{"a route is not a protect", 10001, 0, false},
		{"past the block", LvlProtectBase + maxLevelDests, 0, false},
	} {
		dest, ok := IsLevelProtect(tc.cmd)
		if ok != tc.ok || dest != tc.dest {
			t.Errorf("%s: IsLevelProtect(%d) = %d, %v; want %d, %v",
				tc.name, tc.cmd, dest, ok, tc.dest, tc.ok)
		}
	}
}

func TestTheMonitorReadouts(t *testing.T) {
	// Four monitors, five readouts each, laid out as one base per readout with
	// the monitor number added: the vendor's template draws 401 to 404 in the
	// first row and 441 to 444 in the last.
	if LvlMonitors != 4 {
		t.Errorf("a level publishes %d monitors, want the vendor's four", LvlMonitors)
	}
	for _, tc := range []struct {
		base Command
		mon  int
		want Command
	}{
		{LvlMonKind, 1, 401},
		{LvlMonKind, 4, 404},
		{LvlMonIndex, 1, 411},
		{LvlMonName, 2, 422},
		{LvlMonSrcAddr, 3, 433},
		{LvlMonDstAddr, 4, 444},
	} {
		if got := LvlMonitor(tc.base, tc.mon); got != tc.want {
			t.Errorf("monitor %d on base %d = %d, want %d", tc.mon, tc.base, got, tc.want)
		}
	}
}

func TestTheTwoStateConventions(t *testing.T) {
	// A checkbox on this interface counts from one. A client that writes zero
	// to a protect has written nothing the level recognises.
	if ProtectOff != 1 || ProtectOn != 2 {
		t.Errorf("protect is %d..%d, want the vendor's 1..2", ProtectOff, ProtectOn)
	}
	if TakeImmediate != 0 || TakeOnButton != 1 {
		t.Errorf("take mode is %d/%d, want 0/1", TakeImmediate, TakeOnButton)
	}
}

func TestTellingTheThreePerEntityBlocksApart(t *testing.T) {
	// A level carries three blocks keyed by entity: what is routed to a
	// destination, whether it is protected, and where a source comes from. A
	// command falling into the wrong one would move a crosspoint when a panel
	// asked for a label.
	if got := LvlRefSource(1); got != 30001 {
		t.Errorf("source 1's reference is on %d, want 30001", got)
	}
	if got := LvlRefSource(1450); got != 31450 {
		t.Errorf("source 1450's reference is on %d, want 31450", got)
	}

	for _, tc := range []struct {
		name   string
		cmd    Command
		source int
		ok     bool
	}{
		{"the first reference", 30001, 1, true},
		{"a reference far up the block", 31450, 1450, true},
		{"the base itself is no source", LvlRefSourceBase, 0, false},
		{"a route is not a reference", 10001, 0, false},
		{"a protect is not a reference", 20001, 0, false},
		{"past the block", LvlRefSourceBase + maxLevelDests, 0, false},
	} {
		src, ok := IsLevelRefSource(tc.cmd)
		if ok != tc.ok || src != tc.source {
			t.Errorf("%s: IsLevelRefSource(%d) = %d, %v; want %d, %v",
				tc.name, tc.cmd, src, ok, tc.source, tc.ok)
		}
	}
}
