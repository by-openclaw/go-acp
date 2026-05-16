package main

import (
	"context"
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

// ensureEmberplusTree is the auto-cache wrapper: try hot-load from
// .cache/dm/emberplus/<identity>.json first; on cache miss walk the
// device, then auto-extract the DM so the NEXT call is fast.
//
//  1. dmIdentity != "" + cache hit  -> hot-load, no walk
//  2. dmIdentity != "" + cache miss + !noWalk -> walk, auto-extract
//     under provided identity, return
//  3. dmIdentity == "" + !noWalk -> walk, IdentityProbe, auto-extract
//     under the probed identity (so next run can pass --dm <id>)
//  4. noWalk + cache miss -> error (fail-fast)
//
// Non-Ember+ protocols fall through to a plain Walk — same as before.
// The auto-extract step is best-effort: if IdentityProbe fails or the
// plugin doesn't implement dmSplitter, we still return success because
// the verb's in-RAM tree is populated and the op can proceed.
func ensureEmberplusTree(ctx context.Context, plug protocol.Protocol, host string, port int, dmIdentity string, slot int, noWalk bool) error {
	// Step 1+2: try the explicit-identity hot-load path.
	if dmIdentity != "" {
		seeded, err := hotLoadEmberplusDM(plug, dmIdentity, slot, noWalk)
		if err != nil {
			return err
		}
		if seeded {
			return nil
		}
	}
	if noWalk {
		return fmt.Errorf("--no-walk: no cached DM available (drop --no-walk to walk + auto-extract once, then re-run with --dm)")
	}
	// Step 3: walk + auto-extract.
	if _, err := plug.Walk(ctx, slot); err != nil {
		return fmt.Errorf("walk: %w", err)
	}
	ep, ok := plug.(*emberplus.Plugin)
	if !ok {
		return nil
	}
	// Best-effort identity probe + split-and-persist. Either failure
	// is logged but not fatal — the in-RAM tree is populated and the
	// caller's op (invoke/set/get/matrix) can run.
	identity := dmIdentity
	if identity == "" {
		probed, perr := ep.IdentityProbe(ctx, slot)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "DM auto-extract: identity probe skipped (%v)\n", perr)
			return nil
		}
		identity = probed
	}
	splitDMByMatrix(ctx, plug, host, port, identity)
	fmt.Fprintf(os.Stderr, "DM auto-extract %q — next call: pass --dm %q to skip the walk\n", identity, identity)
	return nil
}
