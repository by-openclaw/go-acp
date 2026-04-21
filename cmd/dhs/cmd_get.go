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
	pathFlag := fs.String("path", "", "dot-separated tree path (e.g. router.oneToN.parameters.sourceGain)")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: acp get <host> --slot N (--path P | --label L | --id I)")
	}
	_ = fs.Parse(rest)
	// Ember+ has no slot concept; default to 0 so users don't have to pass it.
	if cf.protocol == "emberplus" && *slot < 0 {
		*slot = 0
	}
	if *slot < 0 {
		return fmt.Errorf("--slot is required")
	}
	if *pathFlag == "" && *label == "" && *id < 0 {
		return fmt.Errorf("either --path, --label, or --id is required")
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	// Path-based addressing: walk first, then lookup by path key.
	// Label-based: resolve from cache or walk.
	if *pathFlag != "" || *label != "" {
		// Resolution walk uses the raw signal-only ctx, not opCtx.
		// A tree walk takes as long as it takes (44k objects on ACP2
		// slot 1 needs minutes); --timeout only bounds the single
		// GetValue below.
		if _, err := plug.Walk(ctx, *slot); err != nil {
			return fmt.Errorf("walk for resolution: %w", err)
		}
	}
	if *label != "" && *id < 0 {
		if cachedID := resolveLabelFromCache(host, cf.protocol, *slot, *group, *label); cachedID >= 0 {
			*id = cachedID
		}
	}

	req := protocol.ValueRequest{
		Slot:  *slot,
		Path:  *pathFlag,
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
