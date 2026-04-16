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
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"acp/internal/export"
	"acp/internal/protocol"
	"acp/internal/protocol/acp1"
	"acp/internal/protocol/acp2"
	"acp/internal/transport"
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

// ---------------------------------------------------------------- per-command help

func helpInfo() {
	fmt.Println(`acp info — read device info

USAGE
  acp info <host> [flags]

DESCRIPTION
  Connects to the device, reads the rack controller's Frame Status
  object (group=frame, id=0), and prints the slot count plus the
  status of every slot. This is the typical first call after power-on
  or after changing a LAN cable, to confirm the device is reachable
  and see which cards are present.

FLAGS (in addition to global flags)
  (none)

EXAMPLES
  acp info 10.6.239.113
  acp info 10.6.239.113 --timeout 5s
  acp info 10.6.239.113 --verbose`)
}

func helpWalk() {
	fmt.Println(`acp walk — enumerate every object on a slot

USAGE
  acp walk <host> --slot N [flags]

DESCRIPTION
  Reads the root object on the target slot to learn the number of
  objects per group, then issues one getObject per object, producing
  a typed inventory: identity, control, status, alarm. Section
  markers (device-specific grouping hints) are rendered as "── NAME ──".

  The walker caches the result per slot for the lifetime of the CLI
  process so subsequent get/set calls can resolve --label without
  re-walking.

FLAGS
  --slot N           slot number (required)
  --filter TEXT      case-insensitive filter on output lines (like findstr /i or grep -i)

EXAMPLES
  acp walk 10.6.239.113 --slot 0        # rack controller
  acp walk 10.6.239.113 --slot 1        # first card
  acp walk 10.6.239.113 --slot 1 --verbose
  acp walk 10.41.40.195 --protocol acp2 --slot 0 --filter enum
  acp walk 10.41.40.195 --protocol acp2 --slot 0 --filter "Fan Health"`)
}

func helpGet() {
	fmt.Println(`acp get — read one object value

USAGE
  acp get <host> --slot N (--label L | --group G --id I) [flags]

DESCRIPTION
  Reads one object value and decodes it into a typed form:
  integer, float, enum, string, ipaddr, alarm priority, or frame
  status. The corresponding metadata (range, step, default, unit,
  enum items, max string length, alarm messages) is printed below
  the value.

  Addressing:
    • --label L            search the walked tree for the label
                           (walks the slot automatically if needed)
    • --group G --id I     explicit addressing, no walk required
    • --label L --group G  disambiguate a label that exists in
                           multiple groups

FLAGS
  --slot N           slot number (required)
  --label L          object label (preferred — stable across firmware)
  --group G          object group: identity | control | status | alarm | frame
  --id I             object id within a group

EXAMPLES
  acp get 10.6.239.113 --slot 1 --label "Card name"
  acp get 10.6.239.113 --slot 1 --label GainA
  acp get 10.6.239.113 --slot 1 --group control --id 91
  acp get 10.6.239.113 --slot 0 --group frame --id 0`)
}

func helpSet() {
	fmt.Println(`acp set — write one object value

USAGE
  acp set <host> --slot N (--label L | --group G --id I) --value V [flags]
  acp set <host> --slot N ...                                --raw HEX [flags]

DESCRIPTION
  Writes one object value. The device enforces range, step, and
  access constraints — out-of-range writes are silently clamped by
  the device and the echoed reply shows the stored value.

  Typed --value forms, picked automatically by object kind:
    integer / long     "42", "-7"
    float              "-6.3", "50.0"
    byte               "100"
    enum               "On"  (item name, case-sensitive)
                       "1"   (numeric index)
    string             "CH1"
    ipaddr             "192.168.1.5"

  --raw is an escape hatch for advanced users: pass the exact wire
  bytes in hex, bypassing type coercion. Useful when the walker
  hasn't seen the object (no prior walk) or when debugging a quirky
  device.

FLAGS
  --slot N           slot number (required)
  --label L          object label (preferred)
  --group G          object group
  --id I             object id within a group
  --value V          typed value string
  --raw HEX          raw wire bytes (mutually exclusive with --value)

EXAMPLES
  acp set 10.6.239.113 --slot 1 --label GainA --value 50.0
  acp set 10.6.239.113 --slot 0 --label Broadcasts --value On
  acp set 10.6.239.113 --slot 1 --label mIP0 --value 192.168.1.250
  acp set 10.6.239.113 --slot 1 --label "#CVBS-Frmt" --value "PAL-N"
  acp set 10.6.239.113 --slot 1 --label GainA --raw 42c80000`)
}

func helpWatch() {
	fmt.Println(`acp watch — subscribe to live announcements

USAGE
  acp watch <host> [filters] [flags]

DESCRIPTION
  Opens a UDP listener and prints every announcement the device
  broadcasts: value changes (control, status, alarm), frame-status
  transitions (card inserted, removed, booting, error), identity
  updates. Runs until Ctrl-C.

  REQUIREMENTS:
    • The rack controller's "Broadcasts" enable must be ON. When
      it is OFF the device sends no LAN announcements at all.
      Check via: acp get <host> --slot 0 --label Broadcasts
    • Port 2071 must be free on the local host (another acp
      process or a Synapse Cortex running on the same box will
      hold it and prevent binding).

  Filters compose: any combination of --slot, --group, --label,
  --id narrows the stream. No filter = everything.

FLAGS
  --slot N           only events from this slot (default: any)
  --group G          only events in this group (default: any)
  --label L          only events for this label (requires --slot)
  --id I             only events for this object id

EXAMPLES
  acp watch 10.6.239.113                              # everything
  acp watch 10.6.239.113 --slot 1                     # slot 1 only
  acp watch 10.6.239.113 --slot 1 --group control
  acp watch 10.6.239.113 --slot 1 --label GainA
  acp watch 10.6.239.113 --verbose                    # + debug lines`)
}

