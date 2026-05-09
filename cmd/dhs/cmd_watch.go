package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"dhs/internal/dmlib"
	"dhs/internal/export"
	"dhs/internal/protocol"
	"dhs/internal/storage"
)

// runWatch subscribes to live announcements and prints each event as it
// arrives. Blocks until Ctrl-C.
//
// Subscribe filters (what events the user sees):
//
//	--slot N        only this slot (default: any)
//	--group G       only this group (default: any)
//	--label L       only this object (requires prior walk for resolution)
//	--id I          only this object id within --group
//
// Discovery scope (what slots get walked at startup):
//
//	(no flag)             default: walk NOTHING; rely on DM-library seed.
//	--slot N              walk slot N only (legacy single-slot mode).
//	--slots 1,3,7         walk listed slots only.
//	--slots all           walk every present slot (legacy "walk-all" mode).
//	--no-walk             suppress every on-demand walk; pure announce view.
//	--auto-walk-on-plug   walk slot N on no_card -> present transition
//	                      (hot-plug enrichment lands in #254).
//
// Default discovery scope changed: previous behaviour was to walk every
// present slot at connect time. This caused walk-storm latency and
// surprised operators who only wanted frame-status. New default is no
// walks; opt in via --slot, --slots, or --auto-walk-on-plug.
func runWatch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", -1, "slot filter (-1 = any); also walks this slot at startup")
	slotsArg := fs.String("slots", "",
		`walk scope: comma-separated slot numbers (e.g. "1,3,7") or "all" `+
			`for every present slot. Empty means walk nothing (announces only).`)
	noWalk := fs.Bool("no-walk", false, "suppress every on-demand walk; pure announce view")
	autoWalkOnPlug := fs.Bool("auto-walk-on-plug", false,
		"walk a slot on no_card -> present transition (hot-plug enrichment, #254)")
	group := fs.String("group", "", "group filter (empty = any)")
	label := fs.String("label", "", "label filter (requires prior walk)")
	id := fs.Int("id", -1, "object id filter (-1 = any)")
	dmLibrary := fs.String("dm-library", "",
		"DM library root for hot-plug enrichment (#254). Empty disables identity probe + seed.")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer <proto> watch <host> [--slot N | --slots 1,3,7 | --slots all] [--no-walk] [--auto-walk-on-plug] [--dm-library <path>] [--group G] [--label L]")
	}
	_ = fs.Parse(rest)

	walkScope, scopeErr := parseWalkScope(*slot, *slotsArg, *noWalk)
	if scopeErr != nil {
		return scopeErr
	}

	var resolver dmlib.Resolver
	if *dmLibrary != "" {
		resolver = dmlib.New(*dmLibrary)
	}
	enricher := newHotPlugEnricher(resolver, *autoWalkOnPlug, os.Stdout)

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	// Load IP-keyed disk cache for instant label/unit resolution while
	// walk runs. Key by watchCacheKey so ACP1 groups that re-use the
	// same object-id space (control / status / alarm / identity / file
	// / frame all addressable as 0..N within one slot) don't collide
	// (refs #236).
	//
	// ACP2 deliberately skips this path — its DM cache is identity-
	// keyed at .cache/dm/<identity>.json (DHS 2016 MasterView model,
	// #353/#355) and is loaded into the plugin's WalkedTree directly
	// in the block below. Letting the IP-keyed loader run for ACP2
	// would emit a misleading "loaded N labels from cache" line backed
	// by stale per-IP files even when the identity cache is current.
	labelCache := map[string]string{}
	unitCache := map[string]string{}
	if cf.protocol != "acp2" && treeStore != nil && *slot >= 0 {
		if snap, lerr := treeStore.Load(host, *slot); lerr == nil && snap != nil {
			for _, sd := range snap.Slots {
				for _, o := range sd.Objects {
					k := watchCacheKey(o.Group, o.ID)
					if o.Label != "" {
						labelCache[k] = o.Label
					}
					if o.Unit != "" {
						unitCache[k] = o.Unit
					}
				}
				// Seed the plugin's in-memory tree cache from disk.
				// Only the plugins that implement TreeSeeder benefit;
				// others ignore the call (interface assertion is
				// optional).
				if seeder, ok := plug.(interface {
					SeedTreeFromCachedObjects(slot int, objs []protocol.Object)
				}); ok && sd.Slot == *slot {
					seeder.SeedTreeFromCachedObjects(sd.Slot, sd.Objects)
				}
			}
			if len(labelCache) > 0 {
				fmt.Fprintf(os.Stderr, "loaded %d labels from cache\n", len(labelCache))
			}
		}
	}

	// Identity-keyed DM cache hot-load (#353): probe device identity
	// (Card Name + Hardware Version on slot 0, sub-second), look up
	// the persisted DM by identity, and seed the plugin's in-memory
	// tree BEFORE Subscribe fires. The announce decoder then resolves
	// types + enum labels from frame 1 — eliminates the cold-start
	// "idx N" gap on slot 1 watches where the fresh walk takes 30-60s.
	//
	// ACP2-only — other plugins keep the IP-keyed cache today.
	if cf.protocol == "acp2" && treeStore != nil {
		if probe, hot := plug.(interface {
			IdentityProbe(context.Context) (string, error)
			SeedTreeFromCachedObjects(slot int, objs []protocol.Object)
		}); hot {
			identity, perr := probe.IdentityProbe(ctx)
			if perr == nil && identity != "" {
				if snap, lerr := treeStore.LoadByIdentity(identity); lerr == nil && snap != nil {
					seeded := 0
					for _, sd := range snap.Slots {
						probe.SeedTreeFromCachedObjects(sd.Slot, sd.Objects)
						seeded += len(sd.Objects)
					}
					if seeded > 0 {
						fmt.Fprintf(os.Stderr, "DM cache hit %q — seeded %d objects across %d slot(s)\n",
							identity, seeded, len(snap.Slots))
					}
				}
			}
		}
	}

	// Walk in background to populate label/type cache. Announces start
	// immediately — labels resolve as the tree fills.
	if !walkScope.empty() {
		go func() {
			runWalkScope(ctx, plug, host, cf.protocol, walkScope)
		}()
	}

	req := protocol.ValueRequest{
		Slot:  *slot,
		Group: *group,
		Label: *label,
		ID:    *id,
	}

	// Subscribe. The plugin pushes decoded Event values into our channel
	// via the callback; we print them from the main goroutine so output
	// is serialised cleanly with Ctrl-C handling.
	events := make(chan protocol.Event, 128)
	if err := plug.Subscribe(req, func(ev protocol.Event) {
		select {
		case events <- ev:
		default:
			// Drop on full buffer — better than blocking the receive
			// goroutine and missing unrelated events.
		}
	}); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	defer func() { _ = plug.Unsubscribe(req) }()

	fmt.Println("watching — Ctrl-C to stop")
	fmt.Printf("%-8s  %-18s  %-30s  %-20s  %-3s  %-7s  value\n",
		"time", "oid", "path", "label", "acc", "fr")
	fmt.Println(strings.Repeat("-", 117))

	// prevFrame remembers the last frame-status slice so we can emit
	// per-slot deltas instead of dumping the full 31-slot strip on every
	// announce. Kept slot-list-empty until the first frame event arrives.
	var prevFrame []protocol.SlotStatus

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-events:
			// Frame-status announce drives hot-plug enrichment (#254).
			// ACP1 emits group=frame, id=0 with a SlotStatus[] payload.
			if ev.Group == "frame" && ev.ID == 0 && ev.Value.Kind == protocol.KindFrame {
				enricher.observe(ctx, plug, ev.Timestamp, ev.Value.SlotStatus)
			}

			// Use disk cache label if plugin hasn't resolved it yet.
			label := ev.Label
			src := "live"
			if label == "" {
				if cached, ok := labelCache[watchCacheKey(ev.Group, ev.ID)]; ok {
					label = cached
					src = "cache"
				}
			}
			// OID falls back to "s<slot>.<group>.<id>" for protocols
			// that don't populate it (ACP1 / ACP2).
			oid := ev.OID
			if oid == "" {
				oid = fmt.Sprintf("s%d.%s.%d", ev.Slot, ev.Group, ev.ID)
			}

			// Matrix crosspoint events render differently —
			// target/sources/disposition replace the single value
			// column.
			lockedTag := func(b bool) string {
				if b {
					return " locked"
				}
				return ""
			}
			if mc := ev.MatrixChange; mc != nil {
				srcList := "[]"
				if len(mc.Sources) > 0 {
					parts := make([]string, len(mc.Sources))
					for i, s := range mc.Sources {
						parts[i] = fmt.Sprintf("%d", s)
					}
					srcList = "[" + strings.Join(parts, ",") + "]"
				}
				fmt.Printf("%s  %-18s  %-30s  %-20s  [matrix]  t=%d ← %s op=%s disp=%s%s\n",
					ev.Timestamp.Format("15:04:05"),
					truncate(oid, 18),
					truncate(ev.Path, 30),
					truncate(label, 20),
					mc.Target,
					srcList,
					mc.Operation,
					mc.Disposition,
					lockedTag(mc.Locked),
				)
				continue
			}

			// Frame-status announce — emit per-slot deltas instead of
			// dumping the full 31-slot strip every time. First event of
			// the session prints a baseline; subsequent events with no
			// change are suppressed entirely. (refs #239)
			if ev.Value.Kind == protocol.KindFrame {
				cur := ev.Value.SlotStatus
				ts := ev.Timestamp.Format("15:04:05")
				fr := ev.Freshness
				if fr == "" {
					fr = src
				}
				if prevFrame == nil {
					fmt.Printf("%s  %-18s  %-30s  %-20s  %-3s  %-7s  %s\n",
						ts,
						truncate(oid, 18),
						truncate(ev.Path, 30),
						truncate(label, 20),
						accessStr(ev.Access),
						fr,
						"baseline "+formatFrameStatus(cur),
					)
				} else {
					for _, c := range frameStatusDelta(prevFrame, cur) {
						fmt.Printf("%s  %-18s  %-30s  %-20s  %-3s  %-7s  %s\n",
							ts,
							truncate(oid, 18),
							truncate(ev.Path, 30),
							truncate(label, 20),
							accessStr(ev.Access),
							fr,
							c,
						)
					}
					// Silent on no-change re-broadcasts.
				}
				prevFrame = append(prevFrame[:0], cur...)
				continue
			}

			// Parameter event — value column. Prefer the live unit
			// carried on ev.Unit (#359 — populated by the plugin at
			// announce-decode time from the in-memory tree). Fall back
			// to the disk-cache unit when the live tree didn't have the
			// object yet (cold start before walk finishes).
			valStr := formatValueInline(ev.Value)
			unit := ev.Unit
			if unit == "" {
				unit = unitCache[watchCacheKey(ev.Group, ev.ID)]
			}
			if unit != "" {
				valStr += " " + unit
			}
			// Live description + access + freshness + changes tag.
			descTag := ""
			if ev.Description != "" {
				descTag = fmt.Sprintf("  desc=%q", ev.Description)
			}
			changesTag := ""
			if len(ev.Changes) > 0 {
				changesTag = "  changed: " + formatChanges(ev.Changes)
			}
			fr := ev.Freshness
			if fr == "" {
				fr = src
			}
			fmt.Printf("%s  %-18s  %-30s  %-20s  %-3s  %-7s  %s%s%s\n",
				ev.Timestamp.Format("15:04:05"),
				truncate(oid, 18),
				truncate(ev.Path, 30),
				truncate(label, 20),
				accessStr(ev.Access),
				fr,
				valStr,
				descTag,
				changesTag,
			)
		}
	}
}

