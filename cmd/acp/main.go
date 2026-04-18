// Command acp is the ACP toolset CLI.
//
// Usage:
//
//	acp info  <host> [--protocol acp1] [--port 2071]
//	acp walk  <host>  --slot N [--protocol acp1] [--port 2071]
//	acp get   <host>  --slot N --group G (--label L | --id I) [--protocol acp1]
//	acp set   <host>  --slot N --group G (--label L | --id I) --value <hex> [--protocol acp1]
//	acp list-protocols
//
// The CLI is deliberately thin: it parses flags, resolves the protocol
// plugin from the registry, calls the Protocol interface, and prints.
// It knows nothing about ACP1 wire format, object types, or transports —
// all of that lives in internal/protocol/acp1/.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"acp/internal/protocol"

	_ "acp/internal/protocol/acp1"
	_ "acp/internal/protocol/acp2"
	_ "acp/internal/protocol/emberplus"
)

// Build-time variables injected via -ldflags. See Makefile LDFLAGS_FULL.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// command is one dispatch entry. short is for the top-level index; help
// prints the command's detailed usage page. run is called with the
// argument vector following the command name.
type command struct {
	name  string
	short string
	help  func()
	run   func(ctx context.Context, args []string) error
}

// commands is the CLI dispatch table. Adding a subcommand means adding
// one entry here plus its run/help functions below — nothing else.
var commands = []command{
	{"info", "read device info (slot count, per-slot status)", helpInfo, runInfo},
	{"walk", "enumerate every object on a slot", helpWalk, runWalk},
	{"get", "read one object value", helpGet, runGet},
	{"set", "write one object value", helpSet, runSet},
	{"watch", "subscribe to live announcements", helpWatch, runWatch},
	{"export", "dump a walked device to json / yaml / csv", helpExport, runExport},
	{"import", "apply values from a json snapshot file", helpImport, runImport},
	{"discover", "passive + active scan for devices on the local subnet", helpDiscover, runDiscover},
	{"matrix", "set matrix crosspoint connections (Ember+ only)", helpMatrix, runMatrix},
	{"invoke", "invoke an Ember+ function (RPC)", helpInvoke, runInvoke},
	{"stream", "subscribe to Ember+ stream parameters", helpStream, runStream},
	{"diag", "run ACP2 diagnostic probes against a device", helpDiag, runDiag},
	{"list-protocols", "list available protocol plugins", helpListProtocols, func(_ context.Context, _ []string) error { return runListProtocols() }},
}

func main() {
	// Signal-aware root context so Ctrl-C interrupts long operations.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	args := os.Args[1:]

	// No arguments → show top-level help.
	if len(args) == 0 {
		printTopHelp()
		os.Exit(0)
	}

	// Top-level help flags.
	switch args[0] {
	case "help", "-h", "--h", "--help":
		// `acp help <cmd>` prints that command's detailed help.
		if len(args) >= 2 {
			if c := findCommand(args[1]); c != nil {
				c.help()
				return
			}
			fmt.Fprintf(os.Stderr, "unknown command: %q\n\n", args[1])
			printTopHelp()
			os.Exit(2)
		}
		printTopHelp()
		return

	case "version", "--version":
		fmt.Printf("acp %s (commit %s, built %s)\n", version, commit, date)
		fmt.Println("Copyright (c) 2026 BY-SYSTEMS SRL — https://www.by-systems.be")
		fmt.Println("MIT License")
		return
	}

	// Subcommand dispatch.
	c := findCommand(args[0])
	if c == nil {
		fmt.Fprintf(os.Stderr, "unknown command: %q\n\n", args[0])
		printTopHelp()
		os.Exit(2)
	}

	sub := args[1:]

	// `acp <cmd> -h|--help` short-circuits before flag parsing so the
	// command's curated help page is always reachable, regardless of
	// what other flags the user typed.
	if hasHelpFlag(sub) {
		c.help()
		return
	}

	if err := c.run(ctx, sub); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(exitCode(err))
	}
}

// findCommand looks up a command by name. O(n) over the fixed-size
// commands table; not worth indexing.
func findCommand(name string) *command {
	for i := range commands {
		if commands[i].name == name {
			return &commands[i]
		}
	}
	return nil
}

// hasHelpFlag scans args for any of the help-flag variants, without
// consuming them. Used before flag.Parse so help is reachable even when
// the rest of the args are incomplete or malformed.
func hasHelpFlag(args []string) bool {
	for _, a := range args {
		switch a {
		case "-h", "--h", "--help", "help":
			return true
		}
	}
	return false
}

