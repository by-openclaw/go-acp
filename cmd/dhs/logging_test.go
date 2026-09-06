package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNewLoggerToJSON: --log-format json emits one parseable JSON object
// per record with the expected fields (Loki/Promtail contract).
func TestNewLoggerToJSON(t *testing.T) {
	var buf bytes.Buffer
	lg := newLoggerTo(&buf, slog.LevelInfo, "json")
	lg.Info("value_change", slog.String("object", "io.sdi"), slog.String("value", "12G"))

	line := strings.TrimSpace(buf.String())
	var rec map[string]any
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("json log line does not parse: %v\nline: %s", err, line)
	}
	if rec["msg"] != "value_change" || rec["object"] != "io.sdi" || rec["value"] != "12G" {
		t.Fatalf("json fields missing/wrong: %v", rec)
	}
}

var rfc5424 = regexp.MustCompile(`^<\d+>1 \S+ \S+ \S+ \S+ `)

// TestNewLoggerToSyslog: default/syslog format emits RFC 5424 lines
// (<PRIVAL>1 TIMESTAMP HOST APP PROCID ...).
func TestNewLoggerToSyslog(t *testing.T) {
	for _, format := range []string{"syslog", "", "anything-unknown"} {
		var buf bytes.Buffer
		lg := newLoggerTo(&buf, slog.LevelInfo, format)
		lg.Info("cerebrum_value_change", slog.String("object", "x"))
		line := strings.TrimSpace(buf.String())
		if !rfc5424.MatchString(line) {
			t.Fatalf("format %q: not RFC 5424: %q", format, line)
		}
		if !strings.Contains(line, "cerebrum_value_change") {
			t.Fatalf("format %q: message missing: %q", format, line)
		}
	}
}

// TestBuildConsumerLoggers covers the Model B assembly: a file sink writes
// the structured record, the event logger is sink-only (nil without a
// sink), and "off" disables the local file.
func TestBuildConsumerLoggers(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.log")

	op, event, cleanup, hasSink, err := buildConsumerLoggers(slog.LevelInfo, "syslog", logPath, "", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !hasSink || event == nil {
		t.Fatalf("a file sink must set hasSink and a non-nil event logger")
	}
	event.Info("value_change", slog.String("object", "io.sdi"))
	op.Warn("something")
	cleanup()

	b, rerr := os.ReadFile(logPath)
	if rerr != nil {
		t.Fatalf("log file not written: %v", rerr)
	}
	body := string(b)
	if !strings.Contains(body, "value_change") || !strings.Contains(body, "something") {
		t.Fatalf("file missing event and/or operational record:\n%s", body)
	}
	for _, ln := range strings.Split(strings.TrimSpace(body), "\n") {
		if !rfc5424.MatchString(ln) {
			t.Fatalf("file line not RFC 5424: %q", ln)
		}
	}

	// No sink → no event logger, no hasSink.
	_, event2, cleanup2, hasSink2, err2 := buildConsumerLoggers(slog.LevelInfo, "syslog", "off", "", "")
	if err2 != nil {
		t.Fatalf("build off: %v", err2)
	}
	cleanup2()
	if hasSink2 || event2 != nil {
		t.Fatalf("--log off must disable the sink (hasSink=%v event=%v)", hasSink2, event2)
	}
}
