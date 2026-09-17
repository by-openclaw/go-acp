package export

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"dhs/internal/consumer"
)

// dig walks nested map[string]any by key; fails the test on a miss so
// assertions stay one-liners.
func dig(t *testing.T, v any, keys ...string) any {
	t.Helper()
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("dig %v: %T is not an object", keys, v)
		}
		v, ok = m[k]
		if !ok {
			t.Fatalf("dig %v: key %q missing in %v", keys, k, m)
		}
	}
	return v
}

// TestWriteJSON_TreeShape — the hierarchical export: ACP2 paths nest
// under the tree minus the ROOT_NODE_V2 sentinel, containers carry a
// _meta block, ACP1 groups nest label under group, and pathless
// objects land under their group or "other".
func TestWriteJSON_TreeShape(t *testing.T) {
	snap := &Snapshot{
		Device: DeviceInfo{IP: "10.41.40.195", Protocol: "acp2"}, Generator: "acp 1.0",
		CreatedAt: time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC),
		Slots: []SlotDump{{Slot: 0, Status: "present", Objects: []consumer.Object{
			{Path: []string{"ROOT_NODE_V2", "BOARD"}, Kind: consumer.KindRaw, OID: "1.1", ID: 1, Label: "BOARD", Access: 1,
				Meta: map[string]any{"description": "main board"}},
			{Path: []string{"ROOT_NODE_V2", "BOARD", "Temp"}, Kind: consumer.KindInt, ID: 10, Label: "Temp", Access: 1, Unit: "C",
				Min: int64(0), Max: int64(100), Step: int64(1), Def: int64(25),
				Value: consumer.Value{Kind: consumer.KindInt, Int: 42},
				Meta:  map[string]any{"id": 999, "formula": "x*1"}},
			{Path: []string{"ROOT_NODE_V2", "PSU"}, Kind: consumer.KindRaw, ID: 2},
			{Path: []string{"control"}, Group: "control", Label: "GainA", ID: 7, Kind: consumer.KindUint, Access: 3,
				Value: consumer.Value{Kind: consumer.KindUint, Uint: 7}},
			{Path: []string{"control"}, Group: "control", Label: "Mode", ID: 8, Kind: consumer.KindEnum, Access: 3,
				EnumItems: []string{"Off", "On"}, Def: uint64(1),
				Value: consumer.Value{Kind: consumer.KindEnum, Enum: 1, Str: "On"}},
			{Path: []string{"control"}, Group: "control", Label: "Level", ID: 9, Kind: consumer.KindFloat, Access: 3,
				Value: consumer.Value{Kind: consumer.KindFloat, Float: 1.5}},
			{Group: "identity", Label: "Card name", ID: 0, Kind: consumer.KindString, Access: 1, MaxLen: 8, OID: "2.1",
				Value: consumer.Value{Kind: consumer.KindString, Str: "RRS18"}},
			{Label: "Addr", ID: 3, Kind: consumer.KindIPAddr, Access: 1,
				Value: consumer.Value{Kind: consumer.KindIPAddr, IPAddr: [4]byte{10, 6, 250, 101}}},
		}}},
	}
	var buf bytes.Buffer
	if err := WriteJSON(&buf, snap); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, buf.String())
	}
	if dig(t, doc, "created_at") != "2026-04-16T12:00:00Z" || dig(t, doc, "generator") != "acp 1.0" {
		t.Errorf("envelope: %v", doc)
	}
	slot := doc["slots"].([]any)[0]
	if dig(t, slot, "status") != "present" {
		t.Errorf("slot status missing: %v", slot)
	}
	objs := dig(t, slot, "objects")

	checks := []struct {
		name string
		keys []string
		want any
	}{
		{"container oid", []string{"BOARD", "_meta", "oid"}, "1.1"},
		{"container identifier", []string{"BOARD", "_meta", "identifier"}, "BOARD"},
		{"container number", []string{"BOARD", "_meta", "number"}, float64(1)},
		{"container access", []string{"BOARD", "_meta", "access"}, "R--"},
		{"container plugin meta", []string{"BOARD", "_meta", "description"}, "main board"},
		{"bare container still has number", []string{"PSU", "_meta", "number"}, float64(2)},
		{"bare container access", []string{"PSU", "_meta", "access"}, "---"},
		{"leaf nests under container", []string{"BOARD", "Temp", "value"}, float64(42)},
		{"leaf unit", []string{"BOARD", "Temp", "unit"}, "C"},
		{"leaf min", []string{"BOARD", "Temp", "min"}, float64(0)},
		{"leaf default", []string{"BOARD", "Temp", "default"}, float64(25)},
		{"plugin meta merged", []string{"BOARD", "Temp", "formula"}, "x*1"},
		{"standard key wins over meta", []string{"BOARD", "Temp", "id"}, float64(10)},
		{"acp1 group/label uint", []string{"control", "GainA", "value"}, float64(7)},
		{"enum value", []string{"control", "Mode", "value"}, float64(1)},
		{"enum value name", []string{"control", "Mode", "value_name"}, "On"},
		{"enum default", []string{"control", "Mode", "default"}, float64(1)},
		{"float value", []string{"control", "Level", "value"}, float64(1.5)},
		{"pathless under group", []string{"identity", "Card name", "value"}, "RRS18"},
		{"string max_len", []string{"identity", "Card name", "max_len"}, float64(8)},
		{"leaf oid", []string{"identity", "Card name", "oid"}, "2.1"},
		{"pathless groupless under other", []string{"other", "Addr", "value"}, "10.6.250.101"},
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if got := dig(t, objs, c.keys...); got != c.want {
				t.Errorf("%v = %#v, want %#v", c.keys, got, c.want)
			}
		})
	}
	if _, has := dig(t, objs, "PSU", "_meta").(map[string]any)["oid"]; has {
		t.Error("container without an OID must not invent one")
	}
	if items := dig(t, objs, "control", "Mode", "enum_items").([]any); len(items) != 2 || items[1] != "On" {
		t.Errorf("enum_items: %v", items)
	}
	if _, has := objs.(map[string]any)["ROOT_NODE_V2"]; has {
		t.Error("ROOT_NODE_V2 sentinel must be stripped")
	}
}

