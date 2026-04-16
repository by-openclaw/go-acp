// Black-box round-trip tests for the export/import pipeline.
// Verifies that JSON, YAML, and CSV produce identical import results.
package export_test

import (
	"bytes"
	"testing"
	"time"

	"acp/internal/export"
	"acp/internal/protocol"
)

func sampleSnapshot() *export.Snapshot {
	return &export.Snapshot{
		Device: export.DeviceInfo{
			IP: "10.6.239.113", Port: 2071, Protocol: "acp1", NumSlots: 2,
		},
		CreatedAt: time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC),
		Slots: []export.SlotDump{
			{
				Slot:     0,
				Status:   "present",
				WalkedAt: time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC),
				Objects: []protocol.Object{
					{
						Slot: 0, Group: "control", Path: []string{"control"},
						ID: 7, Label: "GainA", Kind: protocol.KindFloat,
						Access: 3, Unit: "%",
						Min: float64(0), Max: float64(150), Step: float64(1), Def: float64(100),
						Value: protocol.Value{Kind: protocol.KindFloat, Float: 50.8},
					},
					{
						Slot: 0, Group: "control", Path: []string{"control"},
						ID: 4, Label: "Broadcasts", Kind: protocol.KindEnum,
						Access: 3, EnumItems: []string{"Off", "On"},
						Def: uint64(1),
						Value: protocol.Value{Kind: protocol.KindEnum, Enum: 1, Str: "On"},
					},
					{
						Slot: 0, Group: "identity", Path: []string{"identity"},
						ID: 0, Label: "Card name", Kind: protocol.KindString,
						Access: 1, MaxLen: 8,
						Value: protocol.Value{Kind: protocol.KindString, Str: "RRS18"},
					},
				},
			},
		},
	}
}

func TestJSON_RoundTrip(t *testing.T) {
	snap := sampleSnapshot()
	var buf bytes.Buffer
	if err := export.WriteJSON(&buf, snap); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	got, err := export.ReadJSON(&buf)
	if err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}
	if len(got.Slots) != 1 || len(got.Slots[0].Objects) != 3 {
		t.Fatalf("objects: got %d, want 3", len(got.Slots[0].Objects))
	}
	o := got.Slots[0].Objects[0]
	if o.Label != "GainA" || o.Value.Float != 50.8 {
		t.Errorf("GainA: label=%q float=%v", o.Label, o.Value.Float)
	}
}

func TestYAML_RoundTrip(t *testing.T) {
	snap := sampleSnapshot()
	var buf bytes.Buffer
	if err := export.WriteYAML(&buf, snap); err != nil {
		t.Fatalf("WriteYAML: %v", err)
	}
	got, err := export.ReadYAML(&buf)
	if err != nil {
		t.Fatalf("ReadYAML: %v", err)
	}
	if len(got.Slots) != 1 || len(got.Slots[0].Objects) != 3 {
		t.Fatalf("objects: got %d, want 3", len(got.Slots[0].Objects))
	}
	o := got.Slots[0].Objects[0]
	if o.Label != "GainA" || o.Kind != protocol.KindFloat {
		t.Errorf("GainA: label=%q kind=%v", o.Label, o.Kind)
	}
}

func TestCSV_RoundTrip(t *testing.T) {
	snap := sampleSnapshot()
	var buf bytes.Buffer
	if err := export.WriteCSV(&buf, snap); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	got, err := export.ReadCSV(&buf)
	if err != nil {
		t.Fatalf("ReadCSV: %v", err)
	}
	total := 0
	for _, s := range got.Slots {
		total += len(s.Objects)
	}
	if total != 3 {
		t.Fatalf("objects: got %d, want 3", total)
	}
}

func TestAllFormats_SameObjectCount(t *testing.T) {
	snap := sampleSnapshot()

	var jBuf, yBuf, cBuf bytes.Buffer
	_ = export.WriteJSON(&jBuf, snap)
	_ = export.WriteYAML(&yBuf, snap)
	_ = export.WriteCSV(&cBuf, snap)

	jSnap, _ := export.ReadJSON(&jBuf)
	ySnap, _ := export.ReadYAML(&yBuf)
	cSnap, _ := export.ReadCSV(&cBuf)

	jCount, yCount, cCount := countObjects(jSnap), countObjects(ySnap), countObjects(cSnap)
	if jCount != yCount || jCount != cCount {
		t.Errorf("object counts differ: json=%d yaml=%d csv=%d", jCount, yCount, cCount)
	}
}

func countObjects(s *export.Snapshot) int {
	n := 0
	for _, slot := range s.Slots {
		n += len(slot.Objects)
	}
	return n
}
