// Package registry defines the interface a Registry-role plugin must
// implement. Tier-1 compile-time slot, sibling to internal/protocol/
// (consumer plugins) and internal/provider/ (provider plugins).
//
// A Registry is a dual-face middleware: its left face consumes
// registrations + heartbeats from devices, its right face provides a
// catalogue + change-stream to controllers. Same process, two faces.
//
// Introduced for NMOS (IS-04 Registration API + Query API). Whether
// other protocols will host plugins here is an undecided architectural
// question and out of scope for the slot itself — the slot exposes a
// neutral interface; each plugin owns its semantics.
package registry

import (
	"context"
	"log/slog"
)

// Registry is the runtime contract a plugin satisfies.
type Registry interface {
	// Serve blocks until ctx is cancelled or a fatal error occurs.
	// Plugins start both faces inside Serve and run them under the
	// passed context.
	Serve(ctx context.Context, opts ServeOptions) error

	// Stop closes both faces and drains in-flight requests. Safe to
	// call multiple times; safe to call before Serve returns.
	Stop() error

	// Stats returns a snapshot of runtime counters. Cheap; safe to
	// call from any goroutine.
	Stats() Stats
}

// ServeOptions is the runtime configuration passed to every Registry
// plugin. Unrecognised fields are ignored — each plugin interprets
// what its spec allows.
type ServeOptions struct {
	// BindAddrs are the host:port addresses to listen on. Multiple
	// entries support multi-NIC binds (ST 2022-7 dual-network).
	// Use ":0" to let the OS pick a free port.
	BindAddrs []string

	// AdvertiseHost overrides the host:port placed in DNS-SD A/SRV
	// records. Empty means derive from BindAddrs[0].
	AdvertiseHost string

	// Priority maps to the NMOS `pri` TXT key (RFC 6763 §6 TXT pair).
	// 0–99 are production; 100+ are dev. Lower = higher priority.
	Priority int

	// DiscoveryMode picks the discovery transport: "mdns" (RFC 6762
	// multicast), "unicast" (RFC 6763 §10 SRV/TXT lookup against a
	// configured resolver), or "static" (no discovery — peers come
	// from PeerList).
	DiscoveryMode string

	// UnicastResolver is the host:port of the DNS server used for
	// unicast DNS-SD. Empty means use the system resolver.
	UnicastResolver string

	// PeerList is a file path holding `host,port[,api_ver]` lines for
	// static mode. Empty otherwise.
	PeerList string
}

// Stats is the standard counter set every Registry plugin exposes.
type Stats struct {
	// Resources currently held in the catalogue.
	Resources int
	// Subscriptions is the count of active change-stream subscriptions.
	Subscriptions int
	// Heartbeats received since Serve() was called.
	Heartbeats uint64
	// Registrations (POST /resource etc.) since Serve() was called.
	Registrations uint64
	// Deregistrations (DELETE /resource etc.) since Serve() was called.
	Deregistrations uint64
}

// Meta is the static description a Factory exposes.
type Meta struct {
	Name        string // e.g. "nmos"
	Description string
	DefaultPort int // recommended bind port
}

// Factory creates Registry instances. Plugins typically expose a
// package-level Factory{} value and register it from init().
type Factory interface {
	Meta() Meta
	New(logger *slog.Logger) Registry
}
