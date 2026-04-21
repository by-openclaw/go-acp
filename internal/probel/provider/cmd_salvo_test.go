package probel

import (
	"testing"

	"acp/internal/probel/codec"
)

// TestSalvoBuildFireInterrogate exercises the full salvo lifecycle on
// the provider side:
//   1. rx 120 adds two crosspoints to salvo 5
//   2. rx 124 iterates the slots via tx 125 with ascending validity
//   3. rx 121 (op=set) applies and returns tx 123 status=set
//   4. tree now reflects the crosspoints
func TestSalvoBuildFireInterrogate(t *testing.T) {
	srv := newComplianceServer(t)

	// Step 1 — add two crosspoints to salvo 5.
	if _, err := srv.handle(codec.EncodeSalvoConnectOnGo(codec.SalvoConnectOnGoParams{
		MatrixID: 0, LevelID: 0, DestinationID: 0, SourceID: 1, SalvoID: 5,
	})); err != nil {
		t.Fatalf("add #1: %v", err)
	}
	if _, err := srv.handle(codec.EncodeSalvoConnectOnGo(codec.SalvoConnectOnGoParams{
		MatrixID: 0, LevelID: 0, DestinationID: 2, SourceID: 3, SalvoID: 5,
	})); err != nil {
		t.Fatalf("add #2: %v", err)
	}

	// Step 2 — interrogate both slots.
	for idx, wantValid := range []codec.SalvoGroupTallyValidity{
		codec.SalvoTallyValidMore, codec.SalvoTallyValidLast,
	} {
		res, err := srv.handle(codec.EncodeSalvoGroupInterrogate(codec.SalvoGroupInterrogateParams{
			SalvoID: 5, ConnectIndex: uint8(idx),
		}))
		if err != nil {
			t.Fatalf("interrogate #%d: %v", idx, err)
		}
		tally, err := codec.DecodeSalvoGroupTally(*res.reply)
		if err != nil {
			t.Fatalf("decode tx 125: %v", err)
		}
		if tally.Validity != wantValid {
			t.Errorf("slot %d validity = %#x; want %#x", idx, tally.Validity, wantValid)
		}
	}

	// Step 2.5 — out-of-range index yields Invalid.
	res, err := srv.handle(codec.EncodeSalvoGroupInterrogate(codec.SalvoGroupInterrogateParams{
		SalvoID: 5, ConnectIndex: 99,
	}))
	if err != nil {
		t.Fatalf("invalid-idx: %v", err)
	}
	tally, _ := codec.DecodeSalvoGroupTally(*res.reply)
	if tally.Validity != codec.SalvoTallyInvalid {
		t.Errorf("oob validity = %#x; want SalvoTallyInvalid", tally.Validity)
	}

	// Step 3 — fire the salvo.
	fire, err := srv.handle(codec.EncodeSalvoGo(codec.SalvoGoParams{
		Op: codec.SalvoOpSet, SalvoID: 5,
	}))
	if err != nil {
		t.Fatalf("fire: %v", err)
	}
	done, _ := codec.DecodeSalvoGoDoneAck(*fire.reply)
	if done.Status != codec.SalvoDoneSet {
		t.Errorf("done status = %#x; want SalvoDoneSet", done.Status)
	}

	// Step 4 — verify tree reflects routes.
	if src, ok := srv.tree.currentSource(0, 0, 0); !ok || src != 1 {
		t.Errorf("dst 0 source = (%d, %v); want (1, true)", src, ok)
	}
	if src, ok := srv.tree.currentSource(0, 0, 2); !ok || src != 3 {
		t.Errorf("dst 2 source = (%d, %v); want (3, true)", src, ok)
	}

	// Step 4.5 — salvo is drained; interrogating returns Invalid.
	res, _ = srv.handle(codec.EncodeSalvoGroupInterrogate(codec.SalvoGroupInterrogateParams{
		SalvoID: 5, ConnectIndex: 0,
	}))
	drained, _ := codec.DecodeSalvoGroupTally(*res.reply)
	if drained.Validity != codec.SalvoTallyInvalid {
		t.Errorf("drained validity = %#x; want Invalid", drained.Validity)
	}
}

// TestSalvoClearDiscards: rx 121 op=01 (clear) wipes the salvo and
// tx 123 reports SalvoDoneCleared.
func TestSalvoClearDiscards(t *testing.T) {
	srv := newComplianceServer(t)
	_, _ = srv.handle(codec.EncodeSalvoConnectOnGo(codec.SalvoConnectOnGoParams{
		MatrixID: 0, LevelID: 0, DestinationID: 0, SourceID: 1, SalvoID: 2,
	}))
	res, err := srv.handle(codec.EncodeSalvoGo(codec.SalvoGoParams{
		Op: codec.SalvoOpClear, SalvoID: 2,
	}))
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	done, _ := codec.DecodeSalvoGoDoneAck(*res.reply)
	if done.Status != codec.SalvoDoneCleared {
		t.Errorf("status = %#x; want SalvoDoneCleared", done.Status)
	}
	// Tree should NOT reflect the route — salvo was cleared, not applied.
	if _, ok := srv.tree.currentSource(0, 0, 0); ok {
		t.Error("crosspoint applied despite clear-salvo")
	}
}

// TestSalvoGoNoneWhenEmpty: firing an empty salvo yields SalvoDoneNone.
func TestSalvoGoNoneWhenEmpty(t *testing.T) {
	srv := newComplianceServer(t)
	res, err := srv.handle(codec.EncodeSalvoGo(codec.SalvoGoParams{
		Op: codec.SalvoOpSet, SalvoID: 99,
	}))
	if err != nil {
		t.Fatalf("fire-empty: %v", err)
	}
	done, _ := codec.DecodeSalvoGoDoneAck(*res.reply)
	if done.Status != codec.SalvoDoneNone {
		t.Errorf("status = %#x; want SalvoDoneNone", done.Status)
	}
}
