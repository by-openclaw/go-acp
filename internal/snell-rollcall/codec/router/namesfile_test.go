package router

import (
	"bytes"
	"encoding/hex"
	"errors"
	"hash/crc32"
	"strings"
	"testing"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatalf("bad test vector %q: %v", s, err)
	}
	return b
}

// TestNamesFileCRC_SpecExample is the worked example from
// §CMD_ASSOC_NAMES_8_FILENAME: a matrix with two source associations and two
// destination associations, at the 8-character width, with the per-name
// checksums and their sum all given.
//
// It settles two things the prose leaves open. The hash is the ordinary IEEE
// CRC-32, so the standard library computes it; and the index is appended as
// four little-endian bytes after the padded name, not before it and not in
// network order.
func TestNamesFileCRC_SpecExample(t *testing.T) {
	f := NamesFile{
		Width: NameWidth8,
		Srcs:  []string{"m01a01", "m01a02"},
		Dsts:  []string{"m01d01", "m01d02"},
	}

	perName := []uint32{0xa2b261ef, 0x2be61c17, 0x422cec2b, 0xcb7891d3}
	names := append(append([]string{}, f.Srcs...), f.Dsts...)
	for i, name := range names {
		if got := NameCRC(name, NameWidth8, uint32(i)); got != perName[i] {
			t.Errorf("NameCRC(%q, 8, %d) = %#08x, want %#08x", name, i, got, perName[i])
		}
	}

	if got := f.CRC(); got != 0xdc3dfc04 {
		t.Errorf("CRC() = %#08x, want the document's %#08x", got, 0xdc3dfc04)
	}
}

// TestNameCRC_CheckData shows the bytes the checksum is taken over, which is
// the part of the rule easiest to get wrong: the name padded to its field
// width, then the index as a little-endian 32-bit value.
func TestNameCRC_CheckData(t *testing.T) {
	// The first row of the document's table.
	want := mustHex(t, "6d 30 31 61 30 31 00 00"+"00 00 00 00")
	got := crc32.ChecksumIEEE(want)
	if got != 0xa2b261ef {
		t.Fatalf("the check data is wrong: %x hashes to %#08x", want, got)
	}
	if NameCRC("m01a01", NameWidth8, 0) != got {
		t.Error("NameCRC does not hash the documented check data")
	}

	// The second row, where the index is 1 and appears as 01 00 00 00.
	want = mustHex(t, "6d 30 31 61 30 32 00 00"+"01 00 00 00")
	if NameCRC("m01a02", NameWidth8, 1) != crc32.ChecksumIEEE(want) {
		t.Error("the index is not little-endian")
	}
}

// TestNamesFileCRC_IndexDistinguishesDuplicates is why the index is in the
// hash at all. Two destinations often carry the same name, and without the
// index a file that swapped them would checksum identically and a client would
// keep a stale cache.
func TestNamesFileCRC_IndexDistinguishesDuplicates(t *testing.T) {
	a := NamesFile{Width: NameWidth8, Srcs: []string{"CAM 1", "CAM 2"}}
	b := NamesFile{Width: NameWidth8, Srcs: []string{"CAM 2", "CAM 1"}}

	if a.CRC() == b.CRC() {
		t.Error("swapping two names must change the checksum")
	}

	// The same name twice is fine and still yields distinct contributions.
	dup := NamesFile{Width: NameWidth8, Srcs: []string{"CAM 1", "CAM 1"}}
	if NameCRC("CAM 1", NameWidth8, 0) == NameCRC("CAM 1", NameWidth8, 1) {
		t.Error("the same name at two positions must hash differently")
	}
	if dup.CRC() == 0 {
		t.Error("a file of duplicates still has a checksum")
	}
}

// TestNamesFileCRC_IsIncremental records the property the summing buys: when
// one name changes, the new checksum is the old one with that name's
// contribution swapped, so a controller never re-reads the file.
func TestNamesFileCRC_IsIncremental(t *testing.T) {
	before := NamesFile{
		Width: NameWidth8,
		Srcs:  []string{"CAM 1", "CAM 2", "CAM 3"},
		Dsts:  []string{"MON A"},
	}
	after := before
	after.Srcs = []string{"CAM 1", "VTR 1", "CAM 3"}

	patched := before.CRC() - NameCRC("CAM 2", NameWidth8, 1) + NameCRC("VTR 1", NameWidth8, 1)
	if patched != after.CRC() {
		t.Errorf("incremental update gave %#08x, full recompute %#08x", patched, after.CRC())
	}
}