// exitCode maps error classes to CLI exit codes per the README exit
// table: 0 success, 1 protocol error, 2 validation/usage error,
// 3 transport error, 5 bad flags.
func exitCode(err error) int {
	var verr *protocol.ValidationError
	if errors.As(err, &verr) {
		return 2
	}
	var terr *protocol.TransportError
	if errors.As(err, &terr) {
		return 3
	}
	return 1
}

// printTopHelp prints the top-level index shown by `acp`, `acp help`,
// `acp -h`, and `acp --help`.
func printTopHelp() {
	fmt.Println(`acp — Axon Control Protocol CLI

USAGE
  acp <command> [arguments] [flags]
  acp help <command>              show detailed help for a command
  acp -h | --help                 show this page
  acp version                     print version and exit

COMMANDS`)
	for _, c := range commands {
		fmt.Printf("  %-16s %s\n", c.name, c.short)
	}
	fmt.Println(`
GLOBAL FLAGS (accepted by all commands that connect to a device)
  --protocol NAME    protocol plugin: acp1 (default) | acp2
  --transport MODE   transport: udp (default) | tcp  (ACP1 only)
  --port N           override default port (0 = plugin default: 2071/2072)
  --timeout DUR      per-operation timeout (default: 30s, e.g. 10s, 2m, 90s)
  --verbose          debug log output to stderr
  --capture FILE     record raw traffic to JSONL file (for unit tests / replay)

COMMAND-SPECIFIC FLAGS
  info   (no extra flags)
  walk   --slot N            target slot (required, or use --all)
         --all               walk every present slot
         --filter TEXT       case-insensitive filter on output (like grep -i)
  get    --slot N            target slot (required)
         --label L           object label (preferred, stable across firmware)
         --group G           object group: identity|control|status|alarm|frame
         --id I              object id within group
  set    --slot N            target slot (required)
         --label L | --group G --id I     object addressing
         --value V           typed value (int, float, enum name, string, IP)
         --raw HEX           raw wire bytes (escape hatch, bypasses type codec)
  watch  --slot N            filter by slot (default: any)
         --group G           filter by group (default: any)
         --label L           filter by label (requires --slot)
         --id I              filter by object id
  export --format F          json (default) | yaml | csv
         --out FILE          output file (default: stdout)
  import --file PATH         snapshot file (json only, required)
         --dry-run           preview without writing
  discover
         --duration DUR      scan window (default: 5s)
         --active            send broadcast probe (default: true)
         --scan-port N       ACP port (default: 2071)
  diag   --slot N            target slot (default: 0)
  list-protocols             (no flags)

EXAMPLES — ACP1 (emulator / real device, UDP)
  acp info     10.6.239.113
  acp walk     10.6.239.113 --slot 0
  acp walk     10.6.239.113 --all
  acp get      10.6.239.113 --slot 1 --label GainA
  acp get      10.6.239.113 --slot 1 --group control --id 91
  acp set      10.6.239.113 --slot 1 --label GainA --value 50.0
  acp set      10.6.239.113 --slot 0 --label Broadcasts --value On
  acp watch    10.6.239.113 --slot 1 --group control
  acp export   10.6.239.113 --format json --out device.json
  acp import   10.6.239.113 --file device.json --dry-run
  acp discover --duration 10s

EXAMPLES — ACP2 (real device, AN2/TCP)
  acp info     10.41.40.195 --protocol acp2
  acp walk     10.41.40.195 --protocol acp2 --slot 0
  acp walk     10.41.40.195 --protocol acp2 --all
  acp diag     10.41.40.195 --slot 0

EXAMPLES — traffic capture (for unit test data)
  acp walk     10.6.239.113 --slot 0 --capture acp1_slot0.jsonl
  acp walk     10.41.40.195 --protocol acp2 --slot 0 --capture acp2_slot0.jsonl

Environment:
  No environment variables are read. All configuration is via flags.

Exit codes:
  0  success
  1  protocol error (device returned an error reply)
  2  validation / usage error
  3  transport error (connection, timeout, frame decode)
  5  bad flags

See docs/protocols/ for the authoritative wire-format specifications.
Use 'acp help <command>' for detailed help on any command.

Copyright (c) 2026 BY-SYSTEMS SRL — https://www.by-systems.be`)
}
