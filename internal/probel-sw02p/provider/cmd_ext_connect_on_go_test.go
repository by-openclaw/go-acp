package probelsw02p

import (
	"testing"

	"acp/internal/probel-sw02p/codec"
)

// TestExtendedConnectOnGoStagesAndAcks locks rx 069 / tx 070: one
// extended-addressed slot is staged into the unnamed pending buffer
// on (matrix=0, level=0), and the matrix replies with tx 070
// echoing dst/src.
func TestExtendedConnectOnGoStagesAndAcks(t *testing.T) {
	srv := newBareTestServer(t)

	in := codec.EncodeExtendedConnectOnGo(codec.ExtendedConnectOnGoParams{
		Destination: 5000, Source: 10000,
	})
	res, err := srv.dispatch(in)
	if err != nil {
		t.Fatalf("dispatch rx 069: %v", err)
	}
	if res.reply == nil || res.reply.ID != codec.TxExtendedConnectOnGoAck {
		t.Fatalf("reply missing / wrong ID: %+v", res.reply)
	}
	ack, err := codec.DecodeExtendedConnectOnGoAck(*res.reply)
	if err != nil {
		t.Fatalf("decode tx 070: %v", err)
	}
	if ack.Destination != 5000 || ack.Source != 10000 {
		t.Errorf("ack = (dst=%d src=%d); want (5000, 10000)", ack.Destination, ack.Source)
	}

	// Pending buffer carries exactly one slot, marked Extended.
	state := srv.tree.matrices[matrixKey{matrix: 0, level: 0}]
	if state == nil || len(state.pending) != 1 {
		t.Fatalf("pending len = %d; want 1", len(state.pending))
	}
	if state.pending[0] != (pendingSlot{Destination: 5000, Source: 10000, Extended: true}) {
		t.Errorf("pending[0] = %+v; want {5000 10000 Extended:true}", state.pending[0])
	}
}

// TestGoSetEmitsExtendedConnectedForExtendedSlots verifies the GO
// commit path selects the right CONNECTED form per slot origin: tx 04
// for narrow (rx 05) slots, tx 68 for extended (rx 69) slots. Tests
// mixed staging to confirm one GO fires both forms in one broadcast.
func TestGoSetEmitsExtendedConnectedForExtendedSlots(t *testing.T) {
	srv := newBareTestServer(t)

	// Stage a narrow slot via rx 05 + an extended slot via rx 69.
	if _, err := srv.dispatch(codec.EncodeConnectOnGo(codec.ConnectOnGoParams{
		Destination: 10, Source: 20,
	})); err != nil {
		t.Fatalf("stage narrow: %v", err)
	}
	if _, err := srv.dispatch(codec.EncodeExtendedConnectOnGo(codec.ExtendedConnectOnGoParams{
		Destination: 5000, Source: 10000,
	})); err != nil {
		t.Fatalf("stage extended: %v", err)
	}

	res, err := srv.dispatch(codec.EncodeGo(codec.GoParams{Operation: codec.GoOpSet}))
	if err != nil {
		t.Fatalf("dispatch rx 06: %v", err)
	}
	// Expect: tx 04 (narrow) + tx 68 (extended) + tx 13 (GO done) = 3 frames.
	if len(res.broadcast) != 3 {
		t.Fatalf("broadcast len = %d; want 3", len(res.broadcast))
	}
	if res.broadcast[0].ID != codec.TxCrosspointConnected {
		t.Errorf("broadcast[0] = %#x; want TxCrosspointConnected (tx 04)", res.broadcast[0].ID)
	}
	cp, _ := codec.DecodeConnected(res.broadcast[0])
	if cp.Destination != 10 || cp.Source != 20 {
		t.Errorf("narrow slot decoded = (%d,%d); want (10,20)", cp.Destination, cp.Source)
	}

	if res.broadcast[1].ID != codec.TxExtendedConnected {
		t.Errorf("broadcast[1] = %#x; want TxExtendedConnected (tx 68)", res.broadcast[1].ID)
	}
	ecp, _ := codec.DecodeExtendedConnected(res.broadcast[1])
	if ecp.Destination != 5000 || ecp.Source != 10000 {
		t.Errorf("extended slot decoded = (%d,%d); want (5000,10000)", ecp.Destination, ecp.Source)
	}

	if res.broadcast[2].ID != codec.TxGoDoneAck {
		t.Errorf("broadcast[2] = %#x; want TxGoDoneAck (tx 13)", res.broadcast[2].ID)
	}
}

// TestGoGroupSalvoSetEmitsExtendedConnectedForExtendedSlots exercises
// the rx 36 GO GROUP SALVO path with a mixed salvo — narrow rx 35
// slot + extended rx 71 slot under the same SalvoID — and verifies
// each commits with the correct CONNECTED form.
func TestGoGroupSalvoSetEmitsExtendedConnectedForExtendedSlots(t *testing.T) {
	srv := newBareTestServer(t)

	// Stage narrow (rx 35) + extended (rx 71) under salvo 9.
	if _, err := srv.dispatch(codec.EncodeConnectOnGoGroupSalvo(codec.ConnectOnGoGroupSalvoParams{
		Destination: 5, Source: 6, SalvoID: 9,
	})); err != nil {
		t.Fatalf("stage narrow: %v", err)
	}
	if _, err := srv.dispatch(codec.EncodeExtendedConnectOnGoGroupSalvo(codec.ExtendedConnectOnGoGroupSalvoParams{
		Destination: 5000, Source: 10000, SalvoID: 9,
	})); err != nil {
		t.Fatalf("stage extended: %v", err)
	}

	res, err := srv.dispatch(codec.EncodeGoGroupSalvo(codec.GoGroupSalvoParams{
		Operation: codec.GoOpSet, SalvoID: 9,
	}))
	if err != nil {
		t.Fatalf("dispatch rx 36: %v", err)
	}
	// tx 04 + tx 68 + tx 38 = 3 frames.
	if len(res.broadcast) != 3 {
		t.Fatalf("broadcast len = %d; want 3", len(res.broadcast))
	}
	if res.broadcast[0].ID != codec.TxCrosspointConnected {
		t.Errorf("broadcast[0] = %#x; want TxCrosspointConnected", res.broadcast[0].ID)
	}
	if res.broadcast[1].ID != codec.TxExtendedConnected {
		t.Errorf("broadcast[1] = %#x; want TxExtendedConnected", res.broadcast[1].ID)
	}
	if res.broadcast[2].ID != codec.TxGoDoneGroupSalvoAck {
		t.Errorf("broadcast[2] = %#x; want TxGoDoneGroupSalvoAck", res.broadcast[2].ID)
	}
}
