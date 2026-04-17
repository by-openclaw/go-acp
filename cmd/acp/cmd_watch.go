package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"acp/internal/protocol"
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
	labelCache := map[int]string{} // objID → label
	unitCache := map[int]string{}  // objID → unit
	if treeStore != nil && *slot >= 0 {
		if snap, lerr := treeStore.Load(host, *slot); lerr == nil && snap != nil {
			for _, sd := range snap.Slots {
				for _, o := range sd.Objects {
					if o.Label != "" {
						labelCache[o.ID] = o.Label
					}
					if o.Unit != "" {
						unitCache[o.ID] = o.Unit
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
	fmt.Printf("%-8s  %-10s  %-4s  %-20s  %-7s  value\n", "time", "group", "id", "label", "source")
	fmt.Println(strings.Repeat("-", 80))
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-events:
			// Use disk cache label if plugin hasn't resolved it yet.
			label := ev.Label
			src := "live"
			if label == "" {
				if cached, ok := labelCache[ev.ID]; ok {
					label = cached
					src = "cache"
				}
			}
			// Append unit if available.
			valStr := formatValueInline(ev.Value)
			if unit, ok := unitCache[ev.ID]; ok && unit != "" {
				valStr += " " + unit
			}
			fmt.Printf("%s  s%-2d %-7s  %-4d  %-20s  [%-5s]  %s\n",
				ev.Timestamp.Format("15:04:05"),
				ev.Slot,
				ev.Group,
				ev.ID,
				truncate(label, 20),
				src,
				valStr,
			)
		}
	}
}
