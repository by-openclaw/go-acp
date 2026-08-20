package main

// usage + replace — the source-side pair of the Matrix template
// (#722, unit 1; canonical shape defined by cerebrum-nb #718):
//
//	usage    WHERE is a source assigned (reverse tally / source usage)
//	         on one (matrix, level), built from the crosspoint tally
//	         dump (rx 021 / tx 022/023). --protect joins the per-dst
//	         protect state from the protect tally dump (rx 019/tx 020).
//	replace  substitute source A with B on every crosspoint that
//	         carries A (ADR-0007: --check first, diff-only, run-twice
//	         = 0 because nothing carries A afterwards), one
//	         CrosspointConnect (rx 002) per affected dst.
//
// Both are client-side compositions over the existing SW-P-08
// primitives — no new wire commands. SW-P-08 has no virtual-resource
// concept, so cerebrum's --resolve has no equivalent here.

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"dhs/internal/probel-sw08p/codec"
	probelproto "dhs/internal/probel-sw08p/consumer"
)

// probelUsageRow is one reverse-tally assignment: src feeds dst on the
// queried level. Protect carries the dst's protect state when the
// caller joined it (--protect), else "".
type probelUsageRow struct {
	Src, Dst, Level int
	Protect         string
}

// probelTallyTable flattens the byte/word dual-form TallyDumpResult
// into one (firstDst, srcs) pair — the merged dst→src table.
func probelTallyTable(res probelproto.TallyDumpResult) (firstDst int, srcs []int) {
	if res.IsWord {
		srcs = make([]int, len(res.Word.SourceIDs))
		for i, s := range res.Word.SourceIDs {
			srcs[i] = int(s)
		}
		return int(res.Word.FirstDestinationID), srcs
	}
	srcs = make([]int, len(res.Byte.SourceIDs))
	for i, s := range res.Byte.SourceIDs {
		srcs[i] = int(s)
	}
	return int(res.Byte.FirstDestinationID), srcs
}

// probelBuildUsage turns the dst→src table into source-keyed usage
// rows, ordered by (src, dst). protect maps dst → rendered protect
// state; nil = no protect join requested.
func probelBuildUsage(firstDst int, srcs []int, level int, protect map[int]string) []probelUsageRow {
	rows := make([]probelUsageRow, 0, len(srcs))
	for i, src := range srcs {
		dst := firstDst + i
		rows = append(rows, probelUsageRow{Src: src, Dst: dst, Level: level, Protect: protect[dst]})
	}
	return sortProbelUsage(rows)
}

// sortProbelUsage orders rows source-keyed: by (src, dst).
func sortProbelUsage(rows []probelUsageRow) []probelUsageRow {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Src != rows[j].Src {
			return rows[i].Src < rows[j].Src
		}
		return rows[i].Dst < rows[j].Dst
	})
	return rows
}

// probelFilterUsage keeps only the rows matching the src / dst filter
// (-1 = no filter on that axis).
func probelFilterUsage(rows []probelUsageRow, src, dst int) []probelUsageRow {
	if src < 0 && dst < 0 {
		return rows
	}
	out := rows[:0:0]
	for _, r := range rows {
		if (src >= 0 && r.Src == src) || (dst >= 0 && r.Dst == dst) {
			out = append(out, r)
		}
	}
	return out
}

