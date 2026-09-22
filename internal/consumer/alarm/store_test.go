package alarm

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const smallTemplate = `{"model":"m","rows":[{"match":"x","kind":"text","normal":"ok","source":"site"}]}`

func TestResolvePrefersTheModelThenTheProtocolDefault(t *testing.T) {
	root := t.TempDir()
	// Nothing written yet: no rules is not an error.
	tpl, from, err := Resolve(root, "mnset", "FusioN6@0x68cd783f")
	if tpl != nil || from != "" || err != nil {
		t.Fatalf("empty cache = %v, %q, %v", tpl, from, err)
	}

	// The protocol default alone governs every card.
	def := Path(root, "mnset", DefaultName)
	if _, err := Save(def, mustLoad(t, smallTemplate)); err != nil {
		t.Fatal(err)
	}
	tpl, from, err = Resolve(root, "mnset", "FusioN6@0x68cd783f")
	if err != nil || tpl == nil || from != def {
		t.Fatalf("default = %v, %q, %v", tpl, from, err)
	}

	// A model file wins over it.
	own := Path(root, "mnset", "FusioN6@0x68cd783f")
	if _, err := Save(own, mustLoad(t, `{"model":"FusioN6@0x68cd783f","rows":[{"match":"y","kind":"text","normal":"ok","source":"site"}]}`)); err != nil {
		t.Fatal(err)
	}
	tpl, from, err = Resolve(root, "mnset", "FusioN6@0x68cd783f")
	if err != nil || from != own || tpl.Rows[0].Match != "y" {
		t.Fatalf("model file = %v, %q, %v", tpl, from, err)
	}
	// A device with no identity still gets the protocol default.
	if _, from, _ := Resolve(root, "mnset", ""); from != def {
		t.Errorf("identity-less resolve = %q", from)
	}
	// A device of another protocol sees nothing.
	if tpl, _, _ := Resolve(root, "acp1", "GDR100@1.4"); tpl != nil {
		t.Errorf("templates must not leak across protocols: %v", tpl)
	}
}

func TestResolveSurfacesABadTemplate(t *testing.T) {
	root := t.TempDir()
	p := Path(root, "mnset", "Broken@1")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"model":"m","rows":[{"match":"a","kind":"number"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, from, err := Resolve(root, "mnset", "Broken@1")
	if err == nil || !strings.Contains(err.Error(), "no band") || from != p {
		t.Fatalf("bad template = %q, %v", from, err)
	}
	// LoadFile reports a missing file as fs.ErrNotExist, so a caller can
	// tell "no rules" from "bad rules".
	if _, err := LoadFile(filepath.Join(root, "nope.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file err = %v", err)
	}
}

func TestSaveIsIdempotentAndAtomic(t *testing.T) {
	root := t.TempDir()
	p := Path(root, "mnset", "FusioN6@0x68cd783f")
	tpl := mustLoad(t, smallTemplate)

	changed, err := Save(p, tpl)
	if err != nil || !changed {
		t.Fatalf("first save = %v, %v", changed, err)
	}
	changed, err = Save(p, tpl)
	if err != nil || changed {
		t.Fatalf("second save of the same rules must change nothing: %v, %v", changed, err)
	}
	// A real edit does change it.
	tpl.Rows[0].Severity = "critical"
	changed, err = Save(p, tpl)
	if err != nil || !changed {
		t.Fatalf("edited save = %v, %v", changed, err)
	}
	back, err := LoadFile(p)
	if err != nil || back.Rows[0].Severity != "critical" {
		t.Fatalf("read back = %v, %v", back, err)
	}
	// No .tmp is left behind.
	entries, _ := os.ReadDir(filepath.Dir(p))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
}

func TestSaveReportsWhereItFailed(t *testing.T) {
	root := t.TempDir()
	// A file where the directory must go: MkdirAll cannot proceed.
	blocked := filepath.Join(root, "alarm")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(Path(root, "mnset", "X@1"), mustLoad(t, smallTemplate)); err == nil {
		t.Error("writing under a file must fail")
	}
}

func TestPathsAreSafeOnEveryOS(t *testing.T) {
	p := Path("/root", "ember+", `Tiny Ember+ Router@1.6.2 <x>`)
	if strings.ContainsAny(filepath.Base(p), `:*?"<>|`) {
		t.Errorf("unsafe file name: %s", p)
	}
	if filepath.Base(filepath.Dir(p)) != "ember+" {
		t.Errorf("proto dir = %s", filepath.Dir(p))
	}
	if Dir("/root", "mnset") != filepath.Join("/root", "alarm", "mnset") {
		t.Errorf("Dir = %s", Dir("/root", "mnset"))
	}
}

func TestSaveSurfacesWriteAndRenameFailures(t *testing.T) {
	root := t.TempDir()
	tpl := mustLoad(t, smallTemplate)

	// A directory sitting where the temporary file must go.
	p := Path(root, "mnset", "A@1")
	if err := os.MkdirAll(p+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(p, tpl); err == nil || !strings.Contains(err.Error(), "write") {
		t.Errorf("blocked temporary file err = %v", err)
	}
	_ = os.RemoveAll(p + ".tmp")

	// A directory sitting where the template must land: the rename fails.
	q := Path(root, "mnset", "B@1")
	if err := os.MkdirAll(q, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(q, tpl); err == nil || !strings.Contains(err.Error(), "install") {
		t.Errorf("blocked install err = %v", err)
	}
}
