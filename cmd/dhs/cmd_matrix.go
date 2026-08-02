package main

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"

	emberplus "dhs/internal/emberplus/consumer"
	"dhs/internal/emberplus/codec/matrix"
)

func runMatrix(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("matrix", flag.ExitOnError)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", 0, "slot number")
	matrixPath := fs.String("path", "", "matrix path: dotted label OR numeric OID (e.g. router.oneToN.matrix or 1.1)")
	target := fs.Int("target", -1, "target number")
	sourcesStr := fs.String("sources", "", "comma-separated source numbers (e.g. 1 or 1,2,3)")
	op := fs.String("op", "absolute", "operation: absolute, connect, disconnect")
	dmIdentity := fs.String("dm", "", `identity-keyed DM hot-load (e.g. "Tiny Ember+ Router@1.6.2"). When set, the tree is seeded from .cache/dm/emberplus/<identity>.json and the per-call walk is skipped — refs #438, ADR-0022.`)
	noWalk := fs.Bool("no-walk", false, "fail fast on cache miss instead of falling back to a wire walk")
	check := fs.Bool("check", false, "dry-run: report would_change, send nothing (ADR-0007)")
	asJSON := fs.Bool("json", false, "deprecated alias for --output json")
	output := fs.String("output", "text", "output format: text | json (ADR-0002)")
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

	jsonOut, oerr := resolveEnsureOutput(*output, *asJSON)
	if oerr != nil {
		return oerr
	}

	// Read current crosspoints for the target (read-back), then converge:
	// send only when the target's source set actually differs from the intent
	// (ADR-0007 idempotency). MatrixSnapshot reflects the connections folded in
	// during the walk/DM-load above.
	current := matrixTargetSources(ep.MatrixSnapshot(*matrixPath), int32(*target))
	changed, projected := matrixConverge(current, sources, operation)
	curStr := joinInt32s(current)
	tgtStr := joinInt32s(projected)

	if *check {
		return emitEnsure(jsonOut, ensureResult{WouldChange: &changed, Current: curStr, Target: tgtStr, Diff: ensureFieldDiff(changed, "sources", curStr, tgtStr)})
	}
	if !changed {
		return emitEnsure(jsonOut, ensureResult{Changed: &changed, Previous: curStr, Current: curStr, Diff: ensureFieldDiff(false, "sources", curStr, curStr)})
	}

	// Per-op timer started AFTER walk/DM-load so MatrixConnect sees
	// a fresh --timeout budget — same pattern as cmd_invoke / cmd_set.
	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	if err := ep.MatrixConnect(opCtx, *matrixPath, int32(*target), sources, operation); err != nil {
		return err
	}

	applied := true
	return emitEnsure(jsonOut, ensureResult{Changed: &applied, Previous: curStr, Current: tgtStr, Diff: ensureFieldDiff(applied, "sources", curStr, tgtStr)})
}

// matrixTargetSources returns the current source set for target in a snapshot,
// or an empty slice when the target has no connections.
func matrixTargetSources(snap []matrix.TargetState, target int32) []int32 {
	for _, ts := range snap {
		if ts.Target == target {
			return append([]int32(nil), ts.Sources...)
		}
	}
	return nil
}

// matrixConverge computes, for a Connection operation (0=absolute/replace,
// 1=connect/add, 2=disconnect/remove), whether applying `desired` to the
// target's `current` sources changes anything, and the projected resulting set.
// This mirrors the provider's union/difference/replace semantics so the CLI can
// decide idempotently whether a send is needed and report the ADR-0007 diff.
func matrixConverge(current, desired []int32, op int64) (changed bool, projected []int32) {
	switch op {
	case 1: // connect / add
		projected = unionInt32s(current, desired)
	case 2: // disconnect / remove
		projected = differenceInt32s(current, desired)
	default: // 0 absolute / replace
		projected = append([]int32(nil), desired...)
	}
	return !int32SetEqual(current, projected), projected
}

// int32SetEqual reports set equality (order-independent, dedup-independent).
func int32SetEqual(a, b []int32) bool {
	sa, sb := int32Set(a), int32Set(b)
	if len(sa) != len(sb) {
		return false
	}
	for k := range sa {
		if _, ok := sb[k]; !ok {
			return false
		}
	}
	return true
}

func int32Set(xs []int32) map[int32]struct{} {
	m := make(map[int32]struct{}, len(xs))
	for _, x := range xs {
		m[x] = struct{}{}
	}
	return m
}

func unionInt32s(a, b []int32) []int32 {
	out := append([]int32(nil), a...)
	seen := int32Set(a)
	for _, x := range b {
		if _, ok := seen[x]; !ok {
			out = append(out, x)
			seen[x] = struct{}{}
		}
	}
	return out
}

func differenceInt32s(a, remove []int32) []int32 {
	drop := int32Set(remove)
	var out []int32
	for _, x := range a {
		if _, ok := drop[x]; !ok {
			out = append(out, x)
		}
	}
	return out
}

// joinInt32s renders a source set as a sorted comma-joined string for stable,
// comparable output (e.g. "1,2,3"); empty set renders as "".
func joinInt32s(xs []int32) string {
	if len(xs) == 0 {
		return ""
	}
	sorted := append([]int32(nil), xs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	parts := make([]string, len(sorted))
	for i, x := range sorted {
		parts[i] = strconv.Itoa(int(x))
	}
	return strings.Join(parts, ",")
}

// ensureFieldDiff is the general ADR-0007 diff[] builder for a single named
// field (the value-case helper ensureValueDiff is the field=="value" form).
func ensureFieldDiff(changed bool, field, from, to string) []ensureDiff {
	if !changed {
		return []ensureDiff{}
	}
	return []ensureDiff{{Field: field, From: from, To: to}}
}
