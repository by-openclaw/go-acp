package probelsw02p

import (
	"testing"

	"acp/internal/probel-sw02p/codec"
)

// TestConnectAppliesRouteAndBroadcastsConnected locks in the rx 02 /
// tx 04 contract from SW-P-02 §3.2.4 + §3.2.6:
//   - handler decodes (dst, src) from the Multiplier + MESSAGE bytes
//   - route is applied on (matrix=0, level=0)
//   - tx 04 CROSSPOINT CONNECTED is broadcast on all ports (§3.2.6
//     "issued on ALL ports"), NOT a point-to-point reply
//   - the broadcast echoes the requested dst / src
func TestConnectAppliesRouteAndBroadcastsConnected(t *testing.T) {
	srv := newTestServer(t)

	in := codec.EncodeConnect(codec.ConnectParams{Destination: 1, Source: 2})
	res, err := srv.dispatch(in)
	if err != nil {
		t.Fatalf("dispatch rx 02: %v", err)
	}
	if res.reply != nil {
		t.Errorf("rx 02 returned a point-to-point reply; §3.2.6 requires broadcast only")
	}
	if len(res.broadcast) != 1 {
		t.Fatalf("broadcast len = %d; want 1 (tx 04)", len(res.broadcast))
	}
	if res.broadcast[0].ID != codec.TxCrosspointConnected {
		t.Errorf("broadcast ID = %#x; want TxCrosspointConnected", res.broadcast[0].ID)
	}
	cp, err := codec.DecodeConnected(res.broadcast[0])
	if err != nil {
		t.Fatalf("decode tx 04: %v", err)
	}
	if cp.Destination != 1 || cp.Source != 2 {
		t.Errorf("tx 04 = (dst=%d src=%d); want (1, 2)", cp.Destination, cp.Source)
	}

	// Tree records the crosspoint.
	state := srv.tree.matrices[matrixKey{matrix: 0, level: 0}]
	if got, ok := state.sources[1]; !ok || got != 2 {
		t.Errorf("tree[1] = (%d, ok=%v); want (2, true)", got, ok)
	}
}

// TestConnectOutOfRangeSkipsBroadcast confirms the lenient apply path:
// when (dst, src) falls outside the declared counts, the route is not
// recorded and NO tx 04 broadcast fires.
func TestConnectOutOfRangeSkipsBroadcast(t *testing.T) {
	srv := newTestServer(t) // targetCount = sourceCount = 4

	in := codec.EncodeConnect(codec.ConnectParams{Destination: 999, Source: 999})
	res, err := srv.dispatch(in)
	if err != nil {
		t.Fatalf("dispatch rx 02: %v", err)
	}
	if res.reply != nil || len(res.broadcast) != 0 {
		t.Errorf("out-of-range rx 02: reply=%+v broadcast=%d; want empty", res.reply, len(res.broadcast))
	}
}

// TestConnectHandlerDecodeError confirms a short-payload rx 02 flows
// back as a decode error — session loop fires the compliance event.
func TestConnectHandlerDecodeError(t *testing.T) {
	srv := newTestServer(t)
	bad := codec.Frame{ID: codec.RxConnect, Payload: []byte{0x00, 0x01}} // 2 bytes
	res, err := srv.dispatch(bad)
	if err == nil {
		t.Fatal("want ErrShortPayload; got nil")
	}
	if res.reply != nil || len(res.broadcast) != 0 {
		t.Errorf("got reply/broadcast on decode error; want none")
	}
}
