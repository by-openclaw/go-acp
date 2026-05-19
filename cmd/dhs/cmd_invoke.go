package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"dhs/internal/consumer"
	emberplus "dhs/internal/emberplus/consumer"
)

func runInvoke(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("invoke", flag.ExitOnError)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", 0, "slot number")
	funcPath := fs.String("path", "", "function path: dotted label OR numeric OID (e.g. router.functions.add or 1.5.1)")
	argsStr := fs.String("args", "", "comma-separated arguments (e.g. 3,5)")
	format := fs.String("format", "raw", `result presentation: "raw" (default — provider's wire form) or "human" (pretty-print for getSalvo)`)
	ensureRaw := addEnsureFlag(fs)
	dmIdentity := fs.String("dm", "", `Ember+ only: identity-keyed DM hot-load (e.g. "dhs-emberplus-integration@1.0.0"). When set, the tree is seeded from .cache/dm/emberplus/<identity>.json and the per-call walk is skipped — refs #438, ADR-0022.`)
	noWalk := fs.Bool("no-walk", false, "fail fast on cache miss instead of falling back to a wire walk")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer <proto> invoke <host> --path <func.path> [--args val1,val2,...] [--dm <identity> | --no-walk]")
	}
	_ = fs.Parse(rest)
	if *funcPath == "" {
		return fmt.Errorf("--path is required (e.g. router.functions.add)")
	}
	if err := validatePathOrOID(*funcPath); err != nil {
		return err
	}
	if *format != "raw" && *format != "human" {
		return fmt.Errorf("%w: --format must be \"raw\" or \"human\", got %q", consumer.ErrInvalidFormat, *format)
	}

	// R14 #475: parse --ensure now so any mode-specific reject (absent
	// is a hard reject for invoke) fires before we burn a wire walk.
	mode, err := parseEnsureMode(*ensureRaw)
	if err != nil {
		return err
	}
	if mode == ensureAbsent {
		return fmt.Errorf("%w: --ensure absent does not apply to invoke (function calls have no spec'd inverse)",
			errEnsureNotApplicable)
	}

	// Parse arguments — RFC 4180 CSV so a quoted field can carry a comma
	// (e.g. storeSalvo wants `1.3.4,7,"0,2"` where the third arg is the
	// CSV target filter "0,2"). Naïve strings.Split shreds the inner
	// quoted comma into two fields and breaks every multi-target invoke.
	// Per field: try int → float → bool → string.
	var funcArgs []interface{}
	if *argsStr != "" {
		fields, err := csv.NewReader(strings.NewReader(*argsStr)).Read()
		if err != nil {
			return fmt.Errorf("--args parse: %w", err)
		}
		for _, a := range fields {
			a = strings.TrimSpace(a)
			if n, err := strconv.ParseInt(a, 10, 64); err == nil {
				funcArgs = append(funcArgs, n)
			} else if f, err := strconv.ParseFloat(a, 64); err == nil {
				funcArgs = append(funcArgs, f)
			} else if a == "true" {
				funcArgs = append(funcArgs, true)
			} else if a == "false" {
				funcArgs = append(funcArgs, false)
			} else {
				funcArgs = append(funcArgs, a)
			}
		}
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	// Reject non-Ember+ early — invoke is Ember+ only. Doing the
	// type-check before ensureEmberplusTree avoids burning a walk
	// against an ACP1/ACP2 device only to error out afterwards.
	ep, ok := plug.(*emberplus.Plugin)
	if !ok {
		return fmt.Errorf("invoke command is only supported for Ember+ protocol")
	}

	// Ember+ DM auto-cache (refs #438, ADR-0022): hot-load from
	// .cache/dm/emberplus/<identity>.json when present, otherwise walk
	// once + auto-extract the DM so the NEXT invoke is fast. Cuts
	// integration-demo invoke latency from ~3s (363-node walk) to <100ms
	// on the second contact.
	if err := ensureEmberplusTree(ctx, plug, host, cf.port, *dmIdentity, *slot, *noWalk); err != nil {
		return err
	}

	// Start the per-op timer AFTER the walk — otherwise the walk burns
	// through --timeout before Invoke even sends its Command frame.
	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	// R14 #475 dryrun: print the call shape that would be sent and
	// return without invoking. changed=false. Lets playbook authors
	// validate args + path resolution without provider-side effects.
	if mode == ensureDryrun {
		return emitEnsureReport(ensureReport{
			Verb:   "invoke",
			Ensure: string(mode),
			Before: fmt.Sprintf("would invoke %s with args %v", *funcPath, funcArgs),
			After:  fmt.Sprintf("would invoke %s with args %v", *funcPath, funcArgs),
			Reason: "dryrun — no wire send",
		})
	}

	result, err := ep.InvokeFunction(opCtx, *funcPath, funcArgs)
	// emberplus.InvokeFunction returns (result, ErrInvocationFailed) when
	// the provider replies Success=false. Print the visible result before
	// propagating the error so operators still see the (possibly-empty)
	// tuple alongside the typed exit code.
	if result != nil {
		fmt.Printf("invocation %d: success=%v\n", result.InvocationID, result.Success)
		if len(result.Result) > 0 {
			// R11 #482 — pretty-print getSalvo when --format human.
			// For every other function (and for --format raw) keep the
			// existing wire-form display.
			if *format == "human" && isGetSalvoPath(*funcPath) {
				if s, ok := result.Result[0].(string); ok {
					fmt.Println(formatGetSalvoHuman(s))
				} else {
					// Unexpected shape — fall back to raw so the operator
					// still sees something.
					fmt.Printf("result: %v\n", result.Result)
				}
			} else {
				fmt.Printf("result: %v\n", result.Result)
			}
		}
	}
	return err
}

// isGetSalvoPath returns true when the function path's last segment is
// "getSalvo". Used by R11 #482 to gate the pretty-print formatter.
func isGetSalvoPath(path string) bool {
	idx := strings.LastIndex(path, ".")
	if idx < 0 {
		return path == "getSalvo"
	}
	return path[idx+1:] == "getSalvo"
}
