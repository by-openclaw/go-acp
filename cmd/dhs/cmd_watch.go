package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"dhs/internal/dmlib"
	"dhs/internal/protocol"
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
		return fmt.Errorf("usage: acp watch <host> [--slot N | --slots 1,3,7 | --slots all] [--no-walk] [--auto-walk-on-plug] [--dm-library <path>] [--group G] [--label L]")
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

	// Load disk cache for instant label/unit resolution while walk runs.
	// Key by watchCacheKey so ACP1 groups that re-use the same object-id
	// space (control / status / alarm / identity / file / frame all
	// addressable as 0..N within one slot) don't collide. (refs #236)
	labelCache := map[string]string{}
	unitCache := map[string]string{}
	if treeStore != nil && *slot >= 0 {
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
			}
			if len(labelCache) > 0 {
				fmt.Fprintf(os.Stderr, "loaded %d labels from cache\n", len(labelCache))
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

			// Parameter event — value column.
			valStr := formatValueInline(ev.Value)
			if unit, ok := unitCache[watchCacheKey(ev.Group, ev.ID)]; ok && unit != "" {
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

func walkSlotAndCache(ctx context.Context, plug protocol.Protocol, host, proto string, slot int) {
	objs, werr := plug.Walk(ctx, slot)
	if werr != nil {
		fmt.Fprintf(os.Stderr, "warning: walk slot %d failed: %v\n", slot, werr)
		return
	}
	if treeStore != nil {
		if serr := treeStore.Save(host, proto, slot, objs); serr != nil {
			fmt.Fprintf(os.Stderr, "warning: cache save slot %d: %v\n", slot, serr)
		}
	}
}
