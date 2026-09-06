package acp2

import (
	"testing"

	"dhs/internal/acp2/codec"
)

// TestCatalogueEntry_Address pins the "<kind>:<id>" canonical address
// produced for `dhs help-cmd acp2 <addr>`.
func TestCatalogueEntry_Address(t *testing.T) {
	cases := []struct {
		entry CatalogueEntry
		want  string
	}{
		{CatalogueEntry{Kind: KindPid, ID: 4}, "pid:4"},
		{CatalogueEntry{Kind: KindAN2Type, ID: 0}, "an2-type:0"},
		{CatalogueEntry{Kind: KindErrStat, ID: 255}, "err-stat:255"},
		{CatalogueEntry{Kind: KindNumberType, ID: 100}, "number-type:100"},
	}
	for _, c := range cases {
		if got := c.entry.Address(); got != c.want {
			t.Errorf("Address(%+v) = %q, want %q", c.entry, got, c.want)
		}
	}
}

// TestCatalogue_NonEmptyAndAddressable asserts every catalogue row has a
// non-empty name + spec ref and round-trips through LookupCatalogue by its
// own address.
func TestCatalogue_NonEmptyAndAddressable(t *testing.T) {
	entries := Catalogue()
	if len(entries) == 0 {
		t.Fatal("Catalogue() returned no entries")
	}
	for _, e := range entries {
		if e.Name == "" {
			t.Errorf("entry %s has empty Name", e.Address())
		}
		if e.SpecRef == "" {
			t.Errorf("entry %s has empty SpecRef", e.Address())
		}
		got, ok := LookupCatalogue(e.Address())
		if !ok {
			t.Errorf("LookupCatalogue(%q) miss for known entry", e.Address())
			continue
		}
		if got.Kind != e.Kind || got.ID != e.ID || got.Name != e.Name {
			t.Errorf("LookupCatalogue(%q) = %+v, want %+v", e.Address(), got, e)
		}
	}
}

// TestLookupCatalogue_KnownEntries spot-checks specific spec-derived rows.
func TestLookupCatalogue_KnownEntries(t *testing.T) {
	cases := []struct {
		addr     string
		wantKind CommandKind
		wantName string
	}{
		{"pid:4", KindPid, "event_delay"},         // ADR-pinned spec name §5.4 row 4
		{"acp2-type:2", KindACP2Type, "announce"}, // not "event"
		{"obj-type:0", KindObjType, "node"},
		{"number-type:8", KindNumberType, "float"},
		{"err-stat:5", KindErrStat, "invalid-value"},
		{"acp2-func:1", KindACP2Func, "get_object"},
		{"an2-type:" + uint8ToDec(uint8(codec.AN2TypeData)), KindAN2Type, "data"},
	}
	for _, c := range cases {
		e, ok := LookupCatalogue(c.addr)
		if !ok {
			t.Errorf("LookupCatalogue(%q) miss", c.addr)
			continue
		}
		if e.Kind != c.wantKind || e.Name != c.wantName {
			t.Errorf("LookupCatalogue(%q) = kind %q name %q, want kind %q name %q",
				c.addr, e.Kind, e.Name, c.wantKind, c.wantName)
		}
	}
}

// TestLookupCatalogue_Misses covers the false-return branches: malformed
// address, unknown kind, and out-of-catalogue id.
func TestLookupCatalogue_Misses(t *testing.T) {
	misses := []string{
		"",         // no colon
		"pid",      // no colon
		"pid:",     // empty id
		"pid:999",  // id overflows uint8 → splitAddress fails
		"pid:4x",   // non-digit
		"pid:7777", // > 3 chars
		"bogus:1",  // unknown kind
		"pid:200",  // valid uint8 but no such pid in catalogue
		":4",       // empty kind
	}
	for _, m := range misses {
		if e, ok := LookupCatalogue(m); ok {
			t.Errorf("LookupCatalogue(%q) = %+v, want miss", m, e)
		}
	}
}

// TestUint8ToDec covers the zero fast-path and multi-digit formatting.
func TestUint8ToDec(t *testing.T) {
	cases := map[uint8]string{0: "0", 7: "7", 42: "42", 100: "100", 255: "255"}
	for in, want := range cases {
		if got := uint8ToDec(in); got != want {
			t.Errorf("uint8ToDec(%d) = %q, want %q", in, got, want)
		}
	}
}

// TestDecToUint8 covers valid parses and every error branch (empty, too
// long, non-digit, overflow).
func TestDecToUint8(t *testing.T) {
	ok := map[string]uint8{"0": 0, "9": 9, "42": 42, "255": 255}
	for in, want := range ok {
		got, err := decToUint8(in)
		if err {
			t.Errorf("decToUint8(%q) errored, want %d", in, want)
			continue
		}
		if got != want {
			t.Errorf("decToUint8(%q) = %d, want %d", in, got, want)
		}
	}
	bad := []string{"", "256", "999", "1234", "12a", "-1", " 1"}
	for _, in := range bad {
		if _, err := decToUint8(in); !err {
			t.Errorf("decToUint8(%q) did not error", in)
		}
	}
}

// TestSplitAddress covers the parsed and unparsed branches directly.
func TestSplitAddress(t *testing.T) {
	k, id, ok := splitAddress("pid:14")
	if !ok || k != KindPid || id != 14 {
		t.Errorf("splitAddress(pid:14) = %q,%d,%v", k, id, ok)
	}
	if _, _, ok := splitAddress("nocolon"); ok {
		t.Error("splitAddress(nocolon) reported ok")
	}
	if _, _, ok := splitAddress("pid:300"); ok {
		t.Error("splitAddress(pid:300) reported ok for overflow id")
	}
}