// frameStatusDelta returns one human-readable transition line per slot
// where prev[i] != cur[i]. The caller is expected to emit a baseline
// line for the first observation (when prev is nil/empty); this helper
// returns no entries in that case so the caller can branch cleanly.
// Length mismatches between prev and cur are tolerated by treating the
// missing positions as SlotNoCard. (refs #239)
func frameStatusDelta(prev, cur []protocol.SlotStatus) []string {
	if len(prev) == 0 {
		return nil
	}
	n := len(cur)
	if len(prev) > n {
		n = len(prev)
	}
	var out []string
	for i := 0; i < n; i++ {
		var oldS, newS protocol.SlotStatus
		if i < len(prev) {
			oldS = prev[i]
		}
		if i < len(cur) {
			newS = cur[i]
		}
		if oldS != newS {
			out = append(out, fmt.Sprintf("slot %d: %s -> %s", i, oldS, newS))
		}
	}
	return out
}

// watchCacheKey builds a stable per-(group, id) cache key for label and
// unit lookups in the watch verb. ACP1 groups (control / status / alarm
// / identity / file / frame) re-use the same small object-id space within
// one slot, so a flat ID-only map collides distinct labels — e.g. slot 1
// has both control.0=IO-Ctrl and status.0=sInp1 (refs #236). ACP2 has no
// Group concept; the empty group reduces the key to ".N" and stays
// unique per slot.
func watchCacheKey(group string, id int) string {
	return fmt.Sprintf("%s.%d", group, id)
}

