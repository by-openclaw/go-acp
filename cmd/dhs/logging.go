package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// logFlags carries the four uniform logging flags for connectors whose
// per-verb flagsets don't register them directly (probel-sw08p, probel-sw02p,
// osc, nmos). Their dispatcher strips these flags from the args ONCE with
// stripLogFlags and stashes the result in the context; the dial/logger spots
// read it back with consumerLogger. Same flags, same Model B behaviour as
// every other connector (epic #987).
type logFlags struct {
	format     string
	level      string
	path       string
	syslogAddr string
}

type logFlagsKey struct{}

// stripLogFlags pulls --log-format / --log / --syslog-addr / --log-level
// (each as "--flag value" or "--flag=value", one or two dashes) out of args
// so a connector's own FlagSet never sees them, and returns the parsed
// values plus the remaining args. Defaults match every other connector.
func stripLogFlags(args []string) (*logFlags, []string) {
	lf := &logFlags{format: DefaultLogFormat, level: "info", path: "auto"}
	out := make([]string, 0, len(args))
	set := func(name, val string) bool {
		switch name {
		case "log-format":
			lf.format = val
		case "log":
			lf.path = val
		case "syslog-addr":
			lf.syslogAddr = val
		case "log-level":
			lf.level = val
		default:
			return false
		}
		return true
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) < 2 || a[0] != '-' {
			out = append(out, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		if eq := strings.IndexByte(name, '='); eq >= 0 { // --flag=value
			if set(name[:eq], name[eq+1:]) {
				continue
			}
			out = append(out, a)
			continue
		}
		// --flag value
		if i+1 < len(args) && set(name, args[i+1]) {
			i++
			continue
		}
		if set(name, "") { // flag with no following value
			continue
		}
		out = append(out, a)
	}
	return lf, out
}

// withLogFlags stashes stripped log flags on the context for consumerLogger.
func withLogFlags(ctx context.Context, lf *logFlags) context.Context {
	return context.WithValue(ctx, logFlagsKey{}, lf)
}

// consumerLogger builds the Model B loggers for a connector that stashed its
// log flags on ctx (via stripLogFlags + withLogFlags). op is the operational
// logger (human stderr + structured sinks); event is the sink-only event
// logger (nil without a sink) for the verb's event stream; cleanup closes the
// sinks. Falls back to sane defaults (local syslog only) when no flags are on
// the context.
func consumerLogger(ctx context.Context, proto, host, verb string) (op, event *slog.Logger, cleanup func(), hasSink bool) {
	lf, _ := ctx.Value(logFlagsKey{}).(*logFlags)
	if lf == nil {
		lf = &logFlags{format: DefaultLogFormat, level: "info", path: "auto"}
	}
	o, e, c, has, err := buildConsumerLoggers(
		parseLogLevel(lf.level), lf.format, lf.path, lf.syslogAddr,
		defaultLogPath(proto, hostOnly(host), verb))
	if err != nil || o == nil {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})), nil, func() {}, false
	}
	return o, e, c, has
}

// DefaultLogFormat is the uniform logging default for every connector,
// consumer and provider alike (epic #987): RFC 5424 syslog. `text` (the
// human logger) and `json` (Loki/Promtail, one record per line) are opt-in
// via --log-format. This is the LOG stream only — the human data tables
// that watch/tree/walk print are unaffected.
const DefaultLogFormat = "syslog"

// newLoggerTo builds a slog.Logger that writes to w in the given format:
//
//	"syslog" → RFC 5424 lines (newSyslogHandler) — the default
//	"json"   → slog JSON, one record per line (Loki/Promtail)
//	anything → slog text (human)
//
// It is the single place the log FORMAT is chosen, shared by the producer
// (serve) and the consumer verbs so `--log-format` means one thing across
// the whole CLI. The remote (syslog-addr) and local-file sinks are layered
// on by the caller via teeHandler / the file writer.
func newLoggerTo(w io.Writer, level slog.Level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	switch format {
	case "json":
		return slog.New(slog.NewJSONHandler(w, opts))
	case "text":
		return slog.New(slog.NewTextHandler(w, opts))
	default: // "syslog" and any unknown value fall back to the default format
		return slog.New(newSyslogHandler(w, level))
	}
}

// buildConsumerLoggers assembles the uniform-logging Model B loggers a
// consumer verb uses (epic #987):
//
//   - op:    stderr HUMAN text at `level` (the operator's operational log)
//     PLUS the structured sinks — this is the logger the plugin uses.
//   - event: the structured sinks ONLY (no stderr) — a verb (e.g. watch)
//     emits its event stream through this so records reach the file/server
//     without ever cluttering the terminal (which keeps its human data
//     tables on stdout). nil when no sink is configured.
//
// Sinks: --log FILE (autoPath resolves the literal "auto") and
// --syslog-addr host:port, both in `format` (syslog default). hasSink is
// true when at least one exists. With no sink, op is exactly the old
// stderr text logger and event is nil — a pure no-op change to defaults.
func buildConsumerLoggers(level slog.Level, format, logPath, syslogAddr, autoPath string) (op, event *slog.Logger, cleanup func(), hasSink bool, err error) {
	if format == "" {
		format = DefaultLogFormat
	}
	var closers []func()
	cleanup = func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
	}

	var sinks []slog.Handler
	// Local file is ON by default (like the DM cache): "auto" resolves to
	// the ADR-0028 path; "off"/"none"/"-"/"" disable it.
	switch logPath {
	case "off", "none", "-", "":
		logPath = ""
	case "auto":
		logPath = autoPath
	}
	if logPath != "" {
		if dir := filepath.Dir(logPath); dir != "." {
			if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
				return nil, nil, cleanup, false, fmt.Errorf("--log %s: %w", logPath, mkErr)
			}
		}
		f, ferr := os.Create(logPath)
		if ferr != nil {
			return nil, nil, cleanup, false, fmt.Errorf("--log %s: %w", logPath, ferr)
		}
		sinks = append(sinks, newLoggerTo(f, level, format).Handler())
		closers = append(closers, func() { _ = f.Close() })
	}
	if syslogAddr != "" {
		udp, uerr := dialSyslogUDP(syslogAddr)
		if uerr != nil {
			cleanup()
			return nil, nil, func() {}, false, fmt.Errorf("--syslog-addr %s: %w", syslogAddr, uerr)
		}
		sinks = append(sinks, udp.Handler(level))
		closers = append(closers, func() { udp.Close() })
	}
	hasSink = len(sinks) > 0

	opHandlers := append([]slog.Handler{slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})}, sinks...)
	op = slog.New(teeHandler(opHandlers))
	if hasSink {
		event = slog.New(teeHandler(append([]slog.Handler(nil), sinks...)))
	}
	return op, event, cleanup, hasSink, nil
}
