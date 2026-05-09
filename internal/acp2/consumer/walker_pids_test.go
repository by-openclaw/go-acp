package acp2

import (
	"encoding/binary"
	"io"
	"log/slog"
	"testing"

	"dhs/internal/acp2/codec"
)

// TestWalker_DecodesPid4_AnnounceDelay covers spec §5.4 row 4: pid=4
// data=0, plen=8, body=u32 BE rate (announce delay in milliseconds).
// The walker must stash the decoded value into Meta["acp2.announceDelay"].
func TestWalker_DecodesPid4_AnnounceDelay(t *testing.T) {
	w := &Walker{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	body := make([]byte, 4)
	binary.BigEndian.PutUint32(body, 1500)
	props := []codec.Property{
		{PID: codec.PIDObjectType, VType: uint8(codec.ObjTypeNumber), PLen: 4},
		{PID: codec.PIDLabel, VType: 0, PLen: 4 + 5, Data: []byte("Foo\x00")},
		{PID: codec.PIDAnnounceDelay, VType: 0, PLen: 8, Data: body},
	}
	obj, _, _, _, _ := w.parseObjectProperties(props, 1, 5, nil)
	v, ok := obj.Meta["acp2.announceDelay"]
	if !ok {
		t.Fatal("walker did not stash acp2.announceDelay in Meta")
	}
	if got, _ := v.(uint64); got != 1500 {
		t.Errorf("acp2.announceDelay=%v want 1500", v)
	}
}

// TestWalker_DecodesPid7_PresetDepth covers spec §5.4 row 7: pid=7
// data=0, plen=4+4*depth, body = u32 BE list of valid idx values.
// Walker stashes the list into Meta["acp2.presetIdxList"].
func TestWalker_DecodesPid7_PresetDepth(t *testing.T) {
	w := &Walker{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	// Two idx values: 100, 200 — matches the spec example at line 2613-2632.
	body := make([]byte, 8)
	binary.BigEndian.PutUint32(body[0:4], 100)
	binary.BigEndian.PutUint32(body[4:8], 200)
	props := []codec.Property{
		{PID: codec.PIDObjectType, VType: uint8(codec.ObjTypePreset), PLen: 4},
		{PID: codec.PIDLabel, VType: 0, PLen: 4 + 5, Data: []byte("Bar\x00")},
		{PID: codec.PIDPresetDepth, VType: 0, PLen: 12, Data: body},
	}
	obj, _, _, _, _ := w.parseObjectProperties(props, 1, 6, nil)
	v, ok := obj.Meta["acp2.presetIdxList"]
	if !ok {
		t.Fatal("walker did not stash acp2.presetIdxList in Meta")
	}
	idxList, _ := v.([]uint32)
	if len(idxList) != 2 || idxList[0] != 100 || idxList[1] != 200 {
		t.Errorf("acp2.presetIdxList=%v want [100, 200]", idxList)
	}
}

// TestWalker_DecodesPid18_EventState covers spec §5.4 row 18: pid=18
// inline state byte in the property header's data slot, plen=4.
// Walker stashes the state into Meta["acp2.eventState"].
func TestWalker_DecodesPid18_EventState(t *testing.T) {
	w := &Walker{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	props := []codec.Property{
		{PID: codec.PIDObjectType, VType: uint8(codec.ObjTypeEnum), PLen: 4},
		{PID: codec.PIDLabel, VType: 0, PLen: 4 + 5, Data: []byte("Baz\x00")},
		{PID: codec.PIDEventState, VType: 7, PLen: 4},
	}
	obj, _, _, _, _ := w.parseObjectProperties(props, 1, 7, nil)
	v, ok := obj.Meta["acp2.eventState"]
	if !ok {
		t.Fatal("walker did not stash acp2.eventState in Meta")
	}
	if got, _ := v.(uint8); got != 7 {
		t.Errorf("acp2.eventState=%v want 7", v)
	}
}

// TestWalker_DecodesPid20_PresetParent covers spec §5.4 row 20: pid=20
// data=0, plen=8, body=u32 BE parent obj-id. Walker stashes into
// Meta["acp2.presetParent"].
func TestWalker_DecodesPid20_PresetParent(t *testing.T) {
	w := &Walker{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	body := make([]byte, 4)
	binary.BigEndian.PutUint32(body, 12345)
	props := []codec.Property{
		{PID: codec.PIDObjectType, VType: uint8(codec.ObjTypePreset), PLen: 4},
		{PID: codec.PIDLabel, VType: 0, PLen: 4 + 5, Data: []byte("Qux\x00")},
		{PID: codec.PIDPresetParent, VType: 0, PLen: 8, Data: body},
	}
	obj, _, _, _, _ := w.parseObjectProperties(props, 1, 8, nil)
	v, ok := obj.Meta["acp2.presetParent"]
	if !ok {
		t.Fatal("walker did not stash acp2.presetParent in Meta")
	}
	if got, _ := v.(uint64); got != 12345 {
		t.Errorf("acp2.presetParent=%v want 12345", v)
	}
}
