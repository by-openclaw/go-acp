package main

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFlatYAML(t *testing.T) {
	raw := `# lab defaults
---
bind: 0.0.0.0:18080
api-ver: "v1.3"
no-mdns: true
registry: 'http://10.6.250.101:8235'  # plant
count: 42
`
	m, err := parseFlatYAML(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := map[string]string{
		"bind": "0.0.0.0:18080", "api-ver": "v1.3", "no-mdns": "true",
		"registry": "http://10.6.250.101:8235", "count": "42",
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %q, want %q", k, m[k], v)
		}
	}
}

func TestParseFlatYAMLRefusesNesting(t *testing.T) {
	if _, err := parseFlatYAML("section:\n  key: v\n"); err == nil {
		t.Error("nested YAML accepted — the contract is flat")
	}
	if _, err := parseFlatYAML("- item\n"); err == nil {
		t.Error("list YAML accepted")
	}
}

func TestSettingsPrecedence(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "s.yaml")
	if err := os.WriteFile(file, []byte("bind: from-file\nlabel: file-label\nunknown-elsewhere: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	bind := fs.String("bind", "default-bind", "")
	label := fs.String("label", "default-label", "")
	other := fs.String("other", "default-other", "")

	// Explicit flag wins over file; file wins over default; a key the
	// verb does not define is ignored (the file is shared).
	if err := parseVerbFlags(fs, []string{"--settings", file, "--bind", "from-cli"}); err != nil {
		t.Fatalf("parseVerbFlags: %v", err)
	}
	if *bind != "from-cli" {
		t.Errorf("bind = %q, want the explicit flag", *bind)
	}
	if *label != "file-label" {
		t.Errorf("label = %q, want the file value", *label)
	}
	if *other != "default-other" {
		t.Errorf("other = %q, want the built-in default", *other)
	}
}

func TestSettingsEnvFallback(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "s.yaml")
	if err := os.WriteFile(file, []byte("label: env-file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(settingsEnvVar, file)

	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	label := fs.String("label", "d", "")
	if err := parseVerbFlags(fs, nil); err != nil {
		t.Fatalf("parseVerbFlags: %v", err)
	}
	if *label != "env-file" {
		t.Errorf("label = %q, want the DHS_SETTINGS file value", *label)
	}
}

func TestSettingsBadValueNamesTheFlag(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "s.yaml")
	if err := os.WriteFile(file, []byte("count: notanumber\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Int("count", 0, "")
	err := parseVerbFlags(fs, []string{"--settings", file})
	if err == nil || !strings.Contains(err.Error(), "count") {
		t.Errorf("bad value error must name the flag: %v", err)
	}
}
