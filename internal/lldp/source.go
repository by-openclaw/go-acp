package lldp

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrCaptureUnsupported reports that this OS cannot capture LLDP frames from
// stdlib. Windows returns it always: its raw sockets are IP-level and never
// see a non-IP Ethertype, so capture needs the Npcap driver.
//
// Typed so a caller can tell "this host cannot" from "this host may not"
// (a permission error) and from "nothing arrived" (no error, no neighbour).
// Those three deserve different operator messages.
var ErrCaptureUnsupported = errors.New("lldp: local frame capture is not supported on this platform")

// Source supplies the LLDP neighbours seen on a host's interfaces.
//
// This is the dependency-injection seam, and the reason the package is useful
// beyond capture: a device that reports its own LLDP over an API satisfies
// this interface with no privileges and no platform constraint, and is the
// ordinary case in a plant. Capture is one implementation, not the contract.
//
// Neighbors returns the current view keyed by local interface name. It does
// not block waiting for frames — LLDP is announced on the sender's own
// schedule (30 s by default), so a caller that blocked would stall for half a
// minute on a link that is working perfectly.
type Source interface {
	Neighbors(ctx context.Context) (map[string]Neighbor, error)
}

// SourceFunc adapts a function to [Source].
type SourceFunc func(ctx context.Context) (map[string]Neighbor, error)

// Neighbors calls f.
func (f SourceFunc) Neighbors(ctx context.Context) (map[string]Neighbor, error) { return f(ctx) }

// StaticSource is a fixed set of neighbours: tests, and deployments where the
// operator knows the wiring and would rather state it than discover it.
type StaticSource map[string]Neighbor

// Neighbors returns a copy, so a caller cannot mutate the source's map.
func (s StaticSource) Neighbors(context.Context) (map[string]Neighbor, error) {
	out := make(map[string]Neighbor, len(s))
	for k, v := range s {
		out[k] = v
	}
	return out, nil
}

// Cache wraps a Source with an expiry, so a slow or rate-limited underlying
// source is not consulted per request.
//
// The zero value is not usable; build one with [NewCache].
type Cache struct {
	src Source
	ttl time.Duration
	now func() time.Time

	mu      sync.Mutex
	at      time.Time
	cached  map[string]Neighbor
	lastErr error
}

// NewCache wraps src, refreshing no more often than ttl. A zero or negative
// ttl means every call reaches src.
func NewCache(src Source, ttl time.Duration) *Cache {
	return &Cache{src: src, ttl: ttl, now: time.Now}
}

// Neighbors returns the cached view, refreshing when it has expired.
//
// On a refresh failure the STALE view is served alongside the error, rather
// than an empty map. A momentarily unreachable source must not look like a
// device that unplugged itself — the same reasoning that keeps JWKS keys
// through an outage. A caller that cannot tolerate stale data checks the
// error; one that only wants the best available ignores it.
func (c *Cache) Neighbors(ctx context.Context) (map[string]Neighbor, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cached != nil && c.ttl > 0 && c.now().Sub(c.at) < c.ttl {
		return copyNeighbors(c.cached), c.lastErr
	}
	fresh, err := c.src.Neighbors(ctx)
	if err != nil {
		c.lastErr = err
		if c.cached != nil {
			return copyNeighbors(c.cached), err
		}
		return nil, err
	}
	c.cached, c.at, c.lastErr = fresh, c.now(), nil
	return copyNeighbors(fresh), nil
}

func copyNeighbors(m map[string]Neighbor) map[string]Neighbor {
	out := make(map[string]Neighbor, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
