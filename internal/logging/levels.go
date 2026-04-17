// Package logging provides structured logging primitives for the acp
// toolset. Designed to be Loki/Promtail/Grafana compliant:
//
//   - JSON lines format with standard fields (level, msg, time)
//   - Structured attributes extractable as Loki labels
//   - Custom severity levels: Trace, Debug, Info, Warn, Error, Critical
//   - Direction field (→/←) for protocol I/O
//   - Source path (acp2.session.connect) for module identification
//
// Output modes:
//
//	CLI:     text handler to stderr (human-readable)
//	acp-srv: JSON handler to file (Promtail scrapes → Loki → Grafana)
//
// No external dependencies — stdlib log/slog only.
package logging

import (
	"log/slog"
)

// Custom log levels extending slog's default four.
// slog levels: Debug=-4, Info=0, Warn=4, Error=8.
// We add Trace below Debug and Critical above Error.
const (
	LevelTrace    slog.Level = -8 // raw wire data, hex dumps, full property details
	LevelDebug               = slog.LevelDebug // -4: object metadata, codec internals
	LevelInfo                = slog.LevelInfo   //  0: connected, walk complete, id/label/value
	LevelWarn                = slog.LevelWarn    //  4: child walk failed, retry, timeout
	LevelError               = slog.LevelError  //  8: request failed, decode error
	LevelCritical slog.Level = 12               // connection lost, panic recovery
)

// Direction constants for protocol I/O logging.
const (
	DirOutbound = "→" // tx: request sent to device
	DirInbound  = "←" // rx: reply received from device
)

// LevelNames maps custom levels to human-readable names for the
// slog ReplaceAttr function. Promtail/Loki can filter on these.
func LevelNames(groups []string, a slog.Attr) slog.Attr {
	if a.Key != slog.LevelKey {
		return a
	}
	level, ok := a.Value.Any().(slog.Level)
	if !ok {
		return a
	}
	switch {
	case level <= LevelTrace:
		a.Value = slog.StringValue("TRACE")
	case level <= LevelDebug:
		a.Value = slog.StringValue("DEBUG")
	case level <= LevelInfo:
		a.Value = slog.StringValue("INFO")
	case level <= LevelWarn:
		a.Value = slog.StringValue("WARN")
	case level <= LevelError:
		a.Value = slog.StringValue("ERROR")
	default:
		a.Value = slog.StringValue("CRITICAL")
	}
	return a
}

// ParseLevel converts a string to slog.Level. Accepts:
// trace, debug, info, warn, error, critical.
func ParseLevel(s string) slog.Level {
	switch s {
	case "trace", "TRACE":
		return LevelTrace
	case "debug", "DEBUG":
		return LevelDebug
	case "info", "INFO":
		return LevelInfo
	case "warn", "WARN", "warning":
		return LevelWarn
	case "error", "ERROR":
		return LevelError
	case "critical", "CRITICAL", "fatal", "FATAL":
		return LevelCritical
	default:
		return LevelInfo
	}
}