func helpListProtocols() {
	fmt.Println(`acp list-protocols — list available protocol plugins

USAGE
  acp list-protocols

DESCRIPTION
  Prints every protocol plugin that was compiled into this binary,
  with its canonical name, default port, and one-line description.
  The name shown here is what you pass to --protocol on other
  commands.

EXAMPLES
  acp list-protocols`)
}

// ---------------------------------------------------------------- common

// commonFlags holds the flags every subcommand accepts. Parsed per
// subcommand so positional args (the host) stay in position 1.
type commonFlags struct {
	protocol  string
	transport string
	port      int
	timeout   time.Duration
	verbose   bool
	capture   string
}

func addCommonFlags(fs *flag.FlagSet) *commonFlags {
	cf := &commonFlags{}
	fs.StringVar(&cf.protocol, "protocol", "acp1", "protocol plugin name")
	fs.StringVar(&cf.transport, "transport", "udp",
		"transport: udp (default, subnet broadcast announcements) or tcp "+
			"(ACP1 v1.4 TCP direct, crosses VLANs)")
	fs.IntVar(&cf.port, "port", 0, "override default port (0 = plugin default)")
	fs.DurationVar(&cf.timeout, "timeout", 30*time.Second, "per-operation timeout")
	fs.BoolVar(&cf.verbose, "verbose", false, "debug log output")
	fs.StringVar(&cf.capture, "capture", "", "write raw traffic to JSONL file (for unit test data)")
	return cf
}

// connect builds a fresh plugin instance, dials the host, and returns the
// live Protocol along with a cleanup function. Every subcommand starts
// with this; the cleanup runs on function exit.
func connect(ctx context.Context, host string, cf *commonFlags) (protocol.Protocol, func(), error) {
	if host == "" {
		return nil, nil, fmt.Errorf("host argument is required")
	}

	lvl := slog.LevelInfo
	if cf.verbose {
		lvl = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))

	// Optional traffic capture for test data generation.
	var recorder *transport.Recorder
	if cf.capture != "" {
		var recErr error
		recorder, recErr = transport.NewRecorder(cf.capture)
		if recErr != nil {
			return nil, nil, fmt.Errorf("capture: %w", recErr)
		}
	}

	factory, err := protocol.Get(cf.protocol)
	if err != nil {
		if recorder != nil {
			_ = recorder.Close()
		}
		return nil, nil, err
	}
	plug := factory.New(logger)

	// Attach recorder if --capture was given.
	if recorder != nil {
		if p, ok := plug.(interface{ SetRecorder(*transport.Recorder) }); ok {
			p.SetRecorder(recorder)
		}
	}

	// Transport selection is plugin-specific; cast when possible and
	// apply. Protocols that don't expose SetTransport just ignore it.
	if tcfg, ok := plug.(interface{ SetTransport(acp1.TransportKind) }); ok {
		switch strings.ToLower(cf.transport) {
		case "tcp", "tcp-direct", "tcpdirect":
			tcfg.SetTransport(acp1.TransportTCPDirect)
		case "udp", "":
			tcfg.SetTransport(acp1.TransportUDP)
		default:
			return nil, nil, fmt.Errorf("unknown --transport %q (use udp or tcp)", cf.transport)
		}
	}

	port := cf.port
	if port == 0 {
		port = factory.Meta().DefaultPort
	}

	dialCtx, cancel := context.WithTimeout(ctx, cf.timeout)
	defer cancel()
	if err := plug.Connect(dialCtx, host, port); err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		_ = plug.Disconnect()
		if recorder != nil {
			_ = recorder.Close()
		}
	}
	return plug, cleanup, nil
}

// withTimeout wraps ctx with the subcommand's --timeout.
func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, d)
}

// ---------------------------------------------------------------- info

func runInfo(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("info", flag.ExitOnError)
	cf := addCommonFlags(fs)
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: acp info <host>")
	}
	_ = fs.Parse(rest)

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	info, err := plug.GetDeviceInfo(opCtx)
	if err != nil {
		return err
	}
	fmt.Printf("device       %s:%d\n", info.IP, info.Port)
	fmt.Printf("protocol     %s v%d\n", cf.protocol, info.ProtocolVersion)
	fmt.Printf("slots        %d\n", info.NumSlots)
	fmt.Println()
	fmt.Println("per-slot status:")
	for slot := 0; slot < info.NumSlots; slot++ {
		si, err := plug.GetSlotInfo(opCtx, slot)
		if err != nil {
			fmt.Printf("  slot %2d   <error: %v>\n", slot, err)
			continue
		}
		fmt.Printf("  slot %2d   %s\n", slot, si.Status)
	}
	return nil
}

// ---------------------------------------------------------------- walk

