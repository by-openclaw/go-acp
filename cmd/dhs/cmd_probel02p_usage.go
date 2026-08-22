package main

// usage + replace for SW-P-02 (#722, unit 2; canonical shape defined
// by cerebrum-nb #718, row/format helpers shared with probel-sw08p).
//
// SW-P-02 has no crosspoint tally dump, so usage sweeps one
// interrogate (rx 01, or rx 65 extended) per destination on the
// single configured (matrix, level); replace substitutes source A
// with B via one connect (rx 02 / rx 66) per affected dst (ADR-0007:
// --check first, diff-only, run-twice = 0). Extended forms
// auto-escalate whenever an id exceeds the narrow 0-1023 range (root
// CLAUDE.md scale rule); --extended forces them throughout.
//
// The destination count comes from the global --dsts matrix config;
// when unset, the router configuration request (rx 075) supplies the
// level's size.

import (
	"context"
	"flag"
	"fmt"
	"math/bits"
	"os"
	"path/filepath"
	"strconv"
	"time"

	probelsw02proto "dhs/internal/probel-sw02p/consumer"
)

// sw02NarrowOutOfRange is the tx 03 sentinel: Source 1023 = the
// interrogated destination does not exist (§3.2.5).
const sw02NarrowOutOfRange = 1023

// sw02RouterConfigDsts extracts the destination count for one level
// from the rx 075 response union. The level map is a 28-bit set;
// entries appear in bit-0 → bit-27 order for SET bits only, so the
// entry index is the popcount of the lower bits. Returns 0 when the
// level is absent from the map (or the union is empty).
func sw02RouterConfigDsts(rc probelsw02proto.RouterConfigResponse, level uint8) int {
	if level > 27 {
		return 0
	}
	pick := func(levelMap uint32, n int, dsts func(int) int) int {
		if levelMap&(1<<level) == 0 {
			return 0
		}
		idx := bits.OnesCount32(levelMap & ((1 << level) - 1))
		if idx >= n {
			return 0
		}
		return dsts(idx)
	}
	switch {
	case rc.Response1 != nil:
		return pick(rc.Response1.LevelMap, len(rc.Response1.Levels),
			func(i int) int { return int(rc.Response1.Levels[i].NumDestinations) })
	case rc.Response2 != nil:
		return pick(rc.Response2.LevelMap, len(rc.Response2.Levels),
			func(i int) int { return int(rc.Response2.Levels[i].NumDestinations) })
	}
	return 0
}

