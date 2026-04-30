package probelsw02p

import (
	"io"
	"log/slog"
	"testing"

	"acp/internal/export/canonical"
	"acp/internal/probel-sw02p/codec"
)

// newTestServer builds a minimal SW-P-02 provider against a 4×4
// single-level canonical matrix. Used by handler-level tests that
// want to observe tree + profile mutations without a TCP listener.
func newTestServer(t *testing.T) *server {
	t.Helper()
	exp := &canonical.Export{
		Root: &canonical.Node{
			Header: canonical.Header{
				Number: 1, Identifier: "router", OID: "1",
				Children: []canonical.Element{
					&canonical.Matrix{
						Header: canonical.Header{
							Number: 1, Identifier: "matrix-0", OID: "1.1",
						},
						Type:        canonical.MatrixOneToN,
						Mode:        canonical.ModeLinear,
						TargetCount: 4,
						SourceCount: 4,
						Labels:      []canonical.MatrixLabel{{BasePath: "router.matrix-0.video"}},
					},
				},
			},
		},
	}
	return newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), exp)
}

// TestConnectOnGoAppendsPendingAndAcks locks in the rx 05 / tx 12
// contract from SW-P-02 §3.2.7 + §3.2.14:
//   - handler decodes (dst, src) from the Multiplier + MESSAGE bytes
//   - slot appended to the matrix's pending list on (matrix=0, level=0)
//   - reply is tx 12 CONNECT ON GO ACKNOWLEDGE with matching dst / src
//   - §3.2.14 "bad source bit always 0" honoured on the ack
func TestConnectOnGoAppendsPendingAndAcks(t *testing.T) {
	srv := newTestServer(t)

	// rx 05: dst=130 src=260 with BadSource=true on the ingress.
	in := codec.EncodeConnectOnGo(codec.ConnectOnGoParams{
		Destination: 130, Source: 260, BadSource: true,
	})
	res, err := srv.dispatch(in)
	if err != nil {
		t.Fatalf("dispatch rx 05: %v", err)
	}
	if res.reply == nil {
		t.Fatal("rx 05 produced no reply; want tx 12 ack")
	}
	if res.reply.ID != codec.TxConnectOnGoAck {
		t.Errorf("reply ID = %#x; want TxConnectOnGoAck (%#x)",
			res.reply.ID, codec.TxConnectOnGoAck)
	}

	// Tree pending list carries exactly one slot with our dst/src.
	state, ok := srv.tree.matrices[matrixKey{matrix: 0, level: 0}]
	if !ok {
		t.Fatal("tree has no (matrix=0, level=0) entry after rx 05")
	}
	if len(state.pending) != 1 {
		t.Fatalf("pending len = %d; want 1", len(state.pending))
	}
	if state.pending[0] != (pendingSlot{Destination: 130, Source: 260}) {
		t.Errorf("pending slot = %+v; want {Destination:130 Source:260}", state.pending[0])
	}

	// Ack decodes cleanly and echoes dst/src. §3.2.14 clamps the
	// BadSource bit to 0 regardless of the inbound state.
	ack, err := codec.DecodeConnectOnGoAck(*res.reply)
	if err != nil {
		t.Fatalf("decode tx 12: %v", err)
	}
	if ack.Destination != 130 || ack.Source != 260 {
		t.Errorf("ack echo = (dst=%d src=%d); want (130, 260)",
			ack.Destination, ack.Source)
	}
	if res.reply.Payload[0]&0x08 != 0 {
		t.Errorf("ack Multiplier bit 3 = 1; §3.2.14 requires 0")
	}
}

// TestConnectOnGoHandlerDecodeError confirms a short-payload rx 05
// flows back to the dispatcher as a decode error — the session loop
// notes the compliance event and does not write a reply.
func TestConnectOnGoHandlerDecodeError(t *testing.T) {
	srv := newTestServer(t)
	bad := codec.Frame{ID: codec.RxConnectOnGo, Payload: []byte{0x00, 0x01}} // 2 bytes
	res, err := srv.dispatch(bad)
	if err == nil {
		t.Fatal("want ErrShortPayload; got nil")
	}
	if res.reply != nil {
		t.Errorf("got reply on decode error; want none")
	}
}

