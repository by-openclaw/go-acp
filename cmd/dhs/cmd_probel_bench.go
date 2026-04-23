package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// runProbelBench drives a persistent-TCP scale benchmark against a
// running Probel SW-P-08 provider. Two phases, both optional:
//
//   - interrogate: CrosspointInterrogate(matrix, level=0, dst) for every
//     dst in [0, size), across every matrix id in --matrix
//   - connect:     CrosspointConnect(matrix, level=0, dst, src=1+dst/16)
//     — 16 destinations fan out to one source; 4096 distinct sources
//     used per matrix when size=65535.
//
// One TCP connection is held open for the entire run (no per-op
// connect/disconnect cost), so measured latency is framing + handler +
// ACK RTT per op.
//
// See memory/project_scale_bench_2mtx_65535.md for scope + expected
// numbers and the broader `project_scale_requirements.md` baseline.
func runProbelBench(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probel-bench", flag.ContinueOnError)
	var (
		phase      = fs.String("phase", "both", "interrogate|connect|both")
		matrixCSV  = fs.String("matrix", "0,1", "matrix ids (comma-separated)")
		size       = fs.Int("size", 65535, "dst range [0, size)")
		csvPath    = fs.String("csv", "", "write per-op latency rows to CSV")
		mdPath     = fs.String("md", "", "write summary table to MD")
		progress   = fs.Int("progress", 5000, "print progress every N ops (0 = silent)")
		timeout    = fs.Duration("timeout", 30*time.Minute, "overall timeout")
		warmupDst  = fs.Int("warmup-dst", 0, "dst used for a single warmup op before phases start")
		skipWarmup = fs.Bool("skip-warmup", false, "skip the warmup op")
	)
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}

	matrices, err := parseMatrixList(*matrixCSV)
	if err != nil {
		return err
	}
	if *size <= 0 || *size > 65536 {
		return fmt.Errorf("--size out of range (1-65536)")
	}
	if *phase != "interrogate" && *phase != "connect" && *phase != "both" {
		return fmt.Errorf("--phase must be interrogate|connect|both")
	}

	bctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	p, closer, err := dialProbel(bctx, addr)
	if err != nil {
		return err
	}
	defer closer()

	// Warmup: one interrogate so the first real op doesn't eat TLS-handshake-
	// equivalent setup cost. Not counted.
	if !*skipWarmup {
		if _, werr := p.CrosspointInterrogate(bctx, matrices[0], 0, uint16(*warmupDst)); werr != nil {
			fmt.Fprintf(os.Stderr, "warmup: %v (continuing)\n", werr)
		}
	}

	results := map[string]*phaseResult{}
	overallStart := time.Now()

	if *phase == "interrogate" || *phase == "both" {
		results["interrogate"] = runBenchPhase("interrogate", matrices, *size, *progress,
			func(m uint8, dst int) (time.Duration, error) {
				start := time.Now()
				_, err := p.CrosspointInterrogate(bctx, m, 0, uint16(dst))
				return time.Since(start), err
			})
	}

	if *phase == "connect" || *phase == "both" {
		results["connect"] = runBenchPhase("connect", matrices, *size, *progress,
			func(m uint8, dst int) (time.Duration, error) {
				src := uint16(1 + dst/16)
				start := time.Now()
				_, err := p.CrosspointConnect(bctx, m, 0, uint16(dst), src)
				return time.Since(start), err
			})
	}

	overall := time.Since(overallStart)

	// Stdout summary
	fmt.Println()
	fmt.Println("=== Bench summary ===")
	for _, name := range []string{"interrogate", "connect"} {
		if r, ok := results[name]; ok {
			fmt.Println(r.oneLine(name))
		}
	}
	fmt.Printf("overall wall: %s  (matrices=%v size=%d)\n",
		overall.Round(time.Millisecond), matrices, *size)

	if *csvPath != "" {
		if err := writeBenchCSV(*csvPath, results); err != nil {
			return fmt.Errorf("write csv: %w", err)
		}
		fmt.Fprintf(os.Stderr, "wrote %s\n", *csvPath)
	}
	if *mdPath != "" {
		if err := writeBenchMD(*mdPath, results, matrices, *size, overall); err != nil {
			return fmt.Errorf("write md: %w", err)
		}
		fmt.Fprintf(os.Stderr, "wrote %s\n", *mdPath)
	}
	return nil
}

// phaseResult captures one phase's outcome.
type phaseResult struct {
	ops     int
	errs    int
	wall    time.Duration
	latency []time.Duration // sorted ascending after runBenchPhase
}

