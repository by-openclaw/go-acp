package acp1

import (
	"context"
	"path/filepath"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
	"dhs/internal/wiretrace"
)

// TestValidate_CtxCanceled covers the per-trame ctx.Err() guard.
func TestValidate_CtxCanceled(t *testing.T) {
	p := &Plugin{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	trames := []wiretrace.Trame{
		{Direction: wiretrace.DirectionTx, Hex: hexFrame(t, &codec.Message{
			MTID: 1, MType: codec.MTypeRequest, MCode: byte(codec.MethodGetValue),
			ObjGroup: codec.GroupFrame, ObjID: 0})},
	}
	if _, err := p.Validate(ctx, trames, consumer.ValidateOpts{}); err == nil {
		t.Fatal("cancelled ctx: want error")
	}
}

// TestValidate_RootProbeSkippedInCollect: with --out-tree on, a getObject
// reply for the ROOT group is skipped (not emitted as an Object), plus a
// non-root object with the same group but a different id exercises the
// snapshot sort tiebreak.
func TestValidate_RootProbeSkippedInCollect(t *testing.T) {
	out := filepath.Join(t.TempDir(), "tree.json")
	p := &Plugin{}
	trames := []wiretrace.Trame{
		// getObject(root) reply → skipped in collect.
		{Direction: wiretrace.DirectionTx, Hex: hexFrame(t, &codec.Message{
			MTID: 1, MType: codec.MTypeRequest, MCode: byte(codec.MethodGetObject),
			ObjGroup: codec.GroupRoot, ObjID: 0})},
		{Direction: wiretrace.DirectionRx, Hex: hexFrame(t, &codec.Message{
			MTID: 1, MType: codec.MTypeReply, MCode: byte(codec.MethodGetObject),
			ObjGroup: codec.GroupRoot, ObjID: 0, Value: rootObject(0, 2, 0, 0)})},
		// Two control objects (same group, different id) → sort tiebreak.
		{Direction: wiretrace.DirectionTx, Hex: hexFrame(t, &codec.Message{
			MTID: 2, MType: codec.MTypeRequest, MCode: byte(codec.MethodGetObject),
			ObjGroup: codec.GroupControl, ObjID: 1})},
		{Direction: wiretrace.DirectionRx, Hex: hexFrame(t, &codec.Message{
			MTID: 2, MType: codec.MTypeReply, MCode: byte(codec.MethodGetObject),
			ObjGroup: codec.GroupControl, ObjID: 1, Value: integerObject(5, "B")})},
		{Direction: wiretrace.DirectionTx, Hex: hexFrame(t, &codec.Message{
			MTID: 3, MType: codec.MTypeRequest, MCode: byte(codec.MethodGetObject),
			ObjGroup: codec.GroupControl, ObjID: 0})},
		{Direction: wiretrace.DirectionRx, Hex: hexFrame(t, &codec.Message{
			MTID: 3, MType: codec.MTypeReply, MCode: byte(codec.MethodGetObject),
			ObjGroup: codec.GroupControl, ObjID: 0, Value: integerObject(7, "A")})},
	}
	if _, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{OutTree: out}); err != nil {
		t.Fatalf("Validate out-tree: %v", err)
	}
}

// TestValidate_OutParamsYAML covers the YAML extension branch of
// writeSnapshotFile via --out-params.
func TestValidate_OutParamsYAML(t *testing.T) {
	out := filepath.Join(t.TempDir(), "params.yaml")
	p := &Plugin{}
	trames := []wiretrace.Trame{
		{Direction: wiretrace.DirectionTx, Hex: hexFrame(t, &codec.Message{
			MTID: 1, MType: codec.MTypeRequest, MCode: byte(codec.MethodGetObject),
			ObjGroup: codec.GroupControl, ObjID: 0})},
		{Direction: wiretrace.DirectionRx, Hex: hexFrame(t, &codec.Message{
			MTID: 1, MType: codec.MTypeReply, MCode: byte(codec.MethodGetObject),
			ObjGroup: codec.GroupControl, ObjID: 0, Value: integerObject(7, "A")})},
	}
	if _, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{OutParams: out}); err != nil {
		t.Fatalf("Validate out-params yaml: %v", err)
	}
}

// TestValidate_WriteErrors covers the os.Create failure path for both
// out-tree and out-params (path under a non-existent directory).
func TestValidate_WriteErrors(t *testing.T) {
	badPath := filepath.Join(t.TempDir(), "no-such-dir", "x.json")
	p := &Plugin{}
	if _, err := p.Validate(context.Background(), nil, consumer.ValidateOpts{OutTree: badPath}); err == nil {
		t.Fatal("out-tree bad path: want error")
	}
	badParams := filepath.Join(t.TempDir(), "no-such-dir", "x.csv")
	if _, err := p.Validate(context.Background(), nil, consumer.ValidateOpts{OutParams: badParams}); err == nil {
		t.Fatal("out-params bad path: want error")
	}
}