func runWalk(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("walk", flag.ExitOnError)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", -1, "slot number (omit or pass -1 with --all to walk every present slot)")
	all := fs.Bool("all", false, "walk every present slot on the device")
	filter := fs.String("filter", "", "case-insensitive filter on output lines (like findstr /i or grep -i)")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: acp walk <host> (--slot N | --all)")
	}
	_ = fs.Parse(rest)
	if !*all && *slot < 0 {
		return fmt.Errorf("--slot N or --all is required")
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	// Stream objects as they're discovered during walk — don't wait for
	// the full tree before printing. Essential for large slots (4190+ objects).
	// ACP1 doesn't support streaming, so we fall back to printSlotTree after.
	streaming := false
	filterLower := strings.ToLower(*filter)
	if p, ok := plug.(interface{ SetWalkProgress(acp2.WalkProgressFunc) }); ok {
		streaming = true
		p.SetWalkProgress(func(count int, obj *protocol.Object) {
			if obj.Kind == protocol.KindRaw && obj.Label == "" {
				return // skip node containers
			}
			valStr := walkValueColumn(*obj)
			rngStr := walkRangeColumn(*obj)
			line := fmt.Sprintf("  %3d  %-20s  %-6s  %-3s  %-18s  %s",
				obj.ID,
				truncate(obj.Label, 20),
				kindName(obj.Kind),
				accessStr(obj.Access),
				truncate(valStr, 18),
				rngStr)
			if *filter != "" && !strings.Contains(strings.ToLower(line), filterLower) {
				return
			}
			fmt.Println(line)
		})
	}

	// Walk uses the signal-only context (no timeout). A tree walk takes
	// as long as it takes — 214 objects on slot 0 is ~2s, 4190 objects
	// on slot 1 can be minutes. Ctrl-C is the only interrupt.
	// Short-timeout opCtx is only for device info / slot info queries.
	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	// --all: read frame status, iterate every present slot, walk each.
	// Partial failure on one slot does not abort the rest; we print the
	// error and keep going, since a mid-walk card removal is a normal
	// operational event, not a fatal error.
	if *all {
		info, err := plug.GetDeviceInfo(opCtx)
		if err != nil {
			return fmt.Errorf("device info: %w", err)
		}
		fmt.Printf("device %s:%d — %d slots\n", info.IP, info.Port, info.NumSlots)
		walked := 0
		for s := 0; s < info.NumSlots; s++ {
			si, serr := plug.GetSlotInfo(opCtx, s)
			if serr != nil {
				fmt.Printf("\nslot %d — error reading status: %v\n", s, serr)
				continue
			}
			if si.Status != protocol.SlotPresent {
				continue
			}
			walked++
			fmt.Printf("\nslot %d:\n", s)
			objs, werr := plug.Walk(ctx, s)
			if werr != nil {
				fmt.Printf("\nslot %d — walk error: %v\n", s, werr)
				continue
			}
			if !streaming {
				printSlotTree(s, objs, *filter)
			} else {
				fmt.Printf("\nslot %d — %d objects\n", s, len(objs))
			}
		}
		fmt.Printf("\nwalked %d present slot(s)\n", walked)
		return nil
	}

	fmt.Printf("\nslot %d:\n", *slot)
	objs, err := plug.Walk(ctx, *slot)
	if err != nil {
		return err
	}
	if !streaming {
		printSlotTree(*slot, objs, *filter)
	} else {
		fmt.Printf("\nslot %d — %d objects\n", *slot, len(objs))
	}
	return nil
}

// printSlotTree is the shared render helper used by `walk --slot N` and
// `walk --all`. Moving it out of the runWalk body keeps the --all loop
// readable.
func printSlotTree(slot int, objs []protocol.Object, filter string) {
	fmt.Printf("\nslot %d — %d objects\n\n", slot, len(objs))
	filterLower := strings.ToLower(filter)
	// Group for a readable tree view. We rely on the walker returning
	// objects in (identity, control, status, alarm) order.
	var currentGroup string
	for _, o := range objs {
		if o.Group != currentGroup && filter == "" {
			fmt.Printf("\n[%s]\n", o.Group)
			currentGroup = o.Group
		}
		if o.SubGroupMarker {
			if filter == "" {
				// Render section headers with visual separation and strip the
				// leading-whitespace convention from the label.
				fmt.Printf("\n  ── %s ──\n", strings.TrimSpace(o.Label))
			}
			continue
		}
		// Format the current value captured during walk. For numeric
		// kinds we apply step-based precision so "50.8%" doesn't show
		// up as "50%". For strings/enums/ipaddr the inline formatter
		// is already type-aware.
		valStr := walkValueColumn(o)
		rngStr := walkRangeColumn(o)
		line := fmt.Sprintf("  %3d  %-20s  %-6s  %-3s  %-18s  %s",
			o.ID,
			truncate(o.Label, 20),
			kindName(o.Kind),
			accessStr(o.Access),
			truncate(valStr, 18),
			rngStr)
		if filter != "" && !strings.Contains(strings.ToLower(line), filterLower) {
			continue
		}
		if o.Group != currentGroup {
			fmt.Printf("\n[%s]\n", o.Group)
			currentGroup = o.Group
		}
		fmt.Println(line)
	}
}

// walkRangeColumn renders the per-object constraint column for walk.
// For numeric kinds it shows "min..max step unit". For enums it shows
// the item list. For strings it shows the max length. Empty for kinds
// without meaningful constraints (ipaddr, alarm, frame).
func walkRangeColumn(o protocol.Object) string {
	switch o.Kind {
	case protocol.KindInt:
		return fmt.Sprintf("%s..%s step %s%s",
			fmtNumPlain(o.Min), fmtNumPlain(o.Max), fmtNumPlain(o.Step),
			unitSuffix(o.Unit))
	case protocol.KindUint:
		return fmt.Sprintf("%s..%s step %s%s",
			fmtNumPlain(o.Min), fmtNumPlain(o.Max), fmtNumPlain(o.Step),
			unitSuffix(o.Unit))
	case protocol.KindFloat:
		d := decimalsFromStep(&o)
		minf, _ := o.Min.(float64)
		maxf, _ := o.Max.(float64)
		stepf, _ := o.Step.(float64)
		return fmt.Sprintf("%.*f..%.*f step %.*f%s",
			d, minf, d, maxf, d, stepf, unitSuffix(o.Unit))
	case protocol.KindEnum:
		return "[" + strings.Join(o.EnumItems, ", ") + "]"
	case protocol.KindString:
		if o.MaxLen > 0 {
			return fmt.Sprintf("max %d chars", o.MaxLen)
		}
		return ""
	case protocol.KindAlarm:
		return fmt.Sprintf("tag 0x%02X", o.AlarmTag)
	}
	return ""
}

// fmtNumPlain prints Min/Max/Step/Def values in their native Go type
// without decimals or unit suffix — used in the narrow range column.
func fmtNumPlain(v any) string {
	switch n := v.(type) {
	case int64:
		return fmt.Sprintf("%d", n)
	case uint64:
		return fmt.Sprintf("%d", n)
	case float64:
		return fmt.Sprintf("%g", n)
	case nil:
		return "-"
	default:
		return fmt.Sprintf("%v", n)
	}
}

