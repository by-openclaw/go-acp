package main

import (
	"strings"
	"testing"

	"dhs/internal/cerebrum-nb/codec"
	cerebrumnb "dhs/internal/cerebrum-nb/consumer"
)

func TestRMKind(t *testing.T) {
	for in, want := range map[string]cerebrumnb.RMKind{
		"source": cerebrumnb.RMSources, "Sources": cerebrumnb.RMSources, "srce": cerebrumnb.RMSources,
		"destination": cerebrumnb.RMDestinations, "destinations": cerebrumnb.RMDestinations, " dest ": cerebrumnb.RMDestinations,
	} {
		got, err := rmKind(in, "rm-add")
		if err != nil || got != want {
			t.Errorf("rmKind(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "level", "src"} {
		if _, err := rmKind(in, "rm-add"); err == nil {
			t.Errorf("rmKind(%q): want error", in)
		}
	}
}

func TestRMIDs(t *testing.T) {
	ids, err := rmIDs("21, 22,23")
	if err != nil || strings.Join(ids, ",") != "21,22,23" {
		t.Fatalf("rmIDs = %v, %v", ids, err)
	}
	for _, in := range []string{"", "1,,2", "0", "a", "1, ,2", "1,"} {
		if _, err := rmIDs(in); err == nil {
			t.Errorf("rmIDs(%q): want error", in)
		}
	}
}

func TestPrintRMResult(t *testing.T) {
	ok := &codec.RoutingResult{Type: "RM_SRCE_ADD", Federation: "0", Value: "ACCEPTED", Accepted: true}
	if err := printRMResult(false, "rm-add", cerebrumnb.RMSources, ok, map[string]any{"count": 2, "virtual": false, "federation": false}); err != nil {
		t.Fatalf("accepted text: %v", err)
	}
	if err := printRMResult(true, "rm-delete", cerebrumnb.RMDestinations, ok, map[string]any{"ids": []string{"16"}, "federation": true}); err != nil {
		t.Fatalf("accepted json: %v", err)
	}
	failed := &codec.RoutingResult{Type: "RM_DEST_DELETE", Value: "FAILED"}
	if err := printRMResult(false, "rm-delete", cerebrumnb.RMDestinations, failed, nil); err == nil || !strings.Contains(err.Error(), "FAILED") {
		t.Fatalf("failed result: want error naming FAILED, got %v", err)
	}
	if got := rmExtraText(map[string]any{"federation": true, "count": 4, "virtual": false}); got != "count=4 virtual=false federation=true" {
		t.Errorf("rmExtraText = %q", got)
	}
}