// walkScope describes which slots the watch verb should walk at startup.
// One of three modes:
//
//	mode=none  → walk nothing (default, or --no-walk).
//	mode=list  → walk every entry in slots[].
//	mode=all   → walk every slot the device reports as present.
type walkScope struct {
	mode  walkMode
	slots []int
}

type walkMode int

const (
	walkNone walkMode = iota
	walkList
	walkAll
)

func (s walkScope) empty() bool { return s.mode == walkNone }

// parseWalkScope folds the --slot, --slots, and --no-walk flags into a
// single decision. Mutually exclusive combinations raise a clear error.
func parseWalkScope(slot int, slotsArg string, noWalk bool) (walkScope, error) {
	if noWalk {
		if slotsArg != "" {
			return walkScope{}, fmt.Errorf("--no-walk and --slots are mutually exclusive")
		}
		return walkScope{mode: walkNone}, nil
	}
	if slotsArg != "" {
		if strings.EqualFold(slotsArg, "all") {
			return walkScope{mode: walkAll}, nil
		}
		var slots []int
		for _, part := range strings.Split(slotsArg, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			n, err := strconv.Atoi(part)
			if err != nil || n < 0 {
				return walkScope{}, fmt.Errorf("--slots: %q is not a non-negative integer", part)
			}
			slots = append(slots, n)
		}
		if len(slots) == 0 {
			return walkScope{}, fmt.Errorf(`--slots: empty list (use "all" or a non-empty comma-separated list)`)
		}
		return walkScope{mode: walkList, slots: slots}, nil
	}
	if slot >= 0 {
		return walkScope{mode: walkList, slots: []int{slot}}, nil
	}
	return walkScope{mode: walkNone}, nil
}

