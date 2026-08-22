package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"

	"dhs/internal/consumer"
)

func runGet(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	fs.Usage = verbUsageFn(fs, helpGet) // #751 G5: -h = rich help + all flags
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", 0, "slot number (default 0)")
	group := fs.String("group", "", "object group (optional when --label is unique across groups)")
	label := fs.String("label", "", "object label (preferred over --id, requires prior walk context)")
	id := fs.Int("id", -1, "object id within group (alternative to --label)")
	objectAlias := fs.Int("object", -1, "alias for --id (deprecated; ACP2 callers historically wrote --object)")
	pathFlag := fs.String("path", "", "tree path: dotted label OR numeric OID (e.g. types.vInteger or 1.6.1)")
	idx := fs.Int("idx", 0, "ACP2 preset idx (0 = ACTIVE INDEX; default)")
	pid := fs.Int("pid", 0, "ACP2 property id to read (0 = default pid=8 value; set to read object_type/label/access/etc.)")
	dmIdentity := fs.String("dm", "", `Ember+ only: identity-keyed DM hot-load (e.g. "Tiny Ember+ Router@1.6.2"). When set, the tree is seeded from .cache/dm/emberplus/<identity>.json and the per-call walk is skipped — refs #438, ADR-0022.`)
	output := fs.String("output", "text", "output format: text | json (ADR-0002; #751 G1)")
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
	if *pathFlag != "" {
		if err := validatePathOrOID(*pathFlag); err != nil {
			return err
		}
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	// Ember+ DM auto-cache (refs #438, ADR-0022): hot-load from cache
	// when --dm hits; otherwise walk + auto-extract by identity so the
	// next call is fast. Only kicks in for path/label resolution; pure
	// --id calls skip this entirely.
	if cf.protocol == "emberplus" && (*pathFlag != "" || *label != "") {
		if err := ensureEmberplusTree(ctx, plug, host, cf.port, *dmIdentity, *slot, false); err != nil {
			return err
		}
	} else if cf.protocol != "emberplus" && (*pathFlag != "" || *label != "") {
		// Seed from the identity-keyed DM cache first (same hot-load as
		// watch); only walk the card live on a cache miss. acp1/acp2 DM
		// caches are identity-keyed (.cache/dm/<proto>/<Card>@<Ver>.json,
		// #353/#363), which the host/slot-keyed resolvePathFromCache never
		// sees — so a targeted IdentityProbe + seed avoids the full
		// per-card walk (~40k objects on a CONVERT Hybrid). GetValue then
		// resolves --path/--label from the seeded tree.
		if !hotLoadIdentityDM(ctx, plug, cf.protocol, *slot) {
			if _, err := plug.Walk(ctx, *slot); err != nil {
				return fmt.Errorf("walk for resolution: %w", err)
			}
		}
	}

	// Per-op timer started AFTER walk/DM-load so the GetValue wait
	// sees a fresh --timeout budget — matches cmd_invoke / cmd_set.
	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	req := consumer.ValueRequest{
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
	var meta *consumer.Object
	if *label != "" {
		meta = findObjectByLabel(plug, *slot, *group, *label)
	}
	if jsonOut, oerr := resolveEnsureOutput(*output, false); oerr != nil {
		return oerr
	} else if jsonOut {
		out := getValueJSON{
			Slot: *slot, Path: *pathFlag, Group: *group, Label: *label,
			Value: formatValue(val, meta),
		}
		if *id >= 0 {
			out.ID = id
		}
		if len(val.Raw) > 0 {
			out.Raw = hex.EncodeToString(val.Raw)
		}
		if meta != nil {
			out.Label = meta.Label
		}
		return emitReadJSON(out)
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

// getValueJSON is the machine shape of one point read (#751 G1c).
// Value carries the same human formatting as text mode (units, enum
// names); Raw is the wire hex when the protocol exposes it.
type getValueJSON struct {
	Slot  int    `json:"slot"`
	Path  string `json:"path,omitempty"`
	Group string `json:"group,omitempty"`
	Label string `json:"label,omitempty"`
	ID    *int   `json:"id,omitempty"`
	Value string `json:"value"`
	Raw   string `json:"raw,omitempty"`
}