// unitSuffix returns " unit" (leading space) for non-empty units so we
// can concatenate with a number without worrying about a bare trailing
// space when the unit is missing.
func unitSuffix(u string) string {
	if u == "" {
		return ""
	}
	if u == "%" {
		return "%"
	}
	return " " + u
}

// walkValueColumn renders the per-object value column for `acp walk`.
// Uses the formatValue path (which respects step-based float precision
// and applies the object's unit) when an object has usable metadata;
// falls back to the compact inline formatter otherwise.
func walkValueColumn(o protocol.Object) string {
	switch o.Value.Kind {
	case protocol.KindInt:
		return appendUnit(fmt.Sprintf("%d", o.Value.Int), &o)
	case protocol.KindUint:
		return appendUnit(fmt.Sprintf("%d", o.Value.Uint), &o)
	case protocol.KindFloat:
		return appendUnit(fmt.Sprintf("%.*f", decimalsFromStep(&o), o.Value.Float), &o)
	case protocol.KindEnum:
		if o.Value.Str != "" {
			return fmt.Sprintf("%q", o.Value.Str)
		}
		return fmt.Sprintf("idx %d", o.Value.Enum)
	case protocol.KindString:
		return fmt.Sprintf("%q", o.Value.Str)
	case protocol.KindIPAddr:
		return fmt.Sprintf("%d.%d.%d.%d",
			o.Value.IPAddr[0], o.Value.IPAddr[1], o.Value.IPAddr[2], o.Value.IPAddr[3])
	case protocol.KindFrame:
		return formatFrameStatus(o.Value.SlotStatus)
	}
	return ""
}

// kindName returns a short, human-readable label for a ValueKind.
func kindName(k protocol.ValueKind) string {
	switch k {
	case protocol.KindBool:
		return "bool"
	case protocol.KindInt:
		return "int"
	case protocol.KindUint:
		return "uint"
	case protocol.KindFloat:
		return "float"
	case protocol.KindEnum:
		return "enum"
	case protocol.KindString:
		return "string"
	case protocol.KindIPAddr:
		return "ipaddr"
	case protocol.KindAlarm:
		return "alarm"
	case protocol.KindFrame:
		return "frame"
	case protocol.KindRaw:
		return "raw"
	default:
		return "?"
	}
}

// accessStr renders the ACP1 access bitmask as the familiar R/W/D triplet.
// Bit 0 = read, bit 1 = write, bit 2 = setDefault. A dash in a slot means
// the capability is absent.
func accessStr(a uint8) string {
	r := "-"
	if a&0x01 != 0 {
		r = "R"
	}
	w := "-"
	if a&0x02 != 0 {
		w = "W"
	}
	d := "-"
	if a&0x04 != 0 {
		d = "D"
	}
	return r + w + d
}

// ---------------------------------------------------------------- get

func runGet(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", -1, "slot number (required)")
	group := fs.String("group", "", "object group (optional when --label is unique across groups)")
	label := fs.String("label", "", "object label (preferred over --id, requires prior walk context)")
	id := fs.Int("id", -1, "object id within group (alternative to --label)")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: acp get <host> --slot N --group G (--label L | --id I)")
	}
	_ = fs.Parse(rest)
	if *slot < 0 {
		return fmt.Errorf("--slot is required")
	}
	if *label == "" && *id < 0 {
		return fmt.Errorf("either --label or --id is required")
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	// If addressing by label, run a walk first to populate the plugin's
	// label map. Explicit --id addressing skips the walk.
	if *label != "" {
		if _, err := plug.Walk(opCtx, *slot); err != nil {
			return fmt.Errorf("walk for label resolution: %w", err)
		}
	}

	req := protocol.ValueRequest{
		Slot:  *slot,
		Group: *group,
		Label: *label,
		ID:    *id,
	}
	val, err := plug.GetValue(opCtx, req)
	if err != nil {
		return err
	}
	// Look up the object metadata (range, step, unit) before formatting
	// so we can apply unit suffixes and step-based float precision.
	var meta *protocol.Object
	if *label != "" {
		meta = findObjectByLabel(plug, *slot, *group, *label)
	}
	fmt.Println(formatValue(val, meta))
	if len(val.Raw) > 0 {
		fmt.Printf("raw  = %s\n", hex.EncodeToString(val.Raw))
	}
	if meta != nil {
		printObjectMeta(*meta)
	}
	return nil
}

// findObjectByLabel peeks into the plugin's cached walker tree for the
// Object matching (slot, group, label). The plugin interface doesn't
// expose a "get metadata" method yet — we round-trip through a second
// Walk-less addressing pass by reusing the Plugin's internal resolve via
// a small helper. For now, we just walk again here — the walker caches
// per slot so the second call is a no-op lookup, not a re-traversal.
//
// This function is cmd-only glue; it does not belong in the library.
func findObjectByLabel(plug protocol.Protocol, slot int, group, label string) *protocol.Object {
	// Walk is idempotent and cached per slot inside the plugin, so this
	// returns the already-walked list without re-hitting the device.
	objs, err := plug.Walk(context.Background(), slot)
	if err != nil {
		return nil
	}
	for i := range objs {
		if objs[i].Label != label {
			continue
		}
		if group != "" && objs[i].Group != group {
			continue
		}
		return &objs[i]
	}
	return nil
}

