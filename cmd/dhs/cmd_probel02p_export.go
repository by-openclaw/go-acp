package main

// export for SW-P-02 (#738 unit 1) — the router-pack file-set this
// wire can produce:
//
//	-matrix.csv   ADR-0023 descriptor (sizes from the global --dsts/
//	              --srcs config or the rx 075 router configuration)
//	-xpoint.csv   dest,srce,levels — the CANONICAL family grammar
//	              (cerebrum-nb / ember+), built from the same rx 01/65
//	              interrogate sweep the usage verb uses
//
// No label files: SW-P-02 has no name commands (omit-don't-fake — an
// absent file states the wire lacks the concept). No category/lock
// files for the same reason.

import (
	"context"
	"flag"
	"fmt"
	"math/bits"
	"os"
	"strconv"
	"time"

	probelsw02proto "dhs/internal/probel-sw02p/consumer"
)

// sw02RouterConfigSizes extracts (destinations, sources) for one level
// from the rx 075 response union — same level-map convention as
// sw02RouterConfigDsts (entries appear for SET bits in bit order).
func sw02RouterConfigSizes(rc probelsw02proto.RouterConfigResponse, level uint8) (dsts, srcs int) {
	dsts = sw02RouterConfigDsts(rc, level)
	if level > 27 {
		return dsts, 0
	}
	pick := func(levelMap uint32, n int, val func(int) int) int {
		if levelMap&(1<<level) == 0 {
			return 0
		}
		idx := bits.OnesCount32(levelMap & ((1 << level) - 1))
		if idx >= n {
			return 0
		}
		return val(idx)
	}
	switch {
	case rc.Response1 != nil:
		srcs = pick(rc.Response1.LevelMap, len(rc.Response1.Levels),
			func(i int) int { return int(rc.Response1.Levels[i].NumSources) })
	case rc.Response2 != nil:
		srcs = pick(rc.Response2.LevelMap, len(rc.Response2.Levels),
			func(i int) int { return int(rc.Response2.Levels[i].NumSources) })
	}
	return dsts, srcs
}

// runProbelSW02Export drives `dhs consumer probel-sw02p export`.
func runProbelSW02Export(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probel-sw02p-export", flag.ContinueOnError)
	outDir := fs.String("out", "", "output directory (omitted = snapshots/probel-sw02p/<host>/ with plain facet names, ADR-0028)")
	prefix := fs.String("prefix", "sw02p", "CSV filename prefix (ignored in the default snapshot folder)")
	extended := fs.Bool("extended", false, "force extended forms (rx 65) throughout")
	timeout := fs.Duration("timeout", 120*time.Second, "overall timeout (one interrogate per dst)")
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return fmt.Errorf("missing <host:port>")
	}
	if err := parseVerbFlags(fs, flagArgs); err != nil {
		return err
	}
	if *outDir == "" {
		*outDir = snapshotDir("probel-sw02p", hostOnly(addr))
		*prefix = ""
		fmt.Fprintf(os.Stderr, "probel-sw02p export: default snapshot folder %s (ADR-0028)\n", *outDir)
	}
	cctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	p, closer, err := dialProbelSW02(cctx, addr)
	if err != nil {
		return err
	}
	defer closer()

	mc := p.MatrixConfig()
	count, level, err := sw02UsageDstCount(cctx, p)
	if err != nil {
		return err
	}
	srcs := int(mc.Srcs)
	if srcs == 0 {
		if rc, rerr := p.SendRouterConfigRequest(cctx); rerr == nil {
			_, srcs = sw02RouterConfigSizes(rc, mc.Level)
		}
	}

	rows, err := sw02Interrogations(cctx, p, count, level, *extended)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", *outDir, err)
	}

	// -matrix.csv — ADR-0023 descriptor. SW-P-02 routes one source per
	// destination: behavior 1toN.
	desc := matrixDesc{
		Matrix:   strconv.Itoa(int(mc.MatrixID)),
		Behavior: "1toN",
		Targets:  count,
		Sources:  srcs,
	}
	descPath := facetFile(*outDir, *prefix, "matrix")
	if err := os.WriteFile(descPath, []byte(formatMatrixDescCSV([]matrixDesc{desc})), 0o644); err != nil {
		return err
	}

	// -xpoint.csv — canonical family grammar (dest,srce,levels).
	xrows := make([]cerebrumXpointRow, 0, len(rows))
	for _, r := range rows {
		xrows = append(xrows, cerebrumXpointRow{
			Dest:   strconv.Itoa(r.Dst),
			Srce:   strconv.Itoa(r.Src),
			Levels: []string{r.Levels},
		})
	}
	xpPath := facetFile(*outDir, *prefix, "xpoint")
	if err := os.WriteFile(xpPath, []byte(formatCerebrumXpointCSV(xrows)), 0o644); err != nil {
		return err
	}

	fmt.Printf("exported matrix=%d level=%d:\n  %s  (descriptor %dx%d)\n  %s  (%d crosspoint(s))\n  (no label/category files — SW-P-02 has no name or category commands)\n",
		mc.MatrixID, level, descPath, count, srcs, xpPath, len(xrows))
	return writePackMeta(*outDir, "probel-sw02p", hostOnly(addr))
}
