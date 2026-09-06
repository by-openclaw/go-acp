package acp2

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"dhs/internal/acp2/codec"
	"dhs/internal/consumer"
	"dhs/internal/wiretrace"
)

// acp2HeaderBytes builds a bare 4-byte ACP2 message header + optional body.
func acp2HeaderBytes(typ codec.ACP2MsgType, mtid, funcOrStat, pid uint8, body []byte) []byte {
	b := make([]byte, 4+len(body))
	b[0] = byte(typ)
	b[1] = mtid
	b[2] = funcOrStat
	b[3] = pid
	copy(b[4:], body)
	return b
}

// an2Wrap wraps a payload in an AN2 frame and returns it as a hex Trame.
func an2Trame(t *testing.T, proto codec.AN2Proto, typ codec.AN2Type, mtid uint8, payload []byte, dir wiretrace.Direction, note string) wiretrace.Trame {
	t.Helper()
	raw, err := codec.EncodeAN2Frame(&codec.AN2Frame{
		Proto:   proto,
		Slot:    1,
		MTID:    mtid,
		Type:    typ,
		Payload: payload,
	})
	if err != nil {
		t.Fatalf("EncodeAN2Frame: %v", err)
	}
	return wiretrace.Trame{Direction: dir, Hex: hex.EncodeToString(raw), Note: note}
}

func u32BE(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

// TestValidate_HappyPath drives a clean request→reply→announce→error
// sequence with no invariant violations.
func TestValidate_HappyPath(t *testing.T) {
	p := &Plugin{}
	body := append(u32BE(100), u32BE(0)...) // obj-id, idx

	req := acp2HeaderBytes(codec.ACP2TypeRequest, 7, uint8(codec.ACP2FuncGetProperty), 1, body)
	rep := acp2HeaderBytes(codec.ACP2TypeReply, 7, uint8(codec.ACP2FuncGetProperty), 1, body)
	ann := acp2HeaderBytes(codec.ACP2TypeAnnounce, 0, 0, 8, body)
	errm := acp2HeaderBytes(codec.ACP2TypeError, 1, uint8(codec.ErrInvalidValue), 0, nil)

	trames := []wiretrace.Trame{
		an2Trame(t, codec.AN2ProtoACP2, codec.AN2TypeData, 0, req, wiretrace.DirectionTx, ""),
		an2Trame(t, codec.AN2ProtoACP2, codec.AN2TypeData, 0, rep, wiretrace.DirectionRx, ""),
		an2Trame(t, codec.AN2ProtoACP2, codec.AN2TypeData, 0, ann, wiretrace.DirectionRx, ""),
		an2Trame(t, codec.AN2ProtoACP2, codec.AN2TypeData, 0, errm, wiretrace.DirectionRx, ""),
	}

	rep0, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if rep0.TramesProcessed != 4 {
		t.Errorf("TramesProcessed = %d, want 4", rep0.TramesProcessed)
	}
	if len(rep0.Errors) != 0 {
		t.Errorf("unexpected errors: %v", rep0.Errors)
	}
	if len(rep0.Invariants) != 0 {
		t.Errorf("unexpected invariants: %v", rep0.Invariants)
	}
	if rep0.PerDirection[wiretrace.DirectionTx] != 1 || rep0.PerDirection[wiretrace.DirectionRx] != 3 {
		t.Errorf("per-direction = %v", rep0.PerDirection)
	}
}

// TestValidate_InvariantViolations triggers each ACP2 invariant branch:
// request mtid=0, reply mtid mismatch, announce mtid!=0, error stat out of
// range, and unknown msg type.
func TestValidate_InvariantViolations(t *testing.T) {
	p := &Plugin{}
	body := append(u32BE(1), u32BE(0)...)

	req0 := acp2HeaderBytes(codec.ACP2TypeRequest, 0, uint8(codec.ACP2FuncGetProperty), 1, body) // mtid=0
	reqOK := acp2HeaderBytes(codec.ACP2TypeRequest, 5, uint8(codec.ACP2FuncGetProperty), 1, body)
	repBad := acp2HeaderBytes(codec.ACP2TypeReply, 9, uint8(codec.ACP2FuncGetProperty), 1, body) // mtid!=5
	annBad := acp2HeaderBytes(codec.ACP2TypeAnnounce, 3, 0, 8, body)                             // mtid!=0
	errBad := acp2HeaderBytes(codec.ACP2TypeError, 1, 99, 0, nil)                                // stat>5
	unknown := acp2HeaderBytes(codec.ACP2MsgType(7), 1, 0, 0, body)                              // unknown type

	trames := []wiretrace.Trame{
		an2Trame(t, codec.AN2ProtoACP2, codec.AN2TypeData, 0, req0, wiretrace.DirectionTx, ""),
		an2Trame(t, codec.AN2ProtoACP2, codec.AN2TypeData, 0, reqOK, wiretrace.DirectionTx, ""),
		an2Trame(t, codec.AN2ProtoACP2, codec.AN2TypeData, 0, repBad, wiretrace.DirectionRx, ""),
		an2Trame(t, codec.AN2ProtoACP2, codec.AN2TypeData, 0, annBad, wiretrace.DirectionRx, ""),
		an2Trame(t, codec.AN2ProtoACP2, codec.AN2TypeData, 0, errBad, wiretrace.DirectionRx, ""),
		an2Trame(t, codec.AN2ProtoACP2, codec.AN2TypeData, 0, unknown, wiretrace.DirectionRx, ""),
	}
	rep, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(rep.Invariants) != 5 {
		t.Fatalf("Invariants = %d (%v), want 5", len(rep.Invariants), rep.Invariants)
	}
}

// TestValidate_ACP2FrameWrongAN2Type covers the "ACP2 must use AN2TypeData"
// invariant branch.
func TestValidate_ACP2FrameWrongAN2Type(t *testing.T) {
	p := &Plugin{}
	body := acp2HeaderBytes(codec.ACP2TypeRequest, 1, 2, 1, append(u32BE(1), u32BE(0)...))
	trames := []wiretrace.Trame{
		an2Trame(t, codec.AN2ProtoACP2, codec.AN2TypeRequest, 1, body, wiretrace.DirectionTx, ""),
	}
	rep, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(rep.Invariants) != 1 {
		t.Errorf("Invariants = %v, want 1 (wrong AN2 type)", rep.Invariants)
	}
}

// TestValidate_AN2InternalMTIDZero covers the AN2-internal req/reply mtid=0
// invariant branch.
func TestValidate_AN2InternalMTIDZero(t *testing.T) {
	p := &Plugin{}
	trames := []wiretrace.Trame{
		an2Trame(t, codec.AN2ProtoInternal, codec.AN2TypeRequest, 0, []byte{0x00}, wiretrace.DirectionTx, ""),
	}
	rep, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(rep.Invariants) != 1 {
		t.Errorf("Invariants = %v, want 1 (an2 internal mtid=0)", rep.Invariants)
	}
}

// TestValidate_DecodeErrors covers the hex-decode and AN2-decode error
// branches.
func TestValidate_DecodeErrors(t *testing.T) {
	p := &Plugin{}
	trames := []wiretrace.Trame{
		{Direction: wiretrace.DirectionRx, Hex: "zznothex"},         // hex decode fails
		{Direction: wiretrace.DirectionRx, Hex: "0000000000000000"}, // bad AN2 magic
	}
	rep, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(rep.Errors) != 2 {
		t.Fatalf("Errors = %d (%v), want 2", len(rep.Errors), rep.Errors)
	}
	if rep.TramesProcessed != 0 {
		t.Errorf("TramesProcessed = %d, want 0", rep.TramesProcessed)
	}
}

// TestValidate_ACP2DecodeError covers the inner ACP2 decode failure branch
// (AN2 frame is fine but the ACP2 payload is too short).
func TestValidate_ACP2DecodeError(t *testing.T) {
	p := &Plugin{}
	trames := []wiretrace.Trame{
		an2Trame(t, codec.AN2ProtoACP2, codec.AN2TypeData, 0, []byte{0x00, 0x01}, wiretrace.DirectionRx, ""),
	}
	rep, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(rep.Errors) != 1 {
		t.Errorf("Errors = %v, want 1 (acp2 decode)", rep.Errors)
	}
}

// TestValidate_StopAt covers the early-stop branch on a matching note.
func TestValidate_StopAt(t *testing.T) {
	p := &Plugin{}
	body := append(u32BE(1), u32BE(0)...)
	req := acp2HeaderBytes(codec.ACP2TypeRequest, 1, uint8(codec.ACP2FuncGetProperty), 1, body)
	trames := []wiretrace.Trame{
		an2Trame(t, codec.AN2ProtoACP2, codec.AN2TypeData, 0, req, wiretrace.DirectionTx, "first"),
		an2Trame(t, codec.AN2ProtoACP2, codec.AN2TypeData, 0, req, wiretrace.DirectionTx, "STOP"),
		an2Trame(t, codec.AN2ProtoACP2, codec.AN2TypeData, 0, req, wiretrace.DirectionTx, "third"),
	}
	rep, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{StopAt: "STOP"})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if rep.StoppedAt != "STOP" {
		t.Errorf("StoppedAt = %q, want STOP", rep.StoppedAt)
	}
	if rep.TramesProcessed != 2 {
		t.Errorf("TramesProcessed = %d, want 2 (stopped before third)", rep.TramesProcessed)
	}
}

