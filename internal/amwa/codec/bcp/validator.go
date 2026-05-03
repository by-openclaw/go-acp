package bcp

import (
	"sort"
	"sync"

	"dhs/internal/amwa/codec/spec"
)

// Kind is the NMOS resource kind a Validator targets.
type Kind string

// Recognised host kinds. Per-BCP validators declare their target via
// HostKind() so the registry can dispatch by resource type.
const (
	KindNode     Kind = "node"
	KindDevice   Kind = "device"
	KindSource   Kind = "source"
	KindFlow     Kind = "flow"
	KindSender   Kind = "sender"
	KindReceiver Kind = "receiver"

	// IS-05 staged / activations
	KindIS05Staged       Kind = "is05.staged"
	KindIS05Activations  Kind = "is05.activations"

	// MS-05-02 / IS-12 class fingerprints
	KindMS05Class Kind = "ms05.class"
)

// Validator is the contract every BCP package implements. It pairs
// the spec.Versioned identity with a single Validate entry-point.
//
// Implementations MUST be stateless — the same Validator may be
// invoked concurrently across many goroutines.
type Validator interface {
	spec.Versioned

	// HostKind names the IS-* / MS-05-02 resource kind this BCP
	// constrains. The registry uses it to fan a payload only to
	// validators that target the right shape.
	HostKind() Kind

	// Validate inspects the JSON payload and returns zero or more
	// ComplianceEvent records describing deviations. nil error on
	// "valid"; the returned slice may still be non-empty for
	// MAY/SHOULD warnings (severity=Info).
	Validate(payload []byte) []spec.ComplianceEvent
}

// store is the per-process registry of every BCP validator.
// Plain slice — entry equality is (SpecID, APIVer); multiple
// SpecIDs co-exist (unlike spec.Registry[T] which holds one
// SpecID per instance).
var (
	storeMu sync.Mutex
	store   []Validator
)

// Register installs a validator. Idempotent for same instance;
// panics on conflict (same SpecID + APIVer registered twice with
// different instances).
func Register(v Validator) {
	storeMu.Lock()
	defer storeMu.Unlock()
	if v.SpecID() == "" || v.APIVer() == "" || v.SpecPatch() == "" {
		panic("bcp.Register: SpecID/APIVer/SpecPatch must be non-empty")
	}
	for _, existing := range store {
		if existing.SpecID() == v.SpecID() && existing.APIVer() == v.APIVer() {
			if existing == v {
				return // idempotent re-registration
			}
			panic("bcp.Register: duplicate (SpecID, APIVer): " + v.SpecID() + "/" + v.APIVer())
		}
	}
	store = append(store, v)
	sort.Slice(store, func(i, j int) bool {
		if store[i].SpecID() != store[j].SpecID() {
			return store[i].SpecID() < store[j].SpecID()
		}
		return store[i].APIVer() < store[j].APIVer()
	})
}

// Get returns the Validator for the given (SpecID, APIVer), if any.
func Get(specID, apiVer string) (Validator, bool) {
	storeMu.Lock()
	defer storeMu.Unlock()
	for _, v := range store {
		if v.SpecID() == specID && v.APIVer() == apiVer {
			return v, true
		}
	}
	return nil, false
}

// All returns every registered Validator in deterministic order
// (sorted by SpecID then APIVer).
func All() []Validator {
	storeMu.Lock()
	defer storeMu.Unlock()
	out := make([]Validator, len(store))
	copy(out, store)
	return out
}

// ForKind returns every validator whose HostKind matches k.
func ForKind(k Kind) []Validator {
	storeMu.Lock()
	defer storeMu.Unlock()
	var out []Validator
	for _, v := range store {
		if v.HostKind() == k {
			out = append(out, v)
		}
	}
	return out
}

// Run fans a payload through every validator targeting the host
// kind k and aggregates the resulting ComplianceEvents.
func Run(k Kind, payload []byte) []spec.ComplianceEvent {
	var events []spec.ComplianceEvent
	for _, v := range ForKind(k) {
		events = append(events, v.Validate(payload)...)
	}
	return events
}
