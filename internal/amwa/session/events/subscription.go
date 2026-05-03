package events

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	httpsession "dhs/internal/amwa/session/http"
)

// subscription tracks one publisher-side connected client and its
// declared source-id set.
type subscription struct {
	id  uint64
	ws  *httpsession.WebSocket

	mu      sync.RWMutex
	sources map[string]struct{}
	failed  atomic.Bool
}

// set replaces the subscription's source set in one atomic swap.
// IS-07 command_subscription is a full replacement, not an append.
func (s *subscription) set(srcs []string) {
	next := make(map[string]struct{}, len(srcs))
	for _, id := range srcs {
		next[id] = struct{}{}
	}
	s.mu.Lock()
	s.sources = next
	s.mu.Unlock()
}

// matches reports whether this subscription has declared interest in
// the given source-id. Empty source set matches nothing — the
// controller has to explicitly subscribe.
func (s *subscription) matches(srcID string) bool {
	s.mu.RLock()
	_, ok := s.sources[srcID]
	s.mu.RUnlock()
	return ok
}

// markFailed flags a sub for skip on the next fanout (a delivery
// goroutine ran into a write error). The outer loop drops the entry
// when it next holds the publisher's mu.
func (s *subscription) markFailed() {
	s.failed.Store(true)
}

// is07Now formats the current wall-clock time as the IS-07 TAI
// `<sec>:<nsec>` string. We DO NOT subtract the leap-second offset
// (~37s in 2026) — production code that wants strict TAI must layer
// an NTP-derived offset on top. For dhs's current scope (loopback
// tests + dev integration), Unix-epoch-as-TAI is the documented
// approximation in `internal/amwa/codec/is05/types.go` FormatTAINow.
func is07Now() string {
	t := time.Now()
	return fmt.Sprintf("%d:%d", t.Unix(), t.Nanosecond())
}
