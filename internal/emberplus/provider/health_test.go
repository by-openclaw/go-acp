package emberplus

import (
	"testing"
	"time"
)

// TestPeerHealthSnapshot_EmptyServer asserts a server with no
// connected peers returns an empty snapshot (not nil).
func TestPeerHealthSnapshot_EmptyServer(t *testing.T) {
	srv := minimalServer(t)
	snap := srv.PeerHealthSnapshot()
	if snap == nil {
		t.Fatal("snapshot is nil; want empty slice")
	}
	if len(snap) != 0 {
		t.Errorf("len=%d; want 0", len(snap))
	}
}

// TestPeerHealthSnapshot_LivePeer asserts a fresh session shows up
// as Connected=true / Live=true with the current StaleAfter.
func TestPeerHealthSnapshot_LivePeer(t *testing.T) {
	srv := minimalServer(t)
	sess := fakeSession(t, srv, "peer-1", time.Now(), "1.2")

	snap := srv.PeerHealthSnapshot()
	if len(snap) != 1 {
		t.Fatalf("len=%d; want 1", len(snap))
	}
	if snap[0].Peer != "peer-1" || !snap[0].Connected || !snap[0].Live {
		t.Errorf("snap = %+v", snap[0])
	}
	if snap[0].StaleAfter != idleSessionTTL {
		t.Errorf("StaleAfter = %v; want %v", snap[0].StaleAfter, idleSessionTTL)
	}
	if snap[0].SubsOpen != 1 {
		t.Errorf("SubsOpen = %d; want 1", snap[0].SubsOpen)
	}
	_ = sess
}

// TestPeerHealthSnapshot_StalePeer asserts a session whose lastActive
// is past the TTL shows up as Live=false.
func TestPeerHealthSnapshot_StalePeer(t *testing.T) {
	srv := minimalServer(t)
	stale := time.Now().Add(-2 * idleSessionTTL)
	_ = fakeSession(t, srv, "peer-stale", stale)

	snap := srv.PeerHealthSnapshot()
	if len(snap) != 1 || snap[0].Live {
		t.Errorf("stale peer Live = true; want false (snap=%+v)", snap)
	}
}
