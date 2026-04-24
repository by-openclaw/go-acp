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