// TestNamesFile_RoundTrip covers the layout: sources first, then destinations,
// each name in a zero-padded fixed-width field with no header anywhere.
func TestNamesFile_RoundTrip(t *testing.T) {
	for _, width := range []int{NameWidth8, NameWidth32} {
		f := NamesFile{
			Width: width,
			Srcs:  []string{"CAM 1", "", "CAM 3"},
			Dsts:  []string{"MON A", "MON B"},
		}

		b, err := f.AppendTo(nil)
		if err != nil {
			t.Fatalf("width %d: AppendTo: %v", width, err)
		}
		if want := 5 * width; len(b) != want {
			t.Errorf("width %d: file is %d bytes, want %d", width, len(b), want)
		}

		got, err := DecodeNamesFile(b, width, 3, 2)
		if err != nil {
			t.Fatalf("width %d: DecodeNamesFile: %v", width, err)
		}
		if !equalStrings(got.Srcs, f.Srcs) || !equalStrings(got.Dsts, f.Dsts) {
			t.Errorf("width %d round trip\n got %+v\nwant %+v", width, got, f)
		}
		if got.CRC() != f.CRC() {
			t.Errorf("width %d: checksum changed across a round trip", width)
		}
	}
}

// TestNamesFile_Truncation covers a name longer than its field. The controller
// truncates when it writes the file, so a client that does anything else
// computes a checksum that never matches.
func TestNamesFile_Truncation(t *testing.T) {
	long := "A very long destination name indeed"
	f := NamesFile{Width: NameWidth8, Srcs: []string{long}}

	b, err := f.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	if len(b) != NameWidth8 {
		t.Fatalf("field is %d bytes, want %d", len(b), NameWidth8)
	}

	got, err := DecodeNamesFile(b, NameWidth8, 1, 0)
	if err != nil {
		t.Fatalf("DecodeNamesFile: %v", err)
	}
	if got.Srcs[0] != long[:NameWidth8] {
		t.Errorf("= %q, want %q", got.Srcs[0], long[:NameWidth8])
	}
	// A name that exactly fills its field has no terminator, and must still
	// read back whole.
	if len(got.Srcs[0]) != NameWidth8 {
		t.Errorf("a full field lost bytes: %q", got.Srcs[0])
	}
	if NameCRC(long, NameWidth8, 0) != NameCRC(long[:NameWidth8], NameWidth8, 0) {
		t.Error("the checksum must be taken over the truncated name")
	}
}

func TestNamesFile_Errors(t *testing.T) {
	if _, err := DecodeNamesFile(nil, 16, 0, 0); !errors.Is(err, ErrBadWidth) {
		t.Errorf("err = %v, want ErrBadWidth", err)
	}
	if _, err := (NamesFile{Width: 16}).AppendTo(nil); !errors.Is(err, ErrBadWidth) {
		t.Errorf("err = %v, want ErrBadWidth", err)
	}
	if _, err := DecodeNamesFile(nil, NameWidth8, -1, 0); !errors.Is(err, ErrShortFile) {
		t.Errorf("err = %v, want ErrShortFile", err)
	}
	if _, err := DecodeNamesFile(make([]byte, 15), NameWidth8, 1, 1); !errors.Is(err, ErrShortFile) {
		t.Errorf("err = %v, want ErrShortFile", err)
	}

	// A longer file is accepted and the surplus ignored: refusing would make a
	// whole matrix unnameable over trailing padding.
	got, err := DecodeNamesFile(make([]byte, 100), NameWidth8, 1, 1)
	if err != nil {
		t.Fatalf("a padded file must decode: %v", err)
	}
	if len(got.Srcs) != 1 || len(got.Dsts) != 1 {
		t.Errorf("= %d srcs, %d dsts; want 1 and 1", len(got.Srcs), len(got.Dsts))
	}

	// An empty file with zero counts is legal.
	if _, err := DecodeNamesFile(nil, NameWidth8, 0, 0); err != nil {
		t.Errorf("an empty file with no names must decode: %v", err)
	}
}

