package main

import (
	"io"
	"log/slog"
)

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
