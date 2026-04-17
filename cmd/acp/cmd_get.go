package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"

	"acp/internal/protocol"
)

func runGet(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", -1, "slot number (required)")
	group := fs.String("group", "", "object group (optional when --label is unique across groups)")
	label := fs.String("label", "", "object label (preferred over --id, requires prior walk context)")
	id := fs.Int("id", -1, "object id within group (alternative to --label)")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: acp get <host> --slot N --group G (--label L | --id I)")
	}
	_ = fs.Parse(rest)
	if *slot < 0 {
		return fmt.Errorf("--slot is required")
	}
	if *label == "" && *id < 0 {
		return fmt.Errorf("either --label or --id is required")
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	// If addressing by label, run a walk first to populate the plugin's
	// label map. Explicit --id addressing skips the walk.
	if *label != "" {
		if _, err := plug.Walk(opCtx, *slot); err != nil {
			return fmt.Errorf("walk for label resolution: %w", err)
		}
	}

	req := protocol.ValueRequest{
		Slot:  *slot,
		Group: *group,
		Label: *label,
		ID:    *id,
	}
	val, err := plug.GetValue(opCtx, req)
	if err != nil {
		return err
	}
	// Look up the object metadata (range, step, unit) before formatting
	// so we can apply unit suffixes and step-based float precision.
	var meta *protocol.Object
	if *label != "" {
		meta = findObjectByLabel(plug, *slot, *group, *label)
	}
	fmt.Println(formatValue(val, meta))
	if len(val.Raw) > 0 {
		fmt.Printf("raw  = %s\n", hex.EncodeToString(val.Raw))
	}
	if meta != nil {
		printObjectMeta(*meta)
	}
	return nil
}
