package spec

import (
	"fmt"
	"sync"
	"time"
)

// ComplianceEvent is the cross-cutting record of a peer-side spec
// deviation absorbed by an NMOS codec or plugin. Every NMOS spec
// fires events through the same shape so logs, metrics, and the
// future Grafana board look identical regardless of which spec
// flagged the deviation.
//
// "Absorb-and-fire-event" is the project's no-workaround policy
// (top-level CLAUDE.md "Spec-strict, no-workaround posture"): the
// codec keeps decoding instead of crashing, but it does NOT silently
// patch the deviation away. Every event is auditable.
//
// Fields mirror the cross-protocol compliance.Event shape from
// internal/consumer/compliance/, with NMOS-specific identifiers
// (SpecID + APIVer + SpecPatch) added so reports stay attributable
// across the 14-spec suite.
type ComplianceEvent struct {
	SpecID    string    // e.g. "is-04"
	APIVer    string    // e.g. "v1.3"
	SpecPatch string    // e.g. "v1.3.3"
	Code      string    // stable machine slug, e.g. "nmos_query_api_missing"
	Severity  Severity  // info / warn / error
	Detail    string    // free-form human-readable
	Resource  string    // optional: UUID / URL of the affected resource
	PeerHost  string    // optional: peer host:port that emitted the deviation
	At        time.Time // when the deviation was observed
}

// Severity classifies a compliance event for log filtering.
type Severity int

const (
	// SeverityInfo records a deviation that is harmless — a peer
	// quirk that the spec arguably allows or that we can absorb
	// transparently. Default for "wire shape OK but unusual".
	SeverityInfo Severity = iota
	// SeverityWarn records a deviation that breaks an interop
	// expectation but does not fail the local operation — e.g.
	// a vendor's Query API returning [] for a populated catalogue.
	SeverityWarn
	// SeverityError records a deviation that fails the operation —
	// e.g. a malformed JSON body, a missing required field, or
	// no mutually-supported version.
	SeverityError
)

// String renders the severity for log lines. Stable wire format —
// CI greps on these strings.
func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "info"
	case SeverityWarn:
		return "warn"
	case SeverityError:
		return "error"
	default:
		return fmt.Sprintf("severity(%d)", int(s))
	}
}

// Reporter is the dependency-injection seam codecs use to emit
// compliance events. Plugin code passes one through the constructor;
// codecs never reach into a global. Tests pass a [SliceReporter] that
// records events for assertion; production passes a logger-backed
// adapter wired via DI in cmd/dhs.
//
// Implementations MUST be safe for concurrent use — codecs may fire
// events from goroutines handling distinct connections.
type Reporter interface {
	Report(ComplianceEvent)
}

// NopReporter discards every event. Useful as a default value when
// a caller has not yet wired a real reporter; callers should NEVER
// pass nil to a codec — pass NopReporter{} instead.
type NopReporter struct{}

// Report on NopReporter is a no-op.
func (NopReporter) Report(ComplianceEvent) {}

// SliceReporter records every event in an in-memory slice. Test
// helper; codecs in production take a logger-backed Reporter
// instead.
type SliceReporter struct {
	mu     sync.Mutex
	events []ComplianceEvent
}

// Report appends the event under the internal mutex.
func (s *SliceReporter) Report(e ComplianceEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

// Snapshot returns a copy of every event recorded so far. Safe to
// call concurrently with Report.
func (s *SliceReporter) Snapshot() []ComplianceEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ComplianceEvent, len(s.events))
	copy(out, s.events)
	return out
}
