package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	emberplus "dhs/internal/emberplus/consumer"
)

// runEmberplusBench drives N MatrixConnect ops over one TCP session.
// Start timer → loop → stop timer → print elapsed. No percentiles, no
// CSV — the future "real router behind us" comparison only needs the
// total time anyway.
func runEmberplusBench(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("emberplus-bench", flag.ExitOnError)
	cf := addCommonFlags(fs)
	matrixPath := fs.String("path", "", "matrix path (e.g. dhs-emberplus-integration.nToN.matrix)")
	dmIdentity := fs.String("dm", "", `Ember+ DM identity for hot-load (e.g. "dhs-emberplus-integration@1.0.0").`)
	n := fs.Int("n", 100, "number of crosspoint operations")
	op := fs.String("op", "connect", "operation per op: connect | absolute | disconnect")
	targets := fs.Int("targets", 4, "wrap targets in [0, N) — defaults to nToN size")
	sources := fs.Int("sources", 4, "wrap sources in [0, N) — defaults to nToN size")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer emberplus bench <host> --port N --path <matrix.path> --dm <id> [--n 100] [--op connect|absolute|disconnect] [--targets N] [--sources N]")
	}
	_ = fs.Parse(rest)

	if *matrixPath == "" {
		return fmt.Errorf("--path is required")
	}
	if *n <= 0 {
		return fmt.Errorf("--n must be > 0")
	}
	operation, oerr := parseConnectionOp(*op)
	if oerr != nil {
		return oerr
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	// Reject non-Ember+ before walking — bench is Ember+ only and
	// MatrixConnect needs the concrete plugin type anyway. Same pattern
	// as cmd_matrix.go / cmd_invoke.go.
	ep, ok := plug.(*emberplus.Plugin)
	if !ok {
		return fmt.Errorf("bench is Ember+ only")
	}

	if err := ensureEmberplusTree(ctx, plug, host, cf.port, *dmIdentity, 0, false); err != nil {
		return err
	}

	fmt.Printf("bench: matrix=%s op=%s n=%d (tgt mod %d, src mod %d)\n",
		*matrixPath, *op, *n, *targets, *sources)

	t0 := time.Now()
	errs := 0
	for i := 0; i < *n; i++ {
		tgt := int32(i % *targets)
		src := int32(i % *sources)
		if err := ep.MatrixConnect(ctx, *matrixPath, tgt, []int32{src}, operation); err != nil {
			errs++
		}
	}
	wall := time.Since(t0)
	fmt.Printf("done: n=%d errs=%d wall=%s  -> %.1f ops/sec  (avg %s/op)\n",
		*n, errs, wall.Round(time.Millisecond),
		float64(*n)/wall.Seconds(),
		(wall / time.Duration(*n)).Round(time.Microsecond))
	return nil
}

// parseConnectionOp maps the CLI string to the spec p.89 ConnectionOperation
// wire constant.
func parseConnectionOp(s string) (int64, error) {
	switch s {
	case "absolute", "abs", "set":
		return 0, nil
	case "connect", "add":
		return 1, nil
	case "disconnect", "remove", "rm":
		return 2, nil
	}
	return 0, fmt.Errorf("--op %q: must be connect | absolute | disconnect", s)
}
