package probelsw02p

import (
	"io"
	"log/slog"
	"testing"

	"acp/internal/probel-sw02p/codec"
)

// newBareTestServer returns a provider with no canonical tree —
// auto-created matrices have 0 target/source counts, so the lenient
// apply path accepts dst/src across the full 16383 extended range.
// Used by Extended* tests that need to cross the 1023 narrow boundary.
func newBareTestServer(t *testing.T) *server {
	t.Helper()
	return newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
}

// TestExtendedInterrogateRepliesWithExtendedTally locks in the rx 65 /
// tx 67 contract (§3.2.47 + §3.2.49). Routed dst returns the routed
// src; unrouted dst encodes the §3.2.5 sentinel (1023).
func TestExtendedInterrogateRepliesWithExtendedTally(t *testing.T) {
	srv := newBareTestServer(t)

	// Seed a route on (matrix=0, level=0) via the lenient path so we
	// can cross the 1023 boundary (bare tree has count=0 so the range
	// check is skipped).
	srv.tree.applyConnectLenient(0, 0, 130, 260)

	cases := []struct {
		name    string
		dst     uint16
		wantSrc uint16
	}{
		{"routed", 130, 260},
		{"unrouted → §3.2.5 sentinel", 9999, codec.DestOutOfRangeSource},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := codec.EncodeExtendedInterrogate(codec.ExtendedInterrogateParams{Destination: tc.dst})
			res, err := srv.dispatch(in)
			if err != nil {
				t.Fatalf("dispatch rx 65: %v", err)
			}
			if res.reply == nil || res.reply.ID != codec.TxExtendedTally {
				t.Fatalf("reply missing / wrong ID: %+v", res.reply)
			}
			t67, err := codec.DecodeExtendedTally(*res.reply)
			if err != nil {
				t.Fatalf("decode tx 67: %v", err)
			}
			if t67.Destination != tc.dst || t67.Source != tc.wantSrc {
				t.Errorf("tx 67 = (dst=%d src=%d); want (dst=%d src=%d)",
					t67.Destination, t67.Source, tc.dst, tc.wantSrc)
			}
		})
	}
}

// TestExtendedConnectAppliesRouteAndBroadcastsExtendedConnected locks
// in rx 66 / tx 68 (§3.2.48 + §3.2.50). Extended addressing carries
// dst/src up to 16383; broadcast fan-out matches the narrow path.
func TestExtendedConnectAppliesRouteAndBroadcastsExtendedConnected(t *testing.T) {
	srv := newBareTestServer(t)

	in := codec.EncodeExtendedConnect(codec.ExtendedConnectParams{
		Destination: 5000, Source: 10000,
	})
	res, err := srv.dispatch(in)
	if err != nil {
		t.Fatalf("dispatch rx 66: %v", err)
	}
	if res.reply != nil {
		t.Errorf("rx 66 returned a point-to-point reply; §3.2.50 requires broadcast only")
	}
	if len(res.broadcast) != 1 || res.broadcast[0].ID != codec.TxExtendedConnected {
		t.Fatalf("broadcast = %+v; want 1 x tx 68", res.broadcast)
	}
	cp, err := codec.DecodeExtendedConnected(res.broadcast[0])
	if err != nil {
		t.Fatalf("decode tx 68: %v", err)
	}
	if cp.Destination != 5000 || cp.Source != 10000 {
		t.Errorf("tx 68 = (dst=%d src=%d); want (5000, 10000)", cp.Destination, cp.Source)
	}

	state := srv.tree.matrices[matrixKey{matrix: 0, level: 0}]
	if got, ok := state.sources[5000]; !ok || got != 10000 {
		t.Errorf("tree[5000] = (%d, ok=%v); want (10000, true)", got, ok)
	}
}

// TestExtendedConnectRejectedWhenDstProtected_NoExistingRoute mirrors
// the §3.2.60 silent-drop branch on the rx 66 / tx 68 path.
func TestExtendedConnectRejectedWhenDstProtected_NoExistingRoute(t *testing.T) {
	srv := newBareTestServer(t)

	if _, res := srv.tree.protectApply(5000, 42, codec.ProtectProBel); res != protectApplyAccepted {
		t.Fatalf("protectApply seed: result = %v; want protectApplyAccepted", res)
	}

	in := codec.EncodeExtendedConnect(codec.ExtendedConnectParams{
		Destination: 5000, Source: 10000,
	})
	res, err := srv.dispatch(in)
	if err != nil {
		t.Fatalf("dispatch rx 66: %v", err)
	}
	if res.reply != nil || len(res.broadcast) != 0 {
		t.Errorf("protected-dst rx 66 (no route): reply=%+v broadcast=%d; want both empty", res.reply, len(res.broadcast))
	}
	if _, ok := srv.tree.matrices[matrixKey{matrix: 0, level: 0}].sources[5000]; ok {
		t.Errorf("tree[5000] populated after blocked extended connect; want absent")
	}
	snap := srv.profile.Snapshot()
	if got := snap[ProtectBlocksConnect]; got != 1 {
		t.Errorf("ProtectBlocksConnect = %d; want 1", got)
	}
	if got := snap[ProtectBlocksConnectStateEchoed]; got != 0 {
		t.Errorf("ProtectBlocksConnectStateEchoed = %d; want 0 (no existing route)", got)
	}
}

// TestExtendedConnectRejectedWhenDstProtected_StateEchoed mirrors the
// state-echo branch on the rx 66 / tx 68 path.
func TestExtendedConnectRejectedWhenDstProtected_StateEchoed(t *testing.T) {
	srv := newBareTestServer(t)

	// Seed dst=5000 → src=1234, then protect dst=5000.
	if !srv.tree.applyConnectLenient(0, 0, 5000, 1234) {
		t.Fatal("seed applyConnectLenient(5000 -> 1234) failed")
	}
	if _, res := srv.tree.protectApply(5000, 42, codec.ProtectProBel); res != protectApplyAccepted {
		t.Fatalf("protectApply seed: result = %v; want protectApplyAccepted", res)
	}

	in := codec.EncodeExtendedConnect(codec.ExtendedConnectParams{
		Destination: 5000, Source: 9999,
	})
	res, err := srv.dispatch(in)
	if err != nil {
		t.Fatalf("dispatch rx 66: %v", err)
	}
	if res.reply != nil {
		t.Errorf("protected-dst rx 66 returned a reply; want broadcast-only state echo")
	}
	if len(res.broadcast) != 1 || res.broadcast[0].ID != codec.TxExtendedConnected {
		t.Fatalf("broadcast = %+v; want 1 x tx 68 (state echo)", res.broadcast)
	}
	echo, err := codec.DecodeExtendedConnected(res.broadcast[0])
	if err != nil {
		t.Fatalf("decode tx 68 echo: %v", err)
	}
	if echo.Destination != 5000 || echo.Source != 1234 {
		t.Errorf("tx 68 echo = (dst=%d src=%d); want (5000, 1234) — existing route", echo.Destination, echo.Source)
	}
	if got, ok := srv.tree.matrices[matrixKey{matrix: 0, level: 0}].sources[5000]; !ok || got != 1234 {
		t.Errorf("tree[5000] = (%d, ok=%v); want (1234, true) — unchanged", got, ok)
	}
	snap := srv.profile.Snapshot()
	if got := snap[ProtectBlocksConnect]; got != 1 {
		t.Errorf("ProtectBlocksConnect = %d; want 1", got)
	}
	if got := snap[ProtectBlocksConnectStateEchoed]; got != 1 {
		t.Errorf("ProtectBlocksConnectStateEchoed = %d; want 1", got)
	}
}
