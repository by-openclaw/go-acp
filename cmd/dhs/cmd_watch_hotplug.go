package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"dhs/internal/dmlib"
	"dhs/internal/export"
	"dhs/internal/protocol"
)

// hotPlugEnricher reacts to per-slot frame-status transitions in the
// watch verb. On a no_card -> ... -> present cascade it probes
// CardIdentity via the plugin (#251), looks up the DM-library entry
// (#246), and seeds the plugin's in-memory tree (#252) so subsequent
// announces decode with type info from the very first frame.
//
// State machine (per spec p.20 SlotStatus):
//
//	no_card  -> powerup : skip (no card to identify yet)
//	powerup  -> boot    : skip (firmware booting)
//	boot     -> present : trigger enrichment block (identify+seed[+walk])
//	present  -> error   : keep tree, emit row
//	present  -> removed : keep tree as stale, emit row
//	removed  -> no_card : drop tree, emit row
//
// One enricher per watch session. Methods are safe for concurrent
// access from a single event-handler goroutine.
type hotPlugEnricher struct {
	mu             sync.Mutex
	prev           map[int]protocol.SlotStatus
	prevFP         map[int]protocol.CardIdentity // last-seen fingerprint per slot
	resolver       dmlib.Resolver                // nil ⇒ no DM-library lookups
	autoWalkOnPlug bool
	out            io.Writer
}

// seederIface is the optional contract a plugin satisfies when it
// supports DM-library seeding. ACP1 satisfies it via Plugin.SeedFromDM.
// Defined here (in the watch verb) rather than in internal/protocol so
// that the protocol/ tier stays free of export.* imports.
type seederIface interface {
	SeedFromDM(slot int, snap *export.Snapshot) error
}

func newHotPlugEnricher(resolver dmlib.Resolver, autoWalkOnPlug bool, out io.Writer) *hotPlugEnricher {
	if out == nil {
		out = os.Stdout
	}
	return &hotPlugEnricher{
		prev:           map[int]protocol.SlotStatus{},
		prevFP:         map[int]protocol.CardIdentity{},
		resolver:       resolver,
		autoWalkOnPlug: autoWalkOnPlug,
		out:            out,
	}
}