// TestDispatchUnsupportedCommandNoop confirms the dispatcher falls
// through to a no-op for an unknown command id (outside the codec's
// registered set). Session code fires UnsupportedCommand for this case.
func TestDispatchUnsupportedCommandNoop(t *testing.T) {
	srv := newTestServer(t)
	res, err := srv.dispatch(codec.Frame{ID: codec.CommandID(0xDE)})
	if err != nil {
		t.Errorf("dispatch unsupported: %v; want nil error", err)
	}
	if res.reply != nil {
		t.Errorf("got reply for unsupported cmd; want none")
	}
}

// TestGoSetEmitsConnectedPerSlotAndAck is the behavioural contract
// for rx 06 GO op=Set (§3.2.8) on this provider:
//
//  1. Every staged (dst, src) in the pending buffer is applied to the
//     tree via applyConnectLenient.
//  2. Every APPLIED slot yields one tx 04 CONNECTED broadcast frame.
//  3. A trailing tx 13 GO DONE ACKNOWLEDGE with op=Set completes the
//     broadcast. No per-originator reply — broadcast goes to all.
//  4. Pending buffer is drained (empty afterwards).
//  5. Profile.SalvoEmittedConnected increments once per applied slot
//     to mark the §3.2.8 deviation.
//
// This is the SW-P-08 issue #92 pattern applied to SW-P-02 — real
// controllers never implement the §3.2.8 listener path; they rely on
// tx 04 broadcasts per §3.2.6.
func TestGoSetEmitsConnectedPerSlotAndAck(t *testing.T) {
	srv := newTestServer(t)

	// Stage three crosspoints on the default (matrix=0, level=0).
	for _, s := range []struct{ dst, src uint16 }{{0, 1}, {1, 2}, {2, 3}} {
		if _, err := srv.dispatch(codec.EncodeConnectOnGo(codec.ConnectOnGoParams{
			Destination: s.dst, Source: s.src,
		})); err != nil {
			t.Fatalf("stage slot (%d,%d): %v", s.dst, s.src, err)
		}
	}
	beforeEmit := srv.profile.Snapshot()[SalvoEmittedConnected]

	// rx 06 GO op=Set.
	res, err := srv.dispatch(codec.EncodeGo(codec.GoParams{Operation: codec.GoOpSet}))
	if err != nil {
		t.Fatalf("dispatch rx 06: %v", err)
	}
	if res.reply != nil {
		t.Errorf("GO returned a point-to-point reply; §3.2.15 requires broadcast only")
	}

	// 3 × tx 04 + 1 × tx 13 = 4 broadcast frames, in order.
	if len(res.broadcast) != 4 {
		t.Fatalf("broadcast len = %d; want 4 (3 tx04 + 1 tx13)", len(res.broadcast))
	}
	wantDstSrc := []struct{ dst, src uint16 }{{0, 1}, {1, 2}, {2, 3}}
	for i, want := range wantDstSrc {
		bf := res.broadcast[i]
		if bf.ID != codec.TxCrosspointConnected {
			t.Errorf("broadcast[%d] ID = %#x; want TxCrosspointConnected", i, bf.ID)
			continue
		}
		got, derr := codec.DecodeConnected(bf)
		if derr != nil {
			t.Errorf("broadcast[%d] decode: %v", i, derr)
			continue
		}
		if got.Destination != want.dst || got.Source != want.src {
			t.Errorf("broadcast[%d] = (dst=%d src=%d); want (dst=%d src=%d)",
				i, got.Destination, got.Source, want.dst, want.src)
		}
	}

	tail := res.broadcast[3]
	if tail.ID != codec.TxGoDoneAck {
		t.Errorf("broadcast[3] ID = %#x; want TxGoDoneAck", tail.ID)
	}
	ack, err := codec.DecodeGoDoneAck(tail)
	if err != nil {
		t.Fatalf("decode tx 13: %v", err)
	}
	if ack.Operation != codec.GoOpSet {
		t.Errorf("tx 13 op = %#x; want GoOpSet", ack.Operation)
	}

	// Tree reflects the applied crosspoints.
	state := srv.tree.matrices[matrixKey{matrix: 0, level: 0}]
	for _, want := range wantDstSrc {
		if got, ok := state.sources[want.dst]; !ok || got != want.src {
			t.Errorf("tree[%d] = (%d, ok=%v); want (%d, true)", want.dst, got, ok, want.src)
		}
	}
	if len(state.pending) != 0 {
		t.Errorf("pending not drained: %d slots remain", len(state.pending))
	}

	// Compliance counter moved by exactly 3.
	afterEmit := srv.profile.Snapshot()[SalvoEmittedConnected]
	if afterEmit != beforeEmit+3 {
		t.Errorf("SalvoEmittedConnected %d -> %d; want +3",
			beforeEmit, afterEmit)
	}
}

