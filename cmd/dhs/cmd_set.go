package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"strings"

	"dhs/internal/protocol"
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
	slot := fs.Int("slot", -1, "slot number (required)")
	group := fs.String("group", "", "object group name")
	label := fs.String("label", "", "object label")
	id := fs.Int("id", -1, "object id within group")
	pathFlag := fs.String("path", "", "dot-separated tree path (e.g. router.oneToN.parameters.sourceGain)")
	valueStr := fs.String("value", "", "typed value (e.g. -3.0, \"On\", \"192.168.1.5\", \"CH1\"); empty string is valid for string objects")
	valueHex := fs.String("raw", "", "raw wire bytes as hex — escape hatch bypassing typed encoding")
	noWalk := fs.Bool("no-walk", false, "fail fast on cache miss instead of walking the slot to resolve --path/--label")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: acp set <host> --slot N (--path P | --label L | --id I) --value <v>")
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
	// Ember+ has no slot concept; default to 0.
	if cf.protocol == "emberplus" && *slot < 0 {
		*slot = 0
	}
	if *slot < 0 {
		return fmt.Errorf("--slot is required")
	}
	if !valueSet && !rawSet {
		return fmt.Errorf("either --value or --raw is required")
	}
	if *pathFlag == "" && *label == "" && *id < 0 {
		return fmt.Errorf("either --path, --label, or --id is required")
	}

	var val protocol.Value
	if *valueHex != "" {
		raw, herr := hex.DecodeString(strings.TrimPrefix(*valueHex, "0x"))
		if herr != nil {
			return fmt.Errorf("--raw: %w", herr)
		}
		val = protocol.Value{Kind: protocol.KindRaw, Raw: raw}
	} else {
		// Typed value: stash the user's string and let EncodeValueBytes
		// coerce it to the right wire form based on the object's kind.
		val = protocol.Value{Str: *valueStr}
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	// Resolve --path / --label via the on-disk cache first. Falling
	// through to plug.Walk costs minutes on a 49 000-object Neuron
	// tree; populated cache resolves in microseconds. Only walk on
	// genuine cache miss (and only when the user hasn't asked us to
	// fail fast via --no-walk).
	resolvedFromCache := false
	if *id < 0 {
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

	if !resolvedFromCache && (*pathFlag != "" || *label != "") {
		if *noWalk {
			return fmt.Errorf("--no-walk: %q not found in cache for slot %d (run 'walk --slot %d' first or drop --no-walk)",
				orFirst(*pathFlag, *label), *slot, *slot)
		}
		// Cache miss: walk the slot to populate before SetValue. Uses
		// raw ctx (signal-only) so big trees don't fail their own
		// resolution; only the SetValue below is bounded by --timeout.
		if _, err := plug.Walk(ctx, *slot); err != nil {
			return fmt.Errorf("walk for resolution: %w", err)
		}
	}

	req := protocol.ValueRequest{
		Slot:  *slot,
		Path:  *pathFlag,
		Group: *group,
		Label: *label,
		ID:    *id,
	}
	confirmed, err := plug.SetValue(opCtx, req, val)
	if err != nil {
		return err
	}
	var meta *protocol.Object
	if *label != "" {
		meta = findObjectByLabel(plug, *slot, *group, *label)
	}
	fmt.Println("confirmed " + formatValue(confirmed, meta))
	if len(confirmed.Raw) > 0 {
		fmt.Printf("raw       = %s\n", hex.EncodeToString(confirmed.Raw))
	}
	return nil
}
