package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"dhs/internal/protocol"
)

func runHealth(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("health", flag.ExitOnError)
	cf := addCommonFlags(fs)
	asJSON := fs.Bool("json", false, "emit JSON instead of text")
	watch := fs.Bool("watch", false, "poll until ctrl-C; emit a row whenever any of (reachable, connected, live) flips")
	interval := fs.Duration("interval", 5*time.Second, "poll interval when --watch is set")

	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer <proto> health <host>")
	}
	_ = fs.Parse(rest)

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	checker, ok := plug.(protocol.HealthChecker)
	if !ok {
		return fmt.Errorf("health: protocol %q does not implement HealthChecker yet", cf.protocol)
	}

	if !*watch {
		opCtx, cancel := withTimeout(ctx, cf.timeout)
		defer cancel()
		snap := checker.SessionHealth(opCtx)
		printHealthBuf(os.Stdout, host, cf.protocol, snap, *asJSON)
		return nil
	}

	// --watch: poll on interval, emit row on bit-flip. Initial reading
	// always prints; subsequent prints only on transition.
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	var prev *protocol.SessionHealth
	emit := func() {
		opCtx, cancel := withTimeout(ctx, cf.timeout)
		snap := checker.SessionHealth(opCtx)
		cancel()
		if prev == nil || healthFlipped(*prev, snap) {
			printHealthBuf(os.Stdout, host, cf.protocol, snap, *asJSON)
		}
		prev = &snap
	}
	emit()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			emit()
		}
	}
}

func helpHealth() {
	fmt.Println(`usage: dhs consumer <proto> health <host> [flags]

Print the 3-layer session health snapshot:
  reachable  network path to the device exists
  connected  active session is open
  live       device responded within the per-protocol staleness window

flags:
  --json              emit JSON instead of text
  --watch             poll until ctrl-C; print a row only when a bit flips
  --interval <d>      poll interval (default 5s, --watch only)

Common flags from --help apply (--protocol, --transport, --port, ...).`)
}

func printHealthBuf(w io.Writer, host, proto string, h protocol.SessionHealth, asJSON bool) {
	if asJSON {
		_ = json.NewEncoder(w).Encode(struct {
			Host       string `json:"host"`
			Proto      string `json:"proto"`
			Reachable  bool   `json:"reachable"`
			Connected  bool   `json:"connected"`
			Live       bool   `json:"live"`
			LastRx     string `json:"last_rx,omitempty"`
			LastTx     string `json:"last_tx,omitempty"`
			StaleAfter string `json:"stale_after,omitempty"`
		}{
			Host:       host,
			Proto:      proto,
			Reachable:  h.Reachable,
			Connected:  h.Connected,
			Live:       h.Live,
			LastRx:     timestampOrEmpty(h.LastRx),
			LastTx:     timestampOrEmpty(h.LastTx),
			StaleAfter: durationOrEmpty(h.StaleAfter),
		})
		return
	}
	_, _ = fmt.Fprintf(w, "host=%s proto=%s\n", host, proto)
	_, _ = fmt.Fprintf(w, "  reachable=%-5v\n", h.Reachable)
	_, _ = fmt.Fprintf(w, "  connected=%-5v\n", h.Connected)
	_, _ = fmt.Fprintf(w, "  live=%-5v %s\n", h.Live, liveContext(h))
	if !h.LastRx.IsZero() {
		_, _ = fmt.Fprintf(w, "  last_rx=%s\n", h.LastRx.UTC().Format(time.RFC3339))
	}
	if !h.LastTx.IsZero() {
		_, _ = fmt.Fprintf(w, "  last_tx=%s\n", h.LastTx.UTC().Format(time.RFC3339))
	}
	if h.StaleAfter > 0 {
		_, _ = fmt.Fprintf(w, "  stale_after=%s\n", h.StaleAfter)
	}
}

// healthFlipped reports whether any of the 3 layer bits changed between
// two snapshots. Pure helper so the watch-loop transition logic is unit
// testable.
func healthFlipped(prev, cur protocol.SessionHealth) bool {
	return prev.Reachable != cur.Reachable ||
		prev.Connected != cur.Connected ||
		prev.Live != cur.Live
}

func liveContext(h protocol.SessionHealth) string {
	if h.LastRx.IsZero() {
		return "(no rx yet)"
	}
	age := time.Since(h.LastRx).Truncate(100 * time.Millisecond)
	if h.StaleAfter > 0 {
		return fmt.Sprintf("(last rx %s ago, threshold %s)", age, h.StaleAfter)
	}
	return fmt.Sprintf("(last rx %s ago)", age)
}

func timestampOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func durationOrEmpty(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return d.String()
}
