package logging

import (
	"io"
	"log/slog"
	"os"
)

// NewTextLogger creates a text handler for CLI output to stderr.
// Human-readable, with custom level names. The handler is wrapped in
// AsyncHandler so per-frame log sites on the hot path (Ember+
// keepalive, Probel tally fan-out, ACP1 status announce) never block
// the caller on the underlying writer.
func NewTextLogger(level slog.Level) *slog.Logger {
	inner := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: LevelNames,
	})
	return slog.New(NewAsyncHandler(inner))
}

// NewTextLoggerSync is the synchronous variant of NewTextLogger —
// kept for tests that need to assert on stderr output without racing
// the async drain goroutine. Production code paths use NewTextLogger.
func NewTextLoggerSync(level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: LevelNames,
	}))
}

// NewJSONLogger creates a JSON handler for file/stdout output.
// Loki/Promtail compatible — each line is valid JSON with standard
// fields: time, level, msg, plus structured attributes. Wrapped in
// AsyncHandler — see NewTextLogger.
//
// Example output:
//
//	{"time":"2026-04-17T14:25:28Z","level":"INFO","msg":"connected","source":"acp2.session","dir":"→","host":"10.41.40.195"}
//	{"time":"2026-04-17T14:25:29Z","level":"DEBUG","msg":"get_object","source":"acp2.walker","dir":"→","slot":0,"obj_id":1}
//	{"time":"2026-04-17T14:25:29Z","level":"TRACE","msg":"send","source":"acp2.session","dir":"→","hex":"c63500000100000100"}
func NewJSONLogger(w io.Writer, level slog.Level) *slog.Logger {
	inner := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: LevelNames,
	})
	return slog.New(NewAsyncHandler(inner))
}

// NewJSONLoggerSync is the synchronous variant of NewJSONLogger —
// kept for tests that need to read the written line right after the
// log call without flushing.
func NewJSONLoggerSync(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: LevelNames,
	}))
}

// WithSource returns a child logger with the source path attribute.
// Source path follows OPNsense convention: module.component
//
// Examples:
//
//	WithSource(logger, "acp2.session")
//	WithSource(logger, "acp2.walker")
//	WithSource(logger, "acp1.client")
//	WithSource(logger, "transport.tcp")
//	WithSource(logger, "export.yaml")
func WithSource(logger *slog.Logger, source string) *slog.Logger {
	return logger.With("source", source)
}

// Attr helpers for common structured fields.

// Dir returns a direction attribute for protocol I/O.
func Dir(dir string) slog.Attr {
	return slog.String("dir", dir)
}

// Outbound returns a direction attribute for outbound (tx) messages.
func Outbound() slog.Attr {
	return Dir(DirOutbound)
}

// Inbound returns a direction attribute for inbound (rx) messages.
func Inbound() slog.Attr {
	return Dir(DirInbound)
}

// NewFileLogger creates a JSON logger writing to a file.
// The caller is responsible for closing the file.
// Suitable for acp-srv log file that Promtail scrapes.
func NewFileLogger(path string, level slog.Level) (*slog.Logger, *os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}
	logger := NewJSONLogger(f, level)
	return logger, f, nil
}

// NewLokiLogger creates a Loki-shaped JSON logger writing to w. R15 #476
// shape: `ts` (RFC3339 time key), `level` lowercased, `component` (renamed
// from `source` to match Loki / Promtail label conventions), `msg`, plus
// arbitrary k/v from the slog call passed through unchanged.
//
// Example line:
//
//	{"ts":"2026-05-17T03:11:10Z","level":"info","component":"emberplus.consumer","msg":"session connected","host":"127.0.0.1","port":9100}
func NewLokiLogger(w io.Writer, level slog.Level) *slog.Logger {
	inner := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: lokiAttrs,
	})
	return slog.New(NewAsyncHandler(inner))
}

// NewLokiLoggerSync is the synchronous variant of NewLokiLogger for
// tests that inspect the rendered line directly.
func NewLokiLoggerSync(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: lokiAttrs,
	}))
}

// lokiAttrs applies the Loki field-rename rules per slog.HandlerOptions.
// Time key becomes `ts`; level value is lowercased; `source` is renamed
// to `component`. Everything else passes through.
func lokiAttrs(groups []string, a slog.Attr) slog.Attr {
	switch a.Key {
	case slog.TimeKey:
		a.Key = "ts"
	case slog.LevelKey:
		// Lowercase the level name (slog's default is upper-case).
		if lvl, ok := a.Value.Any().(slog.Level); ok {
			a.Value = slog.StringValue(lokiLevelName(lvl))
		}
	case "source":
		a.Key = "component"
	}
	return a
}

// lokiLevelName returns the lowercase Loki-style level label.
func lokiLevelName(level slog.Level) string {
	switch {
	case level <= LevelTrace:
		return "trace"
	case level <= LevelDebug:
		return "debug"
	case level <= LevelInfo:
		return "info"
	case level <= LevelWarn:
		return "warn"
	case level <= LevelError:
		return "error"
	default:
		return "critical"
	}
}
