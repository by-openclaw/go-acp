package router

import "testing"

// TestRoutingInterfaceIsFixed pins the one table that is not found by
// arithmetic. Everything else in the command set is located from values read
// here, so a wrong number in this block makes the whole interface undiscoverable.
func TestRoutingInterfaceIsFixed(t *testing.T) {
	want := map[Command]string{
		100: "CMD_INTERFACE_VERSION",
		101: "CMD_ROUTER_NAME",
		102: "CMD_NUM_MATRICES",
		103: "CMD_MATRIX_BASE",
		104: "CMD_MATRIX_STEP",
		105: "CMD_NUM_CATEGORIES",
		106: "CMD_CATEGORY_BASE",
		107: "CMD_CATEGORY_STEP",
		108: "CMD_ASSOC_MAKE_ROUTE",
		109: "CMD_NUM_TRACK_TEMPLATES",
		110: "CMD_GET_TRACK_TEMPLATE",
		111: "CMD_NUM_AUDIO_GROUPS",
		112: "CMD_GET_AUDIO_GROUP",
		113: "CMD_NUM_SALVOS",
		114: "CMD_SALVO_NAMES_8_FILENAME",
		115: "CMD_SALVO_NAMES_32_FILENAME",
		116: "CMD_FIRE_SALVO",
		117: "CMD_NUM_DEVICES",
		118: "CMD_DEVICE_NAMES_FILENAME",
		119: "CMD_GET_AHP_NODE",
	}
	got := map[Command]string{
		CmdInterfaceVersion:  "CMD_INTERFACE_VERSION",
		CmdRouterName:        "CMD_ROUTER_NAME",
		CmdNumMatrices:       "CMD_NUM_MATRICES",
		CmdMatrixBase:        "CMD_MATRIX_BASE",
		CmdMatrixStep:        "CMD_MATRIX_STEP",
		CmdNumCategories:     "CMD_NUM_CATEGORIES",
		CmdCategoryBase:      "CMD_CATEGORY_BASE",
		CmdCategoryStep:      "CMD_CATEGORY_STEP",
		CmdAssocMakeRoute:    "CMD_ASSOC_MAKE_ROUTE",
		CmdNumTrackTemplates: "CMD_NUM_TRACK_TEMPLATES",
		CmdGetTrackTemplate:  "CMD_GET_TRACK_TEMPLATE",
		CmdNumAudioGroups:    "CMD_NUM_AUDIO_GROUPS",
		CmdGetAudioGroup:     "CMD_GET_AUDIO_GROUP",
		CmdNumSalvos:         "CMD_NUM_SALVOS",
		CmdSalvoNames8File:   "CMD_SALVO_NAMES_8_FILENAME",
		CmdSalvoNames32File:  "CMD_SALVO_NAMES_32_FILENAME",
		CmdFireSalvo:         "CMD_FIRE_SALVO",
		CmdNumDevices:        "CMD_NUM_DEVICES",
		CmdDeviceNamesFile:   "CMD_DEVICE_NAMES_FILENAME",
		CmdGetAHPNode:        "CMD_GET_AHP_NODE",
	}

	if len(got) != len(want) {
		t.Fatalf("the root table has %d entries, want %d", len(got), len(want))
	}
	for c, name := range want {
		if got[c] != name {
			t.Errorf("command %d is %q, want %q", c, got[c], name)
		}
	}
}

// TestIsRoutingInterface covers the check a client uses to tell a routing
// command from an ordinary one. Command 100 is the probe: a unit that answers
// NACK to it has no routing interface, which is how the audit oracle was
// identified as a non-router.
func TestIsRoutingInterface(t *testing.T) {
	tests := []struct {
		cmd  Command
		want bool
	}{
		{99, false},
		{100, true},
		{110, true},
		{119, true},
		{120, false},
		{0, false},
		{0x10000, false},
	}
	for _, tc := range tests {
		if got := IsRoutingInterface(tc.cmd); got != tc.want {
			t.Errorf("IsRoutingInterface(%d) = %v, want %v", tc.cmd, got, tc.want)
		}
	}
}

