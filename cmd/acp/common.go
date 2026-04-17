package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"acp/internal/protocol"
	"acp/internal/protocol/acp1"
	"acp/internal/transport"
)

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