// observe applies a frame-status announce to the per-slot state map
// and emits a hot-plug row for every transition that requires action.
// Returns the list of slots whose state changed, for downstream
// orchestration in tests.
func (h *hotPlugEnricher) observe(ctx context.Context, plug protocol.Protocol, ts time.Time, statuses []protocol.SlotStatus) []int {
	if len(statuses) == 0 {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	var transitioned []int
	for slot, cur := range statuses {
		prev, ok := h.prev[slot]
		h.prev[slot] = cur
		if !ok {
			// First sight: if the slot is already present (controller
			// boot finished before our watch started), enrich now —
			// there's no boot→present transition to catch later.
			// Synthesise the transition as no_card → cur so the
			// downstream switch routes it correctly.
			if cur == protocol.SlotPresent {
				transitioned = append(transitioned, slot)
				h.handleTransition(ctx, plug, ts, slot, protocol.SlotNoCard, cur)
			}
			continue
		}
		if prev == cur {
			continue
		}
		transitioned = append(transitioned, slot)
		h.handleTransition(ctx, plug, ts, slot, prev, cur)
	}
	return transitioned
}

// handleTransition prints the hot-plug row and triggers enrichment when
// the transition warrants it.
func (h *hotPlugEnricher) handleTransition(ctx context.Context, plug protocol.Protocol, ts time.Time, slot int, prev, cur protocol.SlotStatus) {
	tsStr := ts.Format("15:04:05")
	tag := fmt.Sprintf("s%-2d %s -> %s", slot, prev.State(), cur.State())

	switch {
	case cur == protocol.SlotPresent && prev != protocol.SlotPresent:
		// Any path landing on present triggers enrichment. The
		// canonical path is boot → present (real hardware always
		// passes through boot); simulator and post-controller-boot
		// shortcuts (no_card / powerup / removed / error → present)
		// land here too. Identity probe is idempotent: re-running it
		// on a card we've seen before just confirms or refreshes the
		// fingerprint.
		swapTag, fp, seedRows, err := h.enrich(ctx, plug, slot)
		if err != nil {
			_, _ = fmt.Fprintf(h.out,"%s  hot-plug  %s  enrichment-failed: %v\n", tsStr, tag, err)
			return
		}
		_, _ = fmt.Fprintf(h.out,"%s  hot-plug  %s  %s %s  %s\n", tsStr, tag, swapTag, fp, seedRows)
	case cur == protocol.SlotRemoved, prev == protocol.SlotRemoved && cur == protocol.SlotNoCard, cur == protocol.SlotNoCard:
		// Hot-removal cascade: keep emitting rows, no re-seed. Drop
		// the cached fingerprint so a re-plug starts fresh
		// ("discovered") rather than silently assuming continuity.
		delete(h.prevFP, slot)
		_, _ = fmt.Fprintf(h.out,"%s  hot-plug  %s\n", tsStr, tag)
	case cur == protocol.SlotError:
		_, _ = fmt.Fprintf(h.out,"%s  hot-plug  %s  card faulted\n", tsStr, tag)
	default:
		// Intermediate transitions (no_card→powerup, powerup→boot):
		// surface for visibility, no action.
		_, _ = fmt.Fprintf(h.out,"%s  hot-plug  %s\n", tsStr, tag)
	}
}

// enrich runs the identity probe, DM-library lookup, plugin seed, and
// (optionally) one-shot walk for a freshly-present slot. Returns:
//
//	swapTag    diagnostic prefix: discovered / re-confirmed /
//	           fw-upgrade / fw-downgrade / card-swap
//	fp         current fingerprint MODEL@REV
//	seedRows   seed-result tag (seeded(N objs) / no-resolver /
//	           no-DM-entry / no-Seeder / schema-empty)
func (h *hotPlugEnricher) enrich(ctx context.Context, plug protocol.Protocol, slot int) (string, string, string, error) {
	idr, ok := plug.(protocol.Identifier)
	if !ok {
		return "discovered", "(no Identifier)", "skipped", nil
	}
	id, err := idr.GetIdentity(ctx, slot)
	if err != nil {
		if errors.Is(err, protocol.ErrIdentityUnresolved) {
			return "discovered", "id-unresolved", "no-DM-entry", nil
		}
		return "", "", "", fmt.Errorf("identity: %w", err)
	}
	fp := fmt.Sprintf("%s@%s", id.Model, id.SwRev)

	// Compare against the per-slot cached fingerprint to classify the
	// transition. Caller (observe) holds h.mu so direct map access is
	// safe.
	swapTag := classifyFingerprintShift(h.prevFP[slot], id, h.prevFP)
	h.prevFP[slot] = id

	if h.resolver == nil {
		return swapTag, fp, "no-resolver", nil
	}
	schema, rerr := h.resolver.Resolve(dmlib.Fingerprint{
		Model: id.Model,
		SwRev: id.SwRev,
		Proto: "acp1", // todo: cross-protocol once Phase 2 wires this verb
	})
	if rerr != nil {
		if errors.Is(rerr, dmlib.ErrNotFound) {
			return swapTag, fp, "no-DM-entry", nil
		}
		return swapTag, fp, "", fmt.Errorf("dmlib.Resolve: %w", rerr)
	}

	seeder, ok := plug.(seederIface)
	if !ok {
		return swapTag, fp, "no-Seeder", nil
	}
	snap, snapOK := schema.Slots[slot]
	if !snapOK {
		// Single-slot products often store under slot 1 regardless of
		// where they're inserted. Fall back to the only entry.
		for _, v := range schema.Slots {
			snap = v
			snapOK = true
			break
		}
	}
	if !snapOK {
		return swapTag, fp, "schema-empty", nil
	}
	if err := seeder.SeedFromDM(slot, snap); err != nil {
		return swapTag, fp, "", fmt.Errorf("seed: %w", err)
	}
	objCount := 0
	for _, sd := range snap.Slots {
		objCount += len(sd.Objects)
	}
	tag := fmt.Sprintf("seeded(%d objs)", objCount)

	if h.autoWalkOnPlug {
		go func() {
			objs, werr := plug.Walk(ctx, slot)
			if werr != nil {
				_, _ = fmt.Fprintf(h.out,"warning: post-plug walk slot %d: %v\n", slot, werr)
				return
			}
			_, _ = fmt.Fprintf(h.out,"         hot-plug s%-2d  walk=ok(%d)\n", slot, len(objs))
		}()
	}
	return swapTag, fp, tag, nil
}

// classifyFingerprintShift compares the previous and current per-slot
// fingerprint and returns the diagnostic prefix the row should carry.
// prevFP is the entire enricher cache; we only need the per-slot prior
// here, but passing the map keeps the signature open for future cross-
// slot logic (e.g. card moved between slots).
//
// Outcomes:
//
//	discovered      no prior fingerprint cached
//	re-confirmed    same Model + same SwRev (HwRev variation ignored)
//	fw-upgrade      same Model, current SwRev > prior (lex compare)
//	fw-downgrade    same Model, current SwRev < prior
//	card-swap       different Model
func classifyFingerprintShift(prev, cur protocol.CardIdentity, _ map[int]protocol.CardIdentity) string {
	if prev.Model == "" {
		return "discovered"
	}
	if prev.Model != cur.Model {
		return fmt.Sprintf("card-swap %s@%s →", prev.Model, prev.SwRev)
	}
	if prev.SwRev == cur.SwRev {
		return "re-confirmed"
	}
	if cur.SwRev > prev.SwRev {
		return fmt.Sprintf("fw-upgrade %s %s →", prev.Model, prev.SwRev)
	}
	return fmt.Sprintf("fw-downgrade %s %s →", prev.Model, prev.SwRev)
}
