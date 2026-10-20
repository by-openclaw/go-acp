package consumer

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"dhs/internal/manifest"
)

// The committed manifest fixture (testdata/integration-test, ADR-0025
// deliverable 4) is what Manifest makes of the synthetic system of
// dm_test.go, with the identities of the two committed models.
// RRCS_UPDATE_FIXTURE=1 rewrites it; otherwise it must be in sync.
func TestFixtureManifest(t *testing.T) {
	m := dmModel(t)
	var snap map[string]any
	_ = json.Unmarshal([]byte(dmSnapshot), &snap)
	set, err := BuildDMs(m, "9.0.RR1-11", ExTypeNames(snap))
	if err != nil {
		t.Fatal(err)
	}
	mf := Manifest(m, "192.0.2.10", 8193, set)
	root := filepath.Join("..", "testdata", "integration-test")
	want := filepath.Join(root, "manifest", "rrcs", "192.0.2.10.json")
	if os.Getenv("RRCS_UPDATE_FIXTURE") != "" {
		_ = os.Remove(want)
		path, err := manifest.Write(root, mf)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("fixture written: %s", path)
	}
	raw, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("fixture: %v (RRCS_UPDATE_FIXTURE=1 writes it)", err)
	}
	fresh, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fresh, &b); err != nil {
		t.Fatal(err)
	}
	ca, _ := json.Marshal(a)
	cb, _ := json.Marshal(b)
	if !bytes.Equal(ca, cb) {
		t.Errorf("the manifest fixture is out of sync with Manifest(): RRCS_UPDATE_FIXTURE=1 go test -run TestFixtureManifest")
	}
	// The models the fixture refers to are committed, and read back.
	for _, s := range mf.Frames[0].Slots {
		dm := filepath.Join(root, "dm", "rrcs", s.DM+".json")
		if _, err := os.Stat(dm); err != nil {
			t.Errorf("fixture refers to %s: %v", s.DM, err)
		}
	}
	loaded, err := manifest.Load(want)
	if err != nil {
		t.Fatalf("fixture does not load: %v", err)
	}
	if loaded.Device.Protocol != "rrcs" || len(loaded.Frames[0].Slots) != 7 {
		t.Errorf("fixture: %+v", loaded.Device)
	}
}
