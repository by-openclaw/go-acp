package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"acp/internal/probel-sw08p/codec"
	probelproto "acp/internal/probel-sw08p/consumer"
	"acp/internal/transport"
)

// runProbel dispatches `dhs consumer probel-sw08p <subcommand>` — the Probel SW-P-08
// toolset. Each subcommand runs a single round-trip request and prints
// the decoded reply + the wire hex on stderr (hex goes via the slog
// INFO handler inside codec.Client).
//
// Global --capture FILE.jsonl is parsed at the top level and stashed in
// the context so every subcommand sees the same recorder. Same JSONL
// shape as acp1/acp2/emberplus capture — one {ts, proto, dir, hex, len}
// object per frame (including DLE ACK / DLE NAK control sequences).
func runProbelsw08p(ctx context.Context, args []string) error {
	args, rec, err := extractCaptureFlag(args)
	if err != nil {
		return err
	}
	if rec != nil {
		defer func() { _ = rec.Close() }()
		ctx = context.WithValue(ctx, probelRecorderKey{}, rec)
	}
	args, mc, mcSet, err := extractMatrixConfigFlags(args)
	if err != nil {
		return err
	}
	if mcSet {
		ctx = context.WithValue(ctx, probelMatrixConfigKey{}, mc)
	}
	if len(args) == 0 || hasHelpFlag(args) {
		helpProbel()
		return nil
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "interrogate":
		return runProbelInterrogate(ctx, rest)
	case "connect":
		return runProbelConnect(ctx, rest)
	case "watch":
		return runProbelWatch(ctx, rest)
	case "maintenance":
		return runProbelMaintenance(ctx, rest)
	case "dual-status":
		return runProbelDualStatus(ctx, rest)
	case "tally-dump":
		return runProbelTallyDump(ctx, rest)
	case "protect-interrogate":
		return runProbelProtectInterrogate(ctx, rest)
	case "protect-connect":
		return runProbelProtectConnect(ctx, rest)
	case "protect-disconnect":
		return runProbelProtectDisconnect(ctx, rest)
	case "protect-name":
		return runProbelProtectName(ctx, rest)
	case "protect-dump":
		return runProbelProtectDump(ctx, rest)
	case "master-protect":
		return runProbelMasterProtect(ctx, rest)
	case "discover":
		return runProbelDiscover(ctx, rest)
	case "all-source-names":
		return runProbelAllSourceNames(ctx, rest)
	case "single-source-name":
		return runProbelSingleSourceName(ctx, rest)
	case "all-dest-names":
		return runProbelAllDestAssocNames(ctx, rest)
	case "single-dest-name":
		return runProbelSingleDestAssocName(ctx, rest)
	case "all-source-assoc-names":
		return runProbelAllSourceAssocNames(ctx, rest)
	case "single-source-assoc-name":
		return runProbelSingleSourceAssocName(ctx, rest)
	case "bench":
		return runProbelBench(ctx, rest)
	case "salvo-connect":
		return runProbelSalvoConnect(ctx, rest)
	}
	return fmt.Errorf("unknown probel subcommand %q", sub)
}

