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

	codec02 "acp/internal/probel-sw02p/codec"
	probelsw02proto "acp/internal/probel-sw02p/consumer"
	"acp/internal/transport"
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
	args, mc, mcSet, err := extractSW02MatrixConfigFlags(args)
	if err != nil {
		return err
	}
	if mcSet {
		ctx = context.WithValue(ctx, probelSW02MatrixConfigKey{}, mc)
	}
	if len(args) == 0 || hasHelpFlag(args) {
		helpProbelSW02()
		return nil
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
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
  watch       subscribe to async tallies until Ctrl-C / --timeout

EXAMPLES
  dhs consumer probel-sw02p watch 127.0.0.1:2002 --dsts 64 --srcs 64`)
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
func extractSW02MatrixConfigFlags(args []string) ([]string, probelsw02proto.MatrixConfig, bool, error) {
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