// printObjectMeta prints everything the walker captured about an object:
// kind, access, and whichever constraint fields are relevant to its kind.
// Every numeric type gets range/step/default/unit; enums get their item
// list; strings get max length; alarms get priority/tag/messages; ipaddr
// gets default and optionally the declared range (though most devices
// leave it as 0.0.0.0..255.255.255.255 which we hide to avoid noise).
func printObjectMeta(o protocol.Object) {
	fmt.Printf("kind = %s  access = %s\n", kindName(o.Kind), accessStr(o.Access))

	switch o.Kind {
	case protocol.KindInt:
		fmt.Printf("range = %s .. %s  step = %s  default = %s\n",
			fmtNum(o.Min, &o, false),
			fmtNum(o.Max, &o, false),
			fmtNum(o.Step, &o, false),
			fmtNum(o.Def, &o, true),
		)

	case protocol.KindUint:
		fmt.Printf("range = %s .. %s  step = %s  default = %s\n",
			fmtNum(o.Min, &o, false),
			fmtNum(o.Max, &o, false),
			fmtNum(o.Step, &o, false),
			fmtNum(o.Def, &o, true),
		)

	case protocol.KindFloat:
		fmt.Printf("range = %s .. %s  step = %s  default = %s\n",
			fmtNum(o.Min, &o, false),
			fmtNum(o.Max, &o, false),
			fmtNum(o.Step, &o, false),
			fmtNum(o.Def, &o, true),
		)

	case protocol.KindEnum:
		if o.Def != nil {
			switch d := o.Def.(type) {
			case string:
				fmt.Printf("items = [%s]  (default %q)\n",
					strings.Join(o.EnumItems, ", "), d)
			default:
				fmt.Printf("items = [%s]  (default idx %v)\n",
					strings.Join(o.EnumItems, ", "), o.Def)
			}
		} else {
			fmt.Printf("items = [%s]\n",
				strings.Join(o.EnumItems, ", "))
		}

	case protocol.KindString:
		fmt.Printf("max length = %d chars\n", o.MaxLen)

	case protocol.KindIPAddr:
		if d, ok := o.Def.(uint64); ok {
			fmt.Printf("default = %d.%d.%d.%d\n",
				byte(d>>24), byte(d>>16), byte(d>>8), byte(d))
		}

	case protocol.KindAlarm:
		fmt.Printf("priority = %d  tag = 0x%02X\n", o.AlarmPriority, o.AlarmTag)
		if o.AlarmOnMsg != "" {
			fmt.Printf("event on  = %q\n", o.AlarmOnMsg)
		}
		if o.AlarmOffMsg != "" {
			fmt.Printf("event off = %q\n", o.AlarmOffMsg)
		}

	case protocol.KindFrame:
		fmt.Println("frame status — use `acp info` for slot list")
	}
}

// fmtNum renders any of the numeric constraint fields (Min/Max/Step/Def)
// as a display string with the object's unit appended when `withUnit`
// is true. Falls back to %v on unexpected types.
func fmtNum(v any, obj *protocol.Object, withUnit bool) string {
	var s string
	switch n := v.(type) {
	case int64:
		s = fmt.Sprintf("%d", n)
	case uint64:
		s = fmt.Sprintf("%d", n)
	case float64:
		s = fmt.Sprintf("%.*f", decimalsFromStep(obj), n)
	case nil:
		return "-"
	default:
		s = fmt.Sprintf("%v", n)
	}
	if withUnit {
		return appendUnit(s, obj)
	}
	return s
}

// formatValue renders a typed protocol.Value for human consumption.
// When obj is non-nil it uses the object's Unit and (for floats) its
// Step to pick a sensible decimal precision. When obj is nil it falls
// back to compact %g formatting with no unit suffix.
func formatValue(v protocol.Value, obj *protocol.Object) string {
	switch v.Kind {
	case protocol.KindInt:
		return "value = " + appendUnit(fmt.Sprintf("%d", v.Int), obj)
	case protocol.KindUint:
		return "value = " + appendUnit(fmt.Sprintf("%d", v.Uint), obj)
	case protocol.KindFloat:
		dec := decimalsFromStep(obj)
		return "value = " + appendUnit(fmt.Sprintf("%.*f", dec, v.Float), obj)
	case protocol.KindEnum:
		if v.Str != "" {
			return fmt.Sprintf("value = %q  (enum idx %d)", v.Str, v.Enum)
		}
		return fmt.Sprintf("value = idx %d  (enum)", v.Enum)
	case protocol.KindString:
		return fmt.Sprintf("value = %q", v.Str)
	case protocol.KindIPAddr:
		return fmt.Sprintf("value = %d.%d.%d.%d",
			v.IPAddr[0], v.IPAddr[1], v.IPAddr[2], v.IPAddr[3])
	case protocol.KindFrame:
		return "value = " + formatFrameStatus(v.SlotStatus)
	case protocol.KindRaw:
		return fmt.Sprintf("value = (raw, %d bytes)", len(v.Raw))
	default:
		return fmt.Sprintf("value = ?  (kind %d)", v.Kind)
	}
}

// appendUnit attaches the object's Unit string to a formatted number.
// Convention:
//   - "%"  — no space before the unit ("50%")
//   - other units — single space ("-2.37 dB", "100 ms")
//   - empty — no unit appended
func appendUnit(num string, obj *protocol.Object) string {
	if obj == nil || obj.Unit == "" {
		return num
	}
	if obj.Unit == "%" {
		return num + "%"
	}
	return num + " " + obj.Unit
}

// decimalsFromStep picks a display precision for a float based on the
// object's declared Step. Examples:
//
//	step = 1     → 1 decimal  ("50.8 %")   — minimum 1 for floats
//	step = 0.1   → 1 decimal  ("50.8 %")
//	step = 0.01  → 2 decimals ("-2.37 dB")
//	step = 0.001 → 3 decimals
//
// Minimum is 1 — a "whole" number stored in a float field can still
// carry fractional parts (e.g. the emulator stored 50.8 despite
// declaring step=1). Dropping fractions on display would hide truth.
// Falls back to 2 decimals when no metadata is available.
func decimalsFromStep(obj *protocol.Object) int {
	if obj == nil {
		return 2
	}
	step, ok := obj.Step.(float64)
	if !ok || step <= 0 {
		return 2
	}
	if step >= 1 {
		return 1
	}
	d := -int(math.Floor(math.Log10(step)))
	if d < 1 {
		return 1
	}
	if d > 6 {
		return 6
	}
	return d
}

// ---------------------------------------------------------------- set

