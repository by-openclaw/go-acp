package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"dhs/internal/protocol"
)

// runWatch subscribes to live announcements and prints each event as it
// arrives. Blocks until Ctrl-C. Filters:
//
//	--slot N        only this slot (default: any)
//	--group G       only this group (default: any)
//	--label L       only this object (requires prior walk for resolution)
//	--id I          only this object id within --group
//
// Typical usage: leave filters off and watch everything on the device.
// Useful when debugging an emulator or verifying that a UI change
// reaches the wire.
func runWatch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", -1, "slot filter (-1 = any)")
	group := fs.String("group", "", "group filter (empty = any)")
	label := fs.String("label", "", "label filter (requires prior walk)")
	id := fs.Int("id", -1, "object id filter (-1 = any)")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: acp watch <host> [--slot N] [--group G] [--label L]")
	}
	_ = fs.Parse(rest)

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
	// immediately — labels resolve as the tree fills. ACP1 walks are fast
	// enough to block; ACP2 slot 1 has 44k objects so must be async.
	go func() {
		if *slot >= 0 {
			objs, werr := plug.Walk(ctx, *slot)
			if werr != nil {
				fmt.Fprintf(os.Stderr, "warning: walk slot %d failed: %v\n", *slot, werr)
			} else if treeStore != nil {
				if serr := treeStore.Save(host, cf.protocol, *slot, objs); serr != nil {
					fmt.Fprintf(os.Stderr, "warning: cache save slot %d: %v\n", *slot, serr)
				}
			}
		} else {
			info, ierr := plug.GetDeviceInfo(ctx)
			if ierr == nil {
				for s := 0; s < info.NumSlots; s++ {
					si, serr := plug.GetSlotInfo(ctx, s)
					if serr != nil || si.Status != protocol.SlotPresent {
						continue
					}
					objs, werr := plug.Walk(ctx, s)
					if werr == nil && treeStore != nil {
						_ = treeStore.Save(host, cf.protocol, s, objs)
					}
				}
			}
		}
	}()

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
