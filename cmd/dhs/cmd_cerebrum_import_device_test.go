package main

import (
	"errors"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/export"
)

func TestCerebrumDeviceRows(t *testing.T) {
	snap := &export.Snapshot{Slots: []export.SlotDump{{Objects: []consumer.Object{
		{Label: "Settings.UserLabel", Access: 3, Value: consumer.Value{Kind: consumer.KindString, Str: "Cam 1"}},
		{Label: "Status.Connected", Access: 1, Value: consumer.Value{Kind: consumer.KindInt, Int: 1}},
		{Label: "Gain", Access: 3, Value: consumer.Value{Kind: consumer.KindFloat, Float: 2.5}},
		{Label: "Mode", Access: 3, Value: consumer.Value{Kind: consumer.KindEnum, Str: "Auto"}},
		{Label: "Mode2", Access: 3, EnumItems: []string{"Off", "On"}, Value: consumer.Value{Kind: consumer.KindEnum, Enum: 1}},
		{Path: []string{"Port"}, Access: 3, Value: consumer.Value{Kind: consumer.KindInt, Int: 8080}},
		{Path: []string{"Senders", "[NMOS|2|node|ATX-1]", "Label"}, Label: "Label", Access: 3, Value: consumer.Value{Kind: consumer.KindString, Str: "ATX-1"}},
	}}}}
	rows, readOnly, err := cerebrumDeviceRows(snap)
	if err != nil {
		t.Fatal(err)
	}
	if readOnly != 1 {
		t.Fatalf("read-only rows: %d, want 1", readOnly)
	}
	want := []cerebrumDeviceRow{
		{Object: "Settings.UserLabel", Want: "Cam 1"},
		{Object: "Gain", Want: "2.5"},
		{Object: "Mode", Want: "Auto", IsEnum: true},
		{Object: "Mode2", Want: "On", IsEnum: true},
		{Object: "Port", Want: "8080"},
		{Object: "Senders.[NMOS|2|node|ATX-1].Label", Want: "ATX-1"},
	}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows %+v, want %d", len(rows), rows, len(want))
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Fatalf("row %d: %+v, want %+v", i, rows[i], want[i])
		}
	}

	bad := &export.Snapshot{Slots: []export.SlotDump{{Objects: []consumer.Object{{Label: "X", Access: 3}}}}}
	var verr *consumer.ValidationError
	if _, _, err := cerebrumDeviceRows(bad); !errors.As(err, &verr) {
		t.Fatalf("a writable row without a value must be refused, got %v", err)
	}
}

func TestCerebrumSameValue(t *testing.T) {
	for _, c := range []struct {
		a, b string
		same bool
	}{
		{"5.000000", "5", true},
		{"65536.000000", "65536", true},
		{"Cam 1", "Cam 1", true},
		{"Cam 1", "Cam 2", false},
		{"1", "1.5", false},
	} {
		if got := cerebrumSameValue(c.a, c.b); got != c.same {
			t.Errorf("cerebrumSameValue(%q, %q) = %t", c.a, c.b, got)
		}
	}
}
