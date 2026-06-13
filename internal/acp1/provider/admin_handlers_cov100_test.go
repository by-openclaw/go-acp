package acp1

import (
	"context"
	"testing"

	"dhs/internal/devicemodel"
	"dhs/internal/export"
)

// TestAdminHandlers_MissingAndBadArgs drives every handler's argument-error
// arms plus readIntArg / readStringArg edge cases.
func TestAdminHandlers_MissingAndBadArgs(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	empty := map[string]any{}

	// slot.extract missing slot.
	if _, err := handleSlotExtract(ctx, s, empty); err == nil {
		t.Error("slot.extract missing slot: want error")
	}
	// slot.load missing slot / missing card.
	if _, err := handleSlotLoad(ctx, s, empty); err == nil {
		t.Error("slot.load missing slot: want error")
	}
	if _, err := handleSlotLoad(ctx, s, map[string]any{"slot": 1}); err == nil {
		t.Error("slot.load missing card: want error")
	}
	// slot.unload missing slot.
	if _, err := handleSlotUnload(ctx, s, empty); err == nil {
		t.Error("slot.unload missing slot: want error")
	}
	// slot.state missing slot / missing state / bad state / setStatus fail.
	if _, err := handleSlotState(ctx, s, empty); err == nil {
		t.Error("slot.state missing slot: want error")
	}
	// value.set missing path / missing value.
	if _, err := handleValueSet(ctx, s, empty); err == nil {
		t.Error("value.set missing path: want error")
	}
	if _, err := handleValueSet(ctx, s, map[string]any{"path": "1.2.2.0"}); err == nil {
		t.Error("value.set missing value: want error")
	}
	// value.get missing slot / group / id.
	if _, err := handleValueGet(ctx, s, empty); err == nil {
		t.Error("value.get missing slot: want error")
	}
	if _, err := handleValueGet(ctx, s, map[string]any{"slot": 1}); err == nil {
		t.Error("value.get missing group: want error")
	}
	if _, err := handleValueGet(ctx, s, map[string]any{"slot": 1, "group": 2}); err == nil {
		t.Error("value.get missing id: want error")
	}
}

// TestHandleValueSet_SetValueError: a value.set against a read-only object
// makes the underlying SetValue fail.
func TestHandleValueSet_SetValueError(t *testing.T) {
	s := newTestServer(t)
	// slot 1 identity[0] (Model) is read-only → SetValue returns an error.
	if _, err := handleValueSet(context.Background(), s,
		map[string]any{"path": "1.2.1.0", "value": "X"}); err == nil {
		t.Fatal("value.set on read-only object: want error")
	}
}

// TestHandleSlotState_SetStatusError: a slot beyond the frame array makes
// setSlotStatus fail.
func TestHandleSlotState_SetStatusError(t *testing.T) {
	s := newTestServer(t)
	// Frame fixture has 4 slots; slot 99 is out of range.
	if _, err := handleSlotState(context.Background(), s,
		map[string]any{"slot": 99, "state": "present"}); err == nil {
		t.Fatal("slot.state out-of-range slot: want error")
	}
}

// TestHandleValueGet_NoReply: a getValue for an absent object yields no
// reply → error.
func TestHandleValueGet_NoReply(t *testing.T) {
	s := newTestServer(t)
	if _, err := handleValueGet(context.Background(), s,
		map[string]any{"slot": 30, "group": 2, "id": 200}); err == nil {
		t.Fatal("value.get absent object: want error")
	}
}

// TestHandleSlotLoad_Success drives the success-result branch.
func TestHandleSlotLoad_Success(t *testing.T) {
	s := newTestServer(t)
	s.SetInsertTiming(InsertTimingFast)
	s.SetDMLibrary(&fakeDMResolver{schemas: map[string]*devicemodel.Schema{
		"RRS18-1601": {Slots: map[int]*export.Snapshot{
			1: {Slots: []export.SlotDump{{Slot: 1}}},
		}},
	}})
	resp, err := handleSlotLoad(context.Background(), s,
		map[string]any{"slot": 1, "card": "axon/synapse/RRS18-1601/acp1"})
	if err != nil {
		t.Fatalf("handleSlotLoad: %v", err)
	}
	if resp.Result["started"] != true {
		t.Errorf("started = %v", resp.Result["started"])
	}
}

// TestReadIntArg_Kinds covers the int and int64 conversion arms plus the
// bad-type and missing arms.
func TestReadIntArg_Kinds(t *testing.T) {
	if n, err := readIntArg(map[string]any{"k": int(5)}, "k"); err != nil || n != 5 {
		t.Errorf("int arg = %d,%v", n, err)
	}
	if n, err := readIntArg(map[string]any{"k": int64(7)}, "k"); err != nil || n != 7 {
		t.Errorf("int64 arg = %d,%v", n, err)
	}
	if _, err := readIntArg(map[string]any{"k": "x"}, "k"); err == nil {
		t.Error("string arg: want type error")
	}
	if _, err := readIntArg(map[string]any{}, "k"); err == nil {
		t.Error("missing arg: want error")
	}
}

// TestReadStringArg_Edge covers the missing-arg and bad-type arms.
func TestReadStringArg_Edge(t *testing.T) {
	if _, err := readStringArg(map[string]any{}, "k"); err == nil {
		t.Error("missing arg: want error")
	}
	if _, err := readStringArg(map[string]any{"k": 5}, "k"); err == nil {
		t.Error("non-string arg: want error")
	}
}
