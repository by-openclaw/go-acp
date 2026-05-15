package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"

	"dhs/internal/protocol"
)

func runGet(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", 0, "slot number (default 0)")
	group := fs.String("group", "", "object group (optional when --label is unique across groups)")
	label := fs.String("label", "", "object label (preferred over --id, requires prior walk context)")
	id := fs.Int("id", -1, "object id within group (alternative to --label)")
	objectAlias := fs.Int("object", -1, "alias for --id (deprecated; ACP2 callers historically wrote --object)")
	pathFlag := fs.String("path", "", "dot-separated tree path (e.g. router.oneToN.parameters.sourceGain)")
	idx := fs.Int("idx", 0, "ACP2 preset idx (0 = ACTIVE INDEX; default)")
	pid := fs.Int("pid", 0, "ACP2 property id to read (0 = default pid=8 value; set to read object_type/label/access/etc.)")
	dmIdentity := fs.String("dm", "", `Ember+ only: identity-keyed DM hot-load (e.g. "Tiny Ember+ Router@1.6.2"). When set, the tree is seeded from .cache/dm/emberplus/<identity>.json and the per-call walk is skipped — refs #438, ADR-0022.`)
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer <proto> get <host> --slot N (--path P | --label L | --id I) [--capture out.jsonl]")
	}
	_ = fs.Parse(rest)
	// --object is a deprecated alias for --id. Fold it in unless both
	// were set with conflicting values.
	if *objectAlias >= 0 {
		if *id >= 0 && *id != *objectAlias {
			return fmt.Errorf("--id and --object both set with different values (%d vs %d)", *id, *objectAlias)
		}
		*id = *objectAlias
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

	// Ember+ DM hot-load (refs #438): seed the in-RAM tree from
	// .cache/dm/emberplus/<identity>.json so GetValue's ensureWalked
	// sees a populated tree and skips the per-call wire walk. No-op
	// for non-Ember+ protocols or when --dm is unset.
	dmSeeded, err := hotLoadEmberplusDM(plug, *dmIdentity, *slot, false)
	if err != nil {
		return err
	}

	// Path-based addressing: walk first, then lookup by path key.
	// Label-based: resolve from cache or walk. Skip entirely when the
	// DM has already seeded the tree (Ember+ hot-load path).
	if !dmSeeded && (*pathFlag != "" || *label != "") {
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
		Idx:   *idx,
		PID:   *pid,
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
