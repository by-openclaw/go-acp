// Command dhs — Device Hub Systems CLI.
//
// Usage:
//
//	dhs consumer <protocol> <verb> <target> [flags]
//	dhs producer <protocol> <verb> [flags]
//	dhs list-protocols
//	dhs version
//
// Examples:
//
//	dhs consumer acp1      walk        10.6.239.113
//	dhs consumer acp1      get         10.6.239.113 --slot 1 --label GainA
//	dhs consumer acp2      walk        10.41.40.195
//	dhs consumer acp2      diag        10.41.40.195 --slot 0
//	dhs consumer emberplus walk        10.0.0.10:9000
//	dhs consumer emberplus invoke      10.0.0.10:9000 --path router.salvo.fire
//	dhs consumer probel-sw08p    interrogate 127.0.0.1:2008 --matrix 0 --level 0 --dst 5
//
//	dhs producer acp1      serve --tree tree.json --port 2071
//	dhs producer acp2      serve --tree tree.json --port 2072
//	dhs producer emberplus serve --tree tree.json --port 9000
//	dhs producer probel-sw08p    serve --tree matrix.json --port 2008
//
// The CLI is deliberately thin: it parses the consumer|producer + protocol
// prefix, dispatches to a per-verb runner, and prints. It knows nothing
// about wire formats — that all lives in internal/<protocol>/.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"acp/internal/protocol"

	// Consumer plugins — blank imports register with internal/protocol.
	_ "acp/internal/acp1/consumer"
	_ "acp/internal/acp2/consumer"
	_ "acp/internal/emberplus/consumer"
	_ "acp/internal/probel-sw08p/consumer"

	// Provider plugins — blank imports register with internal/provider.
	_ "acp/internal/acp1/provider"
	_ "acp/internal/acp2/provider"
	_ "acp/internal/emberplus/provider"
	_ "acp/internal/probel-sw08p/provider"
)

// Build-time variables injected via -ldflags. See Makefile LDFLAGS_FULL.
//
//	-X main.version=0.3.0  -X main.commit=7bfc8ab  -X main.gitTag=v0.3.0
//
// `commit` and `gitTag` have sensible fall-backs derived from
// runtime/debug.BuildInfo when the ldflags are absent.
var (
	version = "dev"
	commit  = ""
	date    = "unknown"
	gitTag  = ""
)

// command is one consumer-verb dispatch entry.
type command struct {
	name  string
	short string
	help  func()
	run   func(ctx context.Context, args []string) error
}