// TestGoGroupSalvoSetEmitsConnectedPerSlotAndAck exercises rx 36
// op=Set against a populated SalvoID — every slot applied, tx 04
// broadcast per slot, tx 38 trailer with Result=Set. Also verifies
// the SalvoID-keyed drain doesn't touch other groups.
func TestGoGroupSalvoSetEmitsConnectedPerSlotAndAck(t *testing.T) {
	srv := newTestServer(t)

	// Stage 2 slots under salvo 5, 1 slot under salvo 6.
	for _, s := range []struct {
		dst, src uint16
		salvo    uint8
	}{{0, 1, 5}, {1, 2, 5}, {2, 3, 6}} {
		if _, err := srv.dispatch(codec.EncodeConnectOnGoGroupSalvo(codec.ConnectOnGoGroupSalvoParams{
			Destination: s.dst, Source: s.src, SalvoID: s.salvo,
		})); err != nil {
			t.Fatalf("stage (%d,%d)@salvo%d: %v", s.dst, s.src, s.salvo, err)
		}
	}
	beforeEmit := srv.profile.Snapshot()[SalvoEmittedConnected]

	// Fire salvo 5 only.
	res, err := srv.dispatch(codec.EncodeGoGroupSalvo(codec.GoGroupSalvoParams{
		Operation: codec.GoOpSet, SalvoID: 5,
	}))
	if err != nil {
		t.Fatalf("dispatch rx 36: %v", err)
	}

	// 2 × tx 04 + 1 × tx 38 = 3 broadcasts.
	if len(res.broadcast) != 3 {
		t.Fatalf("broadcast len = %d; want 3", len(res.broadcast))
	}
	for i, want := range []struct{ dst, src uint16 }{{0, 1}, {1, 2}} {
		bf := res.broadcast[i]
		if bf.ID != codec.TxCrosspointConnected {
			t.Errorf("broadcast[%d] ID = %#x; want TxCrosspointConnected", i, bf.ID)
			continue
		}
		got, _ := codec.DecodeConnected(bf)
		if got.Destination != want.dst || got.Source != want.src {
			t.Errorf("broadcast[%d] = (dst=%d src=%d); want (%d, %d)",
				i, got.Destination, got.Source, want.dst, want.src)
		}
	}
	tail := res.broadcast[2]
	if tail.ID != codec.TxGoDoneGroupSalvoAck {
		t.Errorf("broadcast[2] ID = %#x; want TxGoDoneGroupSalvoAck", tail.ID)
	}
	ack, _ := codec.DecodeGoDoneGroupSalvoAck(tail)
	if ack.Result != codec.GoGroupResultSet || ack.SalvoID != 5 {
		t.Errorf("ack = %+v; want Result=Set SalvoID=5", ack)
	}

	// Salvo 6 untouched — still has 1 pending slot.
	state := srv.tree.matrices[matrixKey{matrix: 0, level: 0}]
	if len(state.pendingGroups[6]) != 1 {
		t.Errorf("salvo 6 touched: %d pending", len(state.pendingGroups[6]))
	}
	// Salvo 5 drained.
	if _, ok := state.pendingGroups[5]; ok {
		t.Errorf("salvo 5 not removed from map after drain")
	}
	afterEmit := srv.profile.Snapshot()[SalvoEmittedConnected]
	if afterEmit != beforeEmit+2 {
		t.Errorf("SalvoEmittedConnected %d -> %d; want +2",
			beforeEmit, afterEmit)
	}
}

