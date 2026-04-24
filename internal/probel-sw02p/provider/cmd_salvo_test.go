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
