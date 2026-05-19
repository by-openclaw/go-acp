package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"time"

	emberplus "dhs/internal/emberplus/consumer"
)

// runEmberplusBench drives N MatrixConnect ops over one TCP session and
// captures per-op latency. Per R13 #474, follows the RFC 2544 / 8219
// methodology adapted for synchronous RPC: controlled workload, per-op
// tail-latency capture (p50/p95/p99), CSV output, and a recovery-time
// phase on the rfc2544-recovery profile.
func runEmberplusBench(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("emberplus-bench", flag.ExitOnError)
	cf := addCommonFlags(fs)
	matrixPath := fs.String("path", "", "matrix path: dotted label OR numeric OID (e.g. dhs-emberplus-integration.nToN.matrix or 1.2)")
	dmIdentity := fs.String("dm", "", `Ember+ DM identity for hot-load (e.g. "dhs-emberplus-integration@1.0.0").`)
	n := fs.Int("n", 100, "number of crosspoint operations")
	op := fs.String("op", "connect", "operation per op: connect | absolute | disconnect")
	targets := fs.Int("targets", 4, "wrap targets in [0, N) — defaults to nToN size")
	sources := fs.Int("sources", 4, "wrap sources in [0, N) — defaults to nToN size")
	profile := fs.String("profile", "", "R13 #474: named bench profile. One of rfc2544-throughput (n=10000, op=connect, fast as possible), rfc2544-latency (n=1000, op=absolute, measures per-op tail), rfc2544-recovery (n=500, op=disconnect after burst, measures post-burst recovery time). Profile overrides --n and --op. Empty = use --n/--op directly.")
	csvPath := fs.String("csv", "", "R13 #474: append one summary row (profile, n, op, wall_ms, ops_per_sec, errors, p50_us, p95_us, p99_us, recovery_ms) to this CSV file. `-` writes to stdout. Empty = no CSV.")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer emberplus bench <host> --port N --path <matrix.path> --dm <id> [--n 100] [--op connect|absolute|disconnect] [--targets N] [--sources N] [--profile NAME] [--csv FILE]")
	}
	_ = fs.Parse(rest)

	if *matrixPath == "" {
		return fmt.Errorf("--path is required")
	}
	if err := validatePathOrOID(*matrixPath); err != nil {
		return err
	}
	// R13 #474 v1: named profile dispatch — overrides --n / --op when set.
	if *profile != "" {
		pn, pop, perr := resolveBenchProfile(*profile)
		if perr != nil {
			return perr
		}
		*n = pn
		*op = pop
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

	ep, ok := plug.(*emberplus.Plugin)
	if !ok {
		return fmt.Errorf("bench is Ember+ only")
	}

	if err := ensureEmberplusTree(ctx, plug, host, cf.port, *dmIdentity, 0, false); err != nil {
		return err
	}

	fmt.Printf("bench: matrix=%s op=%s n=%d (tgt mod %d, src mod %d)\n",
		*matrixPath, *op, *n, *targets, *sources)

	// R13 #474: per-op latency capture. Pre-allocate so the slice append
	// during the timed loop never reallocates (the realloc would inflate
	// tail latencies on power-of-two boundaries).
	latencies := make([]time.Duration, 0, *n)
	errs := 0

	t0 := time.Now()
	for i := 0; i < *n; i++ {
		tgt := int32(i % *targets)
		src := int32(i % *sources)
		opStart := time.Now()
		if err := ep.MatrixConnect(ctx, *matrixPath, tgt, []int32{src}, operation); err != nil {
			errs++
		}
		latencies = append(latencies, time.Since(opStart))
	}
	wall := time.Since(t0)

	// Sort the slice in place for percentile computation. nil-safe but
	// expects non-zero len here (we guarded n > 0 above).
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p50 := percentile(latencies, 50)
	p95 := percentile(latencies, 95)
	p99 := percentile(latencies, 99)

	// R13 #474 rfc2544-recovery profile: measure time-to-first-success
	// after the burst loop completes. The provider may queue inbound
	// frames during a high-throughput burst; recovery time tells the
	// operator how long it takes to drain before normal traffic resumes.
	var recoveryMs int64 = -1 // -1 sentinel = not measured for this profile
	if *profile == "rfc2544-recovery" {
		recoveryMs = measureRecovery(ctx, ep, *matrixPath, *targets, *sources, operation)
	}

	fmt.Printf("done: n=%d errs=%d wall=%s  -> %.1f ops/sec  (avg %s/op)\n",
		*n, errs, wall.Round(time.Millisecond),
		float64(*n)/wall.Seconds(),
		(wall / time.Duration(*n)).Round(time.Microsecond))
	fmt.Printf("latency: p50=%s p95=%s p99=%s\n",
		p50.Round(time.Microsecond),
		p95.Round(time.Microsecond),
		p99.Round(time.Microsecond))
	if recoveryMs >= 0 {
		fmt.Printf("recovery: %d ms (time-to-first-success after burst)\n", recoveryMs)
	}

	if *csvPath != "" {
		if err := writeEmberplusBenchCSV(*csvPath, emberplusBenchCSVRow{
			Profile:    *profile,
			N:          *n,
			Op:         *op,
			WallMs:     wall.Milliseconds(),
			OpsPerSec:  float64(*n) / wall.Seconds(),
			Errors:     errs,
			P50us:      p50.Microseconds(),
			P95us:      p95.Microseconds(),
			P99us:      p99.Microseconds(),
			RecoveryMs: recoveryMs,
		}); err != nil {
			return fmt.Errorf("csv write: %w", err)
		}
	}
	return nil
}

