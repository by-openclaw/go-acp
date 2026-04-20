// Package provider defines the interface a protocol provider (server) plugin
// must implement. Symmetric with internal/protocol (consumer / client side).
//
// A Provider serves a tree to one or more consumers. It owns the listener,
// accept loop, per-connection state, and broadcast fan-out. Input is a
// canonical tree (same JSON schema produced by `acp walk --capture`).
//
// Tier-1 compile-time registry — no runtime plugins.
package provider

import (
	"context"
	"log/slog"

	"acp/internal/export/canonical"
)

// Provider is the runtime contract: start serving on an address, stop cleanly.
type Provider interface {
	// Serve blocks until ctx is cancelled or a fatal error occurs.
	// addr is the TCP listen address, e.g. ":9010".
	Serve(ctx context.Context, addr string) error

	// Stop closes the listener and drains active sessions. Safe to call
	// multiple times; safe to call before Serve returns.
	Stop() error

	// SetValue mutates the served tree and fans the change out to all
	// subscribed consumers. Path is dotted OID ("1.4.1.3"). Returns the
	// actually-stored value (providers may clamp to min/max).
	SetValue(ctx context.Context, path string, val any) (any, error)
}

// Meta is the static description a factory exposes.
type Meta struct {
	Name        string // e.g. "emberplus"
	DefaultPort int    // e.g. 9010
	Description string
}

// Factory creates Provider instances from a loaded canonical tree.
type Factory interface {
	Meta() Meta
	New(logger *slog.Logger, tree *canonical.Export) Provider
}
