package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"strings"

	"dhs/internal/consumer"
)

// orFirst returns the first non-empty argument, or "" if all are empty.
// Used to format human-friendly error messages without nested ternaries.
func orFirst(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

func runSet(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("set", flag.ExitOnError)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", 0, "slot number (default 0)")
	group := fs.String("group", "", "object group name")
	label := fs.String("label", "", "object label")
	id := fs.Int("id", -1, "object id within group")
	objectAlias := fs.Int("object", -1, "alias for --id (deprecated; ACP2 callers historically wrote --object)")
	pathFlag := fs.String("path", "", "tree path: dotted label OR numeric OID (e.g. types.vInteger or 1.6.1)")
	valueStr := fs.String("value", "", "typed value (e.g. -3.0, \"On\", \"192.168.1.5\", \"CH1\"); empty string is valid for string objects")
	valueHex := fs.String("raw", "", "raw wire bytes as hex — escape hatch bypassing typed encoding")
	noWalk := fs.Bool("no-walk", false, "fail fast on cache miss instead of walking the slot to resolve --path/--label")
	round := fs.Bool("round", false, "R16 #483: snap an off-step numeric --value to the nearest legal step instead of erroring with validation:step-misaligned (Ember+ only — non-numeric Parameters return validation:round-not-applicable)")
	dmIdentity := fs.String("dm", "", `Ember+ only: identity-keyed DM hot-load (e.g. "Tiny Ember+ Router@1.6.2"). When set, the tree is seeded from .cache/dm/emberplus/<identity>.json and the walk is skipped — refs #438, ADR-0022.`)
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer <proto> set <host> --slot N (--path P | --label L | --id I) (--value <v> | --raw <hex>)")
	}
	_ = fs.Parse(rest)
	// Detect whether --value / --raw were explicitly passed (even if
	// empty). fs.Visit walks only flags the user actually supplied, so
	// `--value ""` is distinguishable from "--value omitted" — needed
	// to clear string objects back to empty.
	valueSet := false
	rawSet := false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "value":
			valueSet = true
		case "raw":
			rawSet = true
		}
	})
	// --object is a deprecated alias for --id. If the user supplied
	// --object and not --id, fold it in. If both are set with different
	// values, reject — the caller's intent is ambiguous.
	if *objectAlias >= 0 {
		if *id >= 0 && *id != *objectAlias {
			return fmt.Errorf("--id and --object both set with different values (%d vs %d)", *id, *objectAlias)
		}
		*id = *objectAlias
	}
	if !valueSet && !rawSet {
		return fmt.Errorf("either --value or --raw is required")
	}
	if *pathFlag == "" && *label == "" && *id < 0 {
		return fmt.Errorf("either --path, --label, or --id is required")
	}
	if *pathFlag != "" {
		if err := validatePathOrOID(*pathFlag); err != nil {
			return err
		}
	}

	var val consumer.Value
	if *valueHex != "" {
		raw, herr := hex.DecodeString(strings.TrimPrefix(*valueHex, "0x"))
		if herr != nil {
			return fmt.Errorf("--raw: %w", herr)
		}
		val = consumer.Value{Kind: consumer.KindRaw, Raw: raw}
	} else {
		// Typed value: stash the user's string and let EncodeValueBytes
		// coerce it to the right wire form based on the object's kind.
		val = consumer.Value{Str: *valueStr}
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	// Ember+: DM auto-cache (refs #438, ADR-0022) — cache hit hot-loads;
	// cache miss walks + auto-extracts under identity so the next call
	// is fast. Non-Ember+: IP-keyed on-disk cache resolution, falling
	// through to a Walk on miss.
	//
	// NOTE: opCtx timer is started AFTER the walk so the walk doesn't
	// burn through --timeout before SetValue even sends its frame —
	// same pattern as cmd_invoke.go.
	resolvedFromCache := false
	if cf.protocol == "emberplus" {
		if err := ensureEmberplusTree(ctx, plug, host, cf.port, *dmIdentity, *slot, *noWalk); err != nil {
			return err
		}
		resolvedFromCache = true
	} else if *id < 0 {
		if *pathFlag != "" {
			if cachedID := resolvePathFromCache(host, cf.protocol, *slot, *pathFlag); cachedID >= 0 {
				*id = cachedID
				resolvedFromCache = true
			}
		}
		if !resolvedFromCache && *label != "" {
			if cachedID := resolveLabelFromCache(host, cf.protocol, *slot, *group, *label); cachedID >= 0 {
				*id = cachedID
				resolvedFromCache = true
			}
		}
	}

	if cf.protocol != "emberplus" && !resolvedFromCache && (*pathFlag != "" || *label != "") {
		if *noWalk {
			return fmt.Errorf("--no-walk: %q not found in cache for slot %d (run 'walk --slot %d' first or drop --no-walk)",
				orFirst(*pathFlag, *label), *slot, *slot)
		}
		// Cache miss on non-Ember+ — walk the slot to populate.
		if _, err := plug.Walk(ctx, *slot); err != nil {
			return fmt.Errorf("walk for resolution: %w", err)
		}
	}

	// Start the per-op timer NOW — after any walk / DM-load has
	// finished — so the SetValue wait-for-confirm sees a fresh
	// --timeout budget instead of the leftovers from an exhausted
	// walk.
	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	req := consumer.ValueRequest{
		Slot:  *slot,
		Path:  *pathFlag,
		Group: *group,
		Label: *label,
		ID:    *id,
		Round: *round,
	}
	// Client-side pre-flight for typed values: coerce --value to the object's
	// kind and validate it before the wire write, so a bad enum / wrong type /
	// read-only target fails as exit 2 (validation) instead of exit 1 (wire) —
	// matching `ensure`. Out-of-range numerics are intentionally NOT rejected
	// (the device clamps them). --raw bypasses this; the caller owns the bytes.
	meta := findObjectMeta(plug, *slot, *group, *label, *id)
	if *valueHex == "" && meta != nil {
		want, cerr := coerceDesired(meta.Kind, *valueStr)
		if cerr != nil {
			return cerr
		}
		if v, ok := plug.(consumer.ValueValidator); ok {
			if verr := v.ValidateValue(opCtx, req, want); verr != nil {
				return verr
			}
		}
		val = want
	}

	confirmed, err := plug.SetValue(opCtx, req, val)
	if err != nil {
		return err
	}
	fmt.Println("confirmed " + formatValue(confirmed, meta))
	if len(confirmed.Raw) > 0 {
		fmt.Printf("raw       = %s\n", hex.EncodeToString(confirmed.Raw))
	}
	return nil
}
