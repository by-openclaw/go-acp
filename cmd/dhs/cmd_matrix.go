package main

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"

	emberplus "dhs/internal/emberplus/consumer"
)

func runMatrix(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("matrix", flag.ExitOnError)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", 0, "slot number")
	matrixPath := fs.String("path", "", "matrix path: dotted label OR numeric OID (e.g. router.oneToN.matrix or 1.1)")
	target := fs.Int("target", -1, "target number")
	sourcesStr := fs.String("sources", "", "comma-separated source numbers (e.g. 1 or 1,2,3)")
	op := fs.String("op", "absolute", "operation: absolute, connect, disconnect")
	ensureRaw := addEnsureFlag(fs)
	dmIdentity := fs.String("dm", "", `identity-keyed DM hot-load (e.g. "Tiny Ember+ Router@1.6.2"). When set, the tree is seeded from .cache/dm/emberplus/<identity>.json and the per-call walk is skipped — refs #438, ADR-0022.`)
	noWalk := fs.Bool("no-walk", false, "fail fast on cache miss instead of falling back to a wire walk")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer <proto> matrix <host> --path <matrix.path> --target N --sources N[,N,...] [--op absolute|connect|disconnect] [--dm <identity> | --no-walk]")
	}
	_ = fs.Parse(rest)
	if *matrixPath == "" {
		return fmt.Errorf("--path is required (e.g. router.oneToN.matrix)")
	}
	if err := validatePathOrOID(*matrixPath); err != nil {
		return err
	}
	if *target < 0 {
		return fmt.Errorf("--target is required")
	}

	// Parse sources.
	var sources []int32
	if *sourcesStr != "" {
		for _, s := range strings.Split(*sourcesStr, ",") {
			s = strings.TrimSpace(s)
			n, perr := strconv.Atoi(s)
			if perr != nil {
				return fmt.Errorf("invalid source number %q: %w", s, perr)
			}
			sources = append(sources, int32(n))
		}
	}

	// Parse operation.
	var operation int64
	switch strings.ToLower(*op) {
	case "absolute", "abs", "replace":
		operation = 0
	case "connect", "add":
		operation = 1
	case "disconnect", "remove":
		operation = 2
	default:
		return fmt.Errorf("unknown --op %q (use absolute, connect, disconnect)", *op)
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	// Cast to Ember+ plugin to access MatrixConnect.
	ep, ok := plug.(*emberplus.Plugin)
	if !ok {
		return fmt.Errorf("matrix command is only supported for Ember+ protocol")
	}

	// DM auto-cache: cache hit → no walk; cache miss → walk + auto-extract
	// so the next call hits the cache (refs #438, ADR-0022).
	if err := ensureEmberplusTree(ctx, plug, host, cf.port, *dmIdentity, *slot, *noWalk); err != nil {
		return err
	}

	// Per-op timer started AFTER walk/DM-load so MatrixConnect sees
	// a fresh --timeout budget — same pattern as cmd_invoke / cmd_set.
	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	// R14 #475: idempotent matrix verb. Read the current target's
	// source set from MatrixSnapshot, compare with the desired
	// sources, and emit a JSON ensureReport. Semantics ignore the
	// op flag — the ensure mode itself dictates the goal state:
	//
	//   ensurePresent — listed sources must be in current (connect missing)
	//   ensureAbsent  — listed sources must NOT be in current (disconnect present)
	//   ensureDryrun  — compare without sending
	mode, err := parseEnsureMode(*ensureRaw)
	if err != nil {
		return err
	}
	if mode != ensureUnset {
		return runMatrixEnsure(opCtx, ep, *matrixPath, int32(*target), sources, mode)
	}

	if err := ep.MatrixConnect(opCtx, *matrixPath, int32(*target), sources, operation); err != nil {
		return err
	}

	fmt.Printf("matrix connect: target %d ← sources %v (op=%s)\n", *target, sources, *op)
	return nil
}

