package main

// import for SW-P-02 (#751 unit G2) — closes the export→import
// round-trip on the last connector missing it. Reads the canonical
// -xpoint.csv (dest,srce,levels — the family grammar its own export
// writes) and converges the single configured (matrix, level) via one
// connect (rx 02 / rx 66) per differing destination, with ADR-0007
// ensure semantics: --check dry-run, diff[], run-twice = 0.
//
// Rows for OTHER levels are counted and reported, never applied —
// one SW-P-02 session addresses one (matrix, level); re-run with a
// different global --level to apply the rest.

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// sw02ImportCell is one crosspoint the import must converge.
type sw02ImportCell struct {
	Dst, From, To int
}

// sw02ImportPlan diffs desired rows against the swept current state
// for one level. Returns the cells to change (dst order), the number
// of desired rows skipped because they target other levels, and an
// error on rows that cannot be typed.
func sw02ImportPlan(current []probelUsageRow, rows []cerebrumXpointRow, level int) ([]sw02ImportCell, int, error) {
	cur := map[int]int{}
	for _, r := range current {
		cur[r.Dst] = r.Src
	}
	want := map[int]int{}
	var order []int
	skipped := 0
	lvl := strconv.Itoa(level)
	for _, r := range rows {
		onLevel := false
		for _, l := range r.Levels {
			if strings.TrimSpace(l) == lvl {
				onLevel = true
			} else {
				skipped++
			}
		}
		if !onLevel {
			continue
		}
		dst, err := strconv.Atoi(strings.TrimSpace(r.Dest))
		if err != nil {
			return nil, 0, fmt.Errorf("dest %q is not a destination id", r.Dest)
		}
		src, err := strconv.Atoi(strings.TrimSpace(r.Srce))
		if err != nil {
			return nil, 0, fmt.Errorf("srce %q is not a source id", r.Srce)
		}
		if _, seen := want[dst]; !seen {
			order = append(order, dst)
		}
		want[dst] = src
	}
	var cells []sw02ImportCell
	for _, dst := range order {
		if cur[dst] != want[dst] {
			cells = append(cells, sw02ImportCell{Dst: dst, From: cur[dst], To: want[dst]})
		}
	}
	return cells, skipped, nil
}

// runProbelSW02Import drives `dhs consumer probel-sw02p import`.
func runProbelSW02Import(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probel-sw02p-import", flag.ContinueOnError)
	inDir := fs.String("in", "", "directory containing the CSV files (omitted = snapshots/probel-sw02p/<host>/ — import reads where export writes, ADR-0028)")
	prefix := fs.String("prefix", "sw02p", "CSV filename prefix (ignored in the default snapshot folder)")
	extended := fs.Bool("extended", false, "force extended forms (rx 65/66) throughout")
	check := fs.Bool("check", false, "dry-run (ADR-0007): list the crosspoints that would change, send nothing")
	output := fs.String("output", "text", "output format: text | json (ADR-0002; json = {changed|would_change, diff[]})")
	timeout := fs.Duration("timeout", 120*time.Second, "overall timeout (one interrogate per dst + one connect per change)")
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	jsonOut, oerr := resolveEnsureOutput(*output, false)
	if oerr != nil {
		return oerr
	}
	if *inDir == "" {
		*inDir = snapshotDir("probel-sw02p", hostOnly(addr))
		*prefix = ""
		fmt.Fprintf(os.Stderr, "probel-sw02p import: default snapshot folder %s (ADR-0028)\n", *inDir)
	}
	data, err := os.ReadFile(facetFile(*inDir, *prefix, "xpoint"))
	if err != nil {
		return err
	}
	rows, err := parseCerebrumXpoint(data, facetFile(*inDir, *prefix, "xpoint"))
	if err != nil {
		return err
	}

	cctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	p, closer, err := dialProbelSW02(cctx, addr)
	if err != nil {
		return err
	}
	defer closer()
	logw := os.Stdout
	if jsonOut {
		logw = os.Stderr
	}
	count, level, err := sw02UsageDstCount(cctx, p)
	if err != nil {
		return err
	}
	current, err := sw02Interrogations(cctx, p, count, level, *extended)
	if err != nil {
		return err
	}
	cells, skipped, err := sw02ImportPlan(current, rows, level)
	if err != nil {
		return err
	}
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "probel-sw02p import: %d desired row-level(s) target other levels — re-run with the matching global --level to apply them\n", skipped)
	}

	changed := len(cells) > 0
	diffs := make([]ensureDiff, 0, len(cells))
	for _, c := range cells {
		diffs = append(diffs, ensureDiff{
			Field: fmt.Sprintf("route.%d.%d", c.Dst, level),
			From:  strconv.Itoa(c.From), To: strconv.Itoa(c.To),
		})
	}

	if *check {
		for _, c := range cells {
			_, _ = fmt.Fprintf(logw, "[would-xpoint] level=%d dst=%d: src %d -> %d\n", level, c.Dst, c.From, c.To)
		}
		_, _ = fmt.Fprintf(logw, "probel-sw02p import --check: would_change=%d of %d desired row(s) — nothing sent\n", len(cells), len(rows))
		if jsonOut {
			return emitEnsure(true, ensureResult{WouldChange: &changed, Diff: diffs})
		}
		return nil
	}

	for _, c := range cells {
		var cerr error
		if *extended || c.Dst > sw02NarrowOutOfRange || c.To > sw02NarrowOutOfRange {
			_, cerr = p.SendExtendedConnect(cctx, uint16(c.Dst), uint16(c.To))
		} else {
			_, cerr = p.SendConnect(cctx, uint16(c.Dst), uint16(c.To), false)
		}
		if cerr != nil {
			return fmt.Errorf("xpoint dst %d: %w", c.Dst, cerr)
		}
		_, _ = fmt.Fprintf(logw, "[xpoint] OK level=%d dst=%d: src %d -> %d\n", level, c.Dst, c.From, c.To)
	}
	if len(cells) == 0 {
		_, _ = fmt.Fprintf(logw, "probel-sw02p import: already converged — nothing sent\n")
	}
	if jsonOut {
		return emitEnsure(true, ensureResult{Changed: &changed, Diff: diffs})
	}
	return nil
}