func helpProbel() {
	fmt.Println(`dhs consumer probel-sw08p — Probel SW-P-08 / SW-P-88 controller toolset

USAGE
  dhs consumer probel-sw08p <subcommand> <host:port> [flags]

GLOBAL FLAGS (apply to every subcommand)
  --capture FILE.jsonl   record every wire frame (TX + RX, including
                         DLE ACK / DLE NAK control sequences) as JSONL;
                         same format as dhs consumer <proto> walk / dhs consumer <proto> get --capture

SUBCOMMANDS
  discover                  one-shot discovery: dual-status + src labels + dst
                            labels + tally-dump on (matrix, level)
  interrogate               query current source on one (matrix, level, dst)
  connect                   route a source to a destination on (matrix, level)
  watch                     subscribe to async tallies until Ctrl-C
  maintenance               send a maintenance message (reset / clear-protects)
  dual-status               read 1:1 redundancy state
  tally-dump                dump every crosspoint on (matrix, level)
  all-source-names          fetch every source label on (matrix, level)
  single-source-name        fetch one source label
  all-dest-names            fetch every destination label on (matrix)
  single-dest-name          fetch one destination label
  all-source-assoc-names    fetch every source association (device) label
  single-source-assoc-name  fetch one source association label
  protect-interrogate       read protect state on one (matrix, level, dst)
  protect-connect           set a protect on (matrix, level, dst) for a device
  protect-disconnect        clear a protect
  protect-name              resolve device id → 8-char name
  protect-dump              dump every protect on (matrix, level)
  master-protect            master-override protect connect
  bench                     scale benchmark: interrogate-all + connect-all
                            on a persistent TCP connection (see
                            memory/project_scale_bench_2mtx_65535.md)

EXAMPLES
  dhs consumer probel-sw08p interrogate         127.0.0.1:2008 --matrix 0 --level 0 --dst 5
  dhs consumer probel-sw08p connect             127.0.0.1:2008 --matrix 0 --level 0 --dst 5 --src 12
  dhs consumer probel-sw08p bench 127.0.0.1:2008 --matrix 0,1 --size 65535 \
    --csv bench.csv --md bench.md
  dhs consumer probel-sw08p tally-dump          127.0.0.1:2008 --matrix 0 --level 0
  dhs consumer probel-sw08p watch               127.0.0.1:2008

All commands log wire bytes (post-escape, post-framing) on stderr as a
space-separated lowercase-hex line for debugging:
  probel TX ... hex=10 02 01 01 00 05 0c 03 1f 10 03`)
}

// dialProbel is the common connect-or-die helper. Returns a connected
// plugin plus a deferred-close func the caller must run. If the context
// carries a *transport.Recorder (set by the global --capture flag at
// the probel root dispatcher) it is attached before Connect so the
// JSONL file captures the full TX/RX stream.
func dialProbel(ctx context.Context, addr string) (*probelproto.Plugin, func(), error) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	f := &probelproto.Factory{}
	p := f.New(logger).(*probelproto.Plugin)
	if rec, ok := ctx.Value(probelRecorderKey{}).(*transport.Recorder); ok && rec != nil {
		p.SetRecorder(rec)
	}
	if mc, ok := ctx.Value(probelMatrixConfigKey{}).(probelproto.MatrixConfig); ok {
		p.SetMatrixConfig(mc)
	}
	host, port, err := splitHostPort(addr, probelproto.DefaultPort)
	if err != nil {
		return nil, func() {}, err
	}
	if err := p.Connect(ctx, host, port); err != nil {
		return nil, func() {}, err
	}
	return p, func() { _ = p.Disconnect() }, nil
}

// probelRecorderKey is the context.Context key for the optional
// JSONL traffic recorder shared across a single `dhs consumer probel-sw08p` invocation.
type probelRecorderKey struct{}

// probelMatrixConfigKey is the context.Context key for the optional
// matrix config (mtxid + level + dsts + srcs) supplied by the global
// CLI flags. Matches VSM's per-matrix configuration pattern.
type probelMatrixConfigKey struct{}

