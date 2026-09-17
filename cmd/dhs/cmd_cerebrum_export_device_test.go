package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/export"
)

// Flag-validation only (repo test topology: wire behaviour lives in
// the consumer package's fake-WS tests; the walk core is shared with
// tree --device / extract and live-proven on the NOC 2026-08-17).
func TestCerebrumExportDeviceValidateFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"device-plus-outdir", []string{"export", "h", "--device", "D", "--out-dir", "x"}, "mutually exclusive"},
		{"no-subdev", []string{"export", "h", "--device", "D", "--by-name"}, "--sub-device"},
		{"no-seeds", []string{"export", "h", "--device", "D", "--by-name", "--sub-device", "1"}, "--path"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := runCerebrum(context.Background(), c.args)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("args %v: got %v, want %q", c.args, err, c.want)
			}
		})
	}
}

// TestWriteCerebrumDeviceSnapshot pins the acp2-parity file shape:
// one snapshot, three formats, same writers as the generic export.
func TestWriteCerebrumDeviceSnapshot(t *testing.T) {
	snap := &export.Snapshot{
		Device:    export.DeviceInfo{IP: "10.44.72.28", Protocol: "cerebrum-nb"},
		Generator: "test",
		CreatedAt: time.Unix(0, 0).UTC(),
		Slots: []export.SlotDump{{
			Slot: 1, Status: consumer.SlotPresent.String(), WalkedAt: time.Unix(0, 0).UTC(),
			Objects: []consumer.Object{{
				ID: 0, Path: []string{"A", "Delay"}, Label: "A.Delay",
				Kind: consumer.KindFloat, Access: 3, Unit: "ms",
				Value: consumer.Value{Kind: consumer.KindFloat, Float: 2},
			}},
		}},
	}
	for _, f := range []export.Format{export.FormatJSON, export.FormatYAML, export.FormatCSV} {
		var buf bytes.Buffer
		if err := writeCerebrumDeviceSnapshot(&buf, f, snap); err != nil {
			t.Fatalf("%v: %v", f, err)
		}
		s := buf.String()
		if !strings.Contains(s, "Delay") || !strings.Contains(s, "cerebrum-nb") {
			t.Fatalf("%v output missing content:\n%s", f, s)
		}
	}
}