// runWalkScope executes the walk decision against the live plugin.
// Errors print to stderr and do not abort other slots in the batch.
func runWalkScope(ctx context.Context, plug protocol.Protocol, host, proto string, scope walkScope) {
	switch scope.mode {
	case walkNone:
		return
	case walkList:
		for _, s := range scope.slots {
			walkSlotAndCache(ctx, plug, host, proto, s)
		}
	case walkAll:
		info, ierr := plug.GetDeviceInfo(ctx)
		if ierr != nil {
			fmt.Fprintf(os.Stderr, "warning: GetDeviceInfo: %v\n", ierr)
			return
		}
		for s := 0; s < info.NumSlots; s++ {
			si, serr := plug.GetSlotInfo(ctx, s)
			if serr != nil || si.Status != protocol.SlotPresent {
				continue
			}
			walkSlotAndCache(ctx, plug, host, proto, s)
		}
	}
}

// identityProber is the optional contract a Protocol plugin satisfies
// to participate in identity-keyed MasterView caching. ACP2 implements
// it via Plugin.IdentityProbe (Card Name + Hardware Version on slot 0).
// ACP1 / Ember+ do not — they fall back to IP-keyed storage.
type identityProber interface {
	IdentityProbe(ctx context.Context) (string, error)
}

func walkSlotAndCache(ctx context.Context, plug protocol.Protocol, host, proto string, slot int) {
	objs, werr := plug.Walk(ctx, slot)
	if werr != nil {
		fmt.Fprintf(os.Stderr, "warning: walk slot %d failed: %v\n", slot, werr)
		return
	}
	prober, _ := plug.(identityProber)
	saveSlotCache(ctx, prober, host, proto, slot, objs)
}

