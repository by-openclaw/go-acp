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

// TestConnectRejectedWhenDstProtected_NoExistingRoute locks in the
// §3.2.60 silent-drop branch: protected dst with NO existing route
// → no tx 04, no state change, ProtectBlocksConnect event only (no
// state-echo event because there's nothing truthful to echo).
func TestConnectRejectedWhenDstProtected_NoExistingRoute(t *testing.T) {
	srv := newTestServer(t)

	// Pre-set dst=2 to ProtectProBel owned by Device 7. No prior route.
	if _, res := srv.tree.protectApply(2, 7, codec.ProtectProBel); res != protectApplyAccepted {
		t.Fatalf("protectApply seed: result = %v; want protectApplyAccepted", res)
	}

	in := codec.EncodeConnect(codec.ConnectParams{Destination: 2, Source: 3})
	res, err := srv.dispatch(in)
	if err != nil {
		t.Fatalf("dispatch rx 02: %v", err)
	}
	if res.reply != nil || len(res.broadcast) != 0 {
		t.Errorf("protected-dst rx 02 (no route): reply=%+v broadcast=%d; want both empty", res.reply, len(res.broadcast))
	}
	if _, ok := srv.tree.matrices[matrixKey{matrix: 0, level: 0}].sources[2]; ok {
		t.Errorf("tree[2] populated after blocked connect; want absent")
	}

	snap := srv.profile.Snapshot()
	if got := snap[ProtectBlocksConnect]; got != 1 {
		t.Errorf("ProtectBlocksConnect = %d; want 1", got)
	}
	if got := snap[ProtectBlocksConnectStateEchoed]; got != 0 {
		t.Errorf("ProtectBlocksConnectStateEchoed = %d; want 0 (no existing route)", got)
	}
}

// TestConnectRejectedWhenDstProtected_StateEchoed locks in the
// state-echo branch: protected dst WITH existing route → reject the
// requested change, broadcast tx 04 carrying the EXISTING (dst, src)
// so controllers see the crosspoint state didn't move.
func TestConnectRejectedWhenDstProtected_StateEchoed(t *testing.T) {
	srv := newTestServer(t)

	// Seed a route dst=2 → src=1, then protect dst=2.
	if !srv.tree.applyConnectLenient(0, 0, 2, 1) {
		t.Fatal("seed applyConnectLenient(2 -> 1) failed")
	}
	if _, res := srv.tree.protectApply(2, 7, codec.ProtectProBel); res != protectApplyAccepted {
		t.Fatalf("protectApply seed: result = %v; want protectApplyAccepted", res)
	}

	// Anonymous controller asks for dst=2 → src=3 — must be rejected.
	in := codec.EncodeConnect(codec.ConnectParams{Destination: 2, Source: 3})
	res, err := srv.dispatch(in)
	if err != nil {
		t.Fatalf("dispatch rx 02: %v", err)
	}
	if res.reply != nil {
		t.Errorf("protected-dst rx 02 returned a reply; want broadcast-only state echo")
	}
	if len(res.broadcast) != 1 || res.broadcast[0].ID != codec.TxCrosspointConnected {
		t.Fatalf("broadcast = %+v; want 1 x tx 04 (state echo)", res.broadcast)
	}
	echo, err := codec.DecodeConnected(res.broadcast[0])
	if err != nil {
		t.Fatalf("decode tx 04 echo: %v", err)
	}
	if echo.Destination != 2 || echo.Source != 1 {
		t.Errorf("tx 04 echo = (dst=%d src=%d); want (2, 1) — existing route", echo.Destination, echo.Source)
	}

	// Route still 1 (unchanged).
	if got, ok := srv.tree.matrices[matrixKey{matrix: 0, level: 0}].sources[2]; !ok || got != 1 {
		t.Errorf("tree[2] = (%d, ok=%v); want (1, true) — unchanged", got, ok)
	}

	snap := srv.profile.Snapshot()
	if got := snap[ProtectBlocksConnect]; got != 1 {
		t.Errorf("ProtectBlocksConnect = %d; want 1", got)
	}
	if got := snap[ProtectBlocksConnectStateEchoed]; got != 1 {
		t.Errorf("ProtectBlocksConnectStateEchoed = %d; want 1", got)
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
