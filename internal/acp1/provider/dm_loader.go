package acp1

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"dhs/internal/dmlib"
)

// SetDMLibrary attaches a DM-library resolver. The resolver drives the
// admin `slot.load` verb: callers ask for a card by
// vendor/product/model-rev/proto path, the resolver locates the schema
// on disk, and the provider models a hot-plug insert ending in
// "present".
//
// Schema-into-tree replacement is intentionally out of scope for #260:
// the served tree from --tree stays the source of truth for object
// values; slot.load only drives the cascade so consumers see the
// transition. The follow-up converter (canonical export <-> []protocol.
// Object) lands separately when integration tests prove a need.
func (s *server) SetDMLibrary(r dmlib.Resolver) {
	s.mu.Lock()
	s.dmLibrary = r
	s.mu.Unlock()
}

// SlotLoad models a hot-plug insert: resolve the named card via the
// DM library, then drive CascadeInsert so the slot transitions
// no_card -> powerup -> boot -> present with realistic timings.
//
// Returns ErrNoDMLibrary when no resolver has been attached.
// Returns dmlib.ErrNotFound when the path does not resolve.
func (s *server) SlotLoad(ctx context.Context, slot uint8, cardPath string) error {
	s.mu.Lock()
	r := s.dmLibrary
	s.mu.Unlock()
	if r == nil {
		return ErrNoDMLibrary
	}
	fp, err := parseCardPath(cardPath)
	if err != nil {
		return err
	}
	if _, err := r.Resolve(fp); err != nil {
		return fmt.Errorf("dmlib resolve %s: %w", cardPath, err)
	}
	s.CascadeInsert(ctx, slot)
	return nil
}

// SlotUnload models a hot-extract.
func (s *server) SlotUnload(slot uint8) {
	s.CascadeExtract(slot)
}

// ErrNoDMLibrary is returned when slot.load is called without a
// resolver attached.
var ErrNoDMLibrary = errors.New("acp1 provider: DM library not configured (use --dm-library on serve)")

// parseCardPath parses "vendor/product/model-rev/proto" into a
// Fingerprint. Examples:
//
//	axon/synapse/RRS18-1601/acp1
//	lawo/vsm/CARD-1.0/acp2
//
// model-rev splits on the last "-" so "RRS18-1601" yields
// (Model="RRS18", SwRev="1601") and "GIO-12-2000" yields
// (Model="GIO-12", SwRev="2000").
func parseCardPath(s string) (dmlib.Fingerprint, error) {
	parts := strings.Split(s, "/")
	if len(parts) != 4 {
		return dmlib.Fingerprint{}, fmt.Errorf("card path %q: expected 4 components vendor/product/model-rev/proto", s)
	}
	vendor, product, modelRev, proto := parts[0], parts[1], parts[2], parts[3]
	if vendor == "" || product == "" || modelRev == "" || proto == "" {
		return dmlib.Fingerprint{}, fmt.Errorf("card path %q: empty component", s)
	}
	idx := strings.LastIndex(modelRev, "-")
	if idx <= 0 || idx == len(modelRev)-1 {
		return dmlib.Fingerprint{}, fmt.Errorf("card path %q: model-rev component %q must be <model>-<rev>", s, modelRev)
	}
	return dmlib.Fingerprint{
		Vendor:  vendor,
		Product: product,
		Model:   modelRev[:idx],
		SwRev:   modelRev[idx+1:],
		Proto:   proto,
	}, nil
}
