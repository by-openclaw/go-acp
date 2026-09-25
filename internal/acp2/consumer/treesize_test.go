package acp2

import (
	"testing"
	"unsafe"

	"dhs/internal/acp2/codec"
	"dhs/internal/consumer"
)

// The estimator's contract is not "exact bytes" — Go cannot give that
// cheaply — but "every variable-length field is accounted, and the
// number tracks the real curve". So these tests assert the DELTA a
// given field contributes, which is both exact and architecture-safe,
// rather than a total that would be brittle.

func TestEstimateTreeBytesNilIsZero(t *testing.T) {
	if got := estimateTreeBytes(nil); got != 0 {
		t.Fatalf("estimateTreeBytes(nil) = %d, want 0", got)
	}
}

func TestObjectBytesCountsEveryVariableField(t *testing.T) {
	var empty consumer.Object
	base := objectBytes(&empty)
	if base == 0 {
		t.Fatal("objectBytes of an empty object = 0, want the struct's own size")
	}

	tests := []struct {
		name  string
		obj   consumer.Object
		delta uint64
	}{
		{
			name:  "string bodies add their length",
			obj:   consumer.Object{Group: "gg", OID: "1.2", Unit: "dB"},
			delta: 2 + 3 + 2,
		},
		{
			name:  "label and alarm messages",
			obj:   consumer.Object{Label: "abcd", AlarmOnMsg: "on", AlarmOffMsg: "off"},
			delta: 4 + 2 + 3,
		},
		{
			name:  "path adds a header plus body per element",
			obj:   consumer.Object{Path: []string{"a", "bb"}},
			delta: 2*stringHeaderBytes + 1 + 2,
		},
		{
			name:  "enum items add a header plus body per element",
			obj:   consumer.Object{EnumItems: []string{"x", "yz"}},
			delta: 2*stringHeaderBytes + 1 + 2,
		},
		{
			name:  "meta counts entry slots only",
			obj:   consumer.Object{Meta: map[string]any{"a": 1, "b": 2}},
			delta: 2 * mapEntryOverhead,
		},
		{
			name: "value raw, string and slot status",
			obj: consumer.Object{Value: consumer.Value{
				Raw:        []byte{1, 2, 3},
				Str:        "hi",
				SlotStatus: []consumer.SlotStatus{1, 2},
			}},
			// SlotStatus is a uint8 → one byte each.
			delta: 3 + 2 + 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			obj := tc.obj
			got := objectBytes(&obj)
			if want := base + tc.delta; got != want {
				t.Errorf("objectBytes = %d, want %d (base %d + %d)", got, want, base, tc.delta)
			}
		})
	}
}

func TestEstimateTreeBytesCountsParallelSlicesAndLabels(t *testing.T) {
	empty := &WalkedTree{}
	base := estimateTreeBytes(empty)
	if base == 0 {
		t.Fatal("estimateTreeBytes of an empty tree = 0, want the struct's own size")
	}

	tree := &WalkedTree{
		Objects:     []consumer.Object{{Label: "ab"}},
		ObjTypes:    []codec.ACP2ObjType{0},
		NumTypes:    []codec.NumberType{0},
		OptionsMaps: []map[uint32]string{{1: "xy"}},
		Labels:      map[string]int{"ab": 0},
	}

	var objType codec.ACP2ObjType
	var numType codec.NumberType
	want := base +
		objectBytes(&tree.Objects[0]) +
		uint64(unsafe.Sizeof(objType)) +
		uint64(unsafe.Sizeof(numType)) +
		sliceHeaderBytes + mapEntryOverhead + 2 + // OptionsMaps: header + entry + "xy"
		mapEntryOverhead + stringHeaderBytes + 2 // Labels: entry + key header + "ab"

	if got := estimateTreeBytes(tree); got != want {
		t.Errorf("estimateTreeBytes = %d, want %d", got, want)
	}
}

func TestWalkedTreeCacheBytes(t *testing.T) {
	var nilCache *walkedTreeCache
	if got := nilCache.Bytes(); got != 0 {
		t.Errorf("nil cache Bytes() = %d, want 0", got)
	}

	c := newWalkedTreeCache(4, 0)
	if got := c.Bytes(); got != 0 {
		t.Errorf("empty cache Bytes() = %d, want 0", got)
	}

	c.Put(1, &WalkedTree{Objects: []consumer.Object{{Label: "a"}}})
	oneSlot := c.Bytes()
	if oneSlot == 0 {
		t.Fatal("cache holding one tree reports 0 bytes — the mem=0B bug")
	}

	// A second slot is additional retained memory, not a replacement.
	c.Put(2, &WalkedTree{Objects: []consumer.Object{{Label: "bb"}}})
	if twoSlots := c.Bytes(); twoSlots <= oneSlot {
		t.Errorf("two slots = %d, want more than one slot = %d", twoSlots, oneSlot)
	}
}