// runProbelSW02Usage drives `dhs consumer probel-sw02p usage`.
func runProbelSW02Usage(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probel-sw02p-usage", flag.ContinueOnError)
	srce := fs.Int("srce", -1, "filter: only this source's assignments")
	dest := fs.Int("dest", -1, "filter: only this destination's feed")
	extended := fs.Bool("extended", false, "force extended forms (rx 65) throughout")
	format := fs.String("format", "csv", "output format: csv | ascii")
	out := fs.String("out", "", "csv output file (omitted = snapshots/probel-sw02p/<host>/usage.csv per ADR-0028; \"-\" = stdout)")
	timeout := fs.Duration("timeout", 60*time.Second, "operation timeout (one interrogate per dst)")
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
	p, closer, err := dialProbelSW02(cctx, addr)
	if err != nil {
		return err
	}
	defer closer()
	count, level, err := sw02UsageDstCount(cctx, p)
	if err != nil {
		return err
	}
	rows, err := sw02Interrogations(cctx, p, count, level, *extended)
	if err != nil {
		return err
	}
	rows = probelFilterUsage(sortProbelUsage(rows), *srce, *dest)

	if *format == "ascii" {
		renderProbelUsageASCII(os.Stdout, rows)
		return nil
	}
	csv := formatProbelUsageCSV(rows, false, false)
	if *out == "-" {
		fmt.Print(csv)
		return nil
	}
	path := *out
	if path == "" {
		host, _, _ := splitHostPort(addr, probelsw02proto.DefaultPort)
		path = filepath.Join(snapshotDir("probel-sw02p", host), "usage.csv")
		fmt.Fprintf(os.Stderr, "probel-sw02p usage: default snapshot file %s (ADR-0028)\n", path)
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(path, []byte(csv), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "probel-sw02p usage: wrote %s (%d row(s))\n", path, len(rows))
	return nil
}

// sw02UsageDstCount resolves the sweep size: the global --dsts matrix
// config when set, else the router configuration (rx 075) size for
// the configured level. Also returns the level for row display.
func sw02UsageDstCount(ctx context.Context, p *probelsw02proto.Plugin) (count, level int, err error) {
	mc := p.MatrixConfig()
	level = int(mc.Level)
	if mc.Dsts > 0 {
		return int(mc.Dsts), level, nil
	}
	rc, rerr := p.SendRouterConfigRequest(ctx)
	if rerr != nil {
		return 0, level, fmt.Errorf("--dsts not set and router-config (rx 075) failed: %w", rerr)
	}
	if n := sw02RouterConfigDsts(rc, mc.Level); n > 0 {
		return n, level, nil
	}
	return 0, level, fmt.Errorf("--dsts not set and router-config reports no destinations on level %d", level)
}

// sw02Interrogations sweeps dsts 0..count-1 (rx 01, auto-escalating
// to rx 65 past the narrow range or when forced) into usage rows,
// skipping the narrow out-of-range sentinel.
func sw02Interrogations(ctx context.Context, p *probelsw02proto.Plugin, count, level int, extended bool) ([]probelUsageRow, error) {
	rows := make([]probelUsageRow, 0, count)
	for dst := 0; dst < count; dst++ {
		if extended || dst > sw02NarrowOutOfRange {
			t, err := p.SendExtendedInterrogate(ctx, uint16(dst))
			if err != nil {
				return nil, fmt.Errorf("extended interrogate dst %d: %w", dst, err)
			}
			rows = append(rows, probelUsageRow{Src: int(t.Source), Dst: int(t.Destination), Levels: strconv.Itoa(level)})
			continue
		}
		t, err := p.SendInterrogate(ctx, uint16(dst))
		if err != nil {
			return nil, fmt.Errorf("interrogate dst %d: %w", dst, err)
		}
		if t.Source == sw02NarrowOutOfRange {
			continue
		}
		rows = append(rows, probelUsageRow{Src: int(t.Source), Dst: int(t.Destination), Levels: strconv.Itoa(level)})
	}
	return rows, nil
}

// runProbelSW02Replace drives `dhs consumer probel-sw02p replace` —
// substitute source A with B on every destination carrying A
// (ADR-0007 ensure: --check dry-run, diff-only apply, run-twice = 0).
func runProbelSW02Replace(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probel-sw02p-replace", flag.ContinueOnError)
	from := fs.Int("srce", -1, "source id to replace (required)")
	with := fs.Int("with", -1, "source id that takes over (required)")
	extended := fs.Bool("extended", false, "force extended forms (rx 65/66) throughout")
	check := fs.Bool("check", false, "dry-run (ADR-0007): list the destinations that would change, send nothing")
	output := fs.String("output", "text", "output format: text | json (ADR-0002; json = {changed|would_change, diff[]})")
	timeout := fs.Duration("timeout", 60*time.Second, "operation timeout")
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
	rows, err := sw02Interrogations(cctx, p, count, level, *extended)
	if err != nil {
		return err
	}
	cells := probelReplaceCells(rows, *from)

	changed := len(cells) > 0
	diffs := make([]ensureDiff, 0, len(cells))
	for _, c := range cells {
		diffs = append(diffs, ensureDiff{
			Field: fmt.Sprintf("route.%d.%s", c.Dst, c.Levels),
			From:  fmt.Sprintf("%d", *from), To: fmt.Sprintf("%d", *with),
		})
	}

	if *check {
		for _, c := range cells {
			_, _ = fmt.Fprintf(logw, "[would-replace] level=%s dst=%d: src %d -> %d\n", c.Levels, c.Dst, *from, *with)
		}
		_, _ = fmt.Fprintf(logw, "probel-sw02p replace --check: would_change=%d destination(s) carrying src %d — nothing sent\n", len(cells), *from)
		if jsonOut {
			return emitEnsure(true, ensureResult{WouldChange: &changed, Diff: diffs})
		}
		return nil
	}

	fails := 0
	for _, c := range cells {
		var cerr error
		if *extended || c.Dst > sw02NarrowOutOfRange || *with > sw02NarrowOutOfRange {
			_, cerr = p.SendExtendedConnect(cctx, uint16(c.Dst), uint16(*with))
		} else {
			_, cerr = p.SendConnect(cctx, uint16(c.Dst), uint16(*with), false)
		}
		if cerr != nil {
			_, _ = fmt.Fprintf(logw, "[replace] FAIL level=%s dst=%d reason=%s\n", c.Levels, c.Dst, cerr)
			fails++
			continue
		}
		_, _ = fmt.Fprintf(logw, "[replace] OK   level=%s dst=%d: src %d -> %d\n", c.Levels, c.Dst, *from, *with)
	}
	if fails > 0 {
		return fmt.Errorf("%d/%d replace connect(s) failed", fails, len(cells))
	}
	_, _ = fmt.Fprintf(logw, "probel-sw02p replace: changed=%d destination(s); run again to verify 0 (nothing carries src %d any more)\n", len(cells), *from)
	if jsonOut {
		return emitEnsure(true, ensureResult{Changed: &changed, Diff: diffs})
	}
	return nil
}
