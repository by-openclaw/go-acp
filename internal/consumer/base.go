package consumer

import (
	"sync"
	"time"

	"dhs/internal/clock"
	"dhs/internal/consumer/compliance"
	"dhs/internal/metrics"
	"dhs/internal/plugin"
	"dhs/internal/transport"
)

// Base is the half of a connector that is not about its protocol.
//
// Four concerns cut across every connector in this repo and none of them is
// protocol-specific: liveness, counters, spec-deviation events, and traffic
// capture. Each was nevertheless written per connector, so the same field
// and the same accessor existed five to eight times over — SetRecorder was
// byte-for-byte identical in five packages, ComplianceProfile in five,
// Metrics in eight. Duplication at that scale does not stay identical: it
// drifts, one copy at a time, and the drift is invisible because each copy
// looks correct on its own. That is exactly how the transport work started,
// with six of eight accept paths missing SO_KEEPALIVE.
//
// A connector embeds Base and gets all four. What stays its own is the
// codec and the session — the parts that genuinely differ.
//
// Embedded BY VALUE, and its zero value works. An embedded pointer would be
// nil in every test that builds a connector as a bare struct literal, and
// this repo has well over a hundred of those; the panic would surface at
// the first accessor rather than at the missing assignment.
type Base struct {
	// Health supplies SessionHealth, Opened/Closed and the rx/tx stamps.
	Health

	// mu guards the three below. It is deliberately NOT the connector's own
	// mutex: these are independent of whatever protocol state that lock
	// protects, and sharing one lock across both would mean a metrics read
	// waits behind a walk.
	mu       sync.Mutex
	profile  *compliance.Profile
	metrics  *metrics.Connector
	recorder *transport.Recorder
	clk      clock.Clock
}

// Clock returns the injected clock — keepalive probers, poll loops and
// settle timers take their time from it so a test drives time instead of
// sleeping. Never nil: a Base that was never Init'ed answers with the
// system clock.
func (b *Base) Clock() clock.Clock {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.clk == nil {
		b.clk = clock.System()
	}
	return b.clk
}

// Init wires the cross-cutting concerns from the injected dependency set.
// Called once at the top of a connector's factory, in place of the four
// assignments it replaces.
//
// stale is the protocol's own silence threshold, which is the one part of
// this that a connector genuinely decides (ACP1/ACP2 90s, Ember+ 30s,
// TSL 5s). Pass 0 to take consumer.DefaultStaleAfter.
func (b *Base) Init(deps plugin.Deps, stale time.Duration) {
	deps = deps.WithDefaults()
	b.Configure(deps.Net, stale)

	b.mu.Lock()
	defer b.mu.Unlock()
	b.metrics = deps.Metrics
	b.clk = deps.Clock
	if b.profile == nil {
		b.profile = &compliance.Profile{}
	}
}

// ComplianceProfile returns the connector's spec-deviation profile. Never
// nil after Init; a connector built without it gets one on first use, so a
// deviation is never dropped for want of a constructor call.
func (b *Base) ComplianceProfile() *compliance.Profile {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.profile == nil {
		b.profile = &compliance.Profile{}
	}
	return b.profile
}

// Metrics returns the connector's counter set, satisfying the optional
// interface the CLI type-asserts for --metrics-addr. Never nil.
func (b *Base) Metrics() *metrics.Connector {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.metrics == nil {
		b.metrics = metrics.NewConnector()
	}
	return b.metrics
}

// SetRecorder attaches a traffic recorder, or clears it with nil. Safe at
// any time; a session reads it through Recorder when it writes a frame.
func (b *Base) SetRecorder(rec *transport.Recorder) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.recorder = rec
}

// Recorder returns the attached recorder, or nil. transport.Recorder's
// methods are nil-safe, so a caller does not have to check.
func (b *Base) Recorder() *transport.Recorder {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.recorder
}