// TestMappingsFile covers the association mappings: one 32-bit little-endian
// entity per level per association, sources first then destinations, one-based
// with zero meaning the association does not reach that level.
func TestMappingsFile(t *testing.T) {
	m := MappingsFile{
		Levels: 3,
		Srcs:   [][]uint32{{1, 2, 0}, {3, 3, 3}},
		Dsts:   [][]uint32{{10, 0, 12}},
	}

	b := m.AppendTo(nil)
	want := mustHex(t,
		"01000000 02000000 00000000"+ // source association 1
			"03000000 03000000 03000000"+ // source association 2
			"0a000000 00000000 0c000000") // destination association 1
	if !bytes.Equal(b, want) {
		t.Errorf("encoded\n got %x\nwant %x", b, want)
	}

	got, err := DecodeMappingsFile(b, 3, 2, 1)
	if err != nil {
		t.Fatalf("DecodeMappingsFile: %v", err)
	}
	if len(got.Srcs) != 2 || len(got.Dsts) != 1 {
		t.Fatalf("= %d srcs, %d dsts", len(got.Srcs), len(got.Dsts))
	}
	for i := range m.Srcs {
		if !equalUints(got.Srcs[i], m.Srcs[i]) {
			t.Errorf("source association %d = %v, want %v", i, got.Srcs[i], m.Srcs[i])
		}
	}
	if !equalUints(got.Dsts[0], m.Dsts[0]) {
		t.Errorf("destination association = %v, want %v", got.Dsts[0], m.Dsts[0])
	}
	if got.CRC() != m.CRC() {
		t.Error("checksum changed across a round trip")
	}
}

// TestMappingsFileCRC_PerAssociation records that this checksum sums one hash
// per association over that association's own entries, with no index mixed in:
// unlike a name, an association's contents already identify it.
func TestMappingsFileCRC_PerAssociation(t *testing.T) {
	m := MappingsFile{Levels: 2, Srcs: [][]uint32{{1, 2}, {3, 4}}}

	first := crc32.ChecksumIEEE(mustHex(t, "01000000 02000000"))
	second := crc32.ChecksumIEEE(mustHex(t, "03000000 04000000"))
	if got, want := m.CRC(), first+second; got != want {
		t.Errorf("CRC() = %#08x, want %#08x", got, want)
	}

	// Two associations with the same entries contribute the same hash, which
	// is the documented difference from the names file.
	same := MappingsFile{Levels: 2, Srcs: [][]uint32{{1, 2}, {1, 2}}}
	if same.CRC() != 2*first {
		t.Error("identical associations must contribute identical hashes")
	}
}

// TestMappingsFile_ShortRow covers a row holding fewer entries than the matrix
// has levels. The missing levels encode as zero, which is what the format uses
// for "this association does not reach that level".
func TestMappingsFile_ShortRow(t *testing.T) {
	m := MappingsFile{Levels: 3, Srcs: [][]uint32{{1}}}

	b := m.AppendTo(nil)
	if want := mustHex(t, "01000000 00000000 00000000"); !bytes.Equal(b, want) {
		t.Errorf("encoded %x, want %x", b, want)
	}
	if m.CRC() != crc32.ChecksumIEEE(mustHex(t, "01000000 00000000 00000000")) {
		t.Error("the checksum must be taken over the padded row")
	}
}

func TestMappingsFile_Errors(t *testing.T) {
	if _, err := DecodeMappingsFile(nil, -1, 0, 0); !errors.Is(err, ErrShortFile) {
		t.Errorf("err = %v, want ErrShortFile", err)
	}
	if _, err := DecodeMappingsFile(make([]byte, 7), 2, 1, 0); !errors.Is(err, ErrShortFile) {
		t.Errorf("err = %v, want ErrShortFile", err)
	}
	if _, err := DecodeMappingsFile(nil, 0, 0, 0); err != nil {
		t.Errorf("an empty file must decode: %v", err)
	}
}

