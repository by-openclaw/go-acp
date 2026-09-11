package audit

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFiles lays out a capture folder from relative paths, so each
// loader test states only the files that matter to it.
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func loadOneDir(t *testing.T, dir string) *Harvest {
	t.Helper()
	hs, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(hs) != 1 {
		t.Fatalf("Load returned %d harvests, want 1", len(hs))
	}
	return hs[0]
}

// TestInterruptedCaptureLoadsAsPartial: the exporter writes device.json
// first and tree.json last, so a run killed in between leaves the
// identity alone. That folder must still load — dropping it would make
// a registry's followed nodes look like unrelated devices — and every
// check must then say "not looked at" rather than "found nothing".
func TestInterruptedCaptureLoadsAsPartial(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"device.json": `{"target":"198.51.100.30:3212","role":"node","label":"cam-09","id":"11111111-1111-4111-8111-111111111111"}`,
	})
	h := loadOneDir(t, dir)
	if !h.Partial {
		t.Fatal("a folder with device.json and no tree.json must load as Partial")
	}
	if h.Target != "198.51.100.30:3212" || h.Label != "cam-09" || h.Role != "node" {
		t.Errorf("identity not read from device.json: %+v", h)
	}

	f := has(t, checkCaptureCompleteness(h), "NMOS-AUDIT-CAPTURE-INCOMPLETE")
	if f.Severity != SevWarn || !strings.Contains(f.Detail, "cam-09") {
		t.Errorf("incomplete-capture finding = %+v, want WARN naming the device", f)
	}
	if got := checkAPISurface(h); got != nil {
		t.Errorf("a partial capture proves nothing about the API surface, got %v", codeList(got))
	}
	// A complete capture reports no such thing.
	if got := checkCaptureCompleteness(mk("node", nil)); got != nil {
		t.Errorf("a complete capture produced %v", codeList(got))
	}
}

// TestPartialRegistryDoesNotJudgeRegistration: a registry whose capture
// stopped before its catalogue was fetched would otherwise report every
// node in the plant as unregistered.
func TestPartialRegistryDoesNotJudgeRegistration(t *testing.T) {
	reg := mk("registry", nil)
	reg.Partial = true
	node := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {"self": map[string]any{"id": "11111111-1111-4111-8111-111111111111"}}},
	})
	reg.Children = []*Harvest{node}
	if got := checkPlantRegistration([]*Harvest{reg}); got != nil {
		t.Errorf("a partial registry must not be compared against, got %v", codeList(got))
	}
}

// TestRegistryWithoutQueryAPIListsNothing: role says registry (from
// device.json) but no query API was captured. The catalogue is empty
// for the audit's purposes and the report must say so.
func TestRegistryWithoutQueryAPIListsNothing(t *testing.T) {
	reg := mk("registry", map[string]map[string]map[string]any{
		"registration": {"v1.3": {}},
	})
	has(t, checkPlantRegistration([]*Harvest{reg}), "NMOS-REGISTRY-EMPTY")
	if union, counts := reg.resourcesEveryVersion("query", "nodes"); union != nil || counts != nil {
		t.Errorf("an absent API should yield nil, nil; got %v, %v", union, counts)
	}
}

// TestLoadRejectsUnreadableTree covers the read failure that is not
// "file absent": here tree.json is a directory. Absent means the
// capture was interrupted; unreadable means something is wrong with
// the export itself, and that must be an error, not a Partial.
func TestLoadRejectsUnreadableTree(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "tree.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if err == nil {
		t.Fatal("a tree.json that cannot be read must fail the load")
	}
	if errors.Is(err, ErrNoHarvest) {
		t.Error("an unreadable tree.json is not the same as no harvest")
	}
}

// TestLoadInitialisesAPIWithoutData: a tree.json that recorded versions
// but no data (the exporter got the index and nothing else) must still
// load with a usable, non-nil map so the checks can index it.
func TestLoadInitialisesAPIWithoutData(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"tree.json": `{"target":"h:1","apis":{"node":{"versions":["v1.3"]}}}`,
	})
	h := loadOneDir(t, dir)
	if h.APIs["node"].Data == nil {
		t.Error("an API captured without data must load with an empty map, not nil")
	}
	if got := len(h.APIs["node"].Versions); got != 1 {
		t.Errorf("versions = %d, want 1", got)
	}
}

// TestLoadFollowedNodes: under nodes/, a folder that is not a capture is
// skipped, a capture that is broken fails the whole load.
func TestLoadFollowedNodes(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"tree.json":                          `{"target":"reg:80","apis":{}}`,
		"nodes/junk/readme.txt":              "not a capture",
		"nodes/198_51_100_21__cam/tree.json": `{"target":"cam:3212","apis":{}}`,
	})
	h := loadOneDir(t, dir)
	if len(h.Children) != 1 || h.Children[0].Target != "cam:3212" {
		t.Fatalf("children = %+v, want the one real capture", h.Children)
	}

	writeFiles(t, dir, map[string]string{
		"nodes/198_51_100_22__broken/tree.json": "{not json",
	})
	if _, err := Load(dir); err == nil {
		t.Error("a broken followed node must fail the load, not silently shrink the plant")
	}
}

// TestFindHarvestDirsIgnoresFiles: a stray file beside the capture
// folders is neither a harvest nor an error.
func TestFindHarvestDirsIgnoresFiles(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"readme.txt":         "export notes",
		"dev-01/tree.json":   `{"target":"a:1","apis":{}}`,
		"dev-02/device.json": `{"target":"b:1","role":"node"}`,
	})
	hs, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(hs) != 2 {
		t.Fatalf("loaded %d harvests, want 2 (the file is not one)", len(hs))
	}
}

// TestLoadSurfacesScanError: a folder the scan cannot enter is reported,
// not skipped — a half-scanned export must never audit as a complete
// plant. The failure is injected through the walkDir seam because an
// unreadable directory cannot be produced on Windows from Go.
func TestLoadSurfacesScanError(t *testing.T) {
	errLocked := errors.New("permission denied")
	orig := walkDir
	walkDir = func(root string, fn fs.WalkDirFunc) error {
		return fn(filepath.Join(root, "locked"), nil, errLocked)
	}
	t.Cleanup(func() { walkDir = orig })

	_, err := Load(t.TempDir())
	if !errors.Is(err, errLocked) {
		t.Fatalf("Load = %v, want the scan error wrapped", err)
	}
	if !strings.Contains(err.Error(), "scanning") {
		t.Errorf("the error should say the scan failed: %v", err)
	}
}

// TestVersionRankRejectsNonNumericMinor: `v1.x` is not a version and
// must sort after every real one rather than panic or win.
func TestVersionRankRejectsNonNumericMinor(t *testing.T) {
	if got := versionRank("v1.x"); got != -1 {
		t.Errorf("versionRank(v1.x) = %d, want -1", got)
	}
	got := sortedVersionsDesc([]string{"v1.x", "v1.2"})
	if got[0] != "v1.2" {
		t.Errorf("sortedVersionsDesc = %v, want v1.2 first", got)
	}
}