// commands is the consumer-verb dispatch table. `dhs consumer <proto> <verb>`
// injects `--protocol <proto>` into the remaining argv and calls run.
var commands = []command{
	{"info", "read device info (slot count, per-slot status)", helpInfo, runInfo},
	{"walk", "enumerate every object on a slot", helpWalk, runWalk},
	{"get", "read one object value", helpGet, runGet},
	{"set", "write one object value", helpSet, runSet},
	{"watch", "subscribe to live announcements", helpWatch, runWatch},
	{"export", "dump a walked device to json / yaml / csv", helpExport, runExport},
	{"import", "apply values from a json snapshot file", helpImport, runImport},
	{"extract", "capture a per-product DM triple (meta + wire + tree) into the fixture layout", helpExtract, runExtract},
	{"diff", "compare two canonical tree.json files; emit text or a CHANGELOG section", helpDiff, runDiff},
	{"convert", "translate a snapshot file between json / yaml / csv (offline)", helpConvert, runConvert},
	{"discover", "passive + active scan for devices on the local subnet", helpDiscover, runDiscover},
	{"matrix", "set matrix crosspoint connections (Ember+ only)", helpMatrix, runMatrix},
	{"invoke", "invoke an Ember+ function (RPC)", helpInvoke, runInvoke},
	{"stream", "subscribe to Ember+ stream parameters", helpStream, runStream},
	{"profile", "classify Ember+ provider compliance (strict / partial)", helpProfile, runProfile},
	{"diag", "run ACP2 diagnostic probes against a device", helpDiag, runDiag},
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	args := os.Args[1:]
	if len(args) == 0 {
		printTopHelp()
		os.Exit(0)
	}

	switch args[0] {
	case "help", "-h", "--h", "--help":
		printTopHelp()
		return
	case "version", "--version":
		fmt.Printf("dhs %s (commit %s, built %s)\n", version, commit, date)
		fmt.Println("Copyright (c) 2026 BY-SYSTEMS SRL — https://www.by-systems.be")
		fmt.Println("MIT License")
		return
	case "list-protocols":
		if err := runListProtocols(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	case "consumer":
		if err := dispatchConsumer(ctx, args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(exitCode(err))
		}
		return
	case "producer":
		if err := dispatchProducer(ctx, args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(exitCode(err))
		}
		return
	case "metrics":
		if err := runMetrics(ctx, args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(exitCode(err))
		}
		return
	}

	fmt.Fprintf(os.Stderr, "unknown top-level command: %q\n\n", args[0])
	printTopHelp()
	os.Exit(2)
}

// dispatchConsumer routes `dhs consumer <proto> <verb> [args...]`.
// For acp1/acp2/emberplus it injects --protocol <proto> and dispatches via
// the generic verb table. Probel has its own verb catalogue and dispatches
// directly to runProbel.
func dispatchConsumer(ctx context.Context, args []string) error {
	if len(args) == 0 || hasHelpFlag(args) {
		printConsumerHelp()
		return nil
	}
	if len(args) < 2 {
		printConsumerHelp()
		return fmt.Errorf("consumer: need <protocol> <verb>")
	}
	proto := args[0]
	verb := args[1]
	rest := args[2:]

	if proto == "probel-sw08p" {
		return runProbel(ctx, append([]string{verb}, rest...))
	}

	c := findCommand(verb)
	if c == nil {
		return fmt.Errorf("consumer %s: unknown verb %q", proto, verb)
	}
	if hasHelpFlag(rest) {
		c.help()
		return nil
	}
	rest = append([]string{"--protocol", proto}, rest...)
	return c.run(ctx, rest)
}

// dispatchProducer routes `dhs producer <proto> <verb> [args...]`.
// Currently only <verb>=serve is defined.
func dispatchProducer(ctx context.Context, args []string) error {
	if len(args) == 0 || hasHelpFlag(args) {
		printProducerHelp()
		return nil
	}
	if len(args) < 2 {
		printProducerHelp()
		return fmt.Errorf("producer: need <protocol> <verb>")
	}
	proto := args[0]
	verb := args[1]
	rest := args[2:]

	switch verb {
	case "serve":
		return runProducer(ctx, proto, rest)
	}
	return fmt.Errorf("producer %s: unknown verb %q (expected: serve)", proto, verb)
}

// findCommand looks up a consumer-verb by name.
func findCommand(name string) *command {
	for i := range commands {
		if commands[i].name == name {
			return &commands[i]
		}
	}
	return nil
}

// hasHelpFlag scans args for any of the help-flag variants without consuming
// them, so help is reachable even when the rest of the args are malformed.
func hasHelpFlag(args []string) bool {
	for _, a := range args {
		switch a {
		case "-h", "--h", "--help", "help":
			return true
		}
	}
	return false
}

// exitCode maps error classes to CLI exit codes: 0 success, 1 protocol
// error, 2 validation/usage error, 3 transport error.
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

func printTopHelp() {
	fmt.Println(`dhs — Device Hub Systems CLI

USAGE
  dhs consumer <protocol> <verb> <target> [flags]
  dhs producer <protocol> <verb> [flags]
  dhs list-protocols
  dhs version
  dhs -h | --help                            this page

CONSUMER (outbound — connect to a device, query / control it)
  Protocols: acp1 | acp2 | emberplus | probel-sw08p
  Verbs (acp1/acp2/emberplus): info, walk, get, set, watch, export, import,
                               extract, diff, convert, discover,
                               matrix, invoke, stream (Ember+ only),
                               profile, diag (ACP2 only)
  Verbs (probel-sw08p):        interrogate, connect, tally-dump, watch, etc.
                               (run 'dhs consumer probel-sw08p --help' for list)

  Examples:
    dhs consumer acp1      walk        10.6.239.113
    dhs consumer acp1      get         10.6.239.113 --slot 1 --label GainA
    dhs consumer acp2      walk        10.41.40.195
    dhs consumer emberplus walk        10.0.0.10:9000
    dhs consumer emberplus invoke      10.0.0.10:9000 --path router.salvo.fire
    dhs consumer probel-sw08p    interrogate 127.0.0.1:2008 --matrix 0 --level 0 --dst 5

PRODUCER (inbound — serve a canonical tree to consumers over the wire)
  Protocols: acp1 | acp2 | emberplus | probel-sw08p
  Verbs:     serve

  Examples:
    dhs producer acp1      serve --tree tree.json --port 2071
    dhs producer acp2      serve --tree tree.json --port 2072
    dhs producer emberplus serve --tree tree.json --port 9000
    dhs producer probel-sw08p    serve --tree matrix.json --port 2008

SERVE FLAGS (common to every producer)
  --tree PATH             canonical tree.json (required)
  --port N                TCP listen port (0 = plugin default)
  --host ADDR             TCP listen host (default 0.0.0.0)
  --log-level LEVEL       debug | info | warn | error
  --announce-demo         oscillate a target value + broadcast announces
                          (acp1/acp2 only; see 'dhs producer <proto> serve -h')

EXIT CODES
  0  success
  1  protocol error (device returned an error reply)
  2  validation / usage error
  3  transport error (connection, timeout, frame decode)

See per-protocol CLAUDE.md under internal/<proto>/ for wire-format details.

Copyright (c) 2026 BY-SYSTEMS SRL — https://www.by-systems.be`)
}

func printConsumerHelp() {
	fmt.Println(`dhs consumer — outbound (connect to a device, query / control it)

USAGE
  dhs consumer <protocol> <verb> <target> [flags]

PROTOCOLS
  acp1        Axon Control Protocol v1 (UDP/TCP direct, AN2/TCP)
  acp2        Axon Control Protocol v2 (AN2/TCP only)
  emberplus   Ember+ (Lawo)
  probel-sw08p  Probel SW-P-08 / SW-P-88 matrix router control

GENERIC VERBS (acp1 / acp2 / emberplus)`)
	for _, c := range commands {
		fmt.Printf("  %-10s %s\n", c.name, c.short)
	}
	fmt.Println(`
PROBEL VERBS
  run 'dhs consumer probel-sw08p -h' for the Probel subcommand catalogue.

Use 'dhs consumer <protocol> <verb> -h' for per-verb flags.`)
}

func printProducerHelp() {
	fmt.Println(`dhs producer — inbound (serve a canonical tree over the wire)

USAGE
  dhs producer <protocol> serve [flags]

PROTOCOLS
  acp1 | acp2 | emberplus | probel-sw08p

FLAGS (common)
  --tree PATH             canonical tree.json (required)
  --port N                TCP listen port (0 = plugin default)
  --host ADDR             TCP listen host (default 0.0.0.0)
  --log-level LEVEL       debug | info | warn | error
  --announce-demo         oscillate a target value + broadcast announces
                          (acp1/acp2 only)
  --announce-demo-slot N             slot for demo target
  --announce-demo-group G            acp1: object group (default 2 = Control)
  --announce-demo-id I               acp1: object id (must be Integer)
  --announce-demo-obj OBJ            acp2: obj-id (must be Number+Float)
  --announce-demo-interval DURATION  tick interval (default 2s)

EXAMPLES
  dhs producer acp1      serve --tree tree.json --port 2071
  dhs producer acp2      serve --tree tree.json --port 2072
  dhs producer emberplus serve --tree tree.json --port 9000
  dhs producer probel-sw08p    serve --tree matrix.json --port 2008`)
}
