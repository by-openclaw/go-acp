package acp2

import (
	"encoding/binary"
	"testing"

	"dhs/internal/acp2/codec"
	"dhs/internal/export/canonical"
)

// TestApplySetEnum_SparseEnumMap pins #346: applySetEnum must validate
// the incoming wire idx against the sparse u32 ids declared in
// canonical.EnumMap.Value, not the positional 0..N-1 length. Real
// Neuron enums use ids like 786 "Automatic" / 791 "Full Speed";
// rejecting them as "out of bounds" when len(EnumMap)==2 freezes the
// VSM UI on every Set.
func TestApplySetEnum_SparseEnumMap(t *testing.T) {
	param := &canonical.Parameter{
		Header: canonical.Header{Number: 21127, Identifier: "Fan Control",
			Access: canonical.AccessReadWrite},
		Type:  canonical.ParamEnum,
		Value: int64(786),
		EnumMap: []canonical.EnumEntry{
			{Key: "Automatic", Value: 786},
			{Key: "Full Speed", Value: 791},
		},
	}
	e := &entry{
		objID: 21127, label: param.Identifier, access: 0x03,
		objType: codec.ObjTypeEnum, numType: codec.NumTypeU32,
		param: param,
	}
	srv := &server{}

	tests := []struct {
		name    string
		idx     uint32
		wantErr codec.ACP2ErrStatus
	}{
		{"sparse_786_Automatic_accept", 786, 0},
		{"sparse_791_FullSpeed_accept", 791, 0},
		{"out_of_set_999_reject", 999, codec.ErrInvalidValue},
		{"positional_0_reject_when_sparse", 0, codec.ErrInvalidValue},
		{"positional_1_reject_when_sparse", 1, codec.ErrInvalidValue},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := make([]byte, 4)
			binary.BigEndian.PutUint32(data, tt.idx)
			in := &codec.Property{PID: codec.PIDValue,
				VType: uint8(codec.NumTypeU32), PLen: 8, Data: data}
			_, status, _ := srv.applySetEnum(e, in)
			if status != tt.wantErr {
				t.Errorf("idx=%d status=%d want=%d", tt.idx, status, tt.wantErr)
			}
			if tt.wantErr == 0 && param.Value != int64(tt.idx) {
				t.Errorf("param.Value=%v want %d", param.Value, tt.idx)
			}
		})
	}
}

// TestApplySetEnum_LegacyEnumeration covers the fallback path: when
// canonical lacks an EnumMap (older fixtures with a comma/newline
// Enumeration string only), accept idx in [0, N) where N is the
// label count. Keeps legacy fixtures round-tripping.
func TestApplySetEnum_LegacyEnumeration(t *testing.T) {
	enum := "Off,On,Auto"
	param := &canonical.Parameter{
		Header: canonical.Header{Number: 1, Identifier: "Mode",
			Access: canonical.AccessReadWrite},
		Type:        canonical.ParamEnum,
		Value:       int64(1),
		Enumeration: &enum,
	}
	e := &entry{
		objID: 1, label: param.Identifier, access: 0x03,
		objType: codec.ObjTypeEnum, numType: codec.NumTypeU32,
		param: param,
	}
	srv := &server{}

	tests := []struct {
		name    string
		idx     uint32
		wantErr codec.ACP2ErrStatus
	}{
		{"legacy_0_accept", 0, 0},
		{"legacy_2_accept", 2, 0},
		{"legacy_3_reject", 3, codec.ErrInvalidValue},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := make([]byte, 4)
			binary.BigEndian.PutUint32(data, tt.idx)
			in := &codec.Property{PID: codec.PIDValue,
				VType: uint8(codec.NumTypeU32), PLen: 8, Data: data}
			_, status, _ := srv.applySetEnum(e, in)
			if status != tt.wantErr {
				t.Errorf("idx=%d status=%d want=%d", tt.idx, status, tt.wantErr)
			}
		})
	}
}