// runMatrixEnsure implements the R14 #475 idempotent matrix path.
// Reads MatrixSnapshot to learn the target's current Sources, then
// branches per ensure mode. Send-on-change only; dryrun never sends.
func runMatrixEnsure(ctx context.Context, ep *emberplus.Plugin, matrixPath string, target int32, sources []int32, mode ensureMode) error {
	snapshot := ep.MatrixSnapshot(matrixPath)
	var current []int32
	for _, ts := range snapshot {
		if ts.Target == target {
			current = append([]int32(nil), ts.Sources...)
			break
		}
	}
	sort.Slice(current, func(i, j int) bool { return current[i] < current[j] })
	desired := append([]int32(nil), sources...)
	sort.Slice(desired, func(i, j int) bool { return desired[i] < desired[j] })

	beforeStr := fmt.Sprintf("%v", current)
	report := ensureReport{
		Verb:   "matrix",
		Ensure: string(mode),
		Before: beforeStr,
	}

	switch mode {
	case ensureDryrun:
		// Both present and absent intents reduce to "what would change"
		// — show desired as After and let the operator read intent from
		// their --sources + --ensure dryrun.
		report.After = fmt.Sprintf("%v", desired)
		report.Changed = false
		return emitEnsureReport(report)

	case ensurePresent:
		toAdd := subtractSorted(desired, current)
		if len(toAdd) == 0 {
			report.After = beforeStr
			report.Changed = false
			report.Reason = "all listed sources already connected"
			return emitEnsureReport(report)
		}
		// op=1 = connect (additive)
		if err := ep.MatrixConnect(ctx, matrixPath, target, toAdd, 1); err != nil {
			return fmt.Errorf("ensure present: matrix connect %v: %w", toAdd, err)
		}
		merged := append([]int32(nil), current...)
		merged = append(merged, toAdd...)
		sort.Slice(merged, func(i, j int) bool { return merged[i] < merged[j] })
		report.After = fmt.Sprintf("%v", merged)
		report.Diff = ensureFmtDiff("sources", beforeStr, report.After)
		report.Changed = true
		return emitEnsureReport(report)

	case ensureAbsent:
		toRemove := intersectSorted(desired, current)
		if len(toRemove) == 0 {
			report.After = beforeStr
			report.Changed = false
			report.Reason = "none of the listed sources are connected"
			return emitEnsureReport(report)
		}
		// op=2 = disconnect (subtractive)
		if err := ep.MatrixConnect(ctx, matrixPath, target, toRemove, 2); err != nil {
			return fmt.Errorf("ensure absent: matrix disconnect %v: %w", toRemove, err)
		}
		remaining := subtractSorted(current, toRemove)
		report.After = fmt.Sprintf("%v", remaining)
		report.Diff = ensureFmtDiff("sources", beforeStr, report.After)
		report.Changed = true
		return emitEnsureReport(report)
	}
	return fmt.Errorf("%w: --ensure=%q", errEnsureInvalidMode, mode)
}

// subtractSorted returns elements of `a` not in `b`. Inputs must be
// sorted ascending; output preserves the ascending order.
func subtractSorted(a, b []int32) []int32 {
	out := make([]int32, 0, len(a))
	bSet := make(map[int32]struct{}, len(b))
	for _, v := range b {
		bSet[v] = struct{}{}
	}
	for _, v := range a {
		if _, in := bSet[v]; !in {
			out = append(out, v)
		}
	}
	return out
}

// intersectSorted returns elements present in both `a` and `b`. Inputs
// must be sorted ascending; output preserves the ascending order.
func intersectSorted(a, b []int32) []int32 {
	out := make([]int32, 0)
	bSet := make(map[int32]struct{}, len(b))
	for _, v := range b {
		bSet[v] = struct{}{}
	}
	for _, v := range a {
		if _, in := bSet[v]; in {
			out = append(out, v)
		}
	}
	return out
}
