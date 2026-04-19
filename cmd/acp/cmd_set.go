package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"strings"

	"acp/internal/protocol"
)

func runSet(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("set", flag.ExitOnError)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", -1, "slot number (required)")
	group := fs.String("group", "", "object group name")
	label := fs.String("label", "", "object label")
	id := fs.Int("id", -1, "object id within group")
	pathFlag := fs.String("path", "", "dot-separated tree path (e.g. router.oneToN.parameters.sourceGain)")
	valueStr := fs.String("value", "", "typed value (e.g. -3.0, \"On\", \"192.168.1.5\", \"CH1\")")
	valueHex := fs.String("raw", "", "raw wire bytes as hex — escape hatch bypassing typed encoding")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: acp set <host> --slot N (--path P | --label L | --id I) --value <v>")
	}
	_ = fs.Parse(rest)
	// Ember+ has no slot concept; default to 0.
	if cf.protocol == "emberplus" && *slot < 0 {
		*slot = 0
	}
	if *slot < 0 {
		return fmt.Errorf("--slot is required")
	}
	if *valueStr == "" && *valueHex == "" {
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

	// Path or label addressing: walk to populate tree.
	if *pathFlag != "" || *label != "" {
		if _, err := plug.Walk(opCtx, *slot); err != nil {
			return fmt.Errorf("walk for resolution: %w", err)
		}
	} else if *label != "" && *id < 0 {
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
