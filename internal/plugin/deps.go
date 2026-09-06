// Package plugin holds the dependency set every connector is constructed
// with.
//
// It exists as its own package because both registries need it and neither
// may import the other: internal/consumer and internal/provider are siblings,
// and a shared type between them has to sit below both.
//
// The point is the one CLAUDE.md principle 2 has always stated and the code
// never enforced — "connectors take transport, logger, tree, clock as
// constructor parameters". Until now the factory signature was
// New(logger *slog.Logger), so the logger was injected and everything else
// was built inside each connector: its own sockets, its own metrics
// registry, its own clock. That is why clearing transport code out of the
// protocol packages cost one refactor per protocol instead of one change.
//
// A connector handed a Deps cannot open a socket except through Net, cannot
// read the wall clock except through Clock, and does not own its metrics.
// Which makes the rules structural rather than a list of exceptions someone
// keeps draining — and makes an implementation swappable, because nothing
// inside a connector names a concrete one.
package plugin

import (
	"log/slog"

	"dhs/internal/clock"
	"dhs/internal/metrics"
	"dhs/internal/transport"
)

// Deps is what a connector is given at construction.
//
// Fields are added here when there is something to put in them, not in
// anticipation. Security posture and a peer allow-list belong in this struct
// and are absent because neither exists yet; a nil field for an unbuilt
// feature is an API pretending to offer something.
type Deps struct {
	// Logger receives operational output. Never nil after WithDefaults.
	Logger *slog.Logger

	// Net is the only way a connector may touch a socket.
	Net transport.Net

	// Clock is the only way a connector may read time, so a test can drive
	// timeouts and keepalive cadence without sleeping.
	Clock clock.Clock

	// Metrics is the connector's counter set. Supplied rather than created,
	// so the process can scrape every connector from one place instead of
	// each one owning a registry nobody else can reach.
	Metrics *metrics.Connector
}

// WithDefaults fills any field left zero with the production default, so a
// caller that only cares about one dependency — a test, a CLI path that
// predates this struct — still gets a usable set.
//
// Called once at the top of a connector's constructor. It returns a copy;
// the caller's Deps is untouched.
func (d Deps) WithDefaults() Deps {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Net == nil {
		d.Net = transport.New(transport.Config{})
	}
	if d.Clock == nil {
		d.Clock = clock.System()
	}
	if d.Metrics == nil {
		d.Metrics = metrics.NewConnector()
	}
	return d
}
