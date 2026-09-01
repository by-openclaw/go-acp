package main

// RFC 5424 syslog output for dhs logs (#751 G6). `--log-format syslog`
// renders every slog record as one RFC 5424 line on stderr, ready for
// rsyslog/syslog-ng ingestion or promtail's syslog scraper:
//
//	<PRI>1 TIMESTAMP HOSTNAME dhs PROCID - - msg key=val ...
//
// Severity mapping (RFC 5424 §6.2.1, facility local0=16):
//
//	slog DEBUG → 7 (debug)     slog WARN  → 4 (warning)
//	slog INFO  → 6 (info)      slog ERROR → 3 (error)
//	LevelCritical (ERROR+4)    → 2 (critical)
//
// Attributes are appended to MSG as key=val pairs (values quoted when
// they contain spaces); STRUCTURED-DATA stays "-" — honest and
// parseable without registering an IANA enterprise number.

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
)

// LevelCritical is the severity above ERROR (RFC 5424 "critical").
// Use logger.Log(ctx, LevelCritical, ...) for conditions that mean
// the process cannot continue safely.
const LevelCritical = slog.Level(12)

const syslogFacility = 16 // local0

// syslogSeverity maps a slog level to the RFC 5424 severity code.
func syslogSeverity(l slog.Level) int {
	switch {
	case l >= LevelCritical:
		return 2
	case l >= slog.LevelError:
		return 3
	case l >= slog.LevelWarn:
		return 4
	case l >= slog.LevelInfo:
		return 6
	default:
		return 7
	}
}

type syslogHandler struct {
	mu       *sync.Mutex
	w        io.Writer
	min      slog.Level
	hostname string
	pid      int
	attrs    []slog.Attr
	group    string
}

func newSyslogHandler(w io.Writer, min slog.Level) *syslogHandler {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "-"
	}
	return &syslogHandler{mu: &sync.Mutex{}, w: w, min: min, hostname: host, pid: os.Getpid()}
}

func (h *syslogHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= h.min }

func (h *syslogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	c := *h
	c.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &c
}

func (h *syslogHandler) WithGroup(name string) slog.Handler {
	c := *h
	if name != "" {
		if c.group != "" {
			c.group += "."
		}
		c.group += name
	}
	return &c
}

func syslogVal(v slog.Value) string {
	s := v.String()
	if strings.ContainsAny(s, " \t\"") {
		return strconv.Quote(s)
	}
	return s
}

// formatRecord renders one record as an RFC 5424 line WITHOUT a
// trailing newline — the stderr path appends '\n' per line, while the
// UDP forwarder sends the bare line as one datagram (RFC 5426 §3.1:
// one message per datagram, no line terminator).
func (h *syslogHandler) formatRecord(r slog.Record) string {
	var b strings.Builder
	pri := syslogFacility*8 + syslogSeverity(r.Level)
	ts := "-"
	if !r.Time.IsZero() {
		ts = r.Time.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	fmt.Fprintf(&b, "<%d>1 %s %s dhs %d - - %s", pri, ts, h.hostname, h.pid, r.Message)
	prefix := ""
	if h.group != "" {
		prefix = h.group + "."
	}
	for _, a := range h.attrs {
		fmt.Fprintf(&b, " %s%s=%s", prefix, a.Key, syslogVal(a.Value))
	}
	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&b, " %s%s=%s", prefix, a.Key, syslogVal(a.Value))
		return true
	})
	return b.String()
}

func (h *syslogHandler) Handle(_ context.Context, r slog.Record) error {
	line := h.formatRecord(r) + "\n"
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, line)
	return err
}
