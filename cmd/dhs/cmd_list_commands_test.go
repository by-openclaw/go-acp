package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestCatalogueRowsForProtoCoversAllProtocols pins that every
// registered protocol returns a non-empty catalogue. New protocols
// are required to extend the dispatcher (or this test fails).
func TestCatalogueRowsForProtoCoversAllProtocols(t *testing.T) {
	for _, p := range []string{"acp1", "acp2", "emberplus", "probel-sw02p", "probel-sw08p"} {
		t.Run(p, func(t *testing.T) {
			rows, err := catalogueRowsForProto(p)
			if err != nil {
				t.Fatalf("catalogueRowsForProto(%q): %v", p, err)
			}
			if len(rows) == 0 {
				t.Fatalf("catalogueRowsForProto(%q): empty", p)
			}
			for i, r := range rows {
				if r.Address == "" {
					t.Errorf("rows[%d].Address empty", i)
				}
				if r.Name == "" {
					t.Errorf("rows[%d].Name empty", i)
				}
			}
		})
	}
}

// TestCatalogueRowsForProtoUnknown rejects unknown protocols with a
// helpful error message.
func TestCatalogueRowsForProtoUnknown(t *testing.T) {
	_, err := catalogueRowsForProto("not-a-protocol")
	if err == nil {
		t.Fatal("expected error for unknown protocol; got nil")
	}
	if !strings.Contains(err.Error(), "unknown protocol") {
		t.Errorf("error missing 'unknown protocol': %v", err)
	}
}

// TestLookupCatalogueProbelByByte exercises the probel-sw02p / -sw08p
// byte-address path (decimal + hex).
func TestLookupCatalogueProbelByByte(t *testing.T) {
	cases := []struct {
		proto, addr string
		wantPrefix  string // expected Name prefix
	}{
		{"probel-sw02p", "0x02", "connect"},
		{"probel-sw02p", "2", "connect"},
		{"probel-sw02p", "0x01", "interrogate"},
		{"probel-sw08p", "0x02", "rx 002 Crosspoint Connect"},
		{"probel-sw08p", "120", "rx 120 Crosspoint Connect-On-Go Salvo"},
	}
	for _, c := range cases {
		t.Run(c.proto+"/"+c.addr, func(t *testing.T) {
			row, ok, err := lookupCatalogueForProto(c.proto, c.addr)
			if err != nil {
				t.Fatalf("lookup(%q, %q): %v", c.proto, c.addr, err)
			}
			if !ok {
				t.Fatalf("lookup(%q, %q): not found", c.proto, c.addr)
			}
			if !strings.HasPrefix(row.Name, c.wantPrefix) {
				t.Errorf("Name = %q; want prefix %q", row.Name, c.wantPrefix)
			}
		})
	}
}

// TestLookupCatalogueACPKindAddressing exercises the ACP1 / ACP2
// kind:id address shape.
func TestLookupCatalogueACPKindAddressing(t *testing.T) {
	cases := []struct {
		proto, addr string
		wantName    string
	}{
		{"acp1", "method:0", "getValue"},
		{"acp1", "method:5", "getObject"},
		{"acp1", "objgroup:2", "control"},
		{"acp2", "pid:4", "announce_delay"},
		{"acp2", "acp2-func:1", "get_object"},
		{"acp2", "obj-type:0", "node"},
	}
	for _, c := range cases {
		t.Run(c.proto+"/"+c.addr, func(t *testing.T) {
			row, ok, err := lookupCatalogueForProto(c.proto, c.addr)
			if err != nil {
				t.Fatalf("lookup: %v", err)
			}
			if !ok {
				t.Fatalf("lookup: not found")
			}
			if row.Name != c.wantName {
				t.Errorf("Name = %q; want %q", row.Name, c.wantName)
			}
		})
	}
}

// TestLookupCatalogueEmberplusPaths checks all three Ember+ address
// forms: kind:Name, OID path, dotted label path.
func TestLookupCatalogueEmberplusPaths(t *testing.T) {
	cases := []struct {
		addr     string
		wantName string
	}{
		{"kind:Parameter", "Parameter"},
		{"cmd:GetDirectory", "GetDirectory"},
		{"1.2.4.1.0.2", "1.2.4.1.0.2"},
		{"root.foo.bar", "root.foo.bar"},
	}
	for _, c := range cases {
		t.Run(c.addr, func(t *testing.T) {
			row, ok, err := lookupCatalogueForProto("emberplus", c.addr)
			if err != nil {
				t.Fatalf("lookup: %v", err)
			}
			if !ok {
				t.Fatalf("lookup: not found")
			}
			if row.Name != c.wantName {
				t.Errorf("Name = %q; want %q", row.Name, c.wantName)
			}
		})
	}
}

// TestLookupCatalogueMissingByte returns ok=false for a byte that
// isn't in the codec's catalogue (e.g. unsupported sw02p byte).
func TestLookupCatalogueMissingByte(t *testing.T) {
	_, ok, err := lookupCatalogueForProto("probel-sw02p", "0x80")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if ok {
		t.Errorf("byte 0x80 should not be in the sw02p catalogue (yet)")
	}
}

// TestRenderCatalogueJSONIsValid pins that --format=json output
// parses back as the expected envelope.
func TestRenderCatalogueJSONIsValid(t *testing.T) {
	rows, err := catalogueRowsForProto("probel-sw02p")
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	var buf bytes.Buffer
	if err := renderCatalogueJSON(&buf, "probel-sw02p", rows); err != nil {
		t.Fatalf("renderJSON: %v", err)
	}
	var got struct {
		Protocol string         `json:"protocol"`
		Count    int            `json:"count"`
		Entries  []catalogueRow `json:"entries"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, buf.String())
	}
	if got.Protocol != "probel-sw02p" {
		t.Errorf("Protocol = %q; want probel-sw02p", got.Protocol)
	}
	if got.Count != len(rows) {
		t.Errorf("Count = %d; want %d", got.Count, len(rows))
	}
	if len(got.Entries) != len(rows) {
		t.Errorf("Entries len = %d; want %d", len(got.Entries), len(rows))
	}
}

// TestParseProbelByte covers hex/decimal accept + reject.
func TestParseProbelByte(t *testing.T) {
	cases := []struct {
		in       string
		want     uint8
		wantOk   bool
	}{
		{"0", 0, true},
		{"255", 255, true},
		{"0x00", 0, true},
		{"0xFF", 255, true},
		{"0xff", 255, true},
		{"256", 0, false},
		{"abc", 0, false},
		{"", 0, false},
		{"0x100", 0, false},
	}
	for _, c := range cases {
		got, ok := parseProbelByte(c.in)
		if ok != c.wantOk || got != c.want {
			t.Errorf("parseProbelByte(%q) = (%d, %t); want (%d, %t)", c.in, got, ok, c.want, c.wantOk)
		}
	}
}
