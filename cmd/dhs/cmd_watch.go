package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"dhs/internal/dmlib"
	emberplus "dhs/internal/emberplus/consumer"
	"dhs/internal/export/canonical"
	"dhs/internal/manifest"
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
	pathFilter := fs.String("path", "",
		"comma-separated OID or label-path prefixes to narrow wildcard subscribe "+
			`(Ember+ only; e.g. "1.2.3,mixers.primary.channels")`)
	noStreams := fs.Bool("no-streams", false,
		"skip Subscribe(30) for Parameters with streamIdentifier != 0 "+
			"(Ember+ — drops metering flood)")
	streamsOnly := fs.Bool("streams-only", false,
		"Subscribe(30) only for Parameters with streamIdentifier != 0 "+
			"(Ember+ — metering view only)")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer <proto> watch <host> [--slot N | --slots 1,3,7 | --slots all] [--no-walk] [--auto-walk-on-plug] [--dm-library <path>] [--group G] [--label L] [--path P1,P2] [--no-streams | --streams-only]")
	}
	_ = fs.Parse(rest)

	if *noStreams && *streamsOnly {
		return fmt.Errorf("--no-streams and --streams-only are mutually exclusive")
	}

	walkScope, scopeErr := parseWalkScope(*slot, *slotsArg, *noWalk)
	if scopeErr != nil {
		return scopeErr
	}

	var resolver dmlib.Resolver
	if *dmLibrary != "" {
		resolver = dmlib.New(*dmLibrary)
	}
	enricher := newHotPlugEnricher(resolver, *autoWalkOnPlug, os.Stdout)

	plug, cleanup, err := connectWithRetry(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	// Load IP-keyed disk cache for instant label/unit resolution while
	// walk runs. Key by watchCacheKey so groups that re-use the same
	// object-id space (control / status / alarm / identity / file /
	// frame all addressable as 0..N within one slot) don't collide
	// (refs #236).
	//
	// ACP1 + ACP2 both skip this path — their DM caches are identity-
	// keyed at .cache/dm/<CardName>@<HwVer>.json per DHS 2016
	// MasterView (#353/#355/#363), loaded into each slot's tree
	// directly in the block below. Letting the IP-keyed loader run
	// would emit a misleading "loaded N labels from cache" line
	// backed by stale per-IP files.
	labelCache := map[string]string{}
	unitCache := map[string]string{}
	if cf.protocol != "acp2" && cf.protocol != "acp1" && treeStore != nil && *slot >= 0 {
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

	// Per-card DM hot-load: each slot can host a different card, and
	// the DM cache is keyed by card identity (CardName@HwVersion),
	// not by device-frame. Probe each slot we plan to watch, load
	// that slot's DM file, seed the slot's tree. Two slots holding
	// the same card share one DM file (loaded once, seeded into
	// multiple slot trees) — DHS 2016 MasterView model.
	//
	// Protocol-agnostic — any plugin satisfying both IdentityProbe +
	// SeedTreeFromCachedObjects participates. ACP1 + ACP2 do today
	// (#363); Ember+ has no per-slot card concept and stays on its
	// existing path.
	cachedSlots := map[int]struct{}{}
	if treeStore != nil && !walkScope.empty() {
		if probe, hot := plug.(interface {
			IdentityProbe(context.Context, int) (string, error)
			SeedTreeFromCachedObjects(slot int, objs []protocol.Object)
		}); hot {
			loaded := map[string]int{} // identity -> objects (for log line)
			seenSlots := map[int]struct{}{}
			for _, s := range hotLoadSlots(ctx, plug, walkScope) {
				if _, dup := seenSlots[s]; dup {
					continue
				}
				seenSlots[s] = struct{}{}
				identity, perr := probe.IdentityProbe(ctx, s)
				if perr != nil || identity == "" {
					continue
				}
				snap, lerr := treeStore.LoadByIdentity(cf.protocol, identity)
				if lerr != nil || snap == nil || len(snap.Slots) == 0 {
					continue
				}
				probe.SeedTreeFromCachedObjects(s, snap.Slots[0].Objects)
				loaded[identity] = len(snap.Slots[0].Objects)
				cachedSlots[s] = struct{}{}
				fmt.Fprintf(os.Stderr, "DM cache hit %q — seeded slot %d with %d objects\n",
					identity, s, len(snap.Slots[0].Objects))
			}
			_ = loaded
		}
	}

	// Walk in background to populate label/type cache. Announces start
	// immediately — labels resolve as the tree fills. Slots that hit
	// the DM cache above already have their tree populated; re-walking
	// them is pure overhead — 49k+ getObject round-trips on a CONVERT
	// Hybrid card — and starves the announce path on a single AN2/TCP
	// socket. Drop those from the scope.
	walkBackground := walkScope.withoutSlots(cachedSlots)
	if !walkBackground.empty() {
		go func() {
			runWalkScope(ctx, plug, host, cf.protocol, walkBackground)
		}()
	}

	req := protocol.ValueRequest{
		Slot:  *slot,
		Group: *group,
		Label: *label,
		ID:    *id,
	}

	// Apply Ember+ wildcard-subscribe filters (--path / --no-streams /
	// --streams-only) BEFORE Subscribe registers the wildcard. Optional
	// capability — plugins that don't satisfy the interface (ACP1/ACP2,
	// Probel, …) silently ignore the flags. No protocol-name compare.
	applyWildcardFilter(plug, *pathFilter, *noStreams, *streamsOnly)

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
				oid,
				ev.Path,
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

// wildcardSubscribeFilterer is the optional capability a plugin
// implements to narrow which Parameters its wildcard "*" Subscribe
// will register Command 30 for. Empty paths slice + both bools false
// = subscribe everything (current default). Defined as a private
// interface in cmd/dhs/ so generic CLI does not import any per-
// protocol package and ACP1/ACP2/Probel silently ignore the new
// flags (they don't satisfy the interface).
type wildcardSubscribeFilterer interface {
	SetWildcardSubscribeFilter(paths []string, noStreams, streamsOnly bool)
}

// applyWildcardFilter parses the --path comma-list and forwards the
// filter to the plugin if it supports it. No-op when the plugin
// doesn't satisfy the optional capability or when no filter flags
// were set.
func applyWildcardFilter(plug protocol.Protocol, pathFilter string, noStreams, streamsOnly bool) {
	if pathFilter == "" && !noStreams && !streamsOnly {
		return
	}
	wf, ok := plug.(wildcardSubscribeFilterer)
	if !ok {
		return
	}
	var paths []string
	for _, p := range strings.Split(pathFilter, ",") {
		if t := strings.TrimSpace(p); t != "" {
			paths = append(paths, t)
		}
	}
	wf.SetWildcardSubscribeFilter(paths, noStreams, streamsOnly)
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

// withoutSlots returns a copy of the scope with the listed slots removed.
// walkList narrows; walkAll degrades to walkList of "every slot" — but
// since walkAll defers slot enumeration to GetDeviceInfo, callers using
// it will only see the filter applied if they list slots explicitly.
// walkNone stays empty.
func (s walkScope) withoutSlots(skip map[int]struct{}) walkScope {
	if len(skip) == 0 || s.mode == walkNone {
		return s
	}
	if s.mode != walkList {
		return s
	}
	kept := make([]int, 0, len(s.slots))
	for _, sl := range s.slots {
		if _, drop := skip[sl]; drop {
			continue
		}
		kept = append(kept, sl)
	}
	if len(kept) == 0 {
		return walkScope{mode: walkNone}
	}
	return walkScope{mode: walkList, slots: kept}
}

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

// hotLoadSlots returns the slot list to hot-load DM for, derived from
// the walk scope. walkList → the listed slots; walkAll → every present
// slot per GetSlotInfo; walkNone → empty (no hot-load needed since no
// subscription scope is set). Errors are best-effort: failure to read
// device info just returns whatever's already known.
func hotLoadSlots(ctx context.Context, plug protocol.Protocol, scope walkScope) []int {
	switch scope.mode {
	case walkList:
		out := make([]int, len(scope.slots))
		copy(out, scope.slots)
		return out
	case walkAll:
		info, err := plug.GetDeviceInfo(ctx)
		if err != nil {
			return nil
		}
		out := make([]int, 0, info.NumSlots)
		for s := 0; s < info.NumSlots; s++ {
			si, serr := plug.GetSlotInfo(ctx, s)
			if serr != nil || si.Status != protocol.SlotPresent {
				continue
			}
			out = append(out, s)
		}
		return out
	}
	return nil
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
// to participate in per-card MasterView caching. The probe walks ONE
// slot's identity scope (Card Name + Hardware Version objects in
// ROOT_NODE_V2.BOARD) and returns "<CardName>@<HwVer>". ACP2
// implements it; ACP1 / Ember+ have no identity contract today and
// fall back to IP-keyed storage.
type identityProber interface {
	IdentityProbe(ctx context.Context, slot int) (string, error)
}

// canonicalExporter is the optional contract a Plugin satisfies to
// produce a hierarchical canonical.Export from its in-RAM tree. When
// present, saveSlotCache persists the canonical Tree alongside the
// flat Objects so the DM file is the exact shape `dhs export
// --format json` outputs and `dhs producer --tree X.json` reads
// (refs #438, ADR-0022). Ember+ satisfies it via
// internal/emberplus/consumer/export_canonical.go.
type canonicalExporter interface {
	ExportCanonical(ctx context.Context) (*canonical.Export, error)
}

func walkSlotAndCache(ctx context.Context, plug protocol.Protocol, host, proto string, slot int) {
	objs, werr := plug.Walk(ctx, slot)
	if werr != nil {
		fmt.Fprintf(os.Stderr, "warning: walk slot %d failed: %v\n", slot, werr)
		return
	}
	prober, _ := plug.(identityProber)
	tree := canonicalTreeFromPlug(ctx, plug)
	// Plugins with both canonicalize + dmSplitter (Ember+) get both
	// the full provider DM AND per-slot DMs so each matrix / function
	// / chassis subtree lives in its own file under
	// .cache/dm/<proto>/<slotIdentity>.json (refs #438, ADR-0022/3).
	if _, splittable := plug.(dmSplitter); splittable && tree != nil {
		saveAndSplitForEmberplus(ctx, plug, prober, host, proto, slot, objs, tree)
		return
	}
	saveSlotCache(ctx, prober, host, proto, slot, objs, tree)
}

// canonicalTreeFromPlug invokes the plugin's ExportCanonical when
// available, surfacing a *canonical.Export the caller passes to
// saveSlotCache so the DM file is written in canonical Tree shape
// (refs #438, ADR-0022). Returns nil when the plugin doesn't satisfy
// the canonicalExporter contract or when canonicalize fails — the
// caller falls back to flat-Objects-only DM in that case.
func canonicalTreeFromPlug(ctx context.Context, plug protocol.Protocol) *canonical.Export {
	ce, ok := plug.(canonicalExporter)
	if !ok {
		return nil
	}
	exp, err := ce.ExportCanonical(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: canonicalize for cache: %v\n", err)
		return nil
	}
	return exp
}

// saveSlotCache routes a walked slot to the right on-disk cache.
//
// Per DHS 2016 MasterView (#363): any plugin that satisfies the
// identityProber contract gets one DM file per CARD identity at
// `.cache/dm/<CardName>@<HwVer>.json`. Each file contains the schema
// for ONE card type — single slot dump. Two slots holding the same
// card share the same DM file (loaded once, reused). Slot index +
// IP are NOT in the path or key.
//
// When the plugin satisfies identityProber but the probe cannot
// determine an identity from the wire, the DM still lands at
// `.cache/dm/<proto>/<fallbackIdentity>.json` using a host-anchored
// pseudo-identity ("unknown@<host>") — preserves the xxx@xxx.json
// naming convention so downstream tooling keeps working, and gives
// the user a file to rename once the real identity is determined.
// A warning is logged so the gap is visible.
//
// Plugins that do NOT satisfy identityProber fall back to the
// legacy IP-keyed `.cache/devices/<ip>/slot_<n>.json` layout.
func saveSlotCache(ctx context.Context, prober identityProber, host, proto string, slot int, objs []protocol.Object, tree *canonical.Export) string {
	if treeStore == nil {
		return ""
	}
	if prober != nil {
		identity, perr := prober.IdentityProbe(ctx, slot)
		if perr != nil || identity == "" {
			identity = fallbackIdentity(host)
			fmt.Fprintf(os.Stderr,
				"warning: identity probe failed for slot %d (%v); writing fallback DM as %s.json — rename when identity is determined\n",
				slot, perr, identity)
		}
		if serr := saveIdentityCache(treeStore, identity, host, proto, slot, objs, tree); serr != nil {
			fmt.Fprintf(os.Stderr, "warning: identity cache save: %v\n", serr)
			return ""
		}
		return identity
	}
	if serr := treeStore.Save(host, proto, slot, objs); serr != nil {
		fmt.Fprintf(os.Stderr, "warning: cache save slot %d: %v\n", slot, serr)
	}
	return ""
}

// fallbackIdentity synthesises a pseudo-identity when the protocol's
// IdentityProbe cannot determine one from the wire. host anchors the
// fallback so two un-probed devices on different hosts do not collide
// on the same DM file. The "unknown" prefix marks the file for manual
// rename once real identity is determined; the "@" separator keeps
// the file matching the xxx@xxx.json naming convention so downstream
// tooling (CLI listing, manifest loaders) keeps working.
//
// Limitation: two un-probed devices on the SAME host but different
// ports collide under one DM file. The walk warning surfaces this so
// the user knows to investigate.
func fallbackIdentity(host string) string {
	return "unknown@" + host
}

// writeSlotManifest emits a generic manifest binding {protocol-specific
// slot addr → cached DM filename}. Same writer for every protocol:
//
//   - acp1 / acp2 : addr = {"slot": N}
//   - emberplus    : addr = {"oid": "1.2"}   (per-slot split path)
//   - probel       : addr = {"matrix": M, "level": L}
//
// Called by callers that walk multiple slots in one session (cmd_walk
// --all, hot-plug watch). slotBindings is keyed by slot number for
// acp* / probel; emberplus uses splitDMByMatrix directly because its
// "slots" are derived from a single Walk's canonical tree.
//
// deviceName is the human-readable label that becomes the manifest
// slug (.cache/manifest/<slug>.json); when empty, derives from the
// first identity's Model part.
type slotBinding struct {
	Slot     int
	Identity string
}

func writeSlotManifest(deviceName, proto, host string, port int, bindings []slotBinding) {
	if len(bindings) == 0 {
		return
	}
	if deviceName == "" {
		// Fall back to the Model of the first cached identity. Strip
		// "@SwRev" suffix so the manifest slug is the card / device
		// name rather than a versioned key.
		m, _ := splitIdentityForManifest(bindings[0].Identity)
		deviceName = m
	}
	mf := &manifest.Manifest{
		Device: manifest.Device{
			Name:     deviceName,
			Protocol: proto,
			Endpoints: []manifest.Endpoint{
				{IP: host, Port: port, Transport: defaultTransportFor(proto)},
			},
		},
		Frames: []manifest.Frame{{
			Name:  "frame",
			Slots: make([]manifest.Slot, 0, len(bindings)),
		}},
	}
	for _, b := range bindings {
		mf.Frames[0].Slots = append(mf.Frames[0].Slots, manifest.Slot{
			Addr: map[string]any{"slot": b.Slot},
			DM:   b.Identity + ".json",
		})
	}
	path, err := manifest.Write(".cache", mf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: manifest write: %v\n", err)
		return
	}
	fmt.Fprintf(os.Stderr, "manifest written: %s (%d slot(s))\n", path, len(bindings))
}

// defaultTransportFor returns the canonical transport per protocol.
// ACP1 is UDP-by-default; AN2 (ACP2) + Ember+ are TCP.
func defaultTransportFor(proto string) string {
	switch proto {
	case "acp1":
		return "udp"
	}
	return "tcp"
}

// saveAndSplitForEmberplus runs the per-slot DM split for plugins
// that satisfy the dmSplitter contract (emberplus today). The full-
// provider DM is intentionally NOT written here — each slot DM is
// the unit of cache, identity, and reuse per ADR-0022/0023. Other
// protocols stay on the legacy single-DM path via saveSlotCache.
//
// Parameters host/objs are unused — kept on the signature to match
// the saveSlotCache contract so caller plumbing is uniform.
func saveAndSplitForEmberplus(ctx context.Context, plug protocol.Protocol, prober identityProber, host, proto string, slot int, objs []protocol.Object, tree *canonical.Export) {
	// Write the full-provider DM first — keeps the consumer's
	// existing `--dm "<Device>@<SwRev>"` fast path working (matrix /
	// get / set / stream hot-load against one file). Then the
	// per-slot split adds the per-matrix / per-function files
	// alongside so federation tooling can compose by slot. Disk
	// redundancy ~few MB; trade-off explicitly approved.
	saveSlotCache(ctx, prober, host, proto, slot, objs, tree)

	if prober == nil {
		return
	}
	identity, perr := prober.IdentityProbe(ctx, slot)
	if perr != nil || identity == "" {
		return
	}
	port := 0
	if info, ierr := plug.GetDeviceInfo(ctx); ierr == nil {
		port = info.Port
	}
	splitDMByMatrix(ctx, plug, host, port, identity)
}

// saveIdentityCache writes a per-card DM file (#430). DM is
// slot-agnostic, host-agnostic — the schema of ONE card type. Two
// slots holding the same card share the same DM file. Slot, IP, host
// info are runtime state and never persisted into the DM.
//
// When tree is non-nil (Ember+ today; ACP1/ACP2 follow-up) the file
// also carries the canonical.Export shape per ADR-0022. host + slot
// params kept for symmetry with the IP-keyed Save path and future
// logging; they are NOT written to disk.
func saveIdentityCache(store *storage.TreeStore, identity, host, proto string, slot int, objs []protocol.Object, tree *canonical.Export) error {
	_ = host
	_ = slot
	if tree == nil {
		return store.SaveByIdentity(proto, identity, objs)
	}
	// Write BOTH shapes: canonical Root for federation / provider
	// serve, flat Objects for the consumer's --dm hot-load (which
	// uses SeedTreeFromCachedObjects → numIndex by OID). One file,
	// two views, no per-verb walk.
	return store.WriteDM(proto, identity, storage.DM{
		Protocol:  proto,
		Root:      tree.Root,
		Templates: tree.Templates,
		Objects:   objs,
	})
}

// splitDMByMatrix is the canonical-tree split documented in
// internal/emberplus/consumer/dm_split.go. Called from
// walkSlotAndCache after the full provider DM lands; emits one
// additional DM per slot-worthy root child so each matrix / function
// subtree gets its own file under .cache/dm/emberplus/. Also writes
// a manifest at .cache/manifest/<device-slug>.json so a future
// consumer can resolve host:port → slot DMs without re-walking.
// Quiet on success; warns on per-slot save errors.
type dmSplitter interface {
	SplitAndPersistDM(ctx context.Context, store *storage.TreeStore, providerIdentity string) ([]emberplus.SlotRef, error)
}

func splitDMByMatrix(ctx context.Context, plug protocol.Protocol, host string, port int, identity string) {
	if treeStore == nil {
		return
	}
	ds, ok := plug.(dmSplitter)
	if !ok {
		return
	}
	persisted, err := ds.SplitAndPersistDM(ctx, treeStore, identity)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: per-slot DM split: %v\n", err)
	}
	if len(persisted) == 0 {
		return
	}
	names := make([]string, len(persisted))
	for i, r := range persisted {
		names[i] = r.Identity
	}
	fmt.Fprintf(os.Stderr, "DM split: %d slot DM(s) written — %s\n",
		len(persisted), strings.Join(names, ", "))

	deviceName, _ := splitIdentityForManifest(identity)
	mf := &manifest.Manifest{
		Device: manifest.Device{
			Name:     deviceName,
			Protocol: "emberplus",
			Endpoints: []manifest.Endpoint{
				{IP: host, Port: port, Transport: "tcp"},
			},
		},
		Frames: []manifest.Frame{{
			Name:  "router",
			Slots: make([]manifest.Slot, 0, len(persisted)),
		}},
	}
	for _, r := range persisted {
		mf.Frames[0].Slots = append(mf.Frames[0].Slots, manifest.Slot{
			Addr: map[string]any{"oid": r.OID},
			DM:   r.Identity + ".json",
		})
	}
	path, werr := manifest.Write(".cache", mf)
	if werr != nil {
		fmt.Fprintf(os.Stderr, "warning: manifest write: %v\n", werr)
		return
	}
	fmt.Fprintf(os.Stderr, "manifest written: %s\n", path)
}

// splitIdentityForManifest separates "<Model>@<SwRev>" on the last
// '@'. Returns (model, swRev) — model is used as the device name in
// the auto-written manifest.
func splitIdentityForManifest(identity string) (string, string) {
	i := strings.LastIndex(identity, "@")
	if i < 0 {
		return identity, ""
	}
	return identity[:i], identity[i+1:]
}
