// Black-box round-trip tests for the export/import pipeline.
// Verifies that JSON, YAML, and CSV produce identical import results.
package export_test

import (
	"bytes"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/export"
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
				Objects: []consumer.Object{
					{
						Slot: 0, Group: "control", Path: []string{"control"},
						ID: 7, Label: "GainA", Kind: consumer.KindFloat,
						Access: 3, Unit: "%",
						Min: float64(0), Max: float64(150), Step: float64(1), Def: float64(100),
						Value: consumer.Value{Kind: consumer.KindFloat, Float: 50.8},
					},
					{
						Slot: 0, Group: "control", Path: []string{"control"},
						ID: 4, Label: "Broadcasts", Kind: consumer.KindEnum,
						Access: 3, EnumItems: []string{"Off", "On"},
						Def:   uint64(1),
						Value: consumer.Value{Kind: consumer.KindEnum, Enum: 1, Str: "On"},
					},
					{
						Slot: 0, Group: "identity", Path: []string{"identity"},
						ID: 0, Label: "Card name", Kind: consumer.KindString,
						Access: 1, MaxLen: 8,
						Value: consumer.Value{Kind: consumer.KindString, Str: "RRS18"},
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
	// Lookup by label — hierarchical JSON uses maps (unordered).
	var gainA *consumer.Object
	for i := range got.Slots[0].Objects {
		if got.Slots[0].Objects[i].Label == "GainA" {
			gainA = &got.Slots[0].Objects[i]
			break
		}
	}
	if gainA == nil {
		t.Fatal("GainA not found in round-trip")
	}
	if gainA.Value.Float != 50.8 {
		t.Errorf("GainA: float=%v, want 50.8", gainA.Value.Float)
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
	if o.Label != "GainA" || o.Kind != consumer.KindFloat {
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

// Writer emits "." as the path separator (matches Ember+ OID and the
// importer's req.Path resolver — see the canonical path-separator rule).
func TestCSV_Writer_UsesDotSeparator(t *testing.T) {
	snap := &export.Snapshot{
		Device: export.DeviceInfo{Protocol: "acp2"},
		Slots: []export.SlotDump{{
			Slot: 1,
			Objects: []consumer.Object{{
				Slot: 1, ID: 67604,
				Path:  []string{"ROOT-NODE-V2", "OUTPUT", "IP", "VIDEO", "STREAM 1", "LEG 1", "Destination IP"},
				Label: "Destination IP", Kind: consumer.KindString, Access: 3,
				Value: consumer.Value{Kind: consumer.KindString, Str: "239.129.1.20"},
			}},
		}},
	}
	var buf bytes.Buffer
	if err := export.WriteCSV(&buf, snap); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	out := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("ROOT-NODE-V2.OUTPUT.IP.VIDEO.STREAM 1.LEG 1.Destination IP")) {
		t.Errorf("expected dot-separated path in CSV, got: %s", out)
	}
	if bytes.Contains(buf.Bytes(), []byte("ROOT-NODE-V2/OUTPUT")) {
		t.Errorf("CSV still emits slash-separated path: %s", out)
	}
}

// Reader accepts the new "." separator (primary).
func TestCSV_Reader_AcceptsDotSeparator(t *testing.T) {
	csv := "ip,protocol,slot,oid,path,id,label,kind,access,value,value_name,unit,min,max,step,default,enum_items,max_len,alarm_priority,alarm_tag,alarm_on,alarm_off,slot_status\n" +
		"10.100.0.103,acp2,1,,ROOT-NODE-V2.OUTPUT.IP.VIDEO.STREAM 1.LEG 1.Destination IP,67604,Destination IP,string,RW-,239.129.1.20,,,,,,,256,,,,,\n"
	got, err := export.ReadCSV(bytes.NewReader([]byte(csv)))
	if err != nil {
		t.Fatalf("ReadCSV: %v", err)
	}
	if len(got.Slots) != 1 || len(got.Slots[0].Objects) != 1 {
		t.Fatalf("got %d slots / %d objects, want 1/1", len(got.Slots), len(got.Slots[0].Objects))
	}
	o := got.Slots[0].Objects[0]
	if len(o.Path) != 7 {
		t.Fatalf("dot-split path: got %d segments, want 7 (%v)", len(o.Path), o.Path)
	}
	if o.Path[4] != "STREAM 1" {
		t.Errorf("segment[4] preserves space: got %q, want %q", o.Path[4], "STREAM 1")
	}
}

// Reader still accepts the legacy "/" separator so CSVs exported before
// #419 keep importing cleanly.
func TestCSV_Reader_BackwardCompat_SlashSeparator(t *testing.T) {
	csv := "ip,protocol,slot,oid,path,id,label,kind,access,value,value_name,unit,min,max,step,default,enum_items,max_len,alarm_priority,alarm_tag,alarm_on,alarm_off,slot_status\n" +
		"10.100.0.103,acp2,1,,ROOT-NODE-V2/OUTPUT/IP/VIDEO/STREAM 1/LEG 1/Destination IP,67604,Destination IP,string,RW-,239.129.1.20,,,,,,,256,,,,,\n"
	got, err := export.ReadCSV(bytes.NewReader([]byte(csv)))
	if err != nil {
		t.Fatalf("ReadCSV: %v", err)
	}
	o := got.Slots[0].Objects[0]
	if len(o.Path) != 7 {
		t.Fatalf("slash-split path: got %d segments, want 7 (%v)", len(o.Path), o.Path)
	}
	if o.Path[6] != "Destination IP" {
		t.Errorf("segment[6]: got %q, want %q", o.Path[6], "Destination IP")
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