func runSet(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("set", flag.ExitOnError)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", -1, "slot number (required)")
	group := fs.String("group", "", "object group name")
	label := fs.String("label", "", "object label")
	id := fs.Int("id", -1, "object id within group")
	valueStr := fs.String("value", "", "typed value (e.g. -3.0, \"On\", \"192.168.1.5\", \"CH1\")")
	valueHex := fs.String("raw", "", "raw wire bytes as hex — escape hatch bypassing typed encoding")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: acp set <host> --slot N --group G (--label L | --id I) --value <v>")
	}
	_ = fs.Parse(rest)
	if *slot < 0 {
		return fmt.Errorf("--slot is required")
	}
	if *valueStr == "" && *valueHex == "" {
		return fmt.Errorf("either --value or --raw is required")
	}
	if *label == "" && *id < 0 {
		return fmt.Errorf("either --label or --id is required")
	}

	var val protocol.Value
	if *valueHex != "" {
		raw, herr := hex.DecodeString(strings.TrimPrefix(*valueHex, "0x"))
		if herr != nil {
			return fmt.Errorf("--raw: %w", herr)
		}
		val = protocol.Value{Kind: protocol.KindRaw, Raw: raw}
	} else {
		// Typed value: stash the user's string and let EncodeValueBytes
		// coerce it to the right wire form based on the object's kind.
		val = protocol.Value{Str: *valueStr}
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	if *label != "" {
		if _, err := plug.Walk(opCtx, *slot); err != nil {
			return fmt.Errorf("walk for label resolution: %w", err)
		}
	}

	req := protocol.ValueRequest{
		Slot:  *slot,
		Group: *group,
		Label: *label,
		ID:    *id,
	}
	confirmed, err := plug.SetValue(opCtx, req, val)
	if err != nil {
		return err
	}
	var meta *protocol.Object
	if *label != "" {
		meta = findObjectByLabel(plug, *slot, *group, *label)
	}
	fmt.Println("confirmed " + formatValue(confirmed, meta))
	if len(confirmed.Raw) > 0 {
		fmt.Printf("raw       = %s\n", hex.EncodeToString(confirmed.Raw))
	}
	return nil
}

// ---------------------------------------------------------------- watch

// runWatch subscribes to live announcements and prints each event as it
// arrives. Blocks until Ctrl-C. Filters:
//
//	--slot N        only this slot (default: any)
//	--group G       only this group (default: any)
//	--label L       only this object (requires prior walk for resolution)
//	--id I          only this object id within --group
//
// Typical usage: leave filters off and watch everything on the device.
// Useful when debugging an emulator or verifying that a UI change
// reaches the wire.
func runWatch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", -1, "slot filter (-1 = any)")
	group := fs.String("group", "", "group filter (empty = any)")
	label := fs.String("label", "", "label filter (requires prior walk)")
	id := fs.Int("id", -1, "object id filter (-1 = any)")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: acp watch <host> [--slot N] [--group G] [--label L]")
	}
	_ = fs.Parse(rest)

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	// Pre-walk to populate the tree for label resolution and typed decode.
	// Same pattern as ACP1: walk before subscribe so callbacks get labels.
	// No timeout — walk takes as long as it takes (Ctrl-C to abort).
	if *slot >= 0 {
		if _, werr := plug.Walk(ctx, *slot); werr != nil {
			fmt.Fprintf(os.Stderr, "warning: walk slot %d failed: %v\n", *slot, werr)
		}
	} else {
		info, ierr := plug.GetDeviceInfo(ctx)
		if ierr == nil {
			for s := 0; s < info.NumSlots; s++ {
				si, err := plug.GetSlotInfo(ctx, s)
				if err != nil || si.Status != protocol.SlotPresent {
					continue
				}
				_, _ = plug.Walk(ctx, s)
			}
		}
	}

	req := protocol.ValueRequest{
		Slot:  *slot,
		Group: *group,
		Label: *label,
		ID:    *id,
	}

	// Subscribe. The plugin pushes decoded Event values into our channel
	// via the callback; we print them from the main goroutine so output
	// is serialised cleanly with Ctrl-C handling.
	events := make(chan protocol.Event, 128)
	if err := plug.Subscribe(req, func(ev protocol.Event) {
		select {
		case events <- ev:
		default:
			// Drop on full buffer — better than blocking the receive
			// goroutine and missing unrelated events.
		}
	}); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	defer func() { _ = plug.Unsubscribe(req) }()

	fmt.Println("watching — Ctrl-C to stop")
	fmt.Printf("%-8s  %-10s  %-4s  %-20s  value\n", "time", "group", "id", "label")
	fmt.Println(strings.Repeat("-", 72))
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-events:
			fmt.Printf("%s  s%-2d %-7s  %-4d  %-20s  %s\n",
				ev.Timestamp.Format("15:04:05"),
				ev.Slot,
				ev.Group,
				ev.ID,
				truncate(ev.Label, 20),
				formatValueInline(ev.Value),
			)
		}
	}
}

// formatValueInline is a compact value renderer for the watch output.
// Loses the unit (we don't have the Object here) but still typed.
func formatValueInline(v protocol.Value) string {
	switch v.Kind {
	case protocol.KindInt:
		return fmt.Sprintf("%d", v.Int)
	case protocol.KindUint:
		return fmt.Sprintf("%d", v.Uint)
	case protocol.KindFloat:
		return fmt.Sprintf("%.2f", v.Float)
	case protocol.KindEnum:
		if v.Str != "" {
			return fmt.Sprintf("%q (idx %d)", v.Str, v.Enum)
		}
		return fmt.Sprintf("idx %d", v.Enum)
	case protocol.KindString:
		return fmt.Sprintf("%q", v.Str)
	case protocol.KindIPAddr:
		return fmt.Sprintf("%d.%d.%d.%d", v.IPAddr[0], v.IPAddr[1], v.IPAddr[2], v.IPAddr[3])
	case protocol.KindFrame:
		return formatFrameStatus(v.SlotStatus)
	case protocol.KindRaw:
		return fmt.Sprintf("raw(%d)", len(v.Raw))
	default:
		return "?"
	}
}