// TestGoGroupSalvoSetEmptyReturnsEmptyResult covers the §3.2.39 third
// status: firing an empty salvo returns Result=Empty and emits no
// tx 04 broadcasts.
func TestGoGroupSalvoSetEmptyReturnsEmptyResult(t *testing.T) {
	srv := newTestServer(t)
	res, err := srv.dispatch(codec.EncodeGoGroupSalvo(codec.GoGroupSalvoParams{
		Operation: codec.GoOpSet, SalvoID: 99,
	}))
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(res.broadcast) != 1 {
		t.Fatalf("broadcast len = %d; want 1 (ack only)", len(res.broadcast))
	}
	ack, _ := codec.DecodeGoDoneGroupSalvoAck(res.broadcast[0])
	if ack.Result != codec.GoGroupResultEmpty {
		t.Errorf("Result = %#x; want GoGroupResultEmpty", ack.Result)
	}
}

// TestConnectOnGoGroupSalvoAppendsPerGroupAndAcks locks in the rx 35
// contract: one slot per frame, stashed under the declared SalvoID,
// ack echoes dst / src / SalvoID with bad-source bit clamped to 0.
func TestConnectOnGoGroupSalvoAppendsPerGroupAndAcks(t *testing.T) {
	srv := newTestServer(t)

	in := codec.EncodeConnectOnGoGroupSalvo(codec.ConnectOnGoGroupSalvoParams{
		Destination: 130, Source: 260, SalvoID: 7, BadSource: true,
	})
	res, err := srv.dispatch(in)
	if err != nil {
		t.Fatalf("dispatch rx 35: %v", err)
	}
	if res.reply == nil || res.reply.ID != codec.TxConnectOnGoGroupSalvoAck {
		t.Fatalf("reply missing / wrong ID: %+v", res.reply)
	}
	if res.reply.Payload[0]&0x08 != 0 {
		t.Errorf("ack Multiplier bit 3 = 1; §3.2.38 requires 0")
	}

	// Tree has the slot under salvo 7.
	state := srv.tree.matrices[matrixKey{matrix: 0, level: 0}]
	if state == nil || state.pendingGroups == nil {
		t.Fatal("tree has no pendingGroups after rx 35")
	}
	slots := state.pendingGroups[7]
	if len(slots) != 1 {
		t.Fatalf("pendingGroups[7] len = %d; want 1", len(slots))
	}
	if slots[0] != (pendingSlot{Destination: 130, Source: 260}) {
		t.Errorf("pendingGroups[7][0] = %+v; want {130 260}", slots[0])
	}
	// A second slot under a different SalvoID stays separate.
	if _, err := srv.dispatch(codec.EncodeConnectOnGoGroupSalvo(codec.ConnectOnGoGroupSalvoParams{
		Destination: 5, Source: 6, SalvoID: 8,
	})); err != nil {
		t.Fatalf("dispatch rx 35 #2: %v", err)
	}
	if len(state.pendingGroups[7]) != 1 {
		t.Errorf("salvo 7 contaminated after salvo 8 stage: %d slots",
			len(state.pendingGroups[7]))
	}
	if len(state.pendingGroups[8]) != 1 {
		t.Errorf("salvo 8 = %d slots; want 1", len(state.pendingGroups[8]))
	}
}

