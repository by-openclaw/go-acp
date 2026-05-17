package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"dhs/internal/consumer"
)

// runTree implements the standalone `tree` verb (R5b #469). It renders
// the device's object tree in one of two formats:
//
//   - ascii    — same shape as `walk --tree` (default)
//   - plantuml — `@startmindmap`/`@endmindmap` ready for `plantuml.jar`
//
// Both formats reuse the shared `pathNode` tree built from
// `consumer.Object.Path` slices, so any protocol that exposes the
// canonical Object surface (ACP1, ACP2, Ember+, Probel SW-P-08, …)
// renders without per-protocol code.
//
// With `--dm <identity>`, the renderer hot-loads the cached DM from
// disk and emits no wire traffic at all — operators can browse a
// previously-captured tree offline (CI artifact review, post-mortem,
// docs generation).
func runTree(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("tree", flag.ExitOnError)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", 0, "slot number (default 0)")
	format := fs.String("format", "ascii", `output format: "ascii" (default) or "plantuml"`)
	pathFlag := fs.String("path", "", "focus subtree at this dotted label (mutually exclusive with --oid)")
	oid := fs.String("oid", "", "focus subtree at this numeric OID (mutually exclusive with --path)")
	depth := fs.Int("depth", 0, "max render depth from the focus node (0 = unlimited)")
	out := fs.String("out", "", "write to this file instead of stdout")
	filter := fs.String("filter", "", "case-insensitive substring filter (drops non-matching lines)")
	dmIdentity := fs.String("dm", "", "Ember+ DM identity for offline hot-load (e.g. \"dhs-emberplus-integration@1.0.0\"). Skips wire walk; no host connection required.")
	noWalk := fs.Bool("no-walk", false, "Ember+ only: fail fast on cache miss instead of falling back to a wire walk")

	host, rest, err := popHost(args)
	dmOnly := false
	if err != nil {
		// Tolerate missing host when --dm is set — offline rendering needs no host.
		// Parse the rest unchanged so --dm/--format/etc still bind.
		_ = fs.Parse(args)
		if *dmIdentity == "" {
			return fmt.Errorf("usage: dhs consumer <proto> tree <host> [--port N] [--format ascii|plantuml] [--path P | --oid O] [--depth N] [--out FILE] [--dm IDENTITY [--no-walk]]")
		}
		dmOnly = true
	} else {
		_ = fs.Parse(rest)
	}

	if *format != "ascii" && *format != "plantuml" {
		return fmt.Errorf("%w: --format must be \"ascii\" or \"plantuml\", got %q",
			consumer.ErrInvalidFormat, *format)
	}
	if *pathFlag != "" && *oid != "" {
		return fmt.Errorf("--path and --oid are mutually exclusive")
	}
	// Path/OID syntax is validated implicitly at the resolver layer
	// (resolveFromPath / resolveFromOID return "no object at ..." for
	// anything that doesn't match a known shape). R21 #486's
	// validation:invalid-oid surface lands once that PR merges and
	// the validator graduates to a shared package.

	// Resolve writer: stdout or --out file. Open the file before any
	// wire traffic so a bad path fails fast.
	var writer io.Writer = os.Stdout
	if *out != "" {
		f, ferr := os.Create(*out)
		if ferr != nil {
			return fmt.Errorf("open %s: %w", *out, ferr)
		}
		defer func() { _ = f.Close() }()
		writer = f
	}

	var objs []consumer.Object
	switch {
	case dmOnly || (*dmIdentity != "" && host == ""):
		// Pure offline path — read the identity-keyed DM straight off
		// disk. No connection, no walk, no host required.
		o, lerr := loadDMObjects(cf.protocol, *dmIdentity, *slot)
		if lerr != nil {
			return lerr
		}
		objs = o
	default:
		plug, cleanup, perr := connect(ctx, host, cf)
		if perr != nil {
			return perr
		}
		defer cleanup()
		// Ember+ DM auto-cache: hot-load when --dm hits, else walk +
		// auto-extract. Non-Ember+ protocols walk every time.
		if cf.protocol == "emberplus" {
			if err := ensureEmberplusTree(ctx, plug, host, cf.port, *dmIdentity, *slot, *noWalk); err != nil {
				return err
			}
		}
		opCtx, cancel := withTimeout(ctx, cf.timeout)
		defer cancel()
		o, werr := plug.Walk(opCtx, *slot)
		if werr != nil {
			return werr
		}
		objs = o
	}

	opts := treeRenderOpts{
		FromOID:  *oid,
		FromPath: *pathFlag,
		Depth:    *depth,
		ASCII:    *format == "ascii",
		Filter:   *filter,
	}
	switch *format {
	case "ascii":
		return renderTree(writer, objs, opts)
	case "plantuml":
		return renderTreePlantUML(writer, objs, opts)
	}
	return nil
}

// loadDMObjects reads the cached DM identified by <proto>+<identity>
// (per ADR-0022) and returns the slot's Object list. Returns
// plugin:object-not-found when the cache file is missing — operators
// know to drop --no-walk or run a full walk first.
func loadDMObjects(proto, identity string, slot int) ([]consumer.Object, error) {
	if treeStore == nil {
		return nil, fmt.Errorf("treeStore not initialised — cannot offline-render --dm %q", identity)
	}
	snap, err := treeStore.LoadByIdentity(proto, identity)
	if err != nil {
		return nil, fmt.Errorf("%w: cached DM %q not present (%v)",
			consumer.ErrObjectNotFound, identity, err)
	}
	if snap == nil || len(snap.Slots) == 0 {
		return nil, fmt.Errorf("%w: cached DM %q has no slots",
			consumer.ErrObjectNotFound, identity)
	}
	for _, sd := range snap.Slots {
		if sd.Slot == slot {
			return sd.Objects, nil
		}
	}
	// Fallback — DMs that don't carry a slot field land in slot 0; use
	// whichever slot the snapshot does carry.
	return snap.Slots[0].Objects, nil
}

// helpTree documents the verb. Registered in main.go's command table.
func helpTree() {
	fmt.Println(`dhs consumer <proto> tree <host> [flags]   render device object tree

Reuses the canonical pathNode infrastructure so every protocol (ACP1,
ACP2, Ember+, Probel SW-P-08) renders without per-protocol code.

Flags:
  --format ascii|plantuml   output format (default: ascii)
  --path <P>                focus subtree at this dotted label
  --oid <OID>               focus subtree at this numeric OID
  --depth N                 max depth from the focus node (0 = unlimited)
  --out <file>              write to file instead of stdout
  --filter <S>              case-insensitive substring filter
  --dm <identity>           Ember+ only — hot-load cached DM, skip wire walk
  --no-walk                 fail fast on cache miss (Ember+ only)

Examples:
  # 2-level ASCII tree, default format
  dhs consumer emberplus tree 127.0.0.1 --port 9100 --depth 2

  # PlantUML mindmap to file
  dhs consumer emberplus tree 127.0.0.1 --port 9100 --format plantuml --out demo.puml

  # Offline render of a cached DM (no host needed)
  dhs consumer emberplus tree --dm dhs-emberplus-integration@1.0.0 --format plantuml

R5b #469.`)
}