// formatFrameStatus renders a slot-status slice compactly: each slot
// becomes one letter so the full 31-slot state of a rack fits on a
// single terminal line. Legend is printed alongside so the symbols are
// self-explanatory.
//
//	.  no card       0
//	U  power-up      1
//	P  present       2
//	E  error         3
//	R  removed       4
//	B  boot mode     5
//	?  unknown       (other)
func formatFrameStatus(statuses []protocol.SlotStatus) string {
	if len(statuses) == 0 {
		return "frame: (empty)"
	}
	var b strings.Builder
	b.WriteString("frame: ")
	for _, s := range statuses {
		b.WriteByte(slotStatusChar(s))
	}
	// Also surface any non-empty slots with their names, so you see
	// "slot 1=boot, slot 10=present" without having to decode the
	// symbol strip by eye.
	first := true
	b.WriteString("  [")
	for i, s := range statuses {
		if s == protocol.SlotNoCard {
			continue
		}
		if !first {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%d=%s", i, s)
		first = false
	}
	if first {
		// no non-empty slots at all
		b.WriteString("empty")
	}
	b.WriteByte(']')
	return b.String()
}

func slotStatusChar(s protocol.SlotStatus) byte {
	switch s {
	case protocol.SlotNoCard:
		return '.'
	case protocol.SlotPowerUp:
		return 'U'
	case protocol.SlotPresent:
		return 'P'
	case protocol.SlotError:
		return 'E'
	case protocol.SlotRemoved:
		return 'R'
	case protocol.SlotBootMode:
		return 'B'
	default:
		return '?'
	}
}

// ---------------------------------------------------------------- export

// runExport walks every present slot on the device and writes the
// snapshot to disk in json / yaml / csv form. Stream to stdout when
// --out is omitted. Format is derived from the --format flag first,
// the --out filename extension second, defaulting to json.
func runExport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	cf := addCommonFlags(fs)
	format := fs.String("format", "", "output format: json | yaml | csv (default: json or from --out extension)")
	out := fs.String("out", "", "output file path (default: stdout)")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: acp export <host> [--format F] [--out FILE]")
	}
	_ = fs.Parse(rest)

	// Format resolution: --format wins; otherwise guess from --out extension.
	fmtStr := *format
	if fmtStr == "" && *out != "" {
		ext := strings.ToLower(filepath.Ext(*out))
		switch ext {
		case ".yaml", ".yml":
			fmtStr = "yaml"
		case ".csv":
			fmtStr = "csv"
		default:
			fmtStr = "json"
		}
	}
	fmtEnum, err := export.ParseFormat(fmtStr)
	if err != nil {
		return err
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	// Build the snapshot: walk every present slot and copy its objects.
	info, err := plug.GetDeviceInfo(opCtx)
	if err != nil {
		return fmt.Errorf("device info: %w", err)
	}
	snap := &export.Snapshot{
		Device: export.DeviceInfo{
			IP:              info.IP,
			Port:            info.Port,
			Protocol:        cf.protocol,
			ProtocolVersion: info.ProtocolVersion,
			NumSlots:        info.NumSlots,
		},
		Generator: "acp " + version,
		CreatedAt: time.Now().UTC(),
	}
	for s := 0; s < info.NumSlots; s++ {
		si, serr := plug.GetSlotInfo(opCtx, s)
		if serr != nil {
			continue
		}
		if si.Status != protocol.SlotPresent {
			continue
		}
		slotCtx, slotCancel := withTimeout(ctx, cf.timeout)
		objs, werr := plug.Walk(slotCtx, s)
		slotCancel()
		if werr != nil {
			fmt.Fprintf(os.Stderr, "warning: slot %d walk failed: %v\n", s, werr)
			continue
		}
		snap.Slots = append(snap.Slots, export.SlotDump{
			Slot:     s,
			Status:   si.Status.String(),
			WalkedAt: time.Now().UTC(),
			Objects:  objs,
		})
	}

	// Pick the output writer: file or stdout.
	var w io.Writer = os.Stdout
	if *out != "" {
		f, ferr := os.Create(*out)
		if ferr != nil {
			return fmt.Errorf("create %s: %w", *out, ferr)
		}
		defer f.Close()
		w = f
	}

	switch fmtEnum {
	case export.FormatJSON:
		if err := export.WriteJSON(w, snap); err != nil {
			return err
		}
	case export.FormatYAML:
		if err := export.WriteYAML(w, snap); err != nil {
			return err
		}
	case export.FormatCSV:
		if err := export.WriteCSV(w, snap); err != nil {
			return err
		}
	}

	if *out != "" {
		fmt.Fprintf(os.Stderr, "exported %d slots to %s (%s)\n",
			len(snap.Slots), *out, fmtEnum)
	}
	return nil
}

func helpExport() {
	fmt.Println(`acp export — dump a walked device to json / yaml / csv

USAGE
  acp export <host> [--format F] [--out FILE] [flags]

DESCRIPTION
  Walks every present slot on the device (same as 'acp walk --all')
  and writes the result to a snapshot file. Three formats:

    json  lossless, stdlib encoding, pretty-printed
    yaml  lossless, hand-rolled emitter, 2-space indent
    csv   lossy, one row per object, header row, '|' for nested fields

  Format is picked from --format first, then the --out extension,
  defaulting to json. With no --out the snapshot streams to stdout.

FLAGS
  --format F         json | yaml | csv   (default: json or from extension)
  --out FILE         output file path    (default: stdout)

EXAMPLES
  acp export 10.6.239.113 --format json --out device.json
  acp export 10.6.239.113 --format yaml --out device.yaml
  acp export 10.6.239.113 --format csv  --out device.csv
  acp export 10.6.239.113 > device.json`)
}

// ---------------------------------------------------------------- import

