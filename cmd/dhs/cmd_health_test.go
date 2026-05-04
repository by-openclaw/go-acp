package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"dhs/internal/protocol"
)

func TestHealthFlipped(t *testing.T) {
	base := protocol.SessionHealth{Reachable: true, Connected: true, Live: true}

	cases := []struct {
		name string
		cur  protocol.SessionHealth
		want bool
	}{
		{"identical", base, false},
		{"reachable-flipped", protocol.SessionHealth{Reachable: false, Connected: true, Live: true}, true},
		{"connected-flipped", protocol.SessionHealth{Reachable: true, Connected: false, Live: true}, true},
		{"live-flipped", protocol.SessionHealth{Reachable: true, Connected: true, Live: false}, true},
		{"all-flipped", protocol.SessionHealth{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := healthFlipped(base, tc.cur)
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHealthFlipped_TimestampChangesIgnored(t *testing.T) {
	// Timestamps moving forward without bit-flips should NOT count as a
	// transition. Only the 3 layer bools matter.
	t0 := time.Now()
	prev := protocol.SessionHealth{
		Reachable: true, Connected: true, Live: true,
		LastRx: t0,
	}
	cur := protocol.SessionHealth{
		Reachable: true, Connected: true, Live: true,
		LastRx: t0.Add(time.Second),
	}
	if healthFlipped(prev, cur) {
		t.Fatal("timestamp-only change should not count as flip")
	}
}

func TestPrintHealth_Text(t *testing.T) {
	var buf bytes.Buffer
	h := protocol.SessionHealth{
		Reachable:  true,
		Connected:  true,
		Live:       false,
		LastRx:     time.Date(2026, 5, 5, 18, 42, 38, 0, time.UTC),
		StaleAfter: 90 * time.Second,
	}
	// Use a pipe to capture output via a real *os.File.
	// Instead, we capture by replacing the printer's writer.
	printHealthBuf(&buf, "10.6.239.113", "acp1", h, false)
	out := buf.String()
	if !strings.Contains(out, "host=10.6.239.113") {
		t.Fatalf("output missing host:\n%s", out)
	}
	if !strings.Contains(out, "reachable=true") {
		t.Fatalf("output missing reachable=true:\n%s", out)
	}
	if !strings.Contains(out, "live=false") {
		t.Fatalf("output missing live=false:\n%s", out)
	}
	if !strings.Contains(out, "last_rx=2026-05-05T18:42:38Z") {
		t.Fatalf("output missing last_rx:\n%s", out)
	}
}

func TestPrintHealth_JSON(t *testing.T) {
	var buf bytes.Buffer
	h := protocol.SessionHealth{
		Reachable:  true,
		Connected:  false,
		Live:       false,
		StaleAfter: 90 * time.Second,
	}
	printHealthBuf(&buf, "10.6.239.113", "acp1", h, true)
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %q", err, buf.String())
	}
	if got["host"] != "10.6.239.113" {
		t.Fatalf("host = %v, want 10.6.239.113", got["host"])
	}
	if got["reachable"] != true || got["connected"] != false || got["live"] != false {
		t.Fatalf("layer bits wrong: %+v", got)
	}
	if got["stale_after"] != "1m30s" {
		t.Fatalf("stale_after = %v, want 1m30s", got["stale_after"])
	}
}
