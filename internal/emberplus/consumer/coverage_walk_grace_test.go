package emberplus

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/emberplus/codec/glow"
)

// TestWalk_GraceWaitsForPendingMatrix covers Walk's deferred-matrix-contents
// grace branch: with a fetch pending at settle, Walk extends the wait
// (bounded by walkGraceMax) before returning. Fast timers make it
// deterministic instead of the 15s/8s production cadence.
func TestWalk_GraceWaitsForPendingMatrix(t *testing.T) {
	s, cleanup := pipeSession(t)
	defer cleanup()
	p := freshPlugin()
	p.session = s
	p.walkSettleInitial = 15 * time.Millisecond
	p.walkSettleInterval = 15 * time.Millisecond
	p.walkGraceInterval = 5 * time.Millisecond
	p.walkGraceMax = 2
	atomic.StoreInt32(&p.pendingMatrixFetches, 1) // simulate a pending fetch

	done := make(chan struct{})
	go func() {
		_, _ = p.Walk(context.Background(), 0)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Walk did not return — grace loop unbounded")
	}
}

// TestNotifyMatrixSubscribers_NoSubscriberDrops covers the no-subscriber drop
// (fn nil + no matching wildcard -> return) for a valid matrix entry.
func TestNotifyMatrixSubscribers_NoSubscriberDrops(t *testing.T) {
	p := freshPlugin()
	entry := &treeEntry{
		glowMatrix:  &glow.Matrix{Number: 1, Identifier: "m"},
		numericPath: []int32{1},
		obj:         consumer.Object{OID: "1"},
	}
	// No subscribers registered → must return without panicking.
	p.notifyMatrixSubscribers(entry, glow.Connection{Target: 0, Sources: []int32{1}})
}
