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
	"flag"
	"fmt"
	"os"
	"os/signal"

	"dhs/internal/consumer"
	"dhs/internal/errcode"

	// Consumer plugins — blank imports register with internal/consumer.
	_ "dhs/internal/acp1/consumer"
	_ "dhs/internal/acp2/consumer"
	_ "dhs/internal/cerebrum-nb/consumer"
	_ "dhs/internal/emberplus/consumer"
	_ "dhs/internal/osc/consumer"
	_ "dhs/internal/probel-sw02p/consumer"
	_ "dhs/internal/probel-sw08p/consumer"
	_ "dhs/internal/tsl/consumer"

	// Provider plugins — blank imports register with internal/provider.
	_ "dhs/internal/acp1/provider"
	_ "dhs/internal/acp2/provider"
	_ "dhs/internal/emberplus/provider"
	_ "dhs/internal/osc/provider"
	_ "dhs/internal/probel-sw02p/provider"
	_ "dhs/internal/probel-sw08p/provider"
	_ "dhs/internal/tsl/provider"

	// Registry plugins — blank imports register with internal/registry.
	_ "dhs/internal/amwa/registry"

	// NMOS codec versions — blank imports trigger init()-time
	// registration with the per-spec spec.Registry[T]. Each minor
	// is its own package; cmd/dhs/main.go is the only place that
	// references the concrete vXX/ packages directly. Plugin code
	// (Layer 3) goes through the host spec's Codec interface only,
	// per `internal/amwa/docs/dependencies.md` forbidden edges.
	_ "dhs/internal/amwa/codec/is04/v10"
	_ "dhs/internal/amwa/codec/is04/v11"
	_ "dhs/internal/amwa/codec/is04/v12"
	_ "dhs/internal/amwa/codec/is04/v13"
	_ "dhs/internal/amwa/codec/is05/v10"
	_ "dhs/internal/amwa/codec/is05/v11"
	_ "dhs/internal/amwa/codec/is07/v10"
	_ "dhs/internal/amwa/codec/is08/v10"
	_ "dhs/internal/amwa/codec/is09/v10"
	_ "dhs/internal/amwa/codec/is12/v10"
	_ "dhs/internal/amwa/codec/ms05/v10"

	// BCP validator packages — register into the shared bcp registry
	// at init() time so the host-resource fanout in
	// `internal/amwa/codec/bcp/Run` sees them.
	_ "dhs/internal/amwa/codec/bcp/bcp00201"
	_ "dhs/internal/amwa/codec/bcp/bcp00202"
	_ "dhs/internal/amwa/codec/bcp/bcp00401"
	_ "dhs/internal/amwa/codec/bcp/bcp00402"
	_ "dhs/internal/amwa/codec/bcp/bcp00601"
	_ "dhs/internal/amwa/codec/bcp/bcp00604"
	_ "dhs/internal/amwa/codec/bcp/bcp00801"
	_ "dhs/internal/amwa/codec/bcp/bcp00802"
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

// Vendor / product identity. Compile-time constants so every build — on
// every OS — carries the same provenance, surfaced via `dhs version` and
// (on Windows) the .exe Properties dialog via the go-winres resource whose
// fields mirror these. Keep in sync with versioninfo.json, LICENSE.md, and
// the COPYRIGHT file.
const (
	productName   = "Device Hub Systems"
	vendorName    = "BY-SYSTEMS SRL"
	vendorURL     = "https://www.by-systems.be"
	repositoryURL = "https://github.com/by-openclaw/go-acp"
	supportURL    = "https://github.com/by-openclaw/go-acp/issues"
	copyrightLine = "Copyright (c) 2026 BY-SYSTEMS SRL"
	licenseName   = "MIT License"
)

