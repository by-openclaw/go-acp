package emberplus

import (
	"net"
	"testing"
	"time"

	"dhs/internal/emberplus/codec/s101"
	"dhs/internal/export/canonical"
)

// fakeSession builds a session attached to one half of a net.Pipe so the
// reader / writer wiring exists but no I/O is ever issued from inside
// the sweeper. lastActive is stamped explicitly by the caller so the
// test can drive the sweep deterministically.
func fakeSession(t *testing.T, srv *server, id string, lastActive time.Time, subs ...string) *session {
	t.Helper()
	c1, _ := net.Pipe()
	sess := &session{
		id:     id,
		conn:   c1,
		reader: s101.NewReader(c1),
		writer: s101.NewWriter(c1),
		srv:    srv,
		out:    make(chan []byte, 1),
		closed: make(chan struct{}),
		subs:   map[string]struct{}{},
	}
	sess.lastActive.Store(lastActive.UnixNano())
	sess.logger = srv.logger
	srv.mu.Lock()
	srv.sessions[sess] = struct{}{}
	for _, oid := range subs {
		set, ok := srv.subs[oid]
		if !ok {
			set = map[*session]struct{}{}
			srv.subs[oid] = set
		}
		set[sess] = struct{}{}
		sess.subs[oid] = struct{}{}
	}
	srv.mu.Unlock()
	t.Cleanup(func() { _ = c1.Close() })
	return sess
}

// minimalServer builds a server with a one-node tree so newServer
// succeeds and lookupOID has something to chew on if a future sub-
// test needs it.
func minimalServer(t *testing.T) *server {
	t.Helper()
	root := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "root", Path: "root", OID: "1",
			IsOnline: true, Access: canonical.AccessRead,
			Children: canonical.EmptyChildren(),
		},
	}
	srv := newServer(nil, &canonical.Export{Root: root})
	if srv.tree == nil {
		t.Fatal("tree failed to build")
	}
	return srv
}

// TestSweepStreamIdleSubs_ClearsStaleSession verifies that a session
// whose lastActive is older than ttl has its subscription set cleared,
// fires StreamIdleTTLExpired, and is NOT closed.
func TestSweepStreamIdleSubs_ClearsStaleSession(t *testing.T) {
	srv := minimalServer(t)
	ttl := 5 * time.Second
	stale := time.Now().Add(-2 * ttl)
	sess := fakeSession(t, srv, "stale-peer", stale, "1.2", "1.3")

	srv.sweepStreamIdleSubs(ttl)

	if len(sess.subs) != 0 {
		t.Errorf("session subs not cleared, len=%d", len(sess.subs))
	}
	srv.mu.Lock()
	if len(srv.subs) != 0 {
		t.Errorf("server subs map not pruned, keys=%v", keysOf(srv.subs))
	}
	srv.mu.Unlock()
	snap := srv.profile.Snapshot()
	if got := snap[StreamIdleTTLExpired]; got != 1 {
		t.Errorf("compliance %s = %d; want 1", StreamIdleTTLExpired, got)
	}
	select {
	case <-sess.closed:
		t.Error("session closed by sweep — must stay open per R9 spec")
	default:
	}
}

// TestSweepStreamIdleSubs_LeavesFreshSession asserts the sweep does not
// touch a session whose lastActive is within the TTL window.
func TestSweepStreamIdleSubs_LeavesFreshSession(t *testing.T) {
	srv := minimalServer(t)
	ttl := 5 * time.Second
	fresh := time.Now()
	sess := fakeSession(t, srv, "fresh-peer", fresh, "1.2")

	srv.sweepStreamIdleSubs(ttl)

	if _, ok := sess.subs["1.2"]; !ok {
		t.Error("fresh session lost its subscription")
	}
	if snap := srv.profile.Snapshot(); snap[StreamIdleTTLExpired] != 0 {
		t.Errorf("event fired on fresh session: %v", snap)
	}
}

// TestSweepStreamIdleSubs_SkipsEmptySubs guards against firing the
// event on a stale session that already has no active subs (e.g. the
// peer explicitly unsubscribed before going silent).
func TestSweepStreamIdleSubs_SkipsEmptySubs(t *testing.T) {
	srv := minimalServer(t)
	ttl := 5 * time.Second
	stale := time.Now().Add(-2 * ttl)
	// no subs[] — session is idle but has nothing to release.
	fakeSession(t, srv, "stale-no-subs", stale)

	srv.sweepStreamIdleSubs(ttl)

	if snap := srv.profile.Snapshot(); snap[StreamIdleTTLExpired] != 0 {
		t.Errorf("event fired on session with no subs: %v", snap)
	}
}

// TestSetStreamIdleTTL_ClampsNegative ensures a negative duration is
// clamped to 0 (sweeper disabled) rather than producing a runaway
// ticker.
func TestSetStreamIdleTTL_ClampsNegative(t *testing.T) {
	srv := minimalServer(t)
	srv.SetStreamIdleTTL(-1 * time.Second)
	if srv.streamIdleTTL != 0 {
		t.Errorf("negative ttl not clamped: %v", srv.streamIdleTTL)
	}
}

// keysOf returns the keys of a string-keyed map for diagnostic output.
func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
