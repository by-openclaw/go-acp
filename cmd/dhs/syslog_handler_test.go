package main

import (
	"bytes"
	"context"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestSyslogSeverityMapping pins the RFC 5424 §6.2.1 mapping incl.
// the critical level (#751 G6).
func TestSyslogSeverityMapping(t *testing.T) {
	cases := []struct {
		level slog.Level
		want  int
	}{
		{slog.LevelDebug, 7},
		{slog.LevelInfo, 6},
		{slog.LevelWarn, 4},
		{slog.LevelError, 3},
		{LevelCritical, 2},
		{LevelCritical + 4, 2},
	}
	for _, c := range cases {
		if got := syslogSeverity(c.level); got != c.want {
			t.Fatalf("severity(%v) = %d, want %d", c.level, got, c.want)
		}
	}
}

func TestSyslogHandler_LineShape(t *testing.T) {
	var buf bytes.Buffer
	h := newSyslogHandler(&buf, slog.LevelInfo)
	logger := slog.New(h).With("proto", "acp2")

	// Below-min level dropped.
	logger.Debug("dropped")
	if buf.Len() != 0 {
		t.Fatalf("debug not filtered: %q", buf.String())
	}

	logger.Warn("session lost", "remote", "10.0.0.1:2072", "note", "two words")
	line := strings.TrimSuffix(buf.String(), "\n")

	// <PRI>1 TIMESTAMP HOST dhs PID - - MSG attrs...
	re := regexp.MustCompile(`^<(\d+)>1 (\S+) (\S+) dhs (\d+) - - session lost proto=acp2 remote=10\.0\.0\.1:2072 note="two words"$`)
	m := re.FindStringSubmatch(line)
	if m == nil {
		t.Fatalf("line does not match RFC 5424 shape:\n%s", line)
	}
	if m[1] != "132" { // local0(16)*8 + warning(4)
		t.Fatalf("PRI = %s, want 132", m[1])
	}
	if _, err := time.Parse("2006-01-02T15:04:05.000Z", m[2]); err != nil {
		t.Fatalf("timestamp %q: %v", m[2], err)
	}

	// Group prefixes flatten dotted.
	buf.Reset()
	g := slog.New(h.WithGroup("session"))
	g.Log(context.Background(), LevelCritical, "wedged", "id", 7)
	crit := buf.String()
	if !strings.HasPrefix(crit, "<130>1 ") { // 16*8+2
		t.Fatalf("critical PRI wrong: %q", crit)
	}
	if !strings.Contains(crit, "session.id=7") {
		t.Fatalf("group prefix missing: %q", crit)
	}
}
