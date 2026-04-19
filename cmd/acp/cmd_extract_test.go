package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSHA256File — byte-identical tree.json produces the identical
// fingerprint. Protects the replay-test contract documented in
// docs/fixtures-products.md.
func TestSHA256File(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tree.json")
	if err := os.WriteFile(p, []byte(`{"protocol":"acp2","n":42}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	first, err := sha256File(p)
	if err != nil {
		t.Fatalf("sha256: %v", err)
	}
	second, err := sha256File(p)
	if err != nil {
		t.Fatalf("sha256 second read: %v", err)
	}
	if first != second {
		t.Errorf("fingerprint non-deterministic: %q vs %q", first, second)
	}
	if !strings.HasPrefix(first, "sha256:") || len(first) != len("sha256:")+64 {
		t.Errorf("fingerprint shape unexpected: %q", first)
	}
}

// TestSHA256File_DifferentBytes — different content = different
// fingerprint. Ensures meta.json.dm_fingerprint actually discriminates.
func TestSHA256File_DifferentBytes(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.json")
	b := filepath.Join(dir, "b.json")
	if err := os.WriteFile(a, []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(`{"v":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	fa, _ := sha256File(a)
	fb, _ := sha256File(b)
	if fa == fb {
		t.Errorf("expected different fingerprints, both are %s", fa)
	}
}

// TestMetaJSONRoundTrip — write + read with the locked schema. Catches
// any field-name drift between the Go struct and the persisted JSON
// (the docs/fixtures-products.md schema is load-bearing for every
// replay test downstream).
func TestMetaJSONRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "meta.json")
	in := &metaJSON{
		Protocol:      "acp2",
		Manufacturer:  "Axon",
		Product:       "DDB08",
		Direction:     "consumer",
		Version:       "2.3",
		VersionKind:   "firmware",
		DiscoveredAt:  "2026-04-19T13:20:00Z",
		Description:   "ACP2 Audio Processor card",
		DMFingerprint: "sha256:abc",
		ObjectCount:   214,
		CaptureTool: captureToolInfo{
			Name:      "acp",
			Version:   "0.3.0",
			GitTag:    "v0.3.0",
			GitCommit: "7bfc8ab",
		},
		Notes: "smoke test",
	}
	if err := writeMetaJSON(p, in); err != nil {
		t.Fatalf("write: %v", err)
	}

	blob, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var out metaJSON
	if err := json.Unmarshal(blob, &out); err != nil {
		t.Fatalf("unmarshal: %v\nbody:\n%s", err, blob)
	}
	if out != *in {
		t.Errorf("round-trip diverged:\n  in:  %+v\n  out: %+v", in, out)
	}

	// Schema stability: make sure the wire JSON has the keys downstream
	// tools depend on. Drift here breaks every replay test.
	wantKeys := []string{
		`"protocol"`, `"manufacturer"`, `"product"`, `"direction"`, `"version"`,
		`"version_kind"`, `"discovered_at"`, `"dm_fingerprint"`,
		`"object_count"`, `"capture_tool"`, `"name"`, `"git_tag"`, `"git_commit"`,
	}
	for _, k := range wantKeys {
		if !strings.Contains(string(blob), k) {
			t.Errorf("meta.json missing expected key %s", k)
		}
	}
}

// TestBuildCaptureToolInfo — always returns name=acp and populates
// GitTag / GitCommit with non-empty fallbacks even when no ldflags
// are set. Guarantees meta.json never emits blank provenance fields.
func TestBuildCaptureToolInfo(t *testing.T) {
	info := buildCaptureToolInfo()
	if info.Name != "acp" {
		t.Errorf("Name got %q, want %q", info.Name, "acp")
	}
	if info.Version == "" {
		t.Errorf("Version must not be empty")
	}
	if info.GitTag == "" {
		t.Errorf("GitTag must not be empty")
	}
	if info.GitCommit == "" {
		t.Errorf("GitCommit must not be empty")
	}
}