// formatProbelUsageCSV renders rows with the canonical usage columns
// (srce,dest,levels — same as cerebrum-nb usage); --protect appends a
// protect column.
func formatProbelUsageCSV(rows []probelUsageRow, withProtect bool) string {
	var b strings.Builder
	if withProtect {
		b.WriteString("srce,dest,levels,protect\n")
	} else {
		b.WriteString("srce,dest,levels\n")
	}
	for _, r := range rows {
		fmt.Fprintf(&b, "%d,%d,%d", r.Src, r.Dst, r.Level)
		if withProtect {
			b.WriteByte(',')
			b.WriteString(r.Protect)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// renderProbelUsageASCII prints the fan-out view: one block per
// feeding source, every dst it feeds nested under it.
func renderProbelUsageASCII(w io.Writer, rows []probelUsageRow) {
	var lastSrc = -1
	var block []probelUsageRow
	flush := func() {
		for i, r := range block {
			branch := "├──"
			if i == len(block)-1 {
				branch = "└──"
			}
			suffix := ""
			if r.Protect != "" {
				suffix = "  [" + r.Protect + "]"
			}
			_, _ = fmt.Fprintf(w, "%s dst %d  level %d%s\n", branch, r.Dst, r.Level, suffix)
		}
		block = block[:0]
	}
	for _, r := range rows {
		if r.Src != lastSrc {
			flush()
			lastSrc = r.Src
			_, _ = fmt.Fprintf(w, "src %d\n", r.Src)
		}
		block = append(block, r)
	}
	flush()
}

// probelReplaceCells returns the dsts currently fed by src, in dst
// order — the replace working set.
func probelReplaceCells(rows []probelUsageRow, src int) []probelUsageRow {
	var out []probelUsageRow
	for _, r := range rows {
		if r.Src == src {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Dst < out[j].Dst })
	return out
}

// probelUsageProtectMap renders the protect dump as dst → "state=N
// device=M", skipping unprotected dsts so the CSV column stays empty
// where nothing is protected.
func probelUsageProtectMap(res codec.ProtectTallyDumpParams) map[int]string {
	out := map[int]string{}
	for i, it := range res.Items {
		if it.State == codec.ProtectNone {
			continue
		}
		out[int(res.FirstDestinationID)+i] = protectStateStr(it.State, int(it.DeviceID))
	}
	return out
}

// runProbelUsage drives `dhs consumer probel-sw08p usage`.
func runProbelUsage(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probel-usage", flag.ContinueOnError)
	matrix := fs.Int("matrix", 0, "matrix id (0-255; global --mtx-id wins when set)")
	srce := fs.Int("srce", -1, "filter: only this source's assignments")
	dest := fs.Int("dest", -1, "filter: only this destination's feed")
	withProtect := fs.Bool("protect", false, "join per-dst protect state (protect tally dump) as an extra column")
	format := fs.String("format", "csv", "output format: csv | ascii")
	out := fs.String("out", "", "csv output file (omitted = snapshots/probel-sw08p/<host>/usage.csv per ADR-0028; \"-\" = stdout)")
	timeout := fs.Duration("timeout", 15*time.Second, "operation timeout")
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if *format != "csv" && *format != "ascii" {
		return fmt.Errorf("--format must be csv or ascii, got %q", *format)
	}
	if *srce >= 0 && *dest >= 0 {
		return fmt.Errorf("--srce and --dest are mutually exclusive")
	}
	cctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	p, closer, err := dialProbel(cctx, addr)
	if err != nil {
		return err
	}
	defer closer()
	mtx, level := probelTarget(p, *matrix)
	res, err := p.CrosspointTallyDump(cctx, mtx, level)
	if err != nil {
		return err
	}
	var protect map[int]string
	if *withProtect {
		pres, perr := p.ProtectTallyDump(cctx, mtx, level, 0)
		if perr != nil {
			return perr
		}
		protect = probelUsageProtectMap(pres)
	}
	firstDst, srcs := probelTallyTable(res)
	rows := probelFilterUsage(probelBuildUsage(firstDst, srcs, int(level), protect), *srce, *dest)

	if *format == "ascii" {
		renderProbelUsageASCII(os.Stdout, rows)
		return nil
	}
	csv := formatProbelUsageCSV(rows, *withProtect)
	if *out == "-" {
		fmt.Print(csv)
		return nil
	}
	path := *out
	if path == "" {
		host, _, _ := splitHostPort(addr, probelproto.DefaultPort)
		path = filepath.Join(snapshotDir("probel-sw08p", host), "usage.csv")
		fmt.Fprintf(os.Stderr, "probel-sw08p usage: default snapshot file %s (ADR-0028)\n", path)
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(path, []byte(csv), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "probel-sw08p usage: wrote %s (%d row(s))\n", path, len(rows))
	return nil
}

// runProbelReplace drives `dhs consumer probel-sw08p replace` —
// substitute source A with B on every crosspoint carrying A (ADR-0007
// ensure: --check dry-run, diff-only apply, run-twice = 0).
func runProbelReplace(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probel-replace", flag.ContinueOnError)
	matrix := fs.Int("matrix", 0, "matrix id (0-255; global --mtx-id wins when set)")
	from := fs.Int("srce", -1, "source id to replace (required)")
	with := fs.Int("with", -1, "source id that takes over (required)")
	check := fs.Bool("check", false, "dry-run (ADR-0007): list the crosspoints that would change, send nothing")
	output := fs.String("output", "text", "output format: text | json (ADR-0002; json = {changed|would_change, diff[]})")
	timeout := fs.Duration("timeout", 30*time.Second, "operation timeout")
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
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
	cctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	p, closer, err := dialProbel(cctx, addr)
	if err != nil {
		return err
	}
	defer closer()
	logw := os.Stdout
	if jsonOut {
		logw = os.Stderr
	}
	mtx, level := probelTarget(p, *matrix)
	res, err := p.CrosspointTallyDump(cctx, mtx, level)
	if err != nil {
		return err
	}
	firstDst, srcs := probelTallyTable(res)
	cells := probelReplaceCells(probelBuildUsage(firstDst, srcs, int(level), nil), *from)

	changed := len(cells) > 0
	diffs := make([]ensureDiff, 0, len(cells))
	for _, c := range cells {
		diffs = append(diffs, ensureDiff{
			Field: fmt.Sprintf("route.%d.%d", c.Dst, c.Level),
			From:  fmt.Sprintf("%d", *from), To: fmt.Sprintf("%d", *with),
		})
	}

	if *check {
		for _, c := range cells {
			_, _ = fmt.Fprintf(logw, "[would-replace] matrix=%d level=%d dst=%d: src %d -> %d\n", mtx, c.Level, c.Dst, *from, *with)
		}
		_, _ = fmt.Fprintf(logw, "probel-sw08p replace --check: would_change=%d crosspoint(s) carrying src %d — nothing sent\n", len(cells), *from)
		if jsonOut {
			return emitEnsure(true, ensureResult{WouldChange: &changed, Diff: diffs})
		}
		return nil
	}

	fails := 0
	for _, c := range cells {
		if _, err := p.CrosspointConnect(cctx, mtx, level, uint16(c.Dst), uint16(*with)); err != nil {
			_, _ = fmt.Fprintf(logw, "[replace] FAIL matrix=%d level=%d dst=%d reason=%s\n", mtx, c.Level, c.Dst, err)
			fails++
			continue
		}
		_, _ = fmt.Fprintf(logw, "[replace] OK   matrix=%d level=%d dst=%d: src %d -> %d\n", mtx, c.Level, c.Dst, *from, *with)
	}
	if fails > 0 {
		return fmt.Errorf("%d/%d replace connect(s) failed", fails, len(cells))
	}
	_, _ = fmt.Fprintf(logw, "probel-sw08p replace: changed=%d crosspoint(s); run again to verify 0 (nothing carries src %d any more)\n", len(cells), *from)
	if jsonOut {
		return emitEnsure(true, ensureResult{Changed: &changed, Diff: diffs})
	}
	return nil
}
