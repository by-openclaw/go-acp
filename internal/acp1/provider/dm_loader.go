package acp1

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"dhs/internal/dmlib"
	"dhs/internal/export"
)

// SetDMLibrary attaches a DM-library resolver. The resolver drives the
// admin `slot.load` verb: callers ask for a card by
// vendor/product/model-rev/proto path, the resolver locates the schema
// on disk, and the provider replaces the served tree entries for the
// target slot before driving the cascade. Subsequent identity-probe
// replies reflect the new card.
func (s *server) SetDMLibrary(r dmlib.Resolver) {
	s.mu.Lock()
	s.dmLibrary = r
	s.mu.Unlock()
}

// SlotLoad models a hot-plug insert: resolve the named card via the
// DM library, REPLACE the served tree entries for the slot from the
// resolved snapshot, then drive CascadeInsert so the slot transitions
// no_card -> powerup -> boot -> present. The order matters: tree
// replacement happens FIRST so by the time the consumer's enricher
// fires its identity probe at the present transition, the wire reads
// the new card's identity bytes.
//
// No state preconditions: works regardless of the slot's current
// status. Repeated SlotLoad calls (A->B->A->C) on the same slot are
// independent — the in-flight cascade pre-empts and the new tree
// content takes effect immediately.
//
// Returns:
//
//	ErrNoDMLibrary       no resolver attached (configure --dm-library)
//	dmlib.ErrNotFound    card path does not resolve
//	tree.ReplaceSlot err underlying type / range conversion failure
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
	schema, err := r.Resolve(fp)
	if err != nil {
		return fmt.Errorf("dmlib resolve %s: %w", cardPath, err)
	}
	snap := pickSnapshot(schema, int(slot))
	if snap == nil {
		return fmt.Errorf("acp1 provider: schema %s has no slot data", cardPath)
	}
	if err := s.tree.ReplaceSlot(slot, snap); err != nil {
		return fmt.Errorf("acp1 provider: ReplaceSlot: %w", err)
	}
	s.CascadeInsert(ctx, slot)
	return nil
}

// SlotUnload models a hot-extract: drive the cascade
// present -> removed -> no_card, then clear the served tree entries
// for the slot so the wire identity reverts to "no card present".
//
// Idempotent on already-empty slots — the cascade is a no-op when the
// slot is already at no_card, and ClearSlot on an empty slot just
// removes the (already-absent) counts entry.
func (s *server) SlotUnload(slot uint8) {
	s.CascadeExtract(slot)
	_ = s.tree.ClearSlot(slot)
}

// pickSnapshot selects the right SlotDump from a multi-slot schema.
// Multi-slot products have one snapshot per slot keyed by slot
// number; single-slot products store under whatever slot the original
// walk hit. Match priority: exact slot match, then any single entry.
func pickSnapshot(schema *dmlib.Schema, slot int) *export.Snapshot {
	if schema == nil {
		return nil
	}
	if snap, ok := schema.Slots[slot]; ok {
		return snap
	}
	if len(schema.Slots) == 1 {
		for _, snap := range schema.Slots {
			return snap
		}
	}
	return nil
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
