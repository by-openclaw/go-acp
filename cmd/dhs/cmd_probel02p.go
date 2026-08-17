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

	codec02 "dhs/internal/probel-sw02p/codec"
	probelsw02proto "dhs/internal/probel-sw02p/consumer"
	"dhs/internal/transport"
)

// runProbelsw02p dispatches `dhs consumer probel-sw02p <subcommand>`.
// Mirrors the SW-P-08 dispatcher but with a smaller verb catalogue —
// sw02p starts with `watch` (subscribe to async tallies) plus the
// global matrix-config flags + capture flag. Future verbs (interrogate,
// connect, protect-*, dual-status, router-config) follow the
// per-command file pattern from SW-P-08.
func runProbelsw02p(ctx context.Context, args []string) error {
	args, rec, err := extractCaptureFlag(args)
	if err != nil {
		return err
	}
	if rec != nil {
		defer func() { _ = rec.Close() }()
		ctx = context.WithValue(ctx, probelSW02RecorderKey{}, rec)
	}
	// The `salvo-connect` verb owns its own --dsts (CSV/range) flag via
	// its per-command flag.FlagSet (runProbelsw02pSalvoConnect). The
	// global matrix-config extractor below treats --dsts as a single uint
	// (matrix bootstrap shape) and would eat `--dsts 0-2` first, failing
	// on strconv.ParseUint and starving the verb of its own flag. Detect
	// the subcommand up front and skip --dsts so salvo-connect is
	// reachable from the CLI. SW-P-02 salvo-connect defines no
	// --mtx-id / --level / --srcs, so those stay globally consumable.
	skip := map[string]bool(nil)
	if probelSW02Subcommand(args) == "salvo-connect" {
		skip = map[string]bool{"--dsts": true}
	}
	args, mc, mcSet, err := extractSW02MatrixConfigFlags(args, skip)
	if err != nil {
		return err
	}
	if mcSet {
		ctx = context.WithValue(ctx, probelSW02MatrixConfigKey{}, mc)
	}
	// Help IN PLACE of a verb = catalogue; after the verb it belongs to
	// the verb's own FlagSet (#462).
	if len(args) == 0 || isHelpToken(args[0]) {
		helpProbelSW02()
		return nil
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "interrogate":
		return runProbelsw02pInterrogate(ctx, rest)
	case "connect":
		return runProbelsw02pConnect(ctx, rest)
	case "connect-on-go":
		return runProbelsw02pConnectOnGo(ctx, rest)
	case "go":
		return runProbelsw02pGo(ctx, rest)
	case "salvo-connect":
		return runProbelsw02pSalvoConnect(ctx, rest)
	case "protect-connect":
		return runProbelsw02pProtectConnect(ctx, rest)
	case "protect-disconnect":
		return runProbelsw02pProtectDisconnect(ctx, rest)
	case "protect-interrogate":
		return runProbelsw02pProtectInterrogate(ctx, rest)
	case "protect-dump":
		return runProbelsw02pProtectDump(ctx, rest)
	case "protect-name":
		return runProbelsw02pProtectName(ctx, rest)
	case "dual-status":
		return runProbelsw02pDualStatus(ctx, rest)
	case "lock-status":
		return runProbelsw02pLockStatus(ctx, rest)
	case "status":
		return runProbelsw02pStatus(ctx, rest)
	case "router-config":
		return runProbelsw02pRouterConfig(ctx, rest)
	case "watch":
		return runProbelsw02pWatch(ctx, rest)
	}
	return fmt.Errorf("unknown probel-sw02p subcommand %q", sub)
}