// extractMatrixConfigFlags pulls --mtx-id / --level / --dsts / --srcs
// out of args BEFORE sub-command dispatch. Returns the remaining args,
// the parsed config, and a sticky bool that's true when any of the
// four flags were observed (so callers know whether to apply the
// MatrixConfig at all). All four flags accept --flag=N or --flag N.
//
// Defaults: --mtx-id=0, --level=0. --dsts and --srcs default to 0
// (not required for verbs that don't need them; required for the
// future bootstrap-sweep / keep-alive ping wiring that mirrors what
// SW-P-02 ships).
func extractMatrixConfigFlags(args []string) ([]string, probelproto.MatrixConfig, bool, error) {
	var mc probelproto.MatrixConfig
	var seen bool
	out := make([]string, 0, len(args))

	parseUint := func(name, val string, max uint64) (uint64, error) {
		n, err := strconv.ParseUint(val, 10, 32)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", name, err)
		}
		if n > max {
			return 0, fmt.Errorf("%s: %d exceeds %d", name, n, max)
		}
		return n, nil
	}

	for i := 0; i < len(args); i++ {
		a := args[i]
		var (
			name string
			val  string
			ok   bool
		)
		switch {
		case strings.HasPrefix(a, "--mtx-id=") || strings.HasPrefix(a, "-mtx-id="):
			name = "--mtx-id"
			val = strings.SplitN(a, "=", 2)[1]
			ok = true
		case a == "--mtx-id" || a == "-mtx-id":
			if i+1 >= len(args) {
				return nil, mc, false, fmt.Errorf("--mtx-id requires a value")
			}
			name, val, ok = "--mtx-id", args[i+1], true
			i++
		case strings.HasPrefix(a, "--level=") || strings.HasPrefix(a, "-level="):
			name = "--level"
			val = strings.SplitN(a, "=", 2)[1]
			ok = true
		case a == "--level" || a == "-level":
			if i+1 >= len(args) {
				return nil, mc, false, fmt.Errorf("--level requires a value")
			}
			name, val, ok = "--level", args[i+1], true
			i++
		case strings.HasPrefix(a, "--dsts=") || strings.HasPrefix(a, "-dsts="):
			name = "--dsts"
			val = strings.SplitN(a, "=", 2)[1]
			ok = true
		case a == "--dsts" || a == "-dsts":
			if i+1 >= len(args) {
				return nil, mc, false, fmt.Errorf("--dsts requires a value")
			}
			name, val, ok = "--dsts", args[i+1], true
			i++
		case strings.HasPrefix(a, "--srcs=") || strings.HasPrefix(a, "-srcs="):
			name = "--srcs"
			val = strings.SplitN(a, "=", 2)[1]
			ok = true
		case a == "--srcs" || a == "-srcs":
			if i+1 >= len(args) {
				return nil, mc, false, fmt.Errorf("--srcs requires a value")
			}
			name, val, ok = "--srcs", args[i+1], true
			i++
		}
		if !ok {
			out = append(out, a)
			continue
		}
		seen = true
		switch name {
		case "--mtx-id":
			n, err := parseUint(name, val, 127)
			if err != nil {
				return nil, mc, false, err
			}
			mc.MatrixID = uint8(n)
		case "--level":
			n, err := parseUint(name, val, 27)
			if err != nil {
				return nil, mc, false, err
			}
			mc.Level = uint8(n)
		case "--dsts":
			n, err := parseUint(name, val, 16383)
			if err != nil {
				return nil, mc, false, err
			}
			mc.Dsts = uint16(n)
		case "--srcs":
			n, err := parseUint(name, val, 16383)
			if err != nil {
				return nil, mc, false, err
			}
			mc.Srcs = uint16(n)
		}
	}
	return out, mc, seen, nil
}

// extractCaptureFlag scans args for "--capture FILE" or "--capture=FILE"
// before sub-command dispatch. Removes the matched tokens from the
// returned args slice and opens the file as a fresh *transport.Recorder.
//
// Global flag — has to run before sub-command dispatch because each
// sub-command has its own flag set and wouldn't otherwise know about
// --capture. Matches the top-level --capture pattern used by dhs consumer <proto> walk /
// get / set.
func extractCaptureFlag(args []string) ([]string, *transport.Recorder, error) {
	out := make([]string, 0, len(args))
	var path string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--capture" || a == "-capture":
			if i+1 >= len(args) {
				return nil, nil, fmt.Errorf("--capture requires a file path")
			}
			path = args[i+1]
			i++
		case strings.HasPrefix(a, "--capture=") || strings.HasPrefix(a, "-capture="):
			path = strings.SplitN(a, "=", 2)[1]
		default:
			out = append(out, a)
		}
	}
	if path == "" {
		return out, nil, nil
	}
	rec, err := transport.NewRecorder(path)
	if err != nil {
		return nil, nil, fmt.Errorf("--capture: %w", err)
	}
	return out, rec, nil
}

// probelFlags parses the common "host:port + matrix + level" tuple
// shared by almost every SW-P-08 subcommand.
type probelFlags struct {
	addr    string
	matrix  int
	level   int
	dst     int
	src     int
	timeout time.Duration
}