// TestMultiChannel covers the variable-length record of
// §CMD_SRCDST_MC_DATA_FILENAME. Records carry no length prefix, so the file
// can only be walked from the start, and every multi-byte field is
// little-endian.
func TestMultiChannel(t *testing.T) {
	in := MultiChannel{
		IsMultiChannel: true,
		TrackTemplate:  0x0102,
		AudioOnly:      false,
		Channels:       []uint16{1, 2, 0, 4},
	}

	b, err := in.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	want := mustHex(t, "01"+"04"+"0201"+"00"+"0100 0200 0000 0400")
	if !bytes.Equal(b, want) {
		t.Errorf("encoded\n got %x\nwant %x", b, want)
	}

	got, n, err := DecodeMultiChannel(b)
	if err != nil {
		t.Fatalf("DecodeMultiChannel: %v", err)
	}
	if n != len(b) {
		t.Errorf("consumed %d of %d bytes", n, len(b))
	}
	if got.IsMultiChannel != in.IsMultiChannel || got.TrackTemplate != in.TrackTemplate ||
		got.AudioOnly != in.AudioOnly || !equalU16(got.Channels, in.Channels) {
		t.Errorf("round trip\n got %+v\nwant %+v", got, in)
	}

	// A source that is not multi-channel is the five-byte minimum.
	plain, err := (MultiChannel{}).AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	if len(plain) != 5 {
		t.Errorf("a plain record is %d bytes, want 5", len(plain))
	}
}

// TestMultiChannelFile walks a whole file, which is the only way to read one:
// the records are variable length and there is no index.
func TestMultiChannelFile(t *testing.T) {
	records := []MultiChannel{
		{IsMultiChannel: true, TrackTemplate: 1, Channels: []uint16{1, 2}},
		{},
		{IsMultiChannel: true, AudioOnly: true, Channels: []uint16{7}},
	}

	var file []byte
	for _, r := range records {
		b, err := r.AppendTo(nil)
		if err != nil {
			t.Fatalf("AppendTo: %v", err)
		}
		file = append(file, b...)
	}

	srcs, dsts, err := DecodeMultiChannelFile(file, 2, 1)
	if err != nil {
		t.Fatalf("DecodeMultiChannelFile: %v", err)
	}
	if len(srcs) != 2 || len(dsts) != 1 {
		t.Fatalf("= %d srcs, %d dsts", len(srcs), len(dsts))
	}
	if !srcs[0].IsMultiChannel || len(srcs[0].Channels) != 2 {
		t.Errorf("source 1 = %+v", srcs[0])
	}
	if srcs[1].IsMultiChannel {
		t.Errorf("source 2 = %+v, want a plain record", srcs[1])
	}
	if !dsts[0].AudioOnly || len(dsts[0].Channels) != 1 {
		t.Errorf("destination 1 = %+v", dsts[0])
	}

	// This file's checksum is over the whole file, unlike the names files,
	// because the configuration does not change while the system runs.
	if crc32.ChecksumIEEE(file) == 0 {
		t.Error("the whole-file checksum should be computed over the bytes")
	}
}

func TestMultiChannel_Errors(t *testing.T) {
	if _, _, err := DecodeMultiChannel(make([]byte, 4)); !errors.Is(err, ErrShortFile) {
		t.Errorf("err = %v, want ErrShortFile", err)
	}
	// A channel count past the ceiling.
	over := append([]byte{1, MaxMultiChannelChannels + 1, 0, 0, 0}, make([]byte, 200)...)
	if _, _, err := DecodeMultiChannel(over); !errors.Is(err, ErrShortFile) {
		t.Errorf("err = %v, want ErrShortFile", err)
	}
	// A record claiming more channels than it carries.
	if _, _, err := DecodeMultiChannel([]byte{1, 4, 0, 0, 0, 1, 0}); !errors.Is(err, ErrShortFile) {
		t.Errorf("err = %v, want ErrShortFile", err)
	}
	tooMany := MultiChannel{Channels: make([]uint16, MaxMultiChannelChannels+1)}
	if _, err := tooMany.AppendTo(nil); !errors.Is(err, ErrShortFile) {
		t.Errorf("err = %v, want ErrShortFile", err)
	}
	// The ceiling itself is accepted.
	atLimit := MultiChannel{Channels: make([]uint16, MaxMultiChannelChannels)}
	if _, err := atLimit.AppendTo(nil); err != nil {
		t.Errorf("%d channels must be accepted: %v", MaxMultiChannelChannels, err)
	}

	if _, _, err := DecodeMultiChannelFile(nil, 1, 0); !errors.Is(err, ErrShortFile) {
		t.Errorf("err = %v, want ErrShortFile", err)
	}
	short, err := (MultiChannel{}).AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	if _, _, err := DecodeMultiChannelFile(short, 1, 1); !errors.Is(err, ErrShortFile) {
		t.Errorf("a file missing its destination records: err = %v, want ErrShortFile", err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalUints(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalU16(a, b []uint16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
