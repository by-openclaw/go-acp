package acp1

import (
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/export/canonical"
)

// TestMutate_BadValueType: each numeric mutator surfaces an error when the
// stored canonical Value is the wrong Go type.
func TestMutate_BadValueType(t *testing.T) {
	s := newMutServer()
	bad := func(at codec.ObjectType) *entry {
		return &entry{acpType: at, param: &canonical.Parameter{Value: "not-a-number"}}
	}
	if _, err := s.mutateInteger(bad(codec.TypeInteger), codec.MethodSetIncValue, nil); err == nil {
		t.Error("integer bad value: want error")
	}
	if _, err := s.mutateLong(bad(codec.TypeLong), codec.MethodSetIncValue, nil); err == nil {
		t.Error("long bad value: want error")
	}
	if _, err := s.mutateByte(bad(codec.TypeByte), codec.MethodSetIncValue, nil); err == nil {
		t.Error("byte bad value: want error")
	}
	if _, err := s.mutateFloat(bad(codec.TypeFloat), codec.MethodSetIncValue, nil); err == nil {
		t.Error("float bad value: want error")
	}
	// IPAddr takes a dotted-quad string or a u32; a list is neither.
	if _, err := s.mutateIPAddr(&entry{acpType: codec.TypeIPAddr,
		param: &canonical.Parameter{Value: []string{"1.2.3.4"}}}, codec.MethodSetIncValue, nil); err == nil {
		t.Error("ipaddr bad value: want error")
	}
}

// TestMutate_UnexpectedMethod: the default arm of each mutator rejects a
// non-mutating method.
func TestMutate_UnexpectedMethod(t *testing.T) {
	s := newMutServer()
	bad := codec.MethodGetObject // not a mutating method
	if _, err := s.mutateInteger(&entry{param: &canonical.Parameter{Value: int64(0)}}, bad, nil); err == nil {
		t.Error("integer unexpected method: want error")
	}
	if _, err := s.mutateLong(&entry{param: &canonical.Parameter{Value: int64(0)}}, bad, nil); err == nil {
		t.Error("long unexpected method: want error")
	}
	if _, err := s.mutateByte(&entry{param: &canonical.Parameter{Value: int64(0)}}, bad, nil); err == nil {
		t.Error("byte unexpected method: want error")
	}
	if _, err := s.mutateFloat(&entry{param: &canonical.Parameter{Value: float64(0)}}, bad, nil); err == nil {
		t.Error("float unexpected method: want error")
	}
	if _, err := s.mutateIPAddr(&entry{param: &canonical.Parameter{Value: "0.0.0.0"}}, bad, nil); err == nil {
		t.Error("ipaddr unexpected method: want error")
	}
}

// TestMutateIPAddr_ClampHigh: setValue above max clamps to max.
func TestMutateIPAddr_ClampHigh(t *testing.T) {
	s := newMutServer()
	e := &entry{acpType: codec.TypeIPAddr, param: &canonical.Parameter{
		Value: "10.0.0.0", Maximum: "10.0.0.10", Minimum: "10.0.0.0",
	}}
	// setValue 10.0.0.255 (above max) → clamp to 10.0.0.10.
	out, err := s.mutateIPAddr(e, codec.MethodSetValue, []byte{10, 0, 0, 255})
	if err != nil {
		t.Fatalf("mutateIPAddr: %v", err)
	}
	if len(out) != 4 {
		t.Fatalf("want 4 bytes, got %d", len(out))
	}
	if e.param.Value != "10.0.0.10" {
		t.Errorf("clamp-high = %v, want 10.0.0.10", e.param.Value)
	}
}

// TestMutateEnum_NoItems: an enum with no items errors out.
func TestMutateEnum_NoItems(t *testing.T) {
	s := newMutServer()
	e := &entry{acpType: codec.TypeEnum, param: &canonical.Parameter{
		Header: canonical.Header{Identifier: "Mode"}}}
	if _, err := s.mutateEnum(e, codec.MethodSetValue, []byte{0}); err == nil {
		t.Fatal("enum with no items: want error")
	}
}