func parseProbelFlags(args []string, want struct{ dst, src bool }) (probelFlags, error) {
	fs := flag.NewFlagSet("probel", flag.ContinueOnError)
	pf := probelFlags{}
	fs.IntVar(&pf.matrix, "matrix", 0, "matrix id (0-255)")
	fs.IntVar(&pf.level, "level", 0, "level id (0-255)")
	fs.IntVar(&pf.dst, "dst", 0, "destination id (0-65535)")
	fs.IntVar(&pf.src, "src", 0, "source id (0-65535)")
	fs.DurationVar(&pf.timeout, "timeout", 5*time.Second, "operation timeout")
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return pf, fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
		return pf, err
	}
	pf.addr = addr
	if pf.matrix < 0 || pf.matrix > 255 {
		return pf, fmt.Errorf("--matrix out of range (0-255)")
	}
	if pf.level < 0 || pf.level > 255 {
		return pf, fmt.Errorf("--level out of range (0-255)")
	}
	if want.dst && (pf.dst < 0 || pf.dst > 0xFFFF) {
		return pf, fmt.Errorf("--dst out of range (0-65535)")
	}
	if want.src && (pf.src < 0 || pf.src > 0xFFFF) {
		return pf, fmt.Errorf("--src out of range (0-65535)")
	}
	return pf, nil
}

func runProbelInterrogate(ctx context.Context, args []string) error {
	pf, err := parseProbelFlags(args, struct{ dst, src bool }{dst: true})
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, pf.timeout)
	defer cancel()
	p, closer, err := dialProbel(cctx, pf.addr)
	if err != nil {
		return err
	}
	defer closer()
	reply, err := p.CrosspointInterrogate(cctx, uint8(pf.matrix), uint8(pf.level), uint16(pf.dst))
	if err != nil {
		return err
	}
	fmt.Printf("crosspoint tally  matrix=%d level=%d dst=%d → src=%d\n",
		reply.MatrixID, reply.LevelID, reply.DestinationID, reply.SourceID)
	return nil
}

func runProbelConnect(ctx context.Context, args []string) error {
	pf, err := parseProbelFlags(args, struct{ dst, src bool }{dst: true, src: true})
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, pf.timeout)
	defer cancel()
	p, closer, err := dialProbel(cctx, pf.addr)
	if err != nil {
		return err
	}
	defer closer()
	reply, err := p.CrosspointConnect(cctx,
		uint8(pf.matrix), uint8(pf.level), uint16(pf.dst), uint16(pf.src))
	if err != nil {
		return err
	}
	fmt.Printf("crosspoint connected  matrix=%d level=%d dst=%d src=%d\n",
		reply.MatrixID, reply.LevelID, reply.DestinationID, reply.SourceID)
	return nil
}

// runProbelWatch subscribes to every async frame the client sees until
// ctx fires. Useful for observing tallies fanned out by the provider
// when a second client (or the provider's API) mutates a crosspoint.
func runProbelWatch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probel-watch", flag.ContinueOnError)
	timeout := fs.Duration("timeout", 0, "stop after this duration (0 = run until Ctrl-C)")
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if *timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}
	p, closer, err := dialProbel(ctx, addr)
	if err != nil {
		return err
	}
	defer closer()
	cli, err := p.ExposeClient()
	if err != nil {
		return err
	}
	cli.Subscribe(func(f codec.Frame) {
		fmt.Printf("event  cmd=0x%02x payload_len=%d\n", byte(f.ID), len(f.Payload))
	})
	<-ctx.Done()
	return nil
}

// Stub subcommands — implemented by per-command commits further on.

func runProbelMaintenance(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probel-maintenance", flag.ContinueOnError)
	fn := fs.String("function", "soft-reset",
		"function: hard-reset | soft-reset | clear-protects | database-transfer")
	matrix := fs.Int("matrix", 0, "matrix id (clear-protects only; 255 = all)")
	level := fs.Int("level", 0, "level id (clear-protects only; 255 = all)")
	timeout := fs.Duration("timeout", 5*time.Second, "operation timeout")
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	var mfn codec.MaintenanceFunction
	switch *fn {
	case "hard-reset":
		mfn = codec.MaintHardReset
	case "soft-reset":
		mfn = codec.MaintSoftReset
	case "clear-protects":
		mfn = codec.MaintClearProtects
	case "database-transfer":
		mfn = codec.MaintDatabaseTransfer
	default:
		return fmt.Errorf("unknown --function %q", *fn)
	}
	cctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	p, closer, err := dialProbel(cctx, addr)
	if err != nil {
		return err
	}
	defer closer()
	if err := p.Maintenance(cctx, mfn, uint8(*matrix), uint8(*level)); err != nil {
		return err
	}
	fmt.Printf("maintenance sent: function=%s matrix=%d level=%d\n", *fn, *matrix, *level)
	return nil
}

