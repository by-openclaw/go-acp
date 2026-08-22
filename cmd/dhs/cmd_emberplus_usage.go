package main

// usage + replace for Ember+ matrices (#722, unit 3; canonical shape
// defined by cerebrum-nb #718, row/format helpers shared with the
// probel units in cmd_probel_usage.go).
//
//	usage    WHERE is a source assigned on one matrix (reverse tally),
//	         built from the walked tree's connection snapshot. Levels
//	         column fixed "0" — tree matrices have no levels (grammar
//	         parity with the levelled protocols, same as export).
//	replace  substitute source A with B on every target that carries A
//	         (ADR-0007: --check first, diff-only, run-twice = 0),
//	         honoring the ADR-0023 behavior: 1to1 / 1toN / dynamic take
//	         the substituted set ABSOLUTELY (pinned semantics — never
//	         toggles), NtoM connects the missing source and EXPLICITLY
//	         disconnects the replaced one.
//
// Both are client-side compositions over the walked tree +
// MatrixConnect — the wire operations stay internal (owner rule).

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"dhs/internal/consumer"
	emberplus "dhs/internal/emberplus/consumer"
	"dhs/internal/emberplus/codec/matrix"
)

// emberUsageRows flattens a matrix connection snapshot into
// source-keyed usage rows (level fixed 0 — tree matrices have no
// levels).
func emberUsageRows(snap []matrix.TargetState) []probelUsageRow {
	var rows []probelUsageRow
	for _, ts := range snap {
		for _, src := range ts.Sources {
			rows = append(rows, probelUsageRow{Src: int(src), Dst: int(ts.Target), Levels: "0"})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Src != rows[j].Src {
			return rows[i].Src < rows[j].Src
		}
		return rows[i].Dst < rows[j].Dst
	})
	return rows
}

