package main

import (
	"fmt"
	"os"

	"dhs/internal/protocol"

	emberplus "dhs/internal/emberplus/consumer"
)

// hotLoadEmberplusDM seeds an Ember+ plugin's in-RAM tree from
// .cache/dm/emberplus/<identity>.json so the calling verb can skip its
// per-call wire walk (refs #438, ADR-0022). Same pattern as ACP1/ACP2.
//
// Return values:
//   - (true, nil)  : cache hit + tree successfully seeded
//   - (false, nil) : cache miss; caller falls back to wire walk
//                    (unless noWalk, in which case error is returned)
//   - (false, err) : configuration/IO error (caller aborts)
//
// dmIdentity empty → no-op (false, nil). Lets verbs pass their flag
// through unconditionally.
func hotLoadEmberplusDM(plug protocol.Protocol, dmIdentity string, slot int, noWalk bool) (bool, error) {
	if dmIdentity == "" {
		if noWalk {
			return false, fmt.Errorf("--no-walk requires --dm <identity> for cache-driven mode")
		}
		return false, nil
	}
	ep, ok := plug.(*emberplus.Plugin)
	if !ok {
		return false, fmt.Errorf("--dm: only Ember+ supports identity-keyed DM cache")
	}
	if treeStore == nil {
		return false, fmt.Errorf("--dm: tree store not initialised")
	}
	snap, err := treeStore.LoadByIdentity("emberplus", dmIdentity)
	if err != nil {
		return false, fmt.Errorf("--dm %q: load: %w", dmIdentity, err)
	}
	if snap == nil || len(snap.Slots) == 0 {
		if noWalk {
			return false, fmt.Errorf("--no-walk: no DM cached for identity %q (run 'watch' once to populate .cache/dm/emberplus/, or drop --no-walk)", dmIdentity)
		}
		return false, nil
	}
	ep.SeedTreeFromCachedObjects(slot, snap.Slots[0].Objects)
	fmt.Fprintf(os.Stderr, "DM hot-load %q — seeded %d objects (no walk)\n",
		dmIdentity, len(snap.Slots[0].Objects))
	return true, nil
}
