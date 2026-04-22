package main

// Diagnostic subcommand for controller-side salvo behavior. Mimics the
// VSM Studio batch-connect flow observed on the wire:
//
//   for each dst:  cmd 120 BUILD(matrix,level,dst,src,salvoID)  ← stage route
//   cmd 121 GO op=set  salvoID                                  ← apply atomically
//   cmd 121 GO op=clear salvoID                                 ← wipe staging
//
// Prints every tx 122 ack + tx 123 go-done + any async tallies seen
// while the salvo runs, so a single-dst run vs a many-dst run can be
// compared side by side. Use against our own provider (127.0.0.1:2008)
// or any SW-P-08 matrix to reproduce / bisect the UI-mismatch symptom
// reported on VSM for batch connects.

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"acp/internal/probel-sw08p/codec"
)

func runProbelSalvoConnect(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probel-salvo-connect", flag.ContinueOnError)
	var (
		matrix  = fs.Int("matrix", 0, "matrix id (0-15)")
		level   = fs.Int("level", 0, "level id (0-15)")
		src     = fs.Int("src", 0, "source id to route to every dst")
		dstsCSV = fs.String("dsts", "", "destination ids (CSV or N-M range, e.g. '5' or '1,2,3' or '0-7')")
		salvoID = fs.Int("salvo", 1, "salvo group id (0-127)")
		clear   = fs.Bool("clear", true, "send cmd 121 op=clear after op=set to wipe the stage buffer")
		wait    = fs.Duration("wait", 500*time.Millisecond, "extra time to wait for async tallies after go-done")
		timeout = fs.Duration("timeout", 5*time.Second, "overall operation timeout")
	)
	addr, rest, err := splitPositional(args, "salvo-connect")
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	dsts, err := parseDsts(*dstsCSV)
	if err != nil {
		return err
	}
	if len(dsts) == 0 {
		return fmt.Errorf("--dsts is required (e.g. --dsts 5 or --dsts 0-7)")
	}

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	plugin, closeFn, err := dialProbel(ctx, addr)
	if err != nil {
		return err
	}
	defer closeFn()

	cli, err := plugin.ExposeClient()
	if err != nil {
		return err
	}

	// Subscribe to every async frame so we can print tallies and extras.
	var tallyCount atomic.Int32
	var cxCount atomic.Int32
	var seen []string
	var seenMu sync.Mutex
	cli.Subscribe(func(f codec.Frame) {
		seenMu.Lock()
		defer seenMu.Unlock()
		switch f.ID {
		case codec.TxCrosspointTally:
			tallyCount.Add(1)
			p, _ := codec.DecodeCrosspointTally(f)
			seen = append(seen, fmt.Sprintf("  async tx 003 Tally mtx=%d lvl=%d dst=%d src=%d",
				p.MatrixID, p.LevelID, p.DestinationID, p.SourceID))
		case codec.TxCrosspointConnected:
			cxCount.Add(1)
			p, _ := codec.DecodeCrosspointTally(codec.Frame{ID: codec.TxCrosspointTally, Payload: f.Payload})
			seen = append(seen, fmt.Sprintf("  async tx 004 Connected mtx=%d lvl=%d dst=%d src=%d",
				p.MatrixID, p.LevelID, p.DestinationID, p.SourceID))
		}
	})

	fmt.Printf("\n=== salvo-connect: matrix=%d level=%d src=%d salvo=%d dsts=%v ===\n\n",
		*matrix, *level, *src, *salvoID, dsts)

	// Phase 1: N × SALVO-BUILD
	fmt.Printf("phase 1 — SALVO-BUILD × %d (one cmd 120 per dst)\n", len(dsts))
	start := time.Now()
	for _, dst := range dsts {
		ack, err := plugin.SalvoConnectOnGo(ctx, codec.SalvoConnectOnGoParams{
			MatrixID:      uint8(*matrix),
			LevelID:       uint8(*level),
			DestinationID: uint16(dst),
			SourceID:      uint16(*src),
			SalvoID:       uint8(*salvoID),
		})
		if err != nil {
			return fmt.Errorf("salvo-build dst=%d: %w", dst, err)
		}
		fmt.Printf("  dst=%-3d → tx 122 ack dst=%d src=%d salvo=%d\n",
			dst, ack.DestinationID, ack.SourceID, ack.SalvoID)
	}
	phase1 := time.Since(start)

	// Phase 2: SALVO-GO op=set
	fmt.Printf("\nphase 2 — SALVO-GO op=set salvo=%d\n", *salvoID)
	start = time.Now()
	setAck, err := plugin.SalvoGo(ctx, codec.SalvoGoParams{
		Op: codec.SalvoOpSet, SalvoID: uint8(*salvoID),
	})
	if err != nil {
		return fmt.Errorf("salvo-go set: %w", err)
	}
	phase2 := time.Since(start)
	fmt.Printf("  tx 123 go-done status=%d (%s) salvo=%d\n",
		setAck.Status, statusName(setAck.Status), setAck.SalvoID)

	// Wait a bit for any async tallies that may arrive after the go-done.
	time.Sleep(*wait)

	// Phase 3: SALVO-GO op=clear
	var phase3 time.Duration
	if *clear {
		fmt.Printf("\nphase 3 — SALVO-GO op=clear salvo=%d\n", *salvoID)
		start = time.Now()
		clearAck, err := plugin.SalvoGo(ctx, codec.SalvoGoParams{
			Op: codec.SalvoOpClear, SalvoID: uint8(*salvoID),
		})
		if err != nil {
			return fmt.Errorf("salvo-go clear: %w", err)
		}
		phase3 = time.Since(start)
		fmt.Printf("  tx 123 go-done status=%d (%s) salvo=%d\n",
			clearAck.Status, statusName(clearAck.Status), clearAck.SalvoID)
	}

	fmt.Printf("\n=== summary ===\n")
	fmt.Printf("  phase 1 (%d BUILDs):  %s\n", len(dsts), phase1.Round(time.Microsecond))
	fmt.Printf("  phase 2 (GO set):    %s\n", phase2.Round(time.Microsecond))
	if *clear {
		fmt.Printf("  phase 3 (GO clear):  %s\n", phase3.Round(time.Microsecond))
	}
	fmt.Printf("  async tx 003 Tally frames observed:    %d\n", tallyCount.Load())
	fmt.Printf("  async tx 004 Connected frames observed: %d\n", cxCount.Load())
	if tallyCount.Load() == 0 && cxCount.Load() == 0 {
		fmt.Printf("\n  NOTE: zero async state-change frames after the go-done.\n")
		fmt.Printf("  Matrix accepts the salvo but emits no per-slot confirmation —\n")
		fmt.Printf("  this is the root cause of the 'batch connect UI mismatch' symptom.\n")
	}

	seenMu.Lock()
	if len(seen) > 0 {
		fmt.Printf("\n  async frames (in order):\n")
		for _, s := range seen {
			fmt.Println(s)
		}
	}
	seenMu.Unlock()

	return nil
}

func statusName(s codec.SalvoGoDoneStatus) string {
	switch s {
	case codec.SalvoDoneSet:
		return "Set"
	case codec.SalvoDoneCleared:
		return "Cleared"
	case codec.SalvoDoneNone:
		return "None"
	}
	return fmt.Sprintf("0x%02x", byte(s))
}

func parseDsts(s string) ([]int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	// Allow N-M ranges in addition to CSV ("0-7" or "1,2,3" or mixed).
	out := []int{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			a, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
			if err != nil {
				return nil, fmt.Errorf("bad dst range %q: %w", part, err)
			}
			b, err := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err != nil {
				return nil, fmt.Errorf("bad dst range %q: %w", part, err)
			}
			if a > b {
				a, b = b, a
			}
			for i := a; i <= b; i++ {
				out = append(out, i)
			}
		} else {
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("bad dst %q: %w", part, err)
			}
			out = append(out, n)
		}
	}
	return out, nil
}

func splitPositional(args []string, verb string) (addr string, rest []string, err error) {
	if len(args) == 0 {
		return "", nil, fmt.Errorf("%s: missing <host:port>", verb)
	}
	return args[0], args[1:], nil
}
