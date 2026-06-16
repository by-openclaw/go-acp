package codec

import "testing"

// TestCommandsCatalogueConsistency pins the catalogue helpers
// (Commands / CommandByID) against the parallel command_names.go
// helpers (CommandName / CommandIDs). All four are derived from the
// same §3.2 command table, so they must agree byte-for-byte:
//
//   - every CommandSpec.ID is known to CommandName and the Name field
//     matches CommandName exactly,
//   - every id reported by CommandIDs has a CommandSpec via CommandByID,
//   - the two sets contain exactly the same command bytes.
//
// This is the spec-derived invariant the catalogue must satisfy; a
// drift between the two tables (a command added to one switch but not
// the other) fails here.
func TestCommandsCatalogueConsistency(t *testing.T) {
	cmds := Commands()
	if len(cmds) == 0 {
		t.Fatal("Commands() returned empty catalogue")
	}

	specByID := make(map[CommandID]CommandSpec, len(cmds))
	for _, c := range cmds {
		if _, dup := specByID[c.ID]; dup {
			t.Errorf("duplicate catalogue entry for id %#x", c.ID)
		}
		specByID[c.ID] = c

		// Name must match command_names.go exactly.
		if got := CommandName(c.ID); got != c.Name {
			t.Errorf("id %#x: CommandName=%q catalogue Name=%q", c.ID, got, c.Name)
		}
		// Every supported catalogue command must have a non-empty
		// SpecRef and be flagged Supported (all shipped bytes are).
		if c.SpecRef == "" {
			t.Errorf("id %#x (%s): empty SpecRef", c.ID, c.Name)
		}
		if !c.Supported {
			t.Errorf("id %#x (%s): Supported=false; every shipped byte is supported", c.ID, c.Name)
		}

		// CommandByID round-trip.
		spec, ok := CommandByID(c.ID)
		if !ok {
			t.Errorf("CommandByID(%#x) ok=false; want true", c.ID)
		}
		if spec != c {
			t.Errorf("CommandByID(%#x) = %+v; want %+v", c.ID, spec, c)
		}
	}

	// CommandIDs set must equal the catalogue id set.
	ids := CommandIDs()
	if len(ids) != len(cmds) {
		t.Errorf("CommandIDs len=%d; Commands len=%d", len(ids), len(cmds))
	}
	idSet := make(map[CommandID]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
		if _, ok := specByID[id]; !ok {
			t.Errorf("CommandIDs has %#x but Commands() does not", id)
		}
		// Every CommandIDs entry must resolve via CommandName.
		if CommandName(id) == "" {
			t.Errorf("CommandIDs has %#x but CommandName returns empty", id)
		}
	}
	for id := range specByID {
		if !idSet[id] {
			t.Errorf("Commands() has %#x but CommandIDs does not", id)
		}
	}
}

// TestCommandByIDUnknown confirms the lookup returns false for a byte
// outside this codec's catalogue (0x00 is not a defined SW-P-02
// command).
func TestCommandByIDUnknown(t *testing.T) {
	if spec, ok := CommandByID(CommandID(0x00)); ok {
		t.Errorf("CommandByID(0x00) ok=true spec=%+v; want false", spec)
	}
	if spec, ok := CommandByID(CommandID(0xAA)); ok {
		t.Errorf("CommandByID(0xAA) ok=true spec=%+v; want false", spec)
	}
}

// TestCommandNameUnknown confirms CommandName returns the empty string
// for an unregistered command byte (the default switch arm).
func TestCommandNameUnknown(t *testing.T) {
	for _, id := range []CommandID{0x00, 0x10, 0xAA, 0xFF} {
		if got := CommandName(id); got != "" {
			t.Errorf("CommandName(%#x) = %q; want empty", id, got)
		}
	}
}

// TestCommandNameSpotChecks pins a few representative bytes to their
// §3.2 names so a typo in the switch is caught independently of the
// catalogue cross-check above.
func TestCommandNameSpotChecks(t *testing.T) {
	cases := []struct {
		id   CommandID
		want string
	}{
		{RxInterrogate, "interrogate"},
		{RxConnect, "connect"},
		{TxTally, "tally"},
		{TxCrosspointConnected, "crosspoint_connected"},
		{RxGo, "go"},
		{TxExtendedProtectTallyDump, "extended_protect_tally_dump"},
		{RxExtendedProtectTallyDumpRequest, "extended_protect_tally_dump_request"},
	}
	for _, tc := range cases {
		if got := CommandName(tc.id); got != tc.want {
			t.Errorf("CommandName(%#x) = %q; want %q", tc.id, got, tc.want)
		}
	}
}
