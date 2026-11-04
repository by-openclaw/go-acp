package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"dhs/internal/datastore"
)

// --data-dir is global: stripped wherever it sits, and every bucket
// then roots under it.
func TestDataDirFlag(t *testing.T) {
	t.Setenv(datastore.DataDirEnv, "")
	prev := treeStore
	t.Cleanup(func() { treeStore = prev })
	dir := filepath.Join(t.TempDir(), "plant")
	for _, args := range [][]string{
		{"--data-dir", dir, "consumer", "acp1", "export", "10.0.0.1"},
		{"consumer", "acp1", "export", "10.0.0.1", "--data-dir=" + dir},
	} {
		rest, err := stripDataDirFlag(args)
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"consumer", "acp1", "export", "10.0.0.1"}; !reflect.DeepEqual(rest, want) {
			t.Errorf("rest %v, want %v", rest, want)
		}
		root, err := datastore.ProjectRoot()
		if err != nil || root != dir {
			t.Errorf("root %q (%v), want %q", root, err, dir)
		}
		if got := snapshotDir("acp1", "10.0.0.1"); got != filepath.Join(dir, "snapshots", "acp1", "10.0.0.1") {
			t.Errorf("snapshotDir %q", got)
		}
		if got := manifestCacheDir(); got != filepath.Join(dir, ".cache") {
			t.Errorf("manifests and DMs in %q, want %q", got, filepath.Join(dir, ".cache"))
		}
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("data dir not created: %v", err)
		}
	}
	if _, err := stripDataDirFlag([]string{"consumer", "--data-dir"}); err == nil {
		t.Error("--data-dir with no directory accepted")
	}
}
