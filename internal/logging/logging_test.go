package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The six dhs levels render by name, including the two slog does not have
// (TRACE below debug, CRITICAL above error), and the parser accepts every
// spelling an operator types; anything else is info.
func TestLevelNamesAndParse(t *testing.T) {
	names := map[slog.Level]string{
		LevelTrace: "TRACE", LevelDebug: "DEBUG", LevelInfo: "INFO",
		LevelWarn: "WARN", LevelError: "ERROR", LevelCritical: "CRITICAL",
	}
	for lvl, want := range names {
		a := LevelNames(nil, slog.Any(slog.LevelKey, lvl))
		if a.Value.String() != want {
			t.Errorf("level %d renders %q, want %q", lvl, a.Value.String(), want)
		}
	}
	if a := LevelNames(nil, slog.String("msg", "x")); a.Value.String() != "x" {
		t.Error("a non-level attribute must pass through untouched")
	}
	if a := LevelNames(nil, slog.String(slog.LevelKey, "raw")); a.Value.String() != "raw" {
		t.Error("a level attribute that is not a slog.Level must pass through untouched")
	}
	parse := map[string]slog.Level{
		"trace": LevelTrace, "TRACE": LevelTrace, "debug": LevelDebug, "DEBUG": LevelDebug,
		"info": LevelInfo, "INFO": LevelInfo, "warn": LevelWarn, "WARN": LevelWarn, "warning": LevelWarn,
		"error": LevelError, "ERROR": LevelError, "critical": LevelCritical, "CRITICAL": LevelCritical,
		"fatal": LevelCritical, "FATAL": LevelCritical, "nonsense": LevelInfo, "": LevelInfo,
	}
	for in, want := range parse {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %d, want %d", in, got, want)
		}
	}
}

// The constructors produce loggers that honour the level and the dhs level
// names; WithSource tags every record; the direction attributes are the
// two arrows the wire logs use.
func TestLoggersAndAttrs(t *testing.T) {
	var buf bytes.Buffer
	l := WithSource(NewJSONLogger(&buf, LevelTrace), "acp1")
	l.Log(context.Background(), LevelCritical, "link lost", Outbound())
	l.Log(context.Background(), LevelTrace, "hex", Inbound())
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d records, want 2: %s", len(lines), buf.String())
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["level"] != "CRITICAL" || rec["source"] != "acp1" || rec["dir"] != DirOutbound {
		t.Errorf("record = %v, want CRITICAL / source acp1 / outbound arrow", rec)
	}
	if !strings.Contains(lines[1], `"TRACE"`) || !strings.Contains(lines[1], DirInbound) {
		t.Errorf("trace record = %s", lines[1])
	}
	if Dir("x").Key != "dir" {
		t.Error("Dir must set the dir attribute")
	}

	if tl := NewTextLogger(LevelWarn); tl == nil || tl.Enabled(context.Background(), LevelInfo) {
		t.Error("text logger must exist and honour its level")
	}

	path := filepath.Join(t.TempDir(), "dhs.log")
	fl, f, err := NewFileLogger(path, LevelInfo)
	if err != nil {
		t.Fatal(err)
	}
	fl.Info("hello")
	_ = f.Close()
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"hello"`) {
		t.Errorf("file logger wrote %q", data)
	}
	if _, _, err := NewFileLogger(filepath.Join(t.TempDir(), "missing", "dir", "x.log"), LevelInfo); err == nil {
		t.Error("an unwritable path must be reported")
	}
}