// printVersion writes the full identity + build provenance block. Shared by
// the `version` verb and the top-level help footer so the binary always
// states who built it, what it is, and how to reach support — on every OS.
func printVersion() {
	fmt.Printf("dhs %s — %s\n", version, productName)
	fmt.Printf("vendor     %s (%s)\n", vendorName, vendorURL)
	fmt.Printf("%s — %s\n", copyrightLine, licenseName)
	fmt.Println()
	fmt.Printf("commit     %s\n", orUnknown(commit))
	fmt.Printf("built      %s\n", date)
	if gitTag != "" {
		fmt.Printf("git tag    %s\n", gitTag)
	}
	fmt.Printf("repository %s\n", repositoryURL)
	fmt.Printf("support    %s\n", supportURL)
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

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
	{"tree", "render the device object tree as ASCII or PlantUML mindmap (R5b)", helpTree, runTree},
	{"get", "read one object value", helpGet, runGet},
	{"set", "write one object value", helpSet, runSet},
	{"inc", "increment an object by its step (ACP1 setIncValue)", helpInc, runInc},
	{"dec", "decrement an object by its step (ACP1 setDecValue)", helpDec, runDec},
	{"reset", "reset an object to its default (ACP1 setDefValue)", helpReset, runReset},
	{"ensure", "converge an object to --value, idempotently (--check dry-run; ADR-0007)", helpEnsure, runEnsure},
	{"watch", "subscribe to live announcements", helpWatch, runWatch},
	{"export", "dump a walked device to json / yaml / csv", helpExport, runExport},
	{"import", "apply values from a json snapshot file", helpImport, runImport},
	{"extract", "capture a per-product DM triple (meta + wire + tree) into the fixture layout", helpExtract, runExtract},
	{"diff", "compare two canonical tree.json files; emit text or a CHANGELOG section", helpDiff, runDiff},
	{"convert", "translate a snapshot file between json / yaml / csv (offline)", helpConvert, runConvert},
	{"discover", "passive + active scan for devices on the local subnet", helpDiscover, runDiscover},
	{"matrix", "set matrix crosspoint connections (Ember+ only)", helpMatrix, runMatrix},
	{"usage", "matrix reverse tally: where is each source assigned (Ember+ only)", helpEmberUsage, runEmberUsage},
	{"replace", "substitute matrix source A with B everywhere (Ember+ only; --check dry-run)", helpEmberReplace, runEmberReplace},
	{"invoke", "invoke an Ember+ function (RPC)", helpInvoke, runInvoke},
	{"stream", "subscribe to Ember+ stream parameters", helpStream, runStream},
	{"profile", "classify Ember+ provider compliance (strict / partial)", helpProfile, runProfile},
	{"diag", "run ACP2 diagnostic probes against a device", helpDiag, runDiag},
	{"validate", "decode a captured frames.jsonl through the codec offline (per ADR-0021)", helpValidate, runValidate},
	{"health", "print 3-layer session health (reachable / connected / live)", helpHealth, runHealth},
	{"status", "one-shot device status: session health + identity (--output json)", helpStatus, runStatus},
	{"bench", "Ember+ — fire N matrix crosspoint ops over one TCP session and time it", helpBench, runEmberplusBench},
}

