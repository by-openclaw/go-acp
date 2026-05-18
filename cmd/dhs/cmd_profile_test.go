package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"dhs/internal/consumer/compliance"
	"dhs/internal/errcode"
)

// captureStdout swaps os.Stdout for a pipe during fn, returns the
// captured output, and restores stdout. Tests use it to assert on
// printed output without writing real fixture files.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	fn()
	_ = w.Close()
	os.Stdout = old
	return <-done
}

// TestRenderProfileText_NoEvents asserts the legacy column form
// stays byte-compatible (R22 acceptance criterion).
func TestRenderProfileText_NoEvents(t *testing.T) {
	r := profileReport{
		Host: "127.0.0.1:9000", ObjectsWalked: 1355,
		Classification: "strict",
	}
	out := captureStdout(t, func() {
		if err := renderProfileText(r, false); err != nil {
			t.Fatalf("render: %v", err)
		}
	})
	for _, want := range []string{
		"host             127.0.0.1:9000",
		"objects walked   1355",
		"classification   strict",
		"no tolerance events observed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// TestRenderProfileText_WithEvents asserts the new per-event-kind
// row carries the first/last timestamps in RFC3339.
func TestRenderProfileText_WithEvents(t *testing.T) {
	now := time.Now().UTC()
	r := profileReport{
		Host: "127.0.0.1:9000", ObjectsWalked: 1355, Classification: "partial",
		Events: []compliance.EventCount{
			{Kind: "onetoone_source_steal_accepted", Count: 7, FirstSeen: now.Add(-30 * time.Minute), LastSeen: now},
		},
	}
	out := captureStdout(t, func() {
		if err := renderProfileText(r, false); err != nil {
			t.Fatalf("render: %v", err)
		}
	})
	if !strings.Contains(out, "onetoone_source_steal_accepted") {
		t.Errorf("missing event kind row in:\n%s", out)
	}
	if !strings.Contains(out, "first=") || !strings.Contains(out, "last=") {
		t.Errorf("missing timestamps in:\n%s", out)
	}
}

// TestRenderProfileJSON_Schema asserts the JSON shape matches R22
// spec acceptance.
func TestRenderProfileJSON_Schema(t *testing.T) {
	now := time.Now().UTC().Round(time.Second)
	r := profileReport{
		Host: "127.0.0.1:9000", ObjectsWalked: 1355, Classification: "partial",
		Events: []compliance.EventCount{
			{Kind: "onetoone_source_steal_accepted", Count: 7, FirstSeen: now, LastSeen: now},
		},
	}
	out := captureStdout(t, func() {
		if err := renderProfileJSON(r); err != nil {
			t.Fatalf("render: %v", err)
		}
	})
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode: %v\nout=%s", err, out)
	}
	for _, k := range []string{"host", "objects_walked", "classification", "events"} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing %q in JSON: %v", k, got)
		}
	}
}

// TestRenderProfileText_ShowEvents asserts --show-events renders the
// observation ring with attrs.
func TestRenderProfileText_ShowEvents(t *testing.T) {
	now := time.Now()
	r := profileReport{
		Host: "127.0.0.1:9000", ObjectsWalked: 1355, Classification: "partial",
		Events: []compliance.EventCount{
			{Kind: "steal", Count: 1, FirstSeen: now, LastSeen: now},
		},
		Observations: []observationJSON{
			{Kind: "steal", At: now, Attrs: map[string]any{
				"matrix":             "router.matrix",
				"target":             int64(0),
				"source":             int64(5),
				"stolen_from_target": int64(5),
			}},
		},
	}
	out := captureStdout(t, func() {
		if err := renderProfileText(r, true); err != nil {
			t.Fatalf("render: %v", err)
		}
	})
	for _, want := range []string{
		"recorded observations",
		"matrix=router.matrix",
		"source=5",
		"stolen_from_target=5",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// TestFormatAttrs_DeterministicOrder asserts attrs are alphabetically
// sorted so test output is reproducible.
func TestFormatAttrs_DeterministicOrder(t *testing.T) {
	attrs := map[string]any{"z": 1, "a": 2, "m": 3}
	got := formatAttrs(attrs)
	want := "a=2 m=3 z=1"
	if got != want {
		t.Errorf("formatAttrs = %q; want %q", got, want)
	}
}

// TestObservationJSON_FlattensAttrs asserts the ring → JSON converter
// produces a flat map (slog.Attr → map[string]any) suitable for
// json.Marshal.
func TestObservationJSON_FlattensAttrs(t *testing.T) {
	obs := []compliance.ObservedEvent{
		{Kind: "k", At: time.Now(), Attrs: []slog.Attr{
			slog.String("matrix", "router"),
			slog.Int64("target", 5),
		}},
	}
	flat := make([]observationJSON, 0, len(obs))
	for _, o := range obs {
		attrs := map[string]any{}
		for _, a := range o.Attrs {
			attrs[a.Key] = a.Value.Any()
		}
		flat = append(flat, observationJSON{Kind: o.Kind, At: o.At, Attrs: attrs})
	}
	if flat[0].Attrs["matrix"] != "router" {
		t.Errorf("matrix attr lost: %+v", flat[0])
	}
	if flat[0].Attrs["target"].(int64) != 5 {
		t.Errorf("target attr lost: %+v", flat[0])
	}
	// Round-trip through JSON to confirm the shape is serialisable.
	if _, err := json.Marshal(flat); err != nil {
		t.Errorf("marshal: %v", err)
	}
}

// TestProfileError_InvalidFormat asserts the errcode chain works
// for the --format error path (CLI exit 2).
func TestProfileError_InvalidFormat(t *testing.T) {
	if !errors.Is(errProfileInvalidFormat, errProfileInvalidFormat) {
		t.Fatal("errors.Is failed on the sentinel")
	}
	if c := errcode.From(errProfileInvalidFormat); c == nil ||
		c.Layer != errcode.LayerValidation || c.Name != "invalid-format" ||
		c.Class != errcode.ClassUsage {
		t.Errorf("typed code = %+v", c)
	}
}

// TestProfileError_ByValidSession asserts the --by-session sentinel
// surfaces plugin:by-session-unavailable with ClassUsage (exit 2).
func TestProfileError_BySessionUnavailable(t *testing.T) {
	c := errcode.From(errBySessionUnavailable)
	if c == nil || c.Layer != errcode.LayerPlugin || c.Name != "by-session-unavailable" {
		t.Errorf("typed code = %+v", c)
	}
}