func runImport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	cf := addCommonFlags(fs)
	file := fs.String("file", "", "snapshot file (.json, .yaml, .csv)")
	dry := fs.Bool("dry-run", false, "validate and list would-write actions without sending")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: acp import <host> --file SNAPSHOT [--dry-run]")
	}
	_ = fs.Parse(rest)
	if *file == "" {
		return fmt.Errorf("--file is required")
	}

	snap, err := export.LoadSnapshot(*file)
	if err != nil {
		return err
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	rep, err := export.Apply(opCtx, plug, snap, *dry)
	if err != nil {
		return err
	}

	tag := "applied"
	if *dry {
		tag = "would apply"
	}
	fmt.Printf("%s %d, skipped %d, failed %d\n", tag, rep.Applied, rep.Skipped, rep.Failed)
	if len(rep.Failures) > 0 {
		fmt.Println("failures:")
		for _, f := range rep.Failures {
			fmt.Println("  -", f)
		}
	}
	return nil
}

func helpImport() {
	fmt.Println(`acp import — apply values from a snapshot file

USAGE
  acp import <host> --file SNAPSHOT [--dry-run] [flags]

DESCRIPTION
  Reads a snapshot produced by 'acp export' and writes every writable
  object's value back to the device. Read-only objects are skipped;
  alarm priorities and frame status are also skipped (they have
  dedicated paths). YAML and CSV import are not supported — use JSON.

  --dry-run lists what WOULD be written without touching the device.
  Run it first to preview the effect.

FLAGS
  --file PATH        snapshot file (json only)             (required)
  --dry-run          validate without writing

EXAMPLES
  acp import 10.6.239.113 --file device.json --dry-run
  acp import 10.6.239.113 --file device.json`)
}

// ---------------------------------------------------------------- discover

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

func helpDiscover() {
	fmt.Println(`acp discover — passive + active LAN scan for ACP1 devices

USAGE
  acp discover [--duration 5s] [--active] [--scan-port 2071]

DESCRIPTION
  Finds ACP1 devices on the local subnet without needing to know
  their IP addresses upfront. Two modes run in parallel:

    PASSIVE  — listen on :2071 for UDP announcements. Catches any
               device whose "Broadcasts" setting is On.

    ACTIVE   — send one getValue(FrameStatus,0) request to the
               subnet broadcast address 255.255.255.255:2071.
               Every rack controller replies with a directed unicast
               message that the listener picks up. Active is ON by
               default.

  IMPORTANT: This ONLY works when your host is on the same subnet
  (same VLAN, same broadcast domain) as the devices. Subnet broadcasts
  do not cross routers. Running 'acp discover' across a pfSense /
  router boundary will return zero results even if the devices are
  reachable via unicast.

FLAGS
  --duration DUR     how long to collect results (default 5s)
  --active           enable the broadcast probe (default: true)
  --scan-port N      ACP port (default 2071)

EXAMPLES
  acp discover
  acp discover --duration 15s
  acp discover --active=false          # passive-only scan`)
}

// ---------------------------------------------------------------- diag

func runDiag(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("diag", flag.ExitOnError)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", 0, "target slot")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: acp diag <host> [--slot N]")
	}
	_ = fs.Parse(rest)
	_ = cf

	lvl := slog.LevelDebug
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))

	port := 2072
	results, err := acp2.RunDiagnostics(ctx, host, port, uint8(*slot), logger)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("%-45s  %-30s  %s\n", "PROBE", "STATUS", "REPLY")
	fmt.Println(strings.Repeat("-", 100))
	for _, r := range results {
		fmt.Printf("%-45s  %-30s  %s\n", r.Name, r.Status, r.Reply)
	}
	fmt.Printf("\nSent payloads:\n")
	for _, r := range results {
		fmt.Printf("  %-45s  %s\n", r.Name, r.Sent)
	}
	return nil
}

func helpDiag() {
	fmt.Println(`acp diag — run ACP2 diagnostic probes

USAGE
  acp diag <host> [--slot N]

DESCRIPTION
  Connects to the device, completes the AN2 handshake, then sends
  multiple ACP2 request variants to discover which format the device
  accepts. Reports success/failure for each probe.

EXAMPLES
  acp diag 10.41.40.195 --slot 0
  acp diag 10.41.40.195 --slot 1`)
}

// ---------------------------------------------------------------- list-protocols

func runListProtocols() error {
	names := protocol.List()
	if len(names) == 0 {
		fmt.Println("(no protocols registered — this is a build configuration bug)")
		return nil
	}
	for _, name := range names {
		f, err := protocol.Get(name)
		if err != nil {
			continue
		}
		m := f.Meta()
		fmt.Printf("%-8s port=%-5d %s\n", m.Name, m.DefaultPort, m.Description)
	}
	return nil
}

// ---------------------------------------------------------------- helpers

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// popHost extracts the first non-flag argument as the host and returns
// the remainder for flag.Parse. Go's stdlib flag package stops parsing at
// the first non-flag token, so we separate the positional host argument
// from the flags manually. This lets users write the natural order:
//
//	acp walk 10.6.239.113 --slot 0
//
// instead of being forced to put flags before positional args.
func popHost(args []string) (string, []string, error) {
	// Skip flags AND their values. A flag like "--capture FILE" means
	// the next arg is the flag's value, not the host. Flags that use
	// "=" syntax (--capture=FILE) are handled by the HasPrefix check.
	// Boolean flags (--verbose, --all, --dry-run, --active) have no
	// separate value arg.
	boolFlags := map[string]bool{
		"-verbose": true, "--verbose": true,
		"-all": true, "--all": true,
		"-dry-run": true, "--dry-run": true,
		"-active": true, "--active": true,
	}
	skipNext := false
	for i, a := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(a, "-") {
			// If it's not a boolean flag AND doesn't use = syntax,
			// the next arg is the flag's value — skip it too.
			if !boolFlags[a] && !strings.Contains(a, "=") {
				skipNext = true
			}
			continue
		}
		rest := make([]string, 0, len(args)-1)
		rest = append(rest, args[:i]...)
		rest = append(rest, args[i+1:]...)
		return a, rest, nil
	}
	return "", nil, fmt.Errorf("host argument missing")
}