// TestExtendedConnectOnGoGroupSalvoUsesExtendedRange verifies rx 71
// accepts dst/src > 1023 and stages them into the same SalvoID-keyed
// buffer as rx 35, so a subsequent rx 36 GO GROUP SALVO fires both
// narrow and extended stages together.
func TestExtendedConnectOnGoGroupSalvoUsesExtendedRange(t *testing.T) {
	srv := newTestServer(t)

	// rx 35 (narrow, dst=5 src=6) under salvo 9.
	if _, err := srv.dispatch(codec.EncodeConnectOnGoGroupSalvo(codec.ConnectOnGoGroupSalvoParams{
		Destination: 5, Source: 6, SalvoID: 9,
	})); err != nil {
		t.Fatalf("stage narrow: %v", err)
	}
	// rx 71 (extended, dst=5000 src=10000) under the same salvo 9.
	in := codec.EncodeExtendedConnectOnGoGroupSalvo(codec.ExtendedConnectOnGoGroupSalvoParams{
		Destination: 5000, Source: 10000, SalvoID: 9,
	})
	res, err := srv.dispatch(in)
	if err != nil {
		t.Fatalf("dispatch rx 71: %v", err)
	}
	if res.reply == nil || res.reply.ID != codec.TxExtendedConnectOnGoGroupSalvoAck {
		t.Fatalf("reply missing / wrong ID: %+v", res.reply)
	}
	ack, _ := codec.DecodeExtendedConnectOnGoGroupSalvoAck(*res.reply)
	if ack.Destination != 5000 || ack.Source != 10000 || ack.SalvoID != 9 {
		t.Errorf("ack = %+v; want dst=5000 src=10000 salvo=9", ack)
	}

	// Shared buffer: salvo 9 has both the narrow + extended slots.
	state := srv.tree.matrices[matrixKey{matrix: 0, level: 0}]
	slots := state.pendingGroups[9]
	if len(slots) != 2 {
		t.Fatalf("pendingGroups[9] len = %d; want 2", len(slots))
	}
	if slots[0].Destination != 5 || slots[1].Destination != 5000 {
		t.Errorf("shared buffer = %+v; want [narrow dst=5, extended dst=5000]", slots)
	}
}

// TestGoClearDropsPendingAndAcks exercises the op=Clear path:
// pending buffer is drained without any tree mutation or tx 04
// broadcast, and the only broadcast frame is a tx 13 with op=Clear.
func TestGoClearDropsPendingAndAcks(t *testing.T) {
	srv := newTestServer(t)

	// Stage one slot.
	if _, err := srv.dispatch(codec.EncodeConnectOnGo(codec.ConnectOnGoParams{
		Destination: 0, Source: 1,
	})); err != nil {
		t.Fatalf("stage: %v", err)
	}
	beforeEmit := srv.profile.Snapshot()[SalvoEmittedConnected]

	res, err := srv.dispatch(codec.EncodeGo(codec.GoParams{Operation: codec.GoOpClear}))
	if err != nil {
		t.Fatalf("dispatch rx 06 clear: %v", err)
	}
	if len(res.broadcast) != 1 {
		t.Fatalf("broadcast len = %d; want 1 (tx13 only)", len(res.broadcast))
	}
	if res.broadcast[0].ID != codec.TxGoDoneAck {
		t.Errorf("broadcast ID = %#x; want TxGoDoneAck", res.broadcast[0].ID)
	}
	ack, _ := codec.DecodeGoDoneAck(res.broadcast[0])
	if ack.Operation != codec.GoOpClear {
		t.Errorf("tx 13 op = %#x; want GoOpClear", ack.Operation)
	}

	state := srv.tree.matrices[matrixKey{matrix: 0, level: 0}]
	if len(state.sources) != 0 {
		t.Errorf("clear path mutated tree: %d crosspoints", len(state.sources))
	}
	if len(state.pending) != 0 {
		t.Errorf("pending not drained: %d slots", len(state.pending))
	}
	if afterEmit := srv.profile.Snapshot()[SalvoEmittedConnected]; afterEmit != beforeEmit {
		t.Errorf("SalvoEmittedConnected moved on clear: %d -> %d", beforeEmit, afterEmit)
	}
}