// TestValidate_OutTreeNotImplemented covers the early-return guard.
func TestValidate_OutTreeNotImplemented(t *testing.T) {
	p := &Plugin{}
	if _, err := p.Validate(context.Background(), nil, consumer.ValidateOpts{OutTree: "x.json"}); err == nil {
		t.Error("expected error for --out-tree")
	}
	if _, err := p.Validate(context.Background(), nil, consumer.ValidateOpts{OutParams: "x.csv"}); err == nil {
		t.Error("expected error for --out-params")
	}
}

// TestValidate_ContextCancelled covers the ctx.Err() early-return branch.
func TestValidate_ContextCancelled(t *testing.T) {
	p := &Plugin{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	body := acp2HeaderBytes(codec.ACP2TypeRequest, 1, uint8(codec.ACP2FuncGetProperty), 1, append(u32BE(1), u32BE(0)...))
	trames := []wiretrace.Trame{
		an2Trame(t, codec.AN2ProtoACP2, codec.AN2TypeData, 0, body, wiretrace.DirectionTx, ""),
	}
	if _, err := p.Validate(ctx, trames, consumer.ValidateOpts{}); err == nil {
		t.Error("expected context error")
	}
}

// TestShortHex covers both the truncation and pass-through branches.
func TestShortHex(t *testing.T) {
	long := make([]byte, 32)
	for i := range long {
		long[i] = byte(i)
	}
	if got := shortHex(long); len(got) != 32 { // 16 bytes → 32 hex chars
		t.Errorf("shortHex(32 bytes) len = %d, want 32", len(got))
	}
	if got := shortHex([]byte{0xAB, 0xCD}); got != "abcd" {
		t.Errorf("shortHex short = %q, want abcd", got)
	}
}