// helpProbelSW02 prints the SW-P-02 subcommand catalogue.
func helpProbelSW02() {
	fmt.Println(`dhs consumer probel-sw02p — Probel SW-P-02 single-matrix matrix controller

USAGE
  dhs consumer probel-sw02p <subcommand> <host:port> [flags]

GLOBAL FLAGS (apply to every subcommand)
  --capture FILE.jsonl   record every wire frame as JSONL
  --mtx-id N             matrix ID (default 0; range 0-127)
  --level L              level (default 0; range 0-27)
  --dsts N               destination count on this (matrix, level)
  --srcs N               source count on this (matrix, level)

  SW-P-02 has no wire-side discovery (rx 75 is supported as an explicit
  command but most controllers configure size externally — VSM does so
  in its UI per matrix). Set --dsts to enable the bootstrap rx 01 sweep
  + rotating keep-alive ping at (re)connect.

SUBCOMMANDS
  interrogate         query current source on one dst (rx 01; --extended → rx 65)
  connect             route a source to a destination (rx 02; --extended → rx 66)
  connect-on-go       stage one crosspoint into the pending salvo buffer (rx 05)
  go                  commit (--op set) or discard (--op clear) the pending buffer (rx 06)
  salvo-connect       stage N crosspoints under a group then fire it (rx 35 + rx 36)
  protect-connect     set a protect on a destination for a device (rx 102)
  protect-disconnect  clear a protect on a destination (rx 104)
  protect-interrogate read protect state on one destination (rx 101)
  protect-dump        request a protect tally dump fan-out (rx 105)
  protect-name        resolve a device id → 8-char name (rx 103)
  dual-status         read dual-controller redundancy state (rx 050)
  lock-status         read source-lock bitmap, GET only (rx 014; SW-P-02 lock is read-only)
  status              read controller status — 2 (rx 07)
  router-config       read router configuration / level map (rx 075)
  watch               subscribe to async tallies until Ctrl-C / --timeout

EXAMPLES
  dhs consumer probel-sw02p interrogate    127.0.0.1:2002 --dst 5
  dhs consumer probel-sw02p connect        127.0.0.1:2002 --dst 5 --src 12
  dhs consumer probel-sw02p connect        127.0.0.1:2002 --dst 5 --src 12 --extended
  dhs consumer probel-sw02p salvo-connect  127.0.0.1:2002 --src 3 --dsts 0-7 --salvo 1
  dhs consumer probel-sw02p protect-connect 127.0.0.1:2002 --dst 5 --device 9
  dhs consumer probel-sw02p router-config   127.0.0.1:2002
  dhs consumer probel-sw02p watch          127.0.0.1:2002 --dsts 64 --srcs 64`)
}

// probelSW02RecorderKey is the context.Context key for the optional
// JSONL traffic recorder.
type probelSW02RecorderKey struct{}

// probelSW02MatrixConfigKey is the context.Context key for the
// caller-supplied matrix shape + bootstrap/keep-alive knobs.
type probelSW02MatrixConfigKey struct{}

// extractSW02MatrixConfigFlags pulls --mtx-id / --level / --dsts /
// --srcs / --initial-poll / --app-keepalive / --bootstrap-spacing
// out of args BEFORE sub-command dispatch.
//
// skip names a set of global flag spellings (e.g. "--dsts") that a
// subcommand owns itself and must NOT be consumed here — both the flag
// and its value are passed through untouched so the subcommand's own
// flag.FlagSet can parse them. Pass nil to consume every global flag.
func extractSW02MatrixConfigFlags(args []string, skip map[string]bool) ([]string, probelsw02proto.MatrixConfig, bool, error) {
	mc := probelsw02proto.MatrixConfig{InitialPoll: true}
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
	parseDur := func(name, val string) (time.Duration, error) {
		d, err := time.ParseDuration(val)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", name, err)
		}
		return d, nil
	}
	parseBool := func(name, val string) (bool, error) {
		b, err := strconv.ParseBool(val)
		if err != nil {
			return false, fmt.Errorf("%s: %w", name, err)
		}
		return b, nil
	}

	consume := func(i int, name string) (string, int, error) {
		if i+1 >= len(args) {
			return "", 0, fmt.Errorf("%s requires a value", name)
		}
		return args[i+1], i + 1, nil
	}

	for i := 0; i < len(args); i++ {
		a := args[i]
		var name, val string
		var ok bool

		match := func(flag string) bool {
			if a == "--"+flag || a == "-"+flag {
				v, ni, err := consume(i, "--"+flag)
				if err != nil {
					return false
				}
				name, val = "--"+flag, v
				i = ni
				ok = true
				return true
			}
			if strings.HasPrefix(a, "--"+flag+"=") || strings.HasPrefix(a, "-"+flag+"=") {
				name = "--" + flag
				val = strings.SplitN(a, "=", 2)[1]
				ok = true
				return true
			}
			return false
		}
		switch {
		case match("mtx-id"):
		case match("level"):
		case match("dsts"):
		case match("srcs"):
		case match("initial-poll"):
		case match("app-keepalive"):
		case match("bootstrap-spacing"):
		}
		if !ok {
			out = append(out, a)
			continue
		}
		// A subcommand-owned flag — pass it (and its value) through
		// untouched so the verb's own flag.FlagSet parses it. The "=" form
		// is a single token; the space form already advanced i past the
		// value, so re-emit both.
		if skip[name] {
			out = append(out, a)
			if !strings.Contains(a, "=") {
				out = append(out, val)
			}
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
		case "--initial-poll":
			b, err := parseBool(name, val)
			if err != nil {
				return nil, mc, false, err
			}
			mc.InitialPoll = b
		case "--app-keepalive":
			d, err := parseDur(name, val)
			if err != nil {
				return nil, mc, false, err
			}
			if d == 0 {
				// Treat explicit "0s" as "disable" — negative is the
				// internal sentinel used by the goroutine.
				mc.AppKeepaliveSpacing = -1
			} else {
				mc.AppKeepaliveSpacing = d
			}
		case "--bootstrap-spacing":
			d, err := parseDur(name, val)
			if err != nil {
				return nil, mc, false, err
			}
			mc.BootstrapSpacing = d
		}
	}
	return out, mc, seen, nil
}

