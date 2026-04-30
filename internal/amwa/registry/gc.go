package registry

import (
	"context"
	"log/slog"
	"time"
)

// DefaultGCInterval is the watchdog tick rate. The spec doesn't pin
// an exact tick rate — only that Nodes whose heartbeat lapsed past
// the timeout MUST be evicted. 1 s gives sub-second detection on a
// 12 s timeout without burning CPU.
const DefaultGCInterval = 1 * time.Second

// DefaultStaleThreshold mirrors IS-04 §6.1 — Registry GCs Nodes that
// have not heartbeat'd in 12 s.
const DefaultStaleThreshold = 12 * time.Second

// runGC ticks every interval and evicts Nodes whose last-seen is
// older than threshold. Blocks until ctx cancels.
func runGC(ctx context.Context, store *Store, logger *slog.Logger, interval, threshold time.Duration) {
	if interval <= 0 {
		interval = DefaultGCInterval
	}
	if threshold <= 0 {
		threshold = DefaultStaleThreshold
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n := store.EvictStale(threshold)
			if n > 0 && logger != nil {
				logger.Info("registry/gc: evicted stale nodes", "count", n, "threshold", threshold)
			}
		}
	}
}