// TestWriteJSON_SinkError — the encoder's failure is wrapped, not
// swallowed.
func TestWriteJSON_SinkError(t *testing.T) {
	err := WriteJSON(brokenSink{}, &Snapshot{})
	if !errors.Is(err, errSink) || !strings.HasPrefix(err.Error(), "json encode:") {
		t.Errorf("got %v, want json encode: sink closed", err)
	}
}

// TestReadJSON_FlatFormat — the original array-of-objects layout is
// accepted as-is.
func TestReadJSON_FlatFormat(t *testing.T) {
	in := `{"device":{"ip":"10.0.0.1"},"slots":[{"slot":1,"objects":[{"id":1,"label":"x","kind":6,"access":3,"value":{"kind":6,"str":"hi"}}]}]}`
	snap, err := ReadJSON(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}
	o := snap.Slots[0].Objects[0]
	if o.Label != "x" || o.Kind != consumer.KindString || o.Value.Str != "hi" {
		t.Errorf("flat object: %+v", o)
	}
}

// TestReadJSON_Hierarchical — the nested layout flattens back to
// objects sorted by key, with path/group recovered, timestamps parsed
// leniently and unparseable cells surfaced as KindUnknown values.
func TestReadJSON_Hierarchical(t *testing.T) {
	in := `{"device":{"ip":"10.0.0.1","protocol":"acp1"},"generator":"acp 1.0","created_at":"2026-04-16T12:00:00+02:00",
	        "slots":[{"slot":1,"status":"present","walked_at":"garbage","objects":{
	          "control":{
	            "GainA":{"id":7,"kind":"float","access":"RW-","unit":"dB","min":-20,"max":20,"step":0.5,"default":0,"value":1.5},
	            "Note":"free text",
	            "Mode":{"id":8,"kind":"enum","access":"RW-","enum_items":["Off","On"],"value":1,"value_name":"On"},
	            "Bad":{"id":9,"kind":"int","access":"R--","value":"NaN-ish"},
	            "NoValue":{"id":10,"kind":"string","access":"R--","max_len":4}}}}]}`
	snap, err := ReadJSON(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}
	if !snap.CreatedAt.Equal(time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("created_at with offset: got %v", snap.CreatedAt)
	}
	if len(snap.Slots) != 1 || snap.Slots[0].Status != "present" || !snap.Slots[0].WalkedAt.IsZero() {
		t.Fatalf("slot: %+v", snap.Slots)
	}
	objs := snap.Slots[0].Objects
	var labels []string
	for _, o := range objs {
		labels = append(labels, o.Label)
	}
	if got := strings.Join(labels, ","); got != "Bad,GainA,Mode,NoValue" {
		t.Fatalf("objects sorted by key, non-object entries skipped: got %s", got)
	}
	g := objs[1]
	if g.Group != "control" || strings.Join(g.Path, ".") != "control.GainA" || g.ID != 7 || g.Access != 3 || g.Unit != "dB" {
		t.Errorf("GainA: %+v", g)
	}
	if g.Min != float64(-20) || g.Step != float64(0.5) || g.Value.Kind != consumer.KindFloat || g.Value.Float != 1.5 {
		t.Errorf("GainA numeric fields: %+v", g)
	}
	if m := objs[2]; m.Value.Enum != 1 || m.Value.Str != "On" || len(m.EnumItems) != 2 {
		t.Errorf("Mode: %+v", m)
	}
	if b := objs[0]; b.Kind != consumer.KindInt || b.Value.Kind != consumer.KindUnknown {
		t.Errorf("unparseable int cell must yield KindUnknown value, got %+v", b)
	}
	if n := objs[3]; n.MaxLen != 4 || n.Value.Kind != consumer.KindUnknown {
		t.Errorf("leaf without value keeps zero Value: %+v", n)
	}
}