// TestTable_Command is the arithmetic the whole command set rests on:
//
//	MatrixBase(m) = CMD_MATRIX_BASE + (m-1) * CMD_MATRIX_STEP
//
// The specification's own suggested allocation is matrix bases at 0x10000,
// 0x20000 and so on, which is the case worked here.
func TestTable_Command(t *testing.T) {
	matrices := Table{Base: 0x10000, Step: 0x10000, Count: 3}

	tests := []struct {
		n    uint32
		want Command
		ok   bool
	}{
		{0, 0, false}, // entities count from 1
		{1, 0x10000, true},
		{2, 0x20000, true},
		{3, 0x30000, true},
		{4, 0, false}, // past Count
	}
	for _, tc := range tests {
		got, ok := matrices.Command(tc.n)
		if ok != tc.ok || got != tc.want {
			t.Errorf("Command(%d) = %#x,%v want %#x,%v", tc.n, got, ok, tc.want, tc.ok)
		}
	}
}

// TestTable_NestedArithmetic walks the three levels of the command space the
// way a client does: read the root, find a matrix, find a level in it, find a
// destination on that level, then name one of that destination's fields.
func TestTable_NestedArithmetic(t *testing.T) {
	// A controller with three matrices, matrix 2 holding four levels, level 3
	// holding 1024 destinations.
	matrices := Table{Base: 0x10000, Step: 0x10000, Count: 3}

	matrix2, ok := matrices.Command(2)
	if !ok || matrix2 != 0x20000 {
		t.Fatalf("matrix 2 base = %#x,%v", matrix2, ok)
	}
	// The matrix publishes its level table through fields at its own base.
	if got, ok := matrices.Field(2, OffLevelBase); !ok || got != 0x20002 {
		t.Fatalf("CMD_LEVEL_BASE of matrix 2 = %#x,%v want 0x20002", got, ok)
	}

	levels := Table{Base: 0x21000, Step: 0x400, Count: 4}
	level3, ok := levels.Command(3)
	if !ok || level3 != 0x21800 {
		t.Fatalf("level 3 base = %#x,%v want 0x21800", level3, ok)
	}

	dests := Table{Base: 0x22000, Step: 8, Count: 1024}
	dest40, ok := dests.Command(40)
	if !ok || dest40 != 0x22000+39*8 {
		t.Fatalf("destination 40 base = %#x,%v", dest40, ok)
	}

	routed, ok := dests.Field(40, OffDestRoutedSrc)
	if !ok || routed != dest40+OffDestRoutedSrc {
		t.Fatalf("routed-source command = %#x,%v", routed, ok)
	}

	// And back again: a command arriving on the back channel has to be
	// resolved to the destination it belongs to.
	n, off, ok := dests.Index(routed)
	if !ok || n != 40 || off != OffDestRoutedSrc {
		t.Errorf("Index(%#x) = %d,%d,%v want 40,%d,true", routed, n, off, ok, OffDestRoutedSrc)
	}
}

// TestTable_ScaleTarget checks the arithmetic holds at the sizes the root
// project mandates: a 65535-destination level does not overflow the command
// space when the controller allocates a sensible base.
func TestTable_ScaleTarget(t *testing.T) {
	dests := Table{Base: 0x0100_0000, Step: DstTableSize, Count: 65535}

	last, ok := dests.Command(65535)
	if !ok {
		t.Fatal("the last destination of a full level must be addressable")
	}
	if want := Command(0x0100_0000 + 65534*DstTableSize); last != want {
		t.Errorf("last destination = %#x, want %#x", last, want)
	}
	if _, ok := dests.Command(65536); ok {
		t.Error("one past the level must not resolve")
	}

	// Every destination resolves back to itself, which is what makes a
	// back-channel tally usable.
	for _, n := range []uint32{1, 2, 1000, 65534, 65535} {
		c, ok := dests.Field(n, OffDestRoutedSrc)
		if !ok {
			t.Fatalf("destination %d did not resolve", n)
		}
		back, off, ok := dests.Index(c)
		if !ok || back != n || off != OffDestRoutedSrc {
			t.Errorf("destination %d round trips to %d,%d,%v", n, back, off, ok)
		}
	}
}