// percentile returns the value at the p-th percentile from a sorted
// slice. Uses nearest-rank: at p=50 with 100 samples, returns
// sorted[50]. p out of range returns the closest bound.
func percentile(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	idx := (p * len(sorted)) / 100
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// measureRecovery probes the provider with a single connect immediately
// after a burst loop and returns how many milliseconds elapsed before
// the call returned successfully. Returns -1 if every attempt within
// the 10-second budget errored — the device may have stayed in a bad
// state.
func measureRecovery(ctx context.Context, ep *emberplus.Plugin, matrixPath string, targets, sources int, operation int64) int64 {
	deadline := time.Now().Add(10 * time.Second)
	probeStart := time.Now()
	tgt := int32(0)
	src := int32(0)
	if targets > 0 {
		tgt = int32(targets - 1)
	}
	if sources > 0 {
		src = int32(sources - 1)
	}
	for time.Now().Before(deadline) {
		err := ep.MatrixConnect(ctx, matrixPath, tgt, []int32{src}, operation)
		if err == nil {
			return time.Since(probeStart).Milliseconds()
		}
	}
	return -1
}

// emberplusBenchCSVRow is the schema R13 #474 emits to the --csv target. Fields
// stay in this order so historical CSV files concatenate cleanly even
// as new columns get appended.
type emberplusBenchCSVRow struct {
	Profile    string
	N          int
	Op         string
	WallMs     int64
	OpsPerSec  float64
	Errors     int
	P50us      int64
	P95us      int64
	P99us      int64
	RecoveryMs int64
}

// writeEmberplusBenchCSV appends one row to path. Writes a header line when the
// file is being created. `-` writes to stdout with no header
// (operators typically pipe to a collector that adds its own).
func writeEmberplusBenchCSV(path string, r emberplusBenchCSVRow) error {
	header := []string{"profile", "n", "op", "wall_ms", "ops_per_sec", "errors", "p50_us", "p95_us", "p99_us", "recovery_ms"}
	row := []string{
		r.Profile,
		strconv.Itoa(r.N),
		r.Op,
		strconv.FormatInt(r.WallMs, 10),
		strconv.FormatFloat(r.OpsPerSec, 'f', 2, 64),
		strconv.Itoa(r.Errors),
		strconv.FormatInt(r.P50us, 10),
		strconv.FormatInt(r.P95us, 10),
		strconv.FormatInt(r.P99us, 10),
		strconv.FormatInt(r.RecoveryMs, 10),
	}
	if path == "-" {
		w := csv.NewWriter(os.Stdout)
		_ = w.Write(row)
		w.Flush()
		return w.Error()
	}
	// Append; write header only on first creation.
	_, err := os.Stat(path)
	exists := err == nil
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	w := csv.NewWriter(f)
	if !exists {
		if err := w.Write(header); err != nil {
			return err
		}
	}
	if err := w.Write(row); err != nil {
		return err
	}
	w.Flush()
	return w.Error()
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