// TestReadJSON_Failures — decode errors name their stage; an objects
// field that is neither array nor map yields an empty slot rather
// than a crash.
func TestReadJSON_Failures(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"empty input", "", "json decode:"},
		{"not json", "{", "json decode:"},
		{"slots not an array", `{"slots": 5}`, "json decode hierarchical:"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ReadJSON(strings.NewReader(c.in))
			if err == nil || !strings.HasPrefix(err.Error(), c.want) {
				t.Errorf("got %v, want prefix %q", err, c.want)
			}
		})
	}
	t.Run("flat document whose first slot is empty keeps later slots", func(t *testing.T) {
		// A frame whose slot 0 has no card: the fast flat path declines
		// (first slot empty) and the hierarchical reader must still take
		// the array form for the slots that do have objects.
		snap, err := ReadJSON(strings.NewReader(`{"slots":[{"slot":0,"objects":[]},{"slot":1,"objects":[{"id":1,"label":"x","kind":6,"access":1}]}]}`))
		if err != nil {
			t.Fatalf("ReadJSON: %v", err)
		}
		if len(snap.Slots) != 2 || len(snap.Slots[0].Objects) != 0 || len(snap.Slots[1].Objects) != 1 || snap.Slots[1].Objects[0].Label != "x" {
			t.Errorf("slots: %+v", snap.Slots)
		}
	})
	t.Run("objects scalar yields empty slot", func(t *testing.T) {
		snap, err := ReadJSON(strings.NewReader(`{"slots":[{"slot":4,"objects":7}]}`))
		if err != nil {
			t.Fatalf("ReadJSON: %v", err)
		}
		if len(snap.Slots) != 1 || snap.Slots[0].Slot != 4 || len(snap.Slots[0].Objects) != 0 {
			t.Errorf("slots: %+v", snap.Slots)
		}
	})
}

// TestParseTime — both timestamp spellings the writers have used.
func TestParseTime(t *testing.T) {
	cases := []struct {
		in   string
		want time.Time
		ok   bool
	}{
		{"2026-04-16T12:00:00Z", time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC), true},
		{"2026-04-16T12:00:00+02:00", time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), true},
		{"yesterday", time.Time{}, false},
	}
	for _, c := range cases {
		got, err := parseTime(c.in)
		if (err == nil) != c.ok || !got.Equal(c.want) {
			t.Errorf("parseTime(%q) = %v, %v; want %v ok=%v", c.in, got, err, c.want, c.ok)
		}
	}
}
