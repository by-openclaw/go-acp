package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"acp/internal/export"
	"acp/internal/protocol"
	"acp/internal/protocol/acp1"
	"acp/internal/protocol/acp2"
	"acp/internal/protocol/emberplus"
)

func runWalk(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("walk", flag.ExitOnError)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", -1, "slot number (omit or pass -1 with --all to walk every present slot)")
	all := fs.Bool("all", false, "walk every present slot on the device")
	filter := fs.String("filter", "", "case-insensitive filter on output lines (like findstr /i or grep -i)")
	pathFlag := fs.String("path", "", "filter objects by path prefix (e.g. BOARD, PSU/1)")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: acp walk <host> (--slot N | --all)")
	}
	_ = fs.Parse(rest)
	// Ember+ has no slot concept (spec: single flat tree per provider);
	// default --slot 0 so the user doesn't have to remember this quirk.
	if cf.protocol == "emberplus" && *slot < 0 && !*all {
		*slot = 0
	}
	if !*all && *slot < 0 {
		return fmt.Errorf("--slot N or --all is required")
	}

	// Parse --path into segments for prefix matching.
	var pathSegs []string
	if *pathFlag != "" {
		pathSegs = strings.Split(*pathFlag, ".")
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	// Stream objects as they're discovered during walk — don't wait for
	// the full tree before printing. Essential for large slots (4190+ objects).
	// ACP1 doesn't support streaming, so we fall back to printSlotTree after.
	streaming := false
	filterLower := strings.ToLower(*filter)
	if p, ok := plug.(interface{ SetWalkProgress(acp2.WalkProgressFunc) }); ok {
		streaming = true
		p.SetWalkProgress(func(count int, obj *protocol.Object) {
			if obj.Kind == protocol.KindRaw && obj.Label == "" {
				return // skip node containers
			}
			if !matchPathPrefix(obj.Path, pathSegs) {
				return
			}
			valStr := walkValueColumn(*obj)
			rngStr := walkRangeColumn(*obj)
			line := fmt.Sprintf("  %3d  %-20s  %-6s  %-3s  %-18s  %s",
				obj.ID,
				truncate(obj.Label, 20),
				kindName(obj.Kind),
				accessStr(obj.Access),
				truncate(valStr, 18),
				rngStr)
			if *filter != "" && !strings.Contains(strings.ToLower(line), filterLower) {
				return
			}
			fmt.Println(line)
		})
	}

	// Walk uses the signal-only context (no timeout). A tree walk takes
	// as long as it takes — 214 objects on slot 0 is ~2s, 4190 objects
	// on slot 1 can be minutes. Ctrl-C is the only interrupt.
	// Short-timeout opCtx is only for device info / slot info queries.
	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	// --all: read frame status, iterate every present slot, walk each.
	// Partial failure on one slot does not abort the rest; we print the
	// error and keep going, since a mid-walk card removal is a normal
	// operational event, not a fatal error.
	if *all {
		info, err := plug.GetDeviceInfo(opCtx)
		if err != nil {
			return fmt.Errorf("device info: %w", err)
		}
		fmt.Printf("device %s:%d — %d slots\n", info.IP, info.Port, info.NumSlots)
		walked := 0
		for s := 0; s < info.NumSlots; s++ {
			si, serr := plug.GetSlotInfo(opCtx, s)
			if serr != nil {
				fmt.Printf("\nslot %d — error reading status: %v\n", s, serr)
				continue
			}
			if si.Status != protocol.SlotPresent {
				continue
			}
			walked++
			fmt.Printf("\nslot %d:\n", s)
			objs, werr := plug.Walk(ctx, s)
			if werr != nil {
				fmt.Printf("\nslot %d — walk error: %v\n", s, werr)
				continue
			}
			// Save walked tree to disk for instant label resolution next time.
			if treeStore != nil {
				if serr := treeStore.Save(host, cf.protocol, s, objs); serr != nil {
					fmt.Fprintf(os.Stderr, "warning: cache save slot %d: %v\n", s, serr)
				}
			}
			objs = filterByPath(objs, pathSegs)
			if !streaming {
				printSlotTree(s, objs, *filter)
			} else {
				fmt.Printf("\nslot %d — %d objects\n", s, len(objs))
			}
		}
		fmt.Printf("\nwalked %d present slot(s)\n", walked)
		return nil
	}

	fmt.Printf("\nslot %d:\n", *slot)
	objs, err := plug.Walk(ctx, *slot)
	if err != nil {
		return err
	}
	// Save walked tree to disk for instant label resolution next time.
	if treeStore != nil {
		if serr := treeStore.Save(host, cf.protocol, *slot, objs); serr != nil {
			fmt.Fprintf(os.Stderr, "warning: cache save slot %d: %v\n", *slot, serr)
		}
	}
	objs = filterByPath(objs, pathSegs)
	if !streaming {
		printSlotTree(*slot, objs, *filter)
	} else {
		fmt.Printf("\nslot %d — %d objects\n", *slot, len(objs))
	}

	// Capture-dir mode: write tree.json (canonical export) alongside
	// the raw.jsonl frame log the recorder produced. Ember+ adds a
	// glow.json intermediate; ACP1/ACP2 don't need one (they have no
	// Glow-equivalent lossless decoded representation).
	if cf.captureDir != "" {
		if err := writeCanonicalCapture(ctx, cf.captureDir, plug, cf); err != nil {
			fmt.Fprintf(os.Stderr, "warning: capture dir: %v\n", err)
		}
	}
	return nil
}

