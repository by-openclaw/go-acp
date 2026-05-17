package spec

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Registry is the per-spec map of APIVer → codec implementation. Each
// AMWA NMOS spec package (is04, is05, is07, …) instantiates one of
// these and exposes thin wrappers for the plugin layer to call. Each
// minor's init() populates the registry by calling Register exactly
// once.
//
// The type parameter T is the spec-specific Codec interface (which
// itself extends [Versioned]) — e.g. is04.Codec, is05.Codec. This
// keeps lookup type-safe: is04.Get returns is04.Codec, not a generic
// Versioned, so callers don't need type assertions.
//
// All operations are safe for concurrent use. Register is the only
// mutating op; everything else is read-only and lock-cheap (RWMutex).
type Registry[T Versioned] struct {
	mu       sync.RWMutex
	specID   string         // captured from first Register call; subsequent calls must match
	byVer    map[string]T   // APIVer → codec
	versions []string       // APIVer list, sorted ascending semver
}

// NewRegistry constructs an empty per-spec Registry. The plugin's spec
// package holds one of these as a package-level var; init()s in vXX/
// subpackages populate it.
func NewRegistry[T Versioned]() *Registry[T] {
	return &Registry[T]{
		byVer: map[string]T{},
	}
}

// Register installs a codec under its (SpecID, APIVer) pair. Called
// from each minor's init().
//
// Register is idempotent: calling it twice with the same instance for
// the same key is a no-op. Calling it with a different instance under
// an already-registered key panics — that's a programming error
// (two minor packages claiming the same APIVer, or a typo in
// SpecID). Same semantics as consumer.Register.
//
// Registering codecs for different SpecIDs in the same Registry also
// panics — Registry is per-spec. The first Register call captures the
// SpecID; subsequent calls must match.
//
// Empty SpecID, empty APIVer, or empty SpecPatch panic — these are
// always programming errors, never legitimate runtime input.
func (r *Registry[T]) Register(c T) {
	specID := strings.ToLower(strings.TrimSpace(c.SpecID()))
	apiVer := strings.ToLower(strings.TrimSpace(c.APIVer()))
	specPatch := strings.TrimSpace(c.SpecPatch())
	if specID == "" {
		panic("spec.Register: empty SpecID")
	}
	if apiVer == "" {
		panic(fmt.Sprintf("spec.Register: empty APIVer for SpecID %q", specID))
	}
	if specPatch == "" {
		panic(fmt.Sprintf("spec.Register: empty SpecPatch for %s/%s", specID, apiVer))
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.specID == "" {
		r.specID = specID
	} else if r.specID != specID {
		panic(fmt.Sprintf(
			"spec.Register: SpecID mismatch — registry holds %q, got %q",
			r.specID, specID,
		))
	}

	if existing, dup := r.byVer[apiVer]; dup {
		// Idempotent if the caller passes the exact same instance.
		// Different instance under same key is a duplicate-init bug.
		if any(existing) == any(c) {
			return
		}
		panic(fmt.Sprintf(
			"spec.Register: duplicate registration for %s/%s",
			specID, apiVer,
		))
	}
	r.byVer[apiVer] = c
	r.versions = append(r.versions, apiVer)
	sort.Strings(r.versions)
}

// Get returns the codec for an APIVer. The boolean is false when no
// such version is registered — caller decides whether to fire a
// compliance event or surface an error to the peer.
//
// APIVer comparison is case-insensitive and ignores leading/trailing
// whitespace; everything else (including the leading "v") must match
// exactly.
func (r *Registry[T]) Get(apiVer string) (T, bool) {
	key := strings.ToLower(strings.TrimSpace(apiVer))
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.byVer[key]
	return c, ok
}

// AllCodecs returns every registered codec sorted by APIVer ascending.
// Plugin code uses this to install per-minor URL routes. The returned
// slice is a fresh copy — safe to mutate.
func (r *Registry[T]) AllCodecs() []T {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]T, 0, len(r.versions))
	for _, v := range r.versions {
		out = append(out, r.byVer[v])
	}
	return out
}

// SupportedVersions returns every registered APIVer string sorted
// ascending — e.g. ["v1.1", "v1.2", "v1.3"]. The DNS-SD announcer
// uses this to publish the `api_ver` TXT comma-list.
func (r *Registry[T]) SupportedVersions() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, len(r.versions))
	copy(out, r.versions)
	return out
}

// SpecID returns the slug captured from the first Register call, or
// "" if the registry is empty. Useful for compliance-event reports
// and log lines that need to identify the spec without holding a
// concrete codec.
func (r *Registry[T]) SpecID() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.specID
}
