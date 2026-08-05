package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"dhs/internal/consumer"
)

// runStatus prints a one-shot device status: the 3-layer session health
// (reachable / connected / live) plus the device identity (GetDeviceInfo).
// Canonical consumer verb (ADR-0002) — a superset of `health` (liveness only)
// and `info` (identity only), meant for monitoring / the future TUI + REST.
// `--output json` (`--json` deprecated alias) is the automation-friendly form.
func runStatus(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	cf := addCommonFlags(fs)
	output := fs.String("output", "text", "output format: text | json (ADR-0002)")
	asJSON := fs.Bool("json", false, "deprecated alias for --output json")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer <proto> status <host>")
	}
	_ = fs.Parse(rest)
	jsonOut, oerr := resolveEnsureOutput(*output, *asJSON)
	if oerr != nil {
		return oerr
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	var health *consumer.SessionHealth
	if checker, ok := plug.(consumer.HealthChecker); ok {
		h := checker.SessionHealth(opCtx)
		health = &h
	}
	info, infoErr := plug.GetDeviceInfo(opCtx)

	if jsonOut {
		out := map[string]any{"host": host, "proto": cf.protocol}
		if health != nil {
			out["reachable"] = health.Reachable
			out["connected"] = health.Connected
			out["live"] = health.Live
		}
		if infoErr == nil {
			out["device"] = info
		} else {
			out["device_error"] = infoErr.Error()
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	}

	fmt.Printf("host=%s proto=%s\n", host, cf.protocol)
	if health != nil {
		fmt.Printf("  reachable=%-5v connected=%-5v live=%-5v\n",
			health.Reachable, health.Connected, health.Live)
	}
	if infoErr == nil {
		fmt.Printf("  device=%+v\n", info)
	} else {
		fmt.Printf("  device=(unavailable: %v)\n", infoErr)
	}
	return nil
}

func helpStatus() {
	fmt.Println(`usage: dhs consumer <proto> status <host> [flags]

One-shot device status — session health + device identity:
  reachable / connected / live   (3-layer session health)
  device                          identity from GetDeviceInfo

flags:
  --output text|json   output format (json is monitoring/Ansible-friendly)
  --json               deprecated alias for --output json

Common flags from --help apply (--protocol, --transport, --port, --timeout).`)
}