// probelSW02Subcommand returns the subcommand verb in args (the first
// non-flag token), looking past any global matrix-config flags that
// precede it. It runs BEFORE extractSW02MatrixConfigFlags so the
// dispatcher can decide which global flags a verb owns itself (e.g.
// salvo-connect's own --dsts). Returns "" when no verb is present.
//
// Every SW-P-02 global flag takes a separate value token in the space
// form, so each is skipped together with its value. The "=" form
// consumes a single token.
func probelSW02Subcommand(args []string) string {
	valued := map[string]bool{
		"--mtx-id": true, "-mtx-id": true,
		"--level": true, "-level": true,
		"--dsts": true, "-dsts": true,
		"--srcs": true, "-srcs": true,
		"--initial-poll": true, "-initial-poll": true,
		"--app-keepalive": true, "-app-keepalive": true,
		"--bootstrap-spacing": true, "-bootstrap-spacing": true,
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			return a
		}
		if !strings.Contains(a, "=") && valued[a] {
			i++
		}
	}
	return ""
}

// dialProbelSW02 mirrors dialProbel for sw08p — connect-or-die helper
// returning a connected plugin + a deferred-close callback.
func dialProbelSW02(ctx context.Context, addr string) (*probelsw02proto.Plugin, func(), error) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	f := &probelsw02proto.Factory{}
	p := f.New(logger).(*probelsw02proto.Plugin)
	if rec, ok := ctx.Value(probelSW02RecorderKey{}).(*transport.Recorder); ok && rec != nil {
		p.SetRecorder(rec)
	}
	if mc, ok := ctx.Value(probelSW02MatrixConfigKey{}).(probelsw02proto.MatrixConfig); ok {
		p.SetMatrixConfig(mc)
	}
	host, port, err := splitHostPort(addr, probelsw02proto.DefaultPort)
	if err != nil {
		return nil, func() {}, err
	}
	if err := p.Connect(ctx, host, port); err != nil {
		return nil, func() {}, err
	}
	return p, func() { _ = p.Disconnect() }, nil
}

// runProbelsw02pWatch keeps the session open and prints every async
// frame. Bootstrap sweep + keep-alive ping are wired automatically by
// Plugin.Connect when --dsts is set.
func runProbelsw02pWatch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probel-sw02p-watch", flag.ContinueOnError)
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
	p, closer, err := dialProbelSW02(ctx, addr)
	if err != nil {
		return err
	}
	defer closer()
	cli, err := p.ExposeClient()
	if err != nil {
		return err
	}
	cli.Subscribe(func(f codec02.Frame) {
		fmt.Printf("event  cmd=0x%02x payload_len=%d\n", byte(f.ID), len(f.Payload))
	})
	<-ctx.Done()
	return nil
}
