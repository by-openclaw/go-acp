package acp1

import (
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/export/canonical"
)

func TestLimitString(t *testing.T) {
	if got := limitString("hello", 3); got != "hel" {
		t.Errorf("limitString truncate = %q, want hel", got)
	}
	if got := limitString("hi", 0); got != "hi" {
		t.Errorf("limitString(maxLen<=0) = %q, want hi", got)
	}
	if got := limitString("hi", 10); got != "hi" {
		t.Errorf("limitString(short) = %q, want hi", got)
	}
}

func TestStringMaxLen(t *testing.T) {
	hint := func(s string) *string { return &s }
	cases := []struct {
		fmt  *string
		want uint8
	}{
		{hint("maxLen=8"), 8},
		{hint("ipv4,maxLen=12"), 12},
		{hint("ipv4"), 0},
		{hint("maxLen=abc"), 0},
		{nil, 0},
	}
	for _, c := range cases {
		got := stringMaxLen(&canonical.Parameter{Format: c.fmt})
		if got != c.want {
			t.Errorf("stringMaxLen(%v) = %d, want %d", c.fmt, got, c.want)
		}
	}
}

func TestEncodeString_Truncation(t *testing.T) {
	e := &entry{acpType: codec.TypeString, access: codec.AccessRead,
		param: param("s", canonical.ParamString, withFormat("maxLen=4"), withValue("longstring"))}
	b, err := encodeObject(e)
	if err != nil {
		t.Fatalf("encodeString: %v", err)
	}
	o, err := codec.DecodeObject(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(o.StrValue) > 4 {
		t.Errorf("string value not truncated to maxLen: %q", o.StrValue)
	}
}

func TestHasCard(t *testing.T) {
	if (*slotCounts)(nil).hasCard() {
		t.Error("nil slotCounts should not be a card")
	}
	if (&slotCounts{}).hasCard() {
		t.Error("all-zero counts (frame-only slot) is not a card")
	}
	if !(&slotCounts{numControl: 1}).hasCard() {
		t.Error("non-zero control count is a card")
	}
	if !(&slotCounts{numIdentity: 2}).hasCard() {
		t.Error("non-zero identity count is a card")
	}
}

func TestSnapInt64(t *testing.T) {
	if got := snapInt64(7, 0, 4); got != 8 {
		t.Errorf("snapInt64(7,0,4) = %d, want 8", got)
	}
	if got := snapInt64(-7, 0, 4); got != -8 {
		t.Errorf("snapInt64(-7,0,4) = %d, want -8", got)
	}
	if got := snapInt64(5, 0, 0); got != 5 {
		t.Errorf("snapInt64 step<=0 should pass through, got %d", got)
	}
}

func TestBroadcastsEnabled_ValueTypes(t *testing.T) {
	mk := func(v any) *tree {
		tr := &tree{entries: map[objectKey]*entry{}, slots: map[uint8]*slotCounts{}}
		tr.entries[objectKey{slot: 0, group: codec.GroupControl, id: 4}] =
			&entry{acpType: codec.TypeEnum, param: &canonical.Parameter{Value: v}}
		return tr
	}
	for _, v := range []any{int(1), uint64(1), float64(1)} {
		if !mk(v).broadcastsEnabled() {
			t.Errorf("bare value %v(%T) should enable", v, v)
		}
	}
	for _, v := range []any{int(0), uint64(0), float64(0)} {
		if mk(v).broadcastsEnabled() {
			t.Errorf("bare value %v(%T) should disable", v, v)
		}
	}
	// Value not present in the enum map → disabled.
	tr := &tree{entries: map[objectKey]*entry{}, slots: map[uint8]*slotCounts{}}
	tr.entries[objectKey{slot: 0, group: codec.GroupControl, id: 4}] = &entry{
		acpType: codec.TypeEnum,
		param: &canonical.Parameter{Value: int64(9),
			EnumMap: []canonical.EnumEntry{{Key: "Off", Value: 0}, {Key: "On", Value: 1}}},
	}
	if tr.broadcastsEnabled() {
		t.Error("value not in enum map should disable broadcasts")
	}
}

func TestMutateIPAddr_BadStoredValue(t *testing.T) {
	s := discardServer()
	e := &entry{acpType: codec.TypeIPAddr, param: &canonical.Parameter{
		Value: "garbage-not-an-ip", Minimum: "0.0.0.0", Maximum: "255.255.255.255",
	}}
	if _, err := s.applyMutation(e, codec.MethodSetValue, []byte{1, 2, 3, 4}); err == nil {
		t.Error("mutateIPAddr with unparseable stored value should error")
	}
}
