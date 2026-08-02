package main

import (
	"testing"

	codec "dhs/internal/probel-sw08p/codec"
	probelproto "dhs/internal/probel-sw08p/consumer"
)

// TestXpointDiff pins the per-crosspoint idempotency decision: skip when the
// destination already carries the desired source; otherwise change, rendering
// the current source as "from" ("" when the dst is unrouted / absent).
func TestXpointDiff(t *testing.T) {
	cur := map[uint16]uint16{0: 3, 1: 5}
	cases := []struct {
		name        string
		dst, src    uint16
		wantChanged bool
		wantFrom    string
	}{
		{"already routed -> skip", 0, 3, false, ""},
		{"different source -> change", 1, 9, true, "5"},
		{"unrouted dst -> change from empty", 7, 4, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changed, from := xpointDiff(cur, tc.dst, tc.src)
			if changed != tc.wantChanged || from != tc.wantFrom {
				t.Errorf("xpointDiff(dst=%d,src=%d) = (%v,%q), want (%v,%q)",
					tc.dst, tc.src, changed, from, tc.wantChanged, tc.wantFrom)
			}
		})
	}
}

// TestTallyToMap pins flattening a tally-dump (byte + word form) into dst→src,
// honoring the contiguous SourceIDs starting at FirstDestinationID.
func TestTallyToMap(t *testing.T) {
	t.Run("byte form", func(t *testing.T) {
		res := probelproto.TallyDumpResult{
			Byte: codec.CrosspointTallyDumpByteParams{FirstDestinationID: 0, SourceIDs: []uint8{3, 4}},
		}
		m := tallyToMap(res)
		if m[0] != 3 || m[1] != 4 || len(m) != 2 {
			t.Errorf("byte tally = %v, want {0:3, 1:4}", m)
		}
	})
	t.Run("word form with offset", func(t *testing.T) {
		res := probelproto.TallyDumpResult{
			IsWord: true,
			Word:   codec.CrosspointTallyDumpWordParams{FirstDestinationID: 2, SourceIDs: []uint16{7, 8}},
		}
		m := tallyToMap(res)
		if m[2] != 7 || m[3] != 8 || len(m) != 2 {
			t.Errorf("word tally = %v, want {2:7, 3:8}", m)
		}
	})
}

// TestParseXpointRows pins the lenient CSV parse: header skipped, short and
// non-integer rows dropped, valid rows typed.
func TestParseXpointRows(t *testing.T) {
	rows := [][]string{
		{"matrix_id", "level_id", "dst_id", "src_id"}, // header (skipped)
		{"0", "0", "0", "3"},                          // valid
		{"0", "0", "1"},                               // short -> dropped
		{"0", "0", "x", "4"},                          // non-int -> dropped
		{"1", "0", "7", "9"},                          // valid
	}
	got := parseXpointRows(rows)
	if len(got) != 2 {
		t.Fatalf("parsed %d rows, want 2: %+v", len(got), got)
	}
	if got[0] != (xpointRow{0, 0, 0, 3}) || got[1] != (xpointRow{1, 0, 7, 9}) {
		t.Errorf("parsed = %+v, want [{0 0 0 3} {1 0 7 9}]", got)
	}
}