func runProbelDualStatus(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probel-dual-status", flag.ContinueOnError)
	timeout := fs.Duration("timeout", 5*time.Second, "operation timeout")
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	p, closer, err := dialProbel(cctx, addr)
	if err != nil {
		return err
	}
	defer closer()
	r, err := p.DualControllerStatus(cctx)
	if err != nil {
		return err
	}
	who := "MASTER"
	if r.SlaveActive {
		who = "SLAVE"
	}
	fmt.Printf("dual-controller  who=%s active=%v idle_faulty=%v\n",
		who, r.Active, r.IdleControllerFaulty)
	return nil
}
func runProbelTallyDump(ctx context.Context, args []string) error {
	pf, err := parseProbelFlags(args, struct{ dst, src bool }{})
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, pf.timeout)
	defer cancel()
	p, closer, err := dialProbel(cctx, pf.addr)
	if err != nil {
		return err
	}
	defer closer()
	res, err := p.CrosspointTallyDump(cctx, uint8(pf.matrix), uint8(pf.level))
	if err != nil {
		return err
	}
	if res.IsWord {
		fmt.Printf("tally-dump (word) matrix=%d level=%d first_dst=%d tallies=%d\n",
			res.Word.MatrixID, res.Word.LevelID,
			res.Word.FirstDestinationID, len(res.Word.SourceIDs))
		for i, src := range res.Word.SourceIDs {
			fmt.Printf("  dst=%d → src=%d\n", int(res.Word.FirstDestinationID)+i, src)
		}
	} else {
		fmt.Printf("tally-dump (byte) matrix=%d level=%d first_dst=%d tallies=%d\n",
			res.Byte.MatrixID, res.Byte.LevelID,
			res.Byte.FirstDestinationID, len(res.Byte.SourceIDs))
		for i, src := range res.Byte.SourceIDs {
			fmt.Printf("  dst=%d → src=%d\n", int(res.Byte.FirstDestinationID)+i, src)
		}
	}
	return nil
}
// parseProbelProtectFlags parses host:port + matrix + level + dst + device.
// Used by the five Protect* subcommands that all take the same shape.
func parseProbelProtectFlags(args []string) (probelFlags, int, error) {
	fs := flag.NewFlagSet("probel-protect", flag.ContinueOnError)
	var pf probelFlags
	fs.IntVar(&pf.matrix, "matrix", 0, "matrix id (0-255)")
	fs.IntVar(&pf.level, "level", 0, "level id (0-255)")
	fs.IntVar(&pf.dst, "dst", 0, "destination id (0-65535)")
	device := 0
	fs.IntVar(&device, "device", 0, "device id (0-1023)")
	fs.DurationVar(&pf.timeout, "timeout", 5*time.Second, "operation timeout")
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return pf, 0, fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
		return pf, 0, err
	}
	pf.addr = addr
	if device < 0 || device > 0x3FF {
		return pf, 0, fmt.Errorf("--device out of range (0-1023)")
	}
	return pf, device, nil
}

func runProbelProtectInterrogate(ctx context.Context, args []string) error {
	pf, device, err := parseProbelProtectFlags(args)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, pf.timeout)
	defer cancel()
	p, closer, err := dialProbel(cctx, pf.addr)
	if err != nil {
		return err
	}
	defer closer()
	reply, err := p.ProtectInterrogate(cctx,
		uint8(pf.matrix), uint8(pf.level), uint16(pf.dst), uint16(device))
	if err != nil {
		return err
	}
	fmt.Printf("protect tally  matrix=%d level=%d dst=%d → state=%d device=%d\n",
		reply.MatrixID, reply.LevelID, reply.DestinationID, reply.State, reply.DeviceID)
	return nil
}

func runProbelProtectConnect(ctx context.Context, args []string) error {
	pf, device, err := parseProbelProtectFlags(args)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, pf.timeout)
	defer cancel()
	p, closer, err := dialProbel(cctx, pf.addr)
	if err != nil {
		return err
	}
	defer closer()
	reply, err := p.ProtectConnect(cctx,
		uint8(pf.matrix), uint8(pf.level), uint16(pf.dst), uint16(device))
	if err != nil {
		return err
	}
	fmt.Printf("protect connected  matrix=%d level=%d dst=%d device=%d state=%d\n",
		reply.MatrixID, reply.LevelID, reply.DestinationID, reply.DeviceID, reply.State)
	return nil
}

