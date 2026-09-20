package main

// `dhs consumer mnset <verb>` — Riedel MuoN eMSFP / FusioN modules over
// their own REST API (emsfp/node/v1), issue #1110. MN SET is not in the
// path: every module answers directly. The generic verbs (info, walk,
// get, set, export, import, status, health …) run through the registry
// like any other protocol; this file adds the two verbs that are not
// per-module:
//
//   dhs consumer mnset discover --range 10.6.40.0/24      sweep for modules
//   dhs consumer mnset inventory <mnset-host> --user u     ask MN SET for its device list

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	mnset "dhs/internal/MNSet/consumer"
)

// runMNSet handles the mnset-only verbs. handled=false hands the verb to
// the generic dispatcher.
func runMNSet(ctx context.Context, args []string) (handled bool, err error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		printMNSetHelp()
		return true, nil
	}
	switch args[0] {
	case "discover":
		return true, runMNSetDiscover(ctx, args[1:])
	case "inventory":
		return true, runMNSetInventory(ctx, args[1:])
	}
	return false, nil
}

func printMNSetHelp() {
	fmt.Println(`usage: dhs consumer mnset <verb> [<host>] [flags]

Riedel MuoN eMSFP / FusioN modules, direct REST (http://<module>/emsfp/node/v1).
One module = one device, slot 0. Paths are the resource then the JSON path:
  self.ipconfig.hostname    flows[0].network[1].dst_ip_addr    devices[7].name

mnset-only verbs:
  discover  --range R [--range R …] [--port 80] [--timeout 2s] [--concurrency 32]
            sweep addresses for modules (R: 10.6.40.53 | 10.6.40.50-99 | 10.6.40.0/24)
  inventory <mnset-host> --user U [--port 8080]   list the modules MN SET manages
            password read from $MNSET_PASS (never a flag, never printed)

generic verbs (see 'dhs consumer --help'):
  info | walk | get --path P | set --path P --value V | export | import | status | health`)
}

func runMNSetDiscover(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("consumer mnset discover", flag.ContinueOnError)
	var ranges multiFlag
	fs.Var(&ranges, "range", "address, last-octet range a.b.c.x-y or CIDR (repeatable)")
	port := fs.Int("port", mnset.DefaultPort, "module REST port")
	timeout := fs.Duration("timeout", 2*time.Second, "per-address probe timeout")
	conc := fs.Int("concurrency", 32, "probes in flight")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	if len(ranges) == 0 {
		return fmt.Errorf("consumer mnset discover: at least one --range is required")
	}
	found, err := mnset.Discover(ctx, mnset.DiscoverConfig{Ranges: ranges, Port: *port, Timeout: *timeout, Concurrency: *conc})
	if err != nil {
		return err
	}
	if len(found) == 0 {
		fmt.Println("no module answered")
		return nil
	}
	fmt.Printf("%-16s %-5s %-10s %-14s %-12s %-30s %s\n", "IP", "PORT", "BASE", "SERIAL", "FW", "TYPE", "APP")
	fmt.Println(strings.Repeat("-", 110))
	for _, d := range found {
		fmt.Printf("%-16s %-5d %-10s %-14s %-12s %-30s %s\n", d.IP, d.Port, d.BaseType, d.Serial, d.Firmware, d.Type, d.App)
	}
	fmt.Printf("\n%d module(s) found\n", len(found))
	return nil
}

func runMNSetInventory(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("consumer mnset inventory", flag.ContinueOnError)
	user := fs.String("user", "", "MN SET login name (password from $MNSET_PASS)")
	port := fs.Int("port", 8080, "MN SET application port")
	timeout := fs.Duration("timeout", 8*time.Second, "per-request timeout")
	host := ""
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		host, args = args[0], args[1:]
	}
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	if host == "" {
		return fmt.Errorf("consumer mnset inventory: the MN SET host is required (e.g. 10.6.250.105)")
	}
	if *user == "" {
		return fmt.Errorf("consumer mnset inventory: --user is required")
	}
	pass, ok := os.LookupEnv("MNSET_PASS")
	if !ok || pass == "" {
		return fmt.Errorf("consumer mnset inventory: set MNSET_PASS in the environment")
	}
	devs, err := mnset.Inventory(ctx, host, *port, *user, pass, *timeout)
	if err != nil {
		return err
	}
	fmt.Printf("%-18s %-8s %-16s %-14s %-30s %s\n", "ID", "STATUS", "IP", "SERIAL", "TYPE", "LLDP")
	fmt.Println(strings.Repeat("-", 110))
	for _, d := range devs {
		fmt.Printf("%-18s %-8s %-16s %-14s %-30s %s\n", d.ID, d.Status, d.IP, d.Serial, d.Type, d.Location)
	}
	fmt.Printf("\n%d device(s) managed\n", len(devs))
	return nil
}

// multiFlag collects a repeatable string flag.
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(s string) error { *m = append(*m, s); return nil }
