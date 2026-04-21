package main

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"acp/internal/acp1/consumer"
)

// runDiscover runs a one-shot LAN scan for ACP1 devices. Works only
// when the host is on the same subnet as the devices — subnet
// broadcasts do not cross routers. Documented in the help text.
func runDiscover(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("discover", flag.ExitOnError)
	cf := addCommonFlags(fs)
	durationStr := fs.String("duration", "5s", "how long to listen (e.g. 5s, 30s)")
	active := fs.Bool("active", true, "also send a broadcast probe (recommended)")
	port := fs.Int("scan-port", 2071, "ACP port to scan")
	_ = fs.Parse(args)
	_ = cf // global flags reserved for parity; discover ignores them

	d, err := time.ParseDuration(*durationStr)
	if err != nil {
		return fmt.Errorf("--duration: %w", err)
	}

	fmt.Printf("scanning for ACP1 devices on :%d for %s (active=%v)...\n",
		*port, d, *active)

	results, err := acp1.Discover(ctx, acp1.DiscoverConfig{
		Duration: d,
		Active:   *active,
		Port:     *port,
	})
	if err != nil {
		return err
	}

	if len(results) == 0 {
		fmt.Println("no devices found — check you are on the same subnet")
		return nil
	}

	fmt.Printf("\n%-16s %-5s %-6s %-16s %-20s %-20s %s\n",
		"IP", "PORT", "SLOTS", "SOURCE", "FIRST SEEN", "LAST SEEN", "")
	fmt.Println(strings.Repeat("-", 90))
	for _, r := range results {
		fmt.Printf("%-16s %-5d %-6d %-16s %-20s %-20s\n",
			r.IP, r.Port, r.NumSlots, r.Source,
			r.FirstSeen.Format("15:04:05.000"),
			r.LastSeen.Format("15:04:05.000"))
	}
	fmt.Printf("\n%d device(s) found\n", len(results))
	return nil
}