func runProbelProtectDisconnect(ctx context.Context, args []string) error {
	pf, device, err := parseProbelProtectFlags(args)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, pf.timeout)
	defer cancel()
	p, closer, err := dialProbel(cctx, pf.addr)
	if err != nil {
		return err
	}
	defer closer()
	reply, err := p.ProtectDisconnect(cctx,
		uint8(pf.matrix), uint8(pf.level), uint16(pf.dst), uint16(device))
	if err != nil {
		return err
	}
	fmt.Printf("protect disconnected  matrix=%d level=%d dst=%d device=%d state=%d\n",
		reply.MatrixID, reply.LevelID, reply.DestinationID, reply.DeviceID, reply.State)
	return nil
}

func runProbelProtectName(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probel-protect-name", flag.ContinueOnError)
	device := fs.Int("device", 0, "device id (0-1023)")
	timeout := fs.Duration("timeout", 5*time.Second, "operation timeout")
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	p, closer, err := dialProbel(cctx, addr)
	if err != nil {
		return err
	}
	defer closer()
	name, err := p.ProtectDeviceName(cctx, uint16(*device))
	if err != nil {
		return err
	}
	fmt.Printf("device %d name=%q\n", *device, name)
	return nil
}

func runProbelProtectDump(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probel-protect-dump", flag.ContinueOnError)
	matrix := fs.Int("matrix", 0, "matrix id (0-255)")
	level := fs.Int("level", 0, "level id (0-255)")
	firstDst := fs.Int("first-dst", 0, "first destination id to dump")
	timeout := fs.Duration("timeout", 5*time.Second, "operation timeout")
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	p, closer, err := dialProbel(cctx, addr)
	if err != nil {
		return err
	}
	defer closer()
	res, err := p.ProtectTallyDump(cctx, uint8(*matrix), uint8(*level), uint16(*firstDst))
	if err != nil {
		return err
	}
	fmt.Printf("protect tally-dump matrix=%d level=%d first_dst=%d items=%d\n",
		res.MatrixID, res.LevelID, res.FirstDestinationID, len(res.Items))
	for i, it := range res.Items {
		fmt.Printf("  dst=%d state=%d device=%d\n",
			int(res.FirstDestinationID)+i, it.State, it.DeviceID)
	}
	return nil
}

func runProbelMasterProtect(ctx context.Context, args []string) error {
	pf, device, err := parseProbelProtectFlags(args)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, pf.timeout)
	defer cancel()
	p, closer, err := dialProbel(cctx, pf.addr)
	if err != nil {
		return err
	}
	defer closer()
	reply, err := p.MasterProtectConnect(cctx,
		uint8(pf.matrix), uint8(pf.level), uint16(pf.dst), uint16(device))
	if err != nil {
		return err
	}
	fmt.Printf("master-protect connected  matrix=%d level=%d dst=%d device=%d state=%d\n",
		reply.MatrixID, reply.LevelID, reply.DestinationID, reply.DeviceID, reply.State)
	return nil
}

// popPositional scans args and pulls out the first token that is NOT a
// flag (does not start with "-" and is not a value for a previous bool-
// less flag). That token is returned as addr; the remaining args (in
// their original order, minus the popped one) are returned for
// flag.Parse.
//
// Needed because the CLI documents `dhs consumer probel-sw08p <cmd> <host:port>
// [flags]` but flag.Parse stops at the first non-flag token — without
// this helper, "127.0.0.1:2008 --matrix 0" would swallow every flag.
func popPositional(args []string) (string, []string) {
	out := make([]string, 0, len(args))
	var addr string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if addr == "" && !strings.HasPrefix(a, "-") {
			addr = a
			continue
		}
		out = append(out, a)
	}
	return addr, out
}

// splitHostPort accepts "host:port" or plain "host"; returns port=def
// when missing. Bare IPv4 is common for small LAN matrices that run on
// the default port.
func splitHostPort(addr string, def int) (string, int, error) {
	if addr == "" {
		return "", 0, fmt.Errorf("empty address")
	}
	// "host:port"
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			host := addr[:i]
			var port int
			if _, err := fmt.Sscanf(addr[i+1:], "%d", &port); err != nil {
				return "", 0, fmt.Errorf("parse port in %q: %w", addr, err)
			}
			return host, port, nil
		}
	}
	return addr, def, nil
}