// saveSlotCache routes a walked slot to the right on-disk cache.
//
// ACP2 has a stable device identity probe (Card Name + Hardware Version
// on slot 0), so its cache is keyed by identity and lives in one
// multi-slot file at .cache/dm/<identity>.json — DHS 2016 MasterView
// model. Walking any slot of the frame accumulates into the same file.
// IP-keyed cache is NOT written for ACP2 (#353, #355).
//
// Other protocols (ACP1, Ember+) keep the legacy IP-keyed layout at
// .cache/devices/<ip>/slot_<n>.json — those plugins have no identity
// probe contract today.
func saveSlotCache(ctx context.Context, prober identityProber, host, proto string, slot int, objs []protocol.Object) {
	if treeStore == nil {
		return
	}
	if proto == "acp2" {
		if prober == nil {
			fmt.Fprintf(os.Stderr, "warning: acp2 plugin missing IdentityProbe; slot %d not cached\n", slot)
			return
		}
		identity, perr := prober.IdentityProbe(ctx)
		if perr != nil || identity == "" {
			fmt.Fprintf(os.Stderr, "warning: identity probe failed; slot %d not cached: %v\n", slot, perr)
			return
		}
		if serr := saveIdentityCache(treeStore, identity, host, proto, slot, objs); serr != nil {
			fmt.Fprintf(os.Stderr, "warning: identity cache save: %v\n", serr)
		}
		return
	}
	if serr := treeStore.Save(host, proto, slot, objs); serr != nil {
		fmt.Fprintf(os.Stderr, "warning: cache save slot %d: %v\n", slot, serr)
	}
}

// saveIdentityCache loads the existing identity-keyed DM (if any),
// merges the just-walked slot into it, and saves the result. So
// successive walks of slot 0 then slot 1 build up a single
// multi-slot cache file keyed by device identity.
func saveIdentityCache(store *storage.TreeStore, identity, host, proto string, slot int, objs []protocol.Object) error {
	existing, _ := store.LoadByIdentity(identity)
	now := time.Now().UTC()
	if existing == nil {
		existing = &export.Snapshot{
			Device:    export.DeviceInfo{IP: host, Protocol: proto},
			Generator: "dhs dm-cache",
			CreatedAt: now,
		}
	}
	// Replace or append this slot's dump.
	updated := false
	for i := range existing.Slots {
		if existing.Slots[i].Slot == slot {
			existing.Slots[i] = export.SlotDump{
				Slot:     slot,
				WalkedAt: now,
				Objects:  objs,
			}
			updated = true
			break
		}
	}
	if !updated {
		existing.Slots = append(existing.Slots, export.SlotDump{
			Slot:     slot,
			WalkedAt: now,
			Objects:  objs,
		})
	}
	return store.SaveByIdentity(identity, existing)
}
