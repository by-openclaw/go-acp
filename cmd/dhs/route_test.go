package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"acp/internal/cerebrum-nb/codec"
)

// TestRouteActionEncoding verifies the wire emitted by the route verb
// matches Ember+ Cerebrum NB §4.1.1 — the exact byte shape the
// authoritative DOCX shows for a ROUTE action.
func TestRouteActionEncoding(t *testing.T) {
	body := &codec.RoutingAction{
		Type:       "ROUTE",
		IPAddress:  "0.0.0.0",
		DeviceType: codec.DeviceType("ROUTER"),
		DestID:     "60",
		SrceID:     "60",
		LevelID:    "1",
	}
	got := string(codec.EncodeAction(42, body))

	for _, want := range []string{
		`<ACTION MTID="42">`,
		`TYPE="ROUTE"`,
		`DEVICE_TYPE="ROUTER"`,
		`IP_ADDRESS="0.0.0.0"`,
		`SRCE_ID="60"`,
		`DEST_ID="60"`,
		`LEVEL_ID="1"`,
		`</ACTION>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("encoded action missing %q\n  got: %s", want, got)
		}
	}
}

func TestParseRouteFlag(t *testing.T) {
	for _, tc := range []struct {
		in      string
		ok      bool
		want    routeSpec
	}{
		{"60:60:1", true, routeSpec{Dest: "60", Srce: "60", Level: "1"}},
		{"61:62:3", true, routeSpec{Dest: "61", Srce: "62", Level: "3"}},
		{"60:60", false, routeSpec{}},
		{"60::1", false, routeSpec{}},
		{":", false, routeSpec{}},
	} {
		got, err := parseRouteFlag(tc.in)
		if tc.ok && err != nil {
			t.Errorf("parseRouteFlag(%q) = err %v", tc.in, err)
			continue
		}
		if !tc.ok && err == nil {
			t.Errorf("parseRouteFlag(%q) expected err, got %+v", tc.in, got)
			continue
		}
		if tc.ok && got != tc.want {
			t.Errorf("parseRouteFlag(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestReadRoutesCSV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.csv")
	body := "dest,srce,level\n60,60,1\n61,61,1\n62,62,1\n63,63,1\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := readRoutesCSV(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 routes, got %d", len(got))
	}
	if got[0].Dest != "60" || got[0].Srce != "60" || got[0].Level != "1" {
		t.Errorf("row 0 = %+v", got[0])
	}
	if got[3].Dest != "63" {
		t.Errorf("row 3 = %+v", got[3])
	}
}

