package emberplus

import (
	"context"
	"fmt"
	"strings"

	"dhs/internal/export/canonical"
	"dhs/internal/storage"
)

// SplitAndPersistDM walks the canonical tree, identifies every "slot"
// (= an immediate root child whose subtree contains a Matrix element),
// and persists each slot as its own DM file at
// .cache/dm/emberplus/<slotIdentifier>@<swRev>.json. Refs #438
// (ADR-0022 card data model) + ADR-0023 (matrix entity).
//
// For TinyEmberPlusRouter@1.6.2 this produces three slot DMs:
//
//	.cache/dm/emberplus/oneToN@1.6.2.json
//	.cache/dm/emberplus/nToN@1.6.2.json
//	.cache/dm/emberplus/dynamic@1.6.2.json
//
// Each slot DM carries:
//   - model    = <slotIdentifier>  (e.g. "oneToN")
//   - sw_rev   = <providerSwRev>   (e.g. "1.6.2")
//   - protocol = "emberplus"
//   - root     = the canonical Node at that slot, with its matrix +
//                labels + parameters subtrees intact, including the
//                inline targetLabels/sourceLabels/targetParams/
//                sourceParams/connectionParams written by chunks 3c +
//                7-fix earlier on this branch.
//
// Slot detection is recursive — a matrix nested deeper than direct
// child still pins the topmost containing root-child as the slot
// boundary (matches the way ACP1/ACP2 frames identify card slots).
//
// SlotRef pairs the on-disk identity of a slot DM with the OID of
// the node it represents. Returned by SplitAndPersistDM so callers
// can stitch a manifest binding {OID → DM filename}.
type SlotRef struct {
	Identity string // e.g. "oneToN@1.6.2"
	OID      string // e.g. "1.1"
}

// Returns the list of slot refs persisted (for manifest writer /
// logger). On any per-slot save error the partial list is returned
// alongside the error so the caller can decide whether to keep going.
func (p *Plugin) SplitAndPersistDM(ctx context.Context, store *storage.TreeStore, providerIdentity string) ([]SlotRef, error) {
	tree, err := p.ExportCanonical(ctx)
	if err != nil {
		return nil, err
	}
	if tree == nil || tree.Root == nil {
		return nil, fmt.Errorf("emberplus: SplitAndPersistDM: empty canonical tree")
	}
	_, swRev := splitIdentityForSplit(providerIdentity)

	rootNode, ok := tree.Root.(*canonical.Node)
	if !ok {
		// Single matrix at root (very unusual). Persist as one DM
		// keyed by providerIdentity itself — degenerate but valid.
		if err := store.WriteDM("emberplus", providerIdentity, storage.DM{
			Protocol: "emberplus",
			Root:     tree.Root,
		}); err != nil {
			return nil, err
		}
		return []SlotRef{{Identity: providerIdentity, OID: rootOID(tree.Root)}}, nil
	}

	var persisted []SlotRef
	for _, child := range rootNode.Children {
		nodeChild, ok := child.(*canonical.Node)
		if !ok {
			continue
		}
		// Emit a slot DM for ANY non-empty root-child Node. That
		// covers:
		//   - matrix slots          (router.oneToN / nToN / dynamic)
		//   - function slots        (router.functions — add /
		//                            doNothing / licensing)
		//   - chassis / identity    (router.identity — product /
		//                            company / version Parameters)
		// Empty Nodes are skipped (nothing to cache).
		if !subtreeIsSlotWorthy(nodeChild) {
			continue
		}
		slotIdentity := nodeChild.Identifier + "@" + swRev
		if err := store.WriteDM("emberplus", slotIdentity, storage.DM{
			Protocol: "emberplus",
			Root:     nodeChild,
		}); err != nil {
			return persisted, fmt.Errorf("emberplus: slot DM %q: %w", slotIdentity, err)
		}
		persisted = append(persisted, SlotRef{
			Identity: slotIdentity,
			OID:      nodeChild.OID,
		})
	}
	return persisted, nil
}

// rootOID returns the OID of a canonical element. Used for the
// degenerate "matrix at root" path in SplitAndPersistDM. Returns ""
// when the element lacks an OID (defensive).
func rootOID(el canonical.Element) string {
	if el == nil {
		return ""
	}
	if h := el.Common(); h != nil {
		return h.OID
	}
	return ""
}

// subtreeIsSlotWorthy returns true when the given Node subtree
// contains at least one Matrix, Function, or Parameter — i.e. it
// holds something the consumer / provider needs to round-trip.
// Empty Nodes (containers with no leaves) are not slot-worthy.
//
// Chassis nodes (identity, functions) are slot-worthy via their
// leaf Parameter / Function children — they get their own DM file
// so the manifest can compose the full router by listing them
// alongside the matrix slots.
func subtreeIsSlotWorthy(n *canonical.Node) bool {
	if n == nil {
		return false
	}
	for _, c := range n.Children {
		switch c.(type) {
		case *canonical.Matrix, *canonical.Function, *canonical.Parameter:
			return true
		}
		if cn, ok := c.(*canonical.Node); ok {
			if subtreeIsSlotWorthy(cn) {
				return true
			}
		}
	}
	return false
}

// splitIdentityForSplit duplicates storage.splitIdentity (private
// there). Returns (model, swRev) split on the LAST '@'.
func splitIdentityForSplit(identity string) (string, string) {
	i := strings.LastIndex(identity, "@")
	if i < 0 {
		return identity, ""
	}
	return identity[:i], identity[i+1:]
}
