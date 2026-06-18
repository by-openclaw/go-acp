package main

import (
	"context"
	"strings"
	"testing"

	"dhs/internal/probel-sw08p/codec"
)

// TestParseUpdateNameType maps every accepted --type spelling to the
// codec enum and rejects the rest.
func TestParseUpdateNameType(t *testing.T) {
	ok := map[string]codec.UpdateNameType{
		"source":       codec.UpdateNameSource,
		"src":          codec.UpdateNameSource,
		"source-assoc": codec.UpdateNameSourceAssoc,
		"src-assoc":    codec.UpdateNameSourceAssoc,
		"dest-assoc":   codec.UpdateNameDestAssoc,
		"dst-assoc":    codec.UpdateNameDestAssoc,
		"umd":          codec.UpdateNameUMDLabel,
		"UMD":          codec.UpdateNameUMDLabel,
	}
	for in, want := range ok {
		got, err := parseUpdateNameType(in)
		if err != nil {
			t.Errorf("parseUpdateNameType(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseUpdateNameType(%q) = %#x, want %#x", in, got, want)
		}
	}
	if _, err := parseUpdateNameType("bogus"); err == nil {
		t.Error("parseUpdateNameType(bogus) returned nil error")
	}
}

// TestParseUpdateNameWidth maps the four legal widths and rejects others.
func TestParseUpdateNameWidth(t *testing.T) {
	ok := map[int]codec.NameLength{
		4: codec.NameLen4, 8: codec.NameLen8, 12: codec.NameLen12, 16: codec.NameLen16,
	}
	for in, want := range ok {
		got, err := parseUpdateNameWidth(in)
		if err != nil {
			t.Errorf("parseUpdateNameWidth(%d) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseUpdateNameWidth(%d) = %#x, want %#x", in, got, want)
		}
	}
	if _, err := parseUpdateNameWidth(7); err == nil {
		t.Error("parseUpdateNameWidth(7) returned nil error")
	}
}

// TestUpdateNameFlagValidation — bad flags fail before any dial, with a
// usage error naming the offending flag. None of these reach the network.
func TestUpdateNameFlagValidation(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no-addr", []string{"--names=A"}, "missing <host:port>"},
		{"bad-type", []string{"127.0.0.1:2008", "--type", "nope", "--names", "A"}, "unknown --type"},
		{"bad-width", []string{"127.0.0.1:2008", "--width", "7", "--names", "A"}, "unknown --width"},
		{"matrix-range", []string{"127.0.0.1:2008", "--matrix", "999", "--names", "A"}, "--matrix out of range"},
		{"level-range", []string{"127.0.0.1:2008", "--level", "999", "--names", "A"}, "--level out of range"},
		{"first-range", []string{"127.0.0.1:2008", "--first-id", "70000", "--names", "A"}, "--first-id out of range"},
		{"empty-names", []string{"127.0.0.1:2008", "--names", "  "}, "--names requires"},
		{"missing-names", []string{"127.0.0.1:2008"}, "--names requires"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runProbelUpdateName(context.Background(), tc.args)
			if err == nil {
				t.Fatalf("args %v returned nil error", tc.args)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.want)
			}
		})
	}
}

// TestUpdateNameDispatched — the verb is reachable through the SW-P-08
// dispatcher (catches a missing switch case) and honours the
// missing-addr contract like every other verb.
func TestUpdateNameDispatched(t *testing.T) {
	err := runProbelsw08p(context.Background(), []string{"update-name"})
	if err == nil {
		t.Fatal("update-name with no <host:port> returned nil error")
	}
	if !strings.Contains(err.Error(), "missing <host:port>") {
		t.Errorf("error = %q, want 'missing <host:port>'", err.Error())
	}
}