func containsInt32(xs []int32, v int32) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// emberReplacePlan computes the converge actions that substitute
// source `from` with `to` on every target carrying `from`, honoring
// the ADR-0023 behavior (see file header).
func emberReplacePlan(behavior string, snap []matrix.TargetState, from, to int32) []xpointChange {
	var out []xpointChange
	for _, ts := range snap {
		if !containsInt32(ts.Sources, from) {
			continue
		}
		have := dedupInt32(ts.Sources)
		want := make([]int32, 0, len(have))
		for _, s := range have {
			if s == from {
				s = to
			}
			want = append(want, s)
		}
		want = dedupInt32(want)
		switch behavior {
		case "NtoM":
			add := subtractInt32(want, have)
			del := subtractInt32(have, want)
			if len(add) > 0 {
				out = append(out, xpointChange{Target: ts.Target, Sources: add, Op: xpOpConnect, From: joinInt32s(have), To: joinInt32s(want)})
			}
			if len(del) > 0 {
				out = append(out, xpointChange{Target: ts.Target, Sources: del, Op: xpOpDisconnect, From: joinInt32s(have), To: joinInt32s(want)})
			}
		default: // 1to1 / 1toN / dynamic — absolute take (never toggles)
			out = append(out, xpointChange{Target: ts.Target, Sources: want, Op: xpOpAbsolute, From: joinInt32s(have), To: joinInt32s(want)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out
}

// emberUsageLabelMap flattens one label group of a walked matrix into
// id → label. group "" selects the first group alphabetically (the
// primary). Labels are FREE on ember+ — they ride in the walked tree.
func emberUsageLabelMap(o consumer.Object, metaKey, group string) map[int]string {
	groups, byGroup := labelGroups(o, metaKey)
	if len(groups) == 0 {
		return nil
	}
	pick := groups[0]
	if group != "" {
		found := false
		for _, g := range groups {
			if g == group {
				pick, found = g, true
				break
			}
		}
		if !found {
			return nil
		}
	}
	out := map[int]string{}
	for idx, label := range byGroup[pick] {
		if n, err := strconv.Atoi(idx); err == nil && label != "" {
			out[n] = label
		}
	}
	return out
}

// emberValidSourceIDs collects the matrix's valid source numbers from
// its walked sourceLabels meta (union across label groups). Empty map
// = the matrix declares no source list (sparse validation impossible).
// Ember+ matrices legally have SPARSE id sets, and at least one
// shipping provider (TinyEmber+ 1.6.2, live 2026-08-21) SILENTLY
// ignores connections naming an id outside the set — so replace warns
// loudly instead of letting the substitution vanish.
func emberValidSourceIDs(o consumer.Object) map[int]bool {
	groups, byGroup := labelGroups(o, "sourceLabels")
	out := map[int]bool{}
	for _, g := range groups {
		for idx := range byGroup[g] {
			if n, err := strconv.Atoi(idx); err == nil {
				out[n] = true
			}
		}
	}
	return out
}

func helpEmberUsage() {
	fmt.Println(`dhs consumer emberplus usage <host> [--port N] [--path <matrix.path>] [--srce N | --dest N] [--format csv|ascii] [--out PATH] [--dm <identity> | --no-walk]

Reverse tally for ONE Ember+ matrix: where is each source assigned.
Built from the walked tree's connection snapshot — no wire mutation.
--path selects the matrix (dotted label path or numeric OID); omitted =
the only matrix in the tree. Levels column is fixed "0" (tree matrices
have no levels — grammar parity with the levelled protocols).

  --srce N        only this source's assignments (fan-out view in ascii)
  --dest N        only this target's feed
  --format        csv (default) | ascii
  --out PATH      csv file (omitted = snapshots/emberplus/<host>/usage.csv
                  per ADR-0028; "-" = stdout)

EXAMPLES
  dhs consumer emberplus usage 10.100.0.102 --port 9000 --path router.nToN.matrix --srce 3 --format ascii`)
}

func helpEmberReplace() {
	fmt.Println(`dhs consumer emberplus replace <host> [--port N] --srce A --with B [--path <matrix.path>] [--check] [--output text|json] [--dm <identity> | --no-walk]

Substitute source A with B on every target of ONE matrix that carries A
(ADR-0007 ensure: --check dry-run, diff-only apply, run-twice = 0).
Honors the ADR-0023 behavior: 1to1 / 1toN / dynamic take the substituted
source set ABSOLUTELY (never toggles); NtoM connects B and EXPLICITLY
disconnects A.

EXAMPLES
  dhs consumer emberplus replace 10.100.0.102 --port 9000 --path router.nToN.matrix --srce 3 --with 7 --check`)
}

// runEmberUsage drives `dhs consumer emberplus usage`.
func runEmberUsage(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("emberplus-usage", flag.ContinueOnError)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", 0, "slot number")
	matrixPath := fs.String("path", "", "matrix path: dotted label OR numeric OID (omitted = the only matrix in the tree)")
	srce := fs.Int("srce", -1, "filter: only this source's assignments")
	dest := fs.Int("dest", -1, "filter: only this target's feed")
	withLabels := fs.Bool("names", false, "resolve src/dst names from the matrix label groups (free — no extra wire traffic); csv appends srce_label,dest_label. (--labels is taken by the canonical-labels mode)")
	labelGroup := fs.String("name-group", "", "which label group to use with --names (omitted = the first group)")
	format := fs.String("format", "csv", "output format: csv | ascii")
	out := fs.String("out", "", "csv output file (omitted = snapshots/emberplus/<host>/usage.csv per ADR-0028; \"-\" = stdout)")
	dmIdentity := fs.String("dm", "", "identity-keyed DM hot-load (ADR-0022): seed the tree from cache, skip the walk")
	noWalk := fs.Bool("no-walk", false, "fail fast on cache miss instead of falling back to a wire walk")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer emberplus usage <host> [--path <matrix.path>] [--srce N | --dest N] [--format csv|ascii]")
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if *format != "csv" && *format != "ascii" {
		return fmt.Errorf("--format must be csv or ascii, got %q", *format)
	}
	if *srce >= 0 && *dest >= 0 {
		return fmt.Errorf("--srce and --dest are mutually exclusive")
	}
	if *matrixPath != "" {
		if err := validatePathOrOID(*matrixPath); err != nil {
			return err
		}
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()
	ep, ok := plug.(*emberplus.Plugin)
	if !ok {
		return fmt.Errorf("usage (matrix reverse tally) is only supported for Ember+ protocol")
	}
	if err := ensureEmberplusTree(ctx, plug, host, cf.port, *dmIdentity, *slot, *noWalk); err != nil {
		return err
	}
	wctx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()
	objs, err := plug.Walk(wctx, *slot)
	if err != nil {
		return err
	}
	obj, dotted, found := findMatrixObject(objs, *matrixPath)
	if !found {
		avail := listMatrixPaths(objs)
		if len(avail) == 0 {
			return fmt.Errorf("no matrix in the walked tree")
		}
		return fmt.Errorf("matrix %q not found; available:\n  %s", *matrixPath, strings.Join(avail, "\n  "))
	}
	rows := emberUsageRows(ep.MatrixSnapshot(dotted))
	if *withLabels {
		applyProbelUsageLabels(rows,
			emberUsageLabelMap(obj, "sourceLabels", *labelGroup),
			emberUsageLabelMap(obj, "targetLabels", *labelGroup))
	}
	rows = probelFilterUsage(rows, *srce, *dest)

	if *format == "ascii" {
		renderProbelUsageASCII(os.Stdout, rows)
		return nil
	}
	csv := formatProbelUsageCSV(rows, false, *withLabels)
	if *out == "-" {
		fmt.Print(csv)
		return nil
	}
	path := *out
	if path == "" {
		path = filepath.Join(snapshotDir("emberplus", host), "usage.csv")
		fmt.Fprintf(os.Stderr, "emberplus usage: default snapshot file %s (ADR-0028)\n", path)
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(path, []byte(csv), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "emberplus usage: wrote %s (%d row(s))\n", path, len(rows))
	return nil
}

// runEmberReplace drives `dhs consumer emberplus replace`.
func runEmberReplace(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("emberplus-replace", flag.ContinueOnError)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", 0, "slot number")
	matrixPath := fs.String("path", "", "matrix path: dotted label OR numeric OID (omitted = the only matrix in the tree)")
	from := fs.Int("srce", -1, "source number to replace (required)")
	with := fs.Int("with", -1, "source number that takes over (required)")
	check := fs.Bool("check", false, "dry-run (ADR-0007): list the targets that would change, send nothing")
	output := fs.String("output", "text", "output format: text | json (ADR-0002; json = {changed|would_change, diff[]})")
	dmIdentity := fs.String("dm", "", "identity-keyed DM hot-load (ADR-0022): seed the tree from cache, skip the walk")
	noWalk := fs.Bool("no-walk", false, "fail fast on cache miss instead of falling back to a wire walk")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer emberplus replace <host> --srce A --with B [--path <matrix.path>] [--check]")
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if *from < 0 || *with < 0 {
		return fmt.Errorf("--srce and --with are required (replace source A with B)")
	}
	if *from == *with {
		return fmt.Errorf("--srce and --with are the same source — nothing to do")
	}
	jsonOut, oerr := resolveEnsureOutput(*output, false)
	if oerr != nil {
		return oerr
	}
	if *matrixPath != "" {
		if err := validatePathOrOID(*matrixPath); err != nil {
			return err
		}
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()
	ep, ok := plug.(*emberplus.Plugin)
	if !ok {
		return fmt.Errorf("replace (matrix source substitution) is only supported for Ember+ protocol")
	}
	if err := ensureEmberplusTree(ctx, plug, host, cf.port, *dmIdentity, *slot, *noWalk); err != nil {
		return err
	}
	wctx, wcancel := withTimeout(ctx, cf.timeout)
	defer wcancel()
	objs, err := plug.Walk(wctx, *slot)
	if err != nil {
		return err
	}
	obj, dotted, found := findMatrixObject(objs, *matrixPath)
	if !found {
		avail := listMatrixPaths(objs)
		if len(avail) == 0 {
			return fmt.Errorf("no matrix in the walked tree")
		}
		return fmt.Errorf("matrix %q not found; available:\n  %s", *matrixPath, strings.Join(avail, "\n  "))
	}
	desc := descFromObject(obj, dotted)
	if valid := emberValidSourceIDs(obj); len(valid) > 0 && !valid[*with] {
		fmt.Fprintf(os.Stderr, "WARNING: source %d is not in %s's declared source set — providers may SILENTLY ignore the substitution (TinyEmber+ does)\n", *with, dotted)
	}
	changes := emberReplacePlan(desc.Behavior, ep.MatrixSnapshot(dotted), int32(*from), int32(*with))

	logw := os.Stdout
	if jsonOut {
		logw = os.Stderr
	}
	diffs := make([]ensureDiff, 0, len(changes))
	for _, c := range changes {
		diffs = append(diffs, ensureDiff{Field: fmt.Sprintf("xpoint.%s.%d", dotted, c.Target), From: c.From, To: c.To})
	}
	changed := len(changes) > 0

	if *check {
		for _, c := range changes {
			_, _ = fmt.Fprintf(logw, "[would-replace] %s target=%d: %q -> %q\n", dotted, c.Target, c.From, c.To)
		}
		_, _ = fmt.Fprintf(logw, "emberplus replace --check: would_change=%d action(s) on %s (%s) carrying src %d — nothing sent\n", len(changes), dotted, desc.Behavior, *from)
		if jsonOut {
			return emitEnsure(true, ensureResult{WouldChange: &changed, Diff: diffs})
		}
		return nil
	}

	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()
	for _, c := range changes {
		if err := ep.MatrixConnect(opCtx, dotted, c.Target, c.Sources, c.Op); err != nil {
			return fmt.Errorf("replace %s target %d: %w", dotted, c.Target, err)
		}
		_, _ = fmt.Fprintf(logw, "[replace] OK %s target=%d: %q -> %q\n", dotted, c.Target, c.From, c.To)
	}
	if len(changes) == 0 {
		_, _ = fmt.Fprintf(logw, "emberplus replace: nothing carries src %d on %s — already converged\n", *from, dotted)
	} else {
		_, _ = fmt.Fprintf(logw, "emberplus replace: changed=%d action(s); run again to verify 0 (nothing carries src %d any more)\n", len(changes), *from)
	}
	if jsonOut {
		return emitEnsure(true, ensureResult{Changed: &changed, Diff: diffs})
	}
	return nil
}