func (r *phaseResult) oneLine(name string) string {
	if len(r.latency) == 0 {
		return fmt.Sprintf("%-12s n=%d errors=%d wall=%s", name, r.ops, r.errs, r.wall)
	}
	return fmt.Sprintf(
		"%-12s n=%d errors=%d wall=%s  min=%s p50=%s p95=%s p99=%s max=%s  mean-op=%s",
		name, r.ops, r.errs, r.wall.Round(time.Millisecond),
		r.latency[0],
		pickPct(r.latency, 0.50),
		pickPct(r.latency, 0.95),
		pickPct(r.latency, 0.99),
		r.latency[len(r.latency)-1],
		r.wall/time.Duration(len(r.latency)),
	)
}

// runBenchPhase iterates every (matrix, dst) pair calling fn, accumulating
// wall-clock durations. Progress logs go to stdout every N ops.
func runBenchPhase(name string, matrices []uint8, size, progress int,
	fn func(m uint8, dst int) (time.Duration, error)) *phaseResult {

	total := len(matrices) * size
	fmt.Printf("bench: %s — %d matrix × %d dst = %d ops\n", name, len(matrices), size, total)
	start := time.Now()
	lats := make([]time.Duration, 0, total)
	errs := 0
	done := 0
	for _, m := range matrices {
		for dst := 0; dst < size; dst++ {
			lat, err := fn(m, dst)
			done++
			if err != nil {
				errs++
				continue
			}
			lats = append(lats, lat)
			if progress > 0 && done%progress == 0 {
				fmt.Printf("  ... %d / %d  (%.1f%% ; elapsed %s)\n",
					done, total, 100*float64(done)/float64(total),
					time.Since(start).Round(time.Millisecond))
			}
		}
	}
	wall := time.Since(start)
	sort.Slice(lats, func(i, j int) bool { return lats[i] < lats[j] })
	return &phaseResult{ops: done, errs: errs, wall: wall, latency: lats}
}

// pickPct returns the q-quantile from a sorted slice.
// Nearest-rank (inclusive) — matches the `metrics.Connector` convention.
func pickPct(sorted []time.Duration, q float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * q)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// parseMatrixList turns "0,1,3" into []uint8{0, 1, 3}.
func parseMatrixList(csv string) ([]uint8, error) {
	out := []uint8{}
	for _, tok := range strings.Split(csv, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		n, err := strconv.Atoi(tok)
		if err != nil || n < 0 || n > 255 {
			return nil, fmt.Errorf("bad matrix id %q", tok)
		}
		out = append(out, uint8(n))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--matrix: no ids parsed")
	}
	return out, nil
}

// writeBenchCSV dumps every measured latency into a CSV with header
// "phase,index,nanos". One row per op — can be 130 k+ rows.
func writeBenchCSV(path string, results map[string]*phaseResult) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"phase", "index", "nanos"}); err != nil {
		return err
	}
	for _, name := range []string{"interrogate", "connect"} {
		r, ok := results[name]
		if !ok {
			continue
		}
		for i, d := range r.latency {
			if err := w.Write([]string{name, strconv.Itoa(i), strconv.FormatInt(d.Nanoseconds(), 10)}); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeBenchMD emits a human summary table — the companion to the CSV.
func writeBenchMD(path string, results map[string]*phaseResult, matrices []uint8, size int, overall time.Duration) error {
	var b strings.Builder
	b.WriteString("# Probel scale bench\n\n")
	fmt.Fprintf(&b, "Generated %s — matrices=%v size=%d\n\n",
		time.Now().Format(time.RFC3339), matrices, size)

	b.WriteString("| Phase | n | errors | wall | mean/op | p50 | p95 | p99 | min | max |\n")
	b.WriteString("|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, name := range []string{"interrogate", "connect"} {
		r, ok := results[name]
		if !ok {
			continue
		}
		mean := time.Duration(0)
		if len(r.latency) > 0 {
			mean = r.wall / time.Duration(len(r.latency))
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %s | %s | %s | %s | %s | %s | %s |\n",
			name, r.ops, r.errs,
			r.wall.Round(time.Millisecond),
			mean,
			pickPct(r.latency, 0.50),
			pickPct(r.latency, 0.95),
			pickPct(r.latency, 0.99),
			firstOr(r.latency, 0),
			lastOr(r.latency, 0),
		)
	}
	fmt.Fprintf(&b, "\n**Overall wall**: %s\n", overall.Round(time.Millisecond))
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func firstOr(s []time.Duration, def time.Duration) time.Duration {
	if len(s) == 0 {
		return def
	}
	return s[0]
}

func lastOr(s []time.Duration, def time.Duration) time.Duration {
	if len(s) == 0 {
		return def
	}
	return s[len(s)-1]
}