func helpBench() {
	fmt.Println(`dhs consumer emberplus bench <host> --port N --path <matrix.path> --dm <identity> [--n 100] [--op connect|absolute|disconnect] [--targets N] [--sources N]

Holds ONE TCP session open and fires N MatrixConnect frames against the
named matrix, wrapping target/source by [--targets, --sources]. Prints
wall-clock + ops/sec + average per-op. No per-op timing.

Example — 1000 connects on the integration demo's nToN matrix:
  dhs consumer emberplus bench 127.0.0.1 --port 9100 \
    --dm dhs-emberplus-integration@1.0.0 \
    --path dhs-emberplus-integration.nToN.matrix \
    --n 1000 --op connect`)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	args := os.Args[1:]
	if len(args) == 0 {
		printTopHelp()
		os.Exit(0)
	}

	// A verb-level `--help` is answered by the verb's own FlagSet (it
	// prints that verb's usage + flags and Parse returns flag.ErrHelp).
	// Help is success, not an error — exit 0 silently instead of the
	// "error: flag: help requested" + catalogue fallback (#462).
	exitOnErr := func(err error) {
		if err == nil {
			return
		}
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(exitCode(err))
	}

	switch args[0] {
	case "help", "-h", "--h", "--help":
		printTopHelp()
		return
	case "version", "--version":
		printVersion()
		return
	case "list-protocols":
		if err := runListProtocols(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	case "list-commands":
		if err := runListCommands(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	case "help-cmd":
		if err := runHelpCmd(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	case "consumer":
		exitOnErr(dispatchConsumer(ctx, args[1:]))
		return
	case "producer":
		exitOnErr(dispatchProducer(ctx, args[1:]))
		return
	case "registry":
		exitOnErr(dispatchRegistry(ctx, args[1:]))
		return
	case "metrics":
		exitOnErr(runMetrics(ctx, args[1:]))
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
	if len(args) == 0 {
		printConsumerHelp()
		return nil
	}
	// Top-level help (no protocol given): print the consumer index.
	if isHelpToken(args[0]) {
		printConsumerHelp()
		return nil
	}
	proto := args[0]
	rest := args[1:]

	// Per-protocol dispatchers handle their own help so users can run
	// `dhs consumer <proto> -h` and see protocol-specific verbs.
	if proto == "probel-sw08p" {
		return runProbelsw08p(ctx, rest)
	}
	if proto == "probel-sw02p" {
		return runProbelsw02p(ctx, rest)
	}
	if proto == "cerebrum-nb" {
		return runCerebrum(ctx, rest)
	}
	if proto == "osc-v10" || proto == "osc-v11" {
		return runOSCConsumer(ctx, proto, rest)
	}
	if proto == "tsl-v31" || proto == "tsl-v40" || proto == "tsl-v50" {
		return runTSLConsumer(ctx, proto, rest)
	}
	if proto == "nmos" {
		return runNMOSConsumer(ctx, rest)
	}

	// Catalogue help ONLY when help is asked in place of a verb — a help
	// flag AFTER the verb belongs to the verb (#462: hasHelpFlag over the
	// whole argv made `walk --help` print this catalogue and shadowed the
	// per-verb help below).
	if len(rest) == 0 || isHelpToken(rest[0]) {
		printConsumerHelp()
		return nil
	}
	verb := rest[0]
	rest = rest[1:]

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
	if len(args) == 0 {
		printProducerHelp()
		return nil
	}
	if isHelpToken(args[0]) {
		printProducerHelp()
		return nil
	}
	proto := args[0]
	rest := args[1:]

	if proto == "osc-v10" || proto == "osc-v11" {
		return runOSCProducer(ctx, proto, rest)
	}
	if proto == "tsl-v31" || proto == "tsl-v40" || proto == "tsl-v50" {
		return runTSLProducer(ctx, proto, rest)
	}
	if proto == "nmos" {
		return runNMOSProducer(ctx, rest)
	}
	// Same rule as dispatchConsumer (#462): help IN PLACE of a verb =
	// catalogue; help after the verb belongs to the verb's own FlagSet.
	if len(rest) == 0 || isHelpToken(rest[0]) {
		printProducerHelp()
		return nil
	}
	verb := rest[0]
	rest = rest[1:]
	switch verb {
	case "serve":
		return runProducer(ctx, proto, rest)
	case "tree":
		return runProducerTree(ctx, proto, rest)
	case "status":
		// Canonical producer status = the live runtime snapshot of a serving
		// instance (frames/bytes/latency/errors/uptime), fetched from its
		// /snapshot.json. Delegates to the existing metrics-show renderer so
		// there is one implementation. Requires the server started with
		// --metrics-addr; pass --url http://host:port/snapshot.json.
		return runMetricsShow(ctx, rest)
	case "stop":
		return runProducerStop(ctx, proto, rest)
	case "ensure":
		// Canonical ADR-0007 ensure for the serving instance: converge to
		// --state present|absent keyed on --pidfile (idempotent teardown; drift
		// report for present). See runProducerEnsure for the honest boundary on
		// apply-present.
		return runProducerEnsure(ctx, proto, rest)
	case "validate":
		// Offline decode of a captured frames.jsonl through the codec — the
		// same generic validator the consumer side uses (direction-agnostic);
		// inject --protocol like dispatchConsumer does.
		return runValidate(ctx, append([]string{"--protocol", proto}, rest...))
	case "admin":
		if proto != "acp1" {
			return fmt.Errorf("producer %s: admin verb is acp1-only (advances #258)", proto)
		}
		return runACP1Admin(ctx, rest)
	case "fuzz":
		if proto != "acp1" {
			return fmt.Errorf("producer %s: fuzz verb is acp1-only (advances #262)", proto)
		}
		return runACP1Fuzz(ctx, rest)
	}
	return fmt.Errorf("producer %s: unknown verb %q (expected: serve | tree | status | stop | ensure | validate | admin | fuzz)", proto, verb)
}

// dispatchRegistry routes `dhs registry <proto> <verb> [args]`. The
// Registry role is the dual-face NMOS Registration + Query API
// middleware (and any future Tier-1 registry plugin).
func dispatchRegistry(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printRegistryHelp()
		return nil
	}
	if isHelpToken(args[0]) {
		printRegistryHelp()
		return nil
	}
	proto := args[0]
	rest := args[1:]
	switch proto {
	case "nmos":
		return runNMOSRegistry(ctx, rest)
	}
	return fmt.Errorf("registry: unknown plugin %q (expected: nmos)", proto)
}

// isHelpToken returns true for the help tokens we accept at any level.
func isHelpToken(s string) bool {
	switch s {
	case "-h", "--h", "--help", "help":
		return true
	}
	return false
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

// exitCode maps an error to the CLI exit code per the locked contract
// (docs/protocols/error-codes.md):
//
//	0 success
//	1 runtime / wire / protocol error (any transport:*, s101:*, glow:*,
//	  matrix:*, emberplus:*, session:* code or untyped runtime error)
//	2 usage / validation / state error (validation:*, plugin:* codes,
//	  legacy *consumer.ValidationError struct)
//
// Standard Unix; never 3+. Cross-OS uniform — PowerShell $LASTEXITCODE,
// Bash $?, cmd.exe %ERRORLEVEL% all parse identically.
//
// Dispatch order:
//  1. Typed *errcode.Code in the err chain → use its Class (0/1/2)
//  2. Legacy *consumer.ValidationError struct → 2 (caller fault)
//  3. Any other non-nil error → 1 (safe runtime fallback)
//
// The errcode.Exit helper handles cases (1) and (3); the
// ValidationError-struct check is the back-compat bridge until every
// callsite emits the typed validation:* codes.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	// Step 1+3 — typed code in chain wins; otherwise runtime fallback.
	if code := errcode.From(err); code != nil {
		return int(code.Class)
	}
	// Step 2 — legacy struct → usage class.
	var verr *consumer.ValidationError
	if errors.As(err, &verr) {
		return 2
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
  Protocols: acp1 | acp2 | cerebrum-nb | emberplus | probel-sw08p
  Verbs (acp1/acp2/emberplus): info, walk, get, set, watch, export, import,
                               extract, diff, convert, discover,
                               matrix, invoke, stream (Ember+ only),
                               profile, diag (ACP2 only)
  Verbs (probel-sw08p):        interrogate, connect, tally-dump, watch, etc.
                               (run 'dhs consumer probel-sw08p --help' for list)
  Verbs (cerebrum-nb):         connect, listen, list-devices, etc.
                               (XML over WebSocket; default port 40007)

  Examples:
    dhs consumer acp1        walk        10.6.239.113
    dhs consumer acp1        get         10.6.239.113 --slot 1 --label GainA
    dhs consumer acp2        walk        10.41.40.195
    dhs consumer emberplus   walk        10.0.0.10:9000
    dhs consumer emberplus   invoke      10.0.0.10:9000 --path router.salvo.fire
    dhs consumer probel-sw08p interrogate 127.0.0.1:2008 --matrix 0 --level 0 --dst 5
    dhs consumer cerebrum-nb listen      10.6.239.50 --user admin --pass s3cr3t

PRODUCER (inbound — serve a canonical tree to consumers over the wire)
  Protocols: acp1 | acp2 | emberplus | probel-sw02p | probel-sw08p
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
  acp1          Axon Control Protocol v1 (UDP/TCP direct, AN2/TCP)
  acp2          Axon Control Protocol v2 (AN2/TCP only)
  cerebrum-nb   EVS Cerebrum Northbound API (XML over WebSocket / Neuron Bridge)
  emberplus     Ember+ (Lawo)
  probel-sw08p  Probel SW-P-08 / SW-P-88 matrix router control
  osc-v10       Open Sound Control 1.0 (UDP + TCP/length-prefix)
  osc-v11       Open Sound Control 1.1 (UDP + TCP/SLIP, adds T/F/N/I + arrays)

GENERIC VERBS (acp1 / acp2 / emberplus)`)
	for _, c := range commands {
		fmt.Printf("  %-10s %s\n", c.name, c.short)
	}
	fmt.Println(`
PROBEL VERBS
  run 'dhs consumer probel-sw08p -h' for the Probel subcommand catalogue.

CEREBRUM VERBS
  run 'dhs consumer cerebrum-nb -h' for the verb catalogue.

OSC VERBS
  watch  bind a port and print every received message
  run 'dhs consumer osc-v10 -h' (or osc-v11) for full flags.

Use 'dhs consumer <protocol> <verb> -h' for per-verb flags.`)
}

func printProducerHelp() {
	fmt.Println(`dhs producer — inbound (serve a canonical tree over the wire)

USAGE
  dhs producer <protocol> <verb> [flags]

VERBS
  serve     bind the transport(s) and serve the canonical tree
  tree      print the canonical tree that would be served (no bind)
  status    live runtime snapshot of a serving instance (--url .../snapshot.json)
  stop      signal a 'serve --pidfile PATH' instance to shut down (--pidfile PATH)
  ensure    ADR-0007 converge to --state present|absent, keyed on --pidfile
  validate  offline decode a captured frames.jsonl through the codec

PROTOCOLS
  acp1 | acp2 | emberplus | probel-sw02p | probel-sw08p
  osc-v10 | osc-v11   (run 'dhs producer osc-v10 -h' for OSC-specific verbs)

FLAGS (common, slot-based protocols)
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

OSC VERBS (osc-v10 / osc-v11)
  send    emit one OSC message and exit
  fader   continuous high-rate fader simulator (perf measurement)
  serve   bind a port and log incoming messages

EXAMPLES
  dhs producer acp1      serve --tree tree.json --port 2071
  dhs producer acp2      serve --tree tree.json --port 2072
  dhs producer emberplus serve --tree tree.json --port 9000
  dhs producer probel-sw08p    serve --tree matrix.json --port 2008
  dhs producer osc-v10  send  --to 127.0.0.1:8000 --address /test --types ifs --args 42 3.14 hi
  dhs producer osc-v11  fader --to 127.0.0.1:8000 --rate 1000 --duration 5s --pattern sine
  dhs producer osc-v10  serve --bind udp:8000`)
}

// printRegistryHelp prints `dhs registry` top-level help.
func printRegistryHelp() {
	fmt.Println(`dhs registry — dual-face middleware (consumer of registrations + provider of catalogue)

USAGE
  dhs registry <plugin> serve [flags]

PLUGINS
  nmos    AMWA NMOS Registry — IS-04 Registration API + Query API

EXAMPLES
  dhs registry nmos serve --bind :8235 --priority 0
  dhs registry nmos serve --no-mdns --advertise-host registry.example.com:8235`)
}
