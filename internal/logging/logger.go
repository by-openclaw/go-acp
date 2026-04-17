package logging

import (
	"io"
	"log/slog"
	"os"
)

// NewTextLogger creates a text handler for CLI output to stderr.
// Human-readable, with custom level names.
func NewTextLogger(level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: LevelNames,
	}))
}

// NewJSONLogger creates a JSON handler for file/stdout output.
// Loki/Promtail compatible — each line is valid JSON with standard
// fields: time, level, msg, plus structured attributes.
//
// Example output:
//
//	{"time":"2026-04-17T14:25:28Z","level":"INFO","msg":"connected","source":"acp2.session","dir":"→","host":"10.41.40.195"}
//	{"time":"2026-04-17T14:25:29Z","level":"DEBUG","msg":"get_object","source":"acp2.walker","dir":"→","slot":0,"obj_id":1}
//	{"time":"2026-04-17T14:25:29Z","level":"TRACE","msg":"send","source":"acp2.session","dir":"→","hex":"c63500000100000100"}
func NewJSONLogger(w io.Writer, level slog.Level) *slog.Logger {
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
