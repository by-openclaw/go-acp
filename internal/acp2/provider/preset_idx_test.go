package acp2

import (
	"encoding/binary"
	"testing"

	"dhs/internal/acp2/codec"
	"dhs/internal/export/canonical"
)

// TestPresetIdx_SpecExampleNonContiguous covers the spec example from
// acp2_protocol.docx §"Preset depth" (line 2613-2632):
//
//	property pid 7: preset depth 2
//	property pid data plen idx0 idx1
//	u8 u8 u16 u32 u32
//	7 0 12 100 200
//
// Encoder must emit the canonical idx values verbatim — not the
// hardcoded contiguous 0..depth-1 fallback.
func TestPresetIdx_SpecExampleNonContiguous(t *testing.T) {
	fmtHint := "depth=2,idx=100|200"
	p := &canonical.Parameter{
		Header: canonical.Header{Number: 9, Identifier: "Slot",
			Access: canonical.AccessReadWrite},
		Type: canonical.ParamInteger, Value: int64(0),
		Format: &fmtHint,
	}
	e := &entry{
		objID: 9, label: p.Identifier, access: 0x03,
		objType: codec.ObjTypePreset, numType: codec.NumTypeU8,
		presetDepth:   2,
		presetIdxList: presetIdxHint(p),
		param:         p,
	}
	if len(e.presetIdxList) != 2 || e.presetIdxList[0] != 100 || e.presetIdxList[1] != 200 {
		t.Fatalf("presetIdxHint returned %v; want [100, 200]", e.presetIdxList)
	}

	got := buildAndDecode(t, e)
	var pd codec.Property
	for _, pr := range got {
		if pr.PID == codec.PIDPresetDepth {
			pd = pr
			break
		}
	}
	if pd.PID != codec.PIDPresetDepth {
		t.Fatal("reply missing pid=7 (preset_depth)")
	}
	if pd.PLen != 12 {
		t.Errorf("pid=7 plen=%d want 12 (4 hdr + 2*4 idx)", pd.PLen)
	}
	if len(pd.Data) != 8 {
		t.Fatalf("pid=7 body len=%d want 8 (2 u32 idx)", len(pd.Data))
	}
	got0 := binary.BigEndian.Uint32(pd.Data[0:4])
	got1 := binary.BigEndian.Uint32(pd.Data[4:8])
	if got0 != 100 || got1 != 200 {
		t.Errorf("pid=7 idx values=[%d, %d]; want [100, 200] (spec docx line 2613-2632)", got0, got1)
	}
}

// TestPresetIdx_FallbackContiguousWhenHintAbsent verifies the
// historical contiguous 0..depth-1 fallback when the canonical
// fixture omits the `idx=...` hint.
func TestPresetIdx_FallbackContiguousWhenHintAbsent(t *testing.T) {
	fmtHint := "depth=3"
	p := &canonical.Parameter{
		Header: canonical.Header{Number: 9, Identifier: "Slot",
			Access: canonical.AccessReadWrite},
		Type: canonical.ParamInteger, Value: int64(0),
		Format: &fmtHint,
	}
	e := &entry{
		objID: 9, label: p.Identifier, access: 0x03,
		objType: codec.ObjTypePreset, numType: codec.NumTypeU8,
		presetDepth:   3,
		presetIdxList: presetIdxHint(p), // expected: nil
		param:         p,
	}
	if e.presetIdxList != nil {
		t.Errorf("presetIdxHint(%q)=%v; want nil (no idx= hint)", fmtHint, e.presetIdxList)
	}
	got := buildAndDecode(t, e)
	var pd codec.Property
	for _, pr := range got {
		if pr.PID == codec.PIDPresetDepth {
			pd = pr
			break
		}
	}
	for i := uint32(0); i < 3; i++ {
		got := binary.BigEndian.Uint32(pd.Data[i*4 : i*4+4])
		if got != i {
			t.Errorf("idx[%d]=%d want %d (contiguous fallback)", i, got, i)
		}
	}
}

// TestPresetIdx_IgnoresMismatchedListLength asserts that a Format
// hint specifying fewer idx values than depth declares falls back to
// contiguous indices (defensive — protects against malformed fixtures).
func TestPresetIdx_IgnoresMismatchedListLength(t *testing.T) {
	fmtHint := "depth=4,idx=100|200" // length mismatch
	p := &canonical.Parameter{
		Header: canonical.Header{Number: 9, Identifier: "Slot",
			Access: canonical.AccessReadWrite},
		Type: canonical.ParamInteger, Value: int64(0),
		Format: &fmtHint,
	}
	e := &entry{
		objID: 9, label: p.Identifier, access: 0x03,
		objType: codec.ObjTypePreset, numType: codec.NumTypeU8,
		presetDepth:   4,
		presetIdxList: presetIdxHint(p), // [100, 200] but depth=4
		param:         p,
	}
	got := buildAndDecode(t, e)
	var pd codec.Property
	for _, pr := range got {
		if pr.PID == codec.PIDPresetDepth {
			pd = pr
			break
		}
	}
	// Mismatch → fallback to 0..3 (NOT [100, 200, 0, 0]).
	for i := uint32(0); i < 4; i++ {
		got := binary.BigEndian.Uint32(pd.Data[i*4 : i*4+4])
		if got != i {
			t.Errorf("idx[%d]=%d want %d (length mismatch should fall back)", i, got, i)
		}
	}
}
