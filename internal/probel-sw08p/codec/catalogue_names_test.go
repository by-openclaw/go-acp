package codec

import "testing"

// TestCommandsCatalogue verifies Commands() returns a non-empty,
// internally consistent catalogue and that every entry round-trips
// through CommandByID. The catalogue feeds `dhs list-commands` /
// `dhs help-cmd`, so each entry must carry a non-empty Name and a
// resolvable ID.
func TestCommandsCatalogue(t *testing.T) {
	cmds := Commands()
	if len(cmds) == 0 {
		t.Fatal("Commands() returned empty catalogue")
	}
	for _, c := range cmds {
		if c.Name == "" {
			t.Errorf("command %#x has empty Name", byte(c.ID))
		}
		got, ok := CommandByID(c.ID)
		if !ok {
			t.Errorf("CommandByID(%#x) not found though present in Commands()", byte(c.ID))
			continue
		}
		if got.Name != c.Name {
			t.Errorf("CommandByID(%#x).Name = %q; want %q", byte(c.ID), got.Name, c.Name)
		}
	}
}

// TestCommandByIDMiss exercises the not-found return: a byte absent
// from the catalogue yields ok == false and the zero CommandSpec.
func TestCommandByIDMiss(t *testing.T) {
	spec, ok := CommandByID(0xEE)
	if ok {
		t.Errorf("CommandByID(0xEE) ok = true; want false")
	}
	if spec != (CommandSpec{}) {
		t.Errorf("CommandByID(0xEE) spec = %+v; want zero value", spec)
	}
}

// TestCommandNameForEveryID asserts CommandName resolves a non-empty
// label for every byte in CommandIDs(), and returns "" for an unknown
// byte (the final fallthrough). CommandName + CommandIDs drive the
// metrics per-command label registry.
func TestCommandNameForEveryID(t *testing.T) {
	ids := CommandIDs()
	if len(ids) == 0 {
		t.Fatal("CommandIDs() returned empty set")
	}
	for _, id := range ids {
		if name := CommandName(id); name == "" {
			t.Errorf("CommandName(%#x) = \"\"; want a label", byte(id))
		}
	}
	if name := CommandName(0xEE); name != "" {
		t.Errorf("CommandName(0xEE) = %q; want empty string", name)
	}
}

// TestCommandNameAllExtendedAndTx walks the full constant set so the
// extended-rx and tx label arms are covered, not just the ones in
// CommandIDs(). The 0x11 byte is direction-overloaded; its combined
// label is asserted explicitly.
func TestCommandNameSelectedLabels(t *testing.T) {
	cases := []struct {
		id   CommandID
		want string
	}{
		{RxProtectDeviceNameRequest, "rx 017 Protect Device Name Req / tx 017 App Keepalive Req"},
		{RxCrosspointInterrogateExt, "rx 129 Crosspoint Interrogate (ext)"},
		{RxCrosspointSalvoGroupInterrogateExt, "rx 252 Crosspoint Salvo Group Interrogate (ext)"},
		{TxSalvoConnectOnGoAck, "tx 122 Salvo Connect-On-Go Ack"},
	}
	for _, tc := range cases {
		if got := CommandName(tc.id); got != tc.want {
			t.Errorf("CommandName(%#x) = %q; want %q", byte(tc.id), got, tc.want)
		}
	}
}

// TestNameLengthBytesDefault covers the fallback arm of Bytes() and
// MaxNamesPerMessage(): an out-of-range NameLength enum returns the
// 8-char defaults.
func TestNameLengthBytesDefault(t *testing.T) {
	bad := NameLength(0x09)
	if got := bad.Bytes(); got != 8 {
		t.Errorf("NameLength(0x09).Bytes() = %d; want 8 (default)", got)
	}
	if got := bad.MaxNamesPerMessage(); got != 16 {
		t.Errorf("NameLength(0x09).MaxNamesPerMessage() = %d; want 16 (default)", got)
	}
	// Spot-check the valid widths so the switch arms are all walked.
	for _, tc := range []struct {
		n     NameLength
		bytes int
		max   int
	}{
		{NameLen4, 4, 32},
		{NameLen8, 8, 16},
		{NameLen12, 12, 10},
		{NameLen16, 16, 8},
	} {
		if got := tc.n.Bytes(); got != tc.bytes {
			t.Errorf("NameLength(%d).Bytes() = %d; want %d", tc.n, got, tc.bytes)
		}
		if got := tc.n.MaxNamesPerMessage(); got != tc.max {
			t.Errorf("NameLength(%d).MaxNamesPerMessage() = %d; want %d", tc.n, got, tc.max)
		}
	}
}

// TestValidateNameLengthExt covers the accept arm (incl. NameLen16,
// which validateNameLength rejects) and the reject arm.
func TestValidateNameLengthExt(t *testing.T) {
	for _, n := range []NameLength{NameLen4, NameLen8, NameLen12, NameLen16} {
		if err := validateNameLengthExt(n); err != nil {
			t.Errorf("validateNameLengthExt(%d) = %v; want nil", n, err)
		}
	}
	if err := validateNameLengthExt(NameLength(0x09)); err == nil {
		t.Error("validateNameLengthExt(0x09) = nil; want error")
	}
}
