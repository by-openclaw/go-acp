package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestLokiLogger_Schema asserts the Loki handler renders the exact
// field set documented in docs/logging.md and R15 #476: `ts`, `level`
// (lowercase), `component` (renamed from slog `source`), `msg`, plus
// arbitrary k/v from the slog call passed through unchanged.
func TestLokiLogger_Schema(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLokiLoggerSync(&buf, LevelInfo)
	logger = WithSource(logger, "emberplus.consumer")
	logger.Info("session connected", "host", "127.0.0.1", "port", 9100)

	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("no log line emitted")
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("decode: %v\nline=%s", err, line)
	}
	for _, key := range []string{"ts", "level", "msg", "component", "host", "port"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing key %q in %v", key, got)
		}
	}
	if _, hasSource := got["source"]; hasSource {
		t.Errorf("legacy `source` key leaked into Loki output: %v", got)
	}
	if _, hasTime := got["time"]; hasTime {
		t.Errorf("legacy `time` key leaked into Loki output: %v", got)
	}
	if lvl, _ := got["level"].(string); lvl != "info" {
		t.Errorf("level = %q; want lowercase 'info'", lvl)
	}
	if c, _ := got["component"].(string); c != "emberplus.consumer" {
		t.Errorf("component = %q; want emberplus.consumer", c)
	}
	if msg, _ := got["msg"].(string); msg != "session connected" {
		t.Errorf("msg = %q; want 'session connected'", msg)
	}
}

// TestLokiLogger_TraceLevel verifies the lowercase level name for the
// custom Trace level is rendered.
func TestLokiLogger_TraceLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLokiLoggerSync(&buf, LevelTrace)
	logger.Log(context.TODO(), LevelTrace, "raw hex", "bytes", "feff...")

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if lvl, _ := got["level"].(string); lvl != "trace" {
		t.Errorf("level = %q; want trace", lvl)
	}
}