// writeCanonicalCapture dispatches to the right per-plugin capture
// writer based on the concrete plugin type. Each plugin produces a
// canonical `tree.json` in the same schema; Ember+ additionally
// writes `glow.json` for wire-level cross-checking.
func writeCanonicalCapture(ctx context.Context, dir string, plug protocol.Protocol, cf *commonFlags) error {
	switch p := plug.(type) {
	case *emberplus.Plugin:
		return writeEmberplusCapture(ctx, dir, p, cf)
	case *acp1.Plugin:
		return writeACP1Capture(ctx, dir, p)
	case *acp2.Plugin:
		return writeACP2Capture(ctx, dir, p)
	}
	return nil
}

// writeACP2Capture writes `tree.json` in canonical shape for an ACP2
// device. Mirrors writeACP1Capture: the raw AN2 frame log (recorder)
// is appended alongside by the transport layer; this function covers
// only the decoded canonical export.
func writeACP2Capture(ctx context.Context, dir string, p *acp2.Plugin) error {
	tree, err := p.Canonicalize(ctx)
	if err != nil {
		return fmt.Errorf("canonicalize: %w", err)
	}
	f, err := os.Create(filepath.Join(dir, "tree.json"))
	if err != nil {
		return fmt.Errorf("create tree.json: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := export.WriteCanonicalJSON(ctx, f, tree); err != nil {
		return fmt.Errorf("write tree.json: %w", err)
	}
	fmt.Printf("capture: wrote tree.json to %s\n", dir)
	return nil
}

// writeACP1Capture writes `tree.json` in canonical shape for an ACP1
// device. The raw frame log (recorder) is already appended alongside
// by the transport layer; this function covers only the decoded
// canonical export.
func writeACP1Capture(ctx context.Context, dir string, p *acp1.Plugin) error {
	tree, err := p.Canonicalize(ctx)
	if err != nil {
		return fmt.Errorf("canonicalize: %w", err)
	}
	f, err := os.Create(filepath.Join(dir, "tree.json"))
	if err != nil {
		return fmt.Errorf("create tree.json: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := export.WriteCanonicalJSON(ctx, f, tree); err != nil {
		return fmt.Errorf("write tree.json: %w", err)
	}
	fmt.Printf("capture: wrote tree.json to %s\n", dir)
	return nil
}

// writeEmberplusCapture dumps glow.json (lossless decoded Glow tree)
// and tree.json (canonical Export) into dir alongside the raw frame
// log. No-op when plug isn't the Ember+ plugin. Mode flags on cf
// select the canonical resolver contract: pointer (default, wire-
// faithful), inline (absorb referenced subtrees), or both.
func writeEmberplusCapture(ctx context.Context, dir string, plug protocol.Protocol, cf *commonFlags) error {
	ep, ok := plug.(*emberplus.Plugin)
	if !ok {
		return nil
	}

	dump, err := ep.GlowSnapshot(ctx)
	if err != nil {
		return fmt.Errorf("glow snapshot: %w", err)
	}
	if err := writeJSONFile(filepath.Join(dir, "glow.json"), dump); err != nil {
		return fmt.Errorf("write glow.json: %w", err)
	}

	tree, err := ep.Canonicalize(ctx, emberplus.CanonicalOptions{
		Templates: cf.canonTemplates,
		Labels:    cf.canonLabels,
		Gain:      cf.canonGain,
	})
	if err != nil {
		return fmt.Errorf("canonicalize: %w", err)
	}
	f, err := os.Create(filepath.Join(dir, "tree.json"))
	if err != nil {
		return fmt.Errorf("create tree.json: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := export.WriteCanonicalJSON(ctx, f, tree); err != nil {
		return fmt.Errorf("write tree.json: %w", err)
	}
	fmt.Printf("capture: wrote glow.json + tree.json to %s\n", dir)
	return nil
}

// writeJSONFile marshals v as indented JSON to path, atomically-ish
// (write-then-rename would be safer but Windows locking makes that
// tricky; plain create+close is fine for capture files).
func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

