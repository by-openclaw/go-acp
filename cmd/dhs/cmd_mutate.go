package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"

	"dhs/internal/consumer"
)

// acp1Mutator is the ACP1-specific no-argument mutating surface (spec methods
// 2/3/4). The plugin implements it; the cross-protocol consumer.Protocol
// interface does not, so the verb type-asserts.
type acp1Mutator interface {
	SetIncValue(ctx context.Context, req consumer.ValueRequest) (consumer.Value, error)
	SetDecValue(ctx context.Context, req consumer.ValueRequest) (consumer.Value, error)
	SetDefValue(ctx context.Context, req consumer.ValueRequest) (consumer.Value, error)
}

func runInc(ctx context.Context, args []string) error   { return runNoArgMutate(ctx, "inc", args) }
func runDec(ctx context.Context, args []string) error   { return runNoArgMutate(ctx, "dec", args) }
func runReset(ctx context.Context, args []string) error { return runNoArgMutate(ctx, "reset", args) }

// runNoArgMutate implements the inc / dec / reset verbs. They resolve an
// object the same way `set` does (cache → walk on miss), then send the
// matching no-argument ACP1 method and print the device-confirmed value.
func runNoArgMutate(ctx context.Context, verb string, args []string) error {
	fs := flag.NewFlagSet(verb, flag.ExitOnError)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", 0, "slot number (default 0)")
	group := fs.String("group", "", "object group name")
	label := fs.String("label", "", "object label")
	id := fs.Int("id", -1, "object id within group")
	pathFlag := fs.String("path", "", "tree path: dotted label OR numeric OID")
	noWalk := fs.Bool("no-walk", false, "fail fast on cache miss instead of walking the slot")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer acp1 %s <host> --slot N (--path P | --label L | --id I)", verb)
	}
	_ = parseVerbFlags(fs, rest)
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

	mut, ok := plug.(acp1Mutator)
	if !ok {
		return fmt.Errorf("%s: protocol %q does not support no-argument mutations (ACP1 only)", verb, cf.protocol)
	}

	// Resolve --path/--label against the on-disk cache, else walk the slot.
	resolved := false
	if *id < 0 {
		if *pathFlag != "" {
			if cachedID := resolvePathFromCache(host, cf.protocol, *slot, *pathFlag); cachedID >= 0 {
				*id = cachedID
				resolved = true
			}
		}
		if !resolved && *label != "" {
			if cachedID := resolveLabelFromCache(host, cf.protocol, *slot, *group, *label); cachedID >= 0 {
				*id = cachedID
				resolved = true
			}
		}
	}
	if !resolved && (*pathFlag != "" || *label != "") {
		if *noWalk {
			return fmt.Errorf("--no-walk: %q not in cache for slot %d (walk first or drop --no-walk)",
				orFirst(*pathFlag, *label), *slot)
		}
		if _, err := plug.Walk(ctx, *slot); err != nil {
			return fmt.Errorf("walk for resolution: %w", err)
		}
	}

	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	req := consumer.ValueRequest{Slot: *slot, Path: *pathFlag, Group: *group, Label: *label, ID: *id}

	var confirmed consumer.Value
	switch verb {
	case "inc":
		confirmed, err = mut.SetIncValue(opCtx, req)
	case "dec":
		confirmed, err = mut.SetDecValue(opCtx, req)
	case "reset":
		confirmed, err = mut.SetDefValue(opCtx, req)
	}
	if err != nil {
		return err
	}
	meta := findObjectMeta(plug, *slot, *group, *label, *id)
	fmt.Println("confirmed " + formatValue(confirmed, meta))
	if len(confirmed.Raw) > 0 {
		fmt.Printf("raw       = %s\n", hex.EncodeToString(confirmed.Raw))
	}
	return nil
}

func helpInc()   { helpNoArgMutate("inc", "increment an object by its step (ACP1 setIncValue)") }
func helpDec()   { helpNoArgMutate("dec", "decrement an object by its step (ACP1 setDecValue)") }
func helpReset() { helpNoArgMutate("reset", "reset an object to its default (ACP1 setDefValue)") }

func helpNoArgMutate(verb, desc string) {
	fmt.Printf(`dhs consumer acp1 %s <host> --slot N (--path P | --label L | --id I)

%s. ACP1 only. Resolves the object from the cache or by walking the
slot, sends the no-argument method, and prints the device-confirmed value.

Example:
  dhs consumer acp1 %s 10.6.224.10 --slot 3 --label "Gain"
`, verb, desc, verb)
}