// TestTable_FieldRespectsTheStep covers a controller whose step is smaller than
// the table it documents. Reading past the step would land on the next
// entity's fields, so it has to be refused rather than silently wrong.
func TestTable_FieldRespectsTheStep(t *testing.T) {
	// Three fields per destination, but the destination table has eight.
	cramped := Table{Base: 1000, Step: 3, Count: 10}

	if got, ok := cramped.Field(2, 2); !ok || got != 1005 {
		t.Errorf("Field(2,2) = %d,%v want 1005,true", got, ok)
	}
	if _, ok := cramped.Field(2, 3); ok {
		t.Error("an offset at the step must not resolve: it is the next entity")
	}
	if _, ok := cramped.Field(2, OffDestProtect); ok {
		t.Error("an offset past the step must not resolve")
	}
	if _, ok := cramped.Field(99, 0); ok {
		t.Error("an out-of-range entity must not resolve")
	}
}

func TestTable_Empty(t *testing.T) {
	var empty Table

	if _, ok := empty.Command(1); ok {
		t.Error("an empty table resolves nothing")
	}
	if empty.Contains(0) {
		t.Error("an empty table contains nothing")
	}
	if _, _, ok := empty.Index(0); ok {
		t.Error("an empty table indexes nothing")
	}

	// A table with a count but no step is malformed: every entity would sit
	// at the same command.
	noStep := Table{Base: 100, Count: 4}
	if noStep.Contains(100) {
		t.Error("a table with no step spans nothing")
	}
	// Field still resolves, because a zero step means the caller has not told
	// us a bound to check against.
	if got, ok := noStep.Field(1, 2); !ok || got != 102 {
		t.Errorf("Field(1,2) = %d,%v want 102,true", got, ok)
	}
}

func TestTable_Contains(t *testing.T) {
	tbl := Table{Base: 1000, Step: 10, Count: 3} // 1000..1029

	tests := []struct {
		cmd  Command
		want bool
	}{
		{999, false},
		{1000, true},
		{1029, true},
		{1030, false},
	}
	for _, tc := range tests {
		if got := tbl.Contains(tc.cmd); got != tc.want {
			t.Errorf("Contains(%d) = %v, want %v", tc.cmd, got, tc.want)
		}
	}
	if _, _, ok := tbl.Index(1030); ok {
		t.Error("Index must refuse a command outside the span")
	}
}

// TestTableSizes records how many commands each sub-table occupies, which is
// the minimum step a controller may allocate without its entities overlapping.
func TestTableSizes(t *testing.T) {
	tests := []struct {
		name string
		size int
		last int
	}{
		{"matrix", MatrixTableSize, OffAssocMappingsFile},
		{"level", LevelTableSize, OffSrcDstMCDataFile},
		{"source", SrcTableSize, OffSrcAltName},
		{"destination", DstTableSize, OffDestMCProtect},
	}
	for _, tc := range tests {
		if tc.size != tc.last+1 {
			t.Errorf("%s table is %d commands but its last offset is %d",
				tc.name, tc.size, tc.last)
		}
	}
}

// TestInterfaceVersions records which revision introduced each group of
// commands. A client reads the version first and must not send a command the
// controller predates.
func TestInterfaceVersions(t *testing.T) {
	if VersionInterfaceVersion != 3 {
		t.Errorf("the version command arrived in revision %d, want 3", VersionInterfaceVersion)
	}
	// Ordering is what a client compares against, so it has to be monotonic.
	order := []int{
		VersionMakeRoute, VersionInterfaceVersion, VersionProtectID16,
		VersionSalvos, VersionDeviceNames, VersionAHPNode, VersionRouteErrors,
	}
	for i := 1; i < len(order); i++ {
		if order[i-1] >= order[i] {
			t.Errorf("revision %d is not below %d", order[i-1], order[i])
		}
	}
}
