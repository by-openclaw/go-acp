package acp1

import (
	"context"
	"encoding/hex"
	"path/filepath"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
	"dhs/internal/wiretrace"
)

func hexFrame(t *testing.T, m *codec.Message) string {
	t.Helper()
	b, err := m.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return hex.EncodeToString(b)
}

func TestValidate_FullScan(t *testing.T) {
	p := &Plugin{}
	bigBad := make([]byte, 20) // PVER byte (offset 4) = 0 → decode error; >16 → shortHex truncation
	trames := []wiretrace.Trame{
		{Direction: wiretrace.DirectionTx, Hex: "zz"},               // hex decode error
		{Direction: wiretrace.DirectionRx, Hex: "0102"},             // too short → decode error + shortHex
		{Direction: wiretrace.DirectionRx, Hex: hex.EncodeToString(bigBad)}, // >16 bytes, bad PVER → shortHex >16 branch
		{Direction: wiretrace.DirectionTx, Hex: hexFrame(t, &codec.Message{
			MTID: 5, MType: codec.MTypeRequest, MCode: byte(codec.MethodGetObject), ObjGroup: codec.GroupControl, ObjID: 0})},
		{Direction: wiretrace.DirectionRx, Hex: hexFrame(t, &codec.Message{
			MTID: 5, MType: codec.MTypeReply, MCode: byte(codec.MethodGetObject),
			ObjGroup: codec.GroupControl, ObjID: 0, Value: integerObject(5, "Level")})},
		{Direction: wiretrace.DirectionRx, Hex: hexFrame(t, &codec.Message{
			MTID: 9, MType: codec.MTypeReply, MCode: byte(codec.MethodGetObject), ObjGroup: codec.GroupControl, ObjID: 0,
			Value: integerObject(5, "Level")})}, // MTID mismatch → invariant
		{Direction: wiretrace.DirectionTx, Hex: hexFrame(t, &codec.Message{
			MTID: 0, MType: codec.MTypeRequest, MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupFrame, ObjID: 0})}, // MTID=0 request → invariant
		{Direction: wiretrace.DirectionTx, Hex: hexFrame(t, &codec.Message{
			MTID: 7, MType: codec.MTypeRequest, MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupFrame, ObjID: 0})},
		{Direction: wiretrace.DirectionRx, Hex: hexFrame(t, &codec.Message{
			MTID: 7, MType: codec.MTypeReply, MCode: byte(codec.MethodGetValue),
			ObjGroup: codec.GroupFrame, ObjID: 0, Value: []byte{2, 2, 0}})}, // frame-status capture
	}

	out := filepath.Join(t.TempDir(), "tree.json")
	rep, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{OutTree: out})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if rep.TramesProcessed == 0 {
		t.Error("no trames processed")
	}
	if len(rep.Errors) < 3 {
		t.Errorf("expected >=3 decode errors, got %d", len(rep.Errors))
	}
	if len(rep.Invariants) < 1 {
		t.Errorf("expected invariant violations, got %d", len(rep.Invariants))
	}
}

func TestValidate_StopAt(t *testing.T) {
	p := &Plugin{}
	trames := []wiretrace.Trame{
		{Direction: wiretrace.DirectionTx, Hex: hexFrame(t, &codec.Message{
			MTID: 1, MType: codec.MTypeRequest, MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupFrame})},
		{Direction: wiretrace.DirectionRx, Note: "HALT", Hex: hexFrame(t, &codec.Message{
			MTID: 1, MType: codec.MTypeReply, MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupFrame, Value: []byte{1, 2}})},
		{Direction: wiretrace.DirectionTx, Hex: hexFrame(t, &codec.Message{
			MTID: 2, MType: codec.MTypeRequest, MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupFrame})},
	}
	rep, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{StopAt: "HALT"})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if rep.StoppedAt != "HALT" {
		t.Errorf("StoppedAt = %q, want HALT", rep.StoppedAt)
	}
}

func TestValidate_OutParamsNowWrites(t *testing.T) {
	p := &Plugin{}
	out := filepath.Join(t.TempDir(), "params.json")
	if _, err := p.Validate(context.Background(), nil, consumer.ValidateOpts{OutParams: out}); err != nil {
		t.Errorf("OutParams should be supported now (empty dump on no input), got: %v", err)
	}
}

func TestShortHex(t *testing.T) {
	if shortHex([]byte{0xAB, 0xCD}) != "abcd" {
		t.Error("shortHex short")
	}
	long := make([]byte, 32)
	if got := shortHex(long); len(got) != 32 { // 16 bytes → 32 hex chars
		t.Errorf("shortHex long = %d chars, want 32", len(got))
	}
}
