package devicemodel

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/export"
	"dhs/internal/identity"
)

// rrs1601 is the fixture's anchor fingerprint: axon/synapse RRS18-1601/acp1.
var rrs1601 = Fingerprint{Vendor: "axon", Product: "synapse", Model: "RRS18", SwRev: "1601", Proto: "acp1"}

func swapOpenFile(t *testing.T, fn func(string) (*os.File, error)) {
	t.Helper()
	orig := openFile
	openFile = fn
	t.Cleanup(func() { openFile = orig })
}

// swapCloseFile really closes the file (so Windows lets writeSlot unlink
// its .tmp) and then reports the injected failure.
func swapCloseFile(t *testing.T, err error) {
	t.Helper()
	orig := closeFile
	closeFile = func(f *os.File) error {
		_ = f.Close()
		return err
	}
	t.Cleanup(func() { closeFile = orig })
}

func plantFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("plant %s: %v", path, err)
	}
}

func plantDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("plant dir %s: %v", path, err)
	}
}

// unaddressableRoot is a library root the OS refuses to address (an
// embedded NUL is EINVAL on Windows and Linux alike, and never
// IsNotExist). It is the one non-ENOENT stat / readdir failure that
// can be produced honestly on both OSes without privileges.
func unaddressableRoot(t *testing.T) string {
	t.Helper()
	return t.TempDir() + "\x00"
}

// TestPathHelpers pins the catalogue layout
// <root>/<vendor>/<product>/<model>-<sw_rev>/<proto>/ and its
// gate: every segment passes identity.PathSegment, so a traversal or
// otherwise unsafe value in ANY field is refused (ErrUnsafe), and a
// missing vendor/product is ErrInvalid — the directory cannot be
// located without them.
func TestPathHelpers(t *testing.T) {
	r := &fileResolver{root: "ROOT"}
	ok := rrs1601
	cases := []struct {
		name    string
		fp      Fingerprint
		wantErr error
	}{
		{"vendor missing", Fingerprint{Product: "p", Model: "m", SwRev: "1", Proto: "acp1"}, ErrInvalid},
		{"product missing", Fingerprint{Vendor: "v", Model: "m", SwRev: "1", Proto: "acp1"}, ErrInvalid},
		{"vendor traversal", with(ok, func(f *Fingerprint) { f.Vendor = "../etc" }), identity.ErrUnsafe},
		{"product traversal", with(ok, func(f *Fingerprint) { f.Product = "../etc" }), identity.ErrUnsafe},
		{"model traversal", with(ok, func(f *Fingerprint) { f.Model = ".." }), identity.ErrUnsafe},
		{"sw_rev traversal", with(ok, func(f *Fingerprint) { f.SwRev = ".." }), identity.ErrUnsafe},
		{"proto traversal", with(ok, func(f *Fingerprint) { f.Proto = ".." }), identity.ErrUnsafe},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, perr := r.protoDir(tc.fp)
			if !errors.Is(perr, tc.wantErr) {
				t.Fatalf("protoDir err = %v, want %v", perr, tc.wantErr)
			}
			_, yerr := r.productYAMLPath(tc.fp)
			// product.yaml lives one level above the proto dir, so the
			// proto segment is not part of its path check.
			if tc.name == "proto traversal" {
				if yerr != nil {
					t.Fatalf("productYAMLPath must ignore proto: %v", yerr)
				}
				return
			}
			if !errors.Is(yerr, tc.wantErr) {
				t.Fatalf("productYAMLPath err = %v, want %v", yerr, tc.wantErr)
			}
		})
	}
	t.Run("layout", func(t *testing.T) {
		pd, err := r.protoDir(ok)
		if err != nil || pd != filepath.Join("ROOT", "axon", "synapse", "RRS18-1601", "acp1") {
			t.Fatalf("protoDir = %q, %v", pd, err)
		}
		py, err := r.productYAMLPath(ok)
		if err != nil || py != filepath.Join("ROOT", "axon", "synapse", "RRS18-1601", "product.yaml") {
			t.Fatalf("productYAMLPath = %q, %v", py, err)
		}
	})
}

func with(fp Fingerprint, mut func(*Fingerprint)) Fingerprint {
	mut(&fp)
	return fp
}

// TestResolve_Errors pins what Resolve answers when the catalogue is
// present but unusable: an unsafe fingerprint is refused before any
// I/O; a proto dir with no slot files is ErrNotFound (a directory is
// not a schema); a stat failure that is not ENOENT, an unreadable or
// corrupt slot file, and a corrupt product.yaml all surface as errors
// naming the file — never as a silent "not found".
func TestResolve_Errors(t *testing.T) {
	t.Run("unsafe fingerprint refused", func(t *testing.T) {
		r := New(fixture(t))
		_, err := r.Resolve(with(rrs1601, func(f *Fingerprint) { f.Vendor = "../axon" }))
		if !errors.Is(err, identity.ErrUnsafe) {
			t.Fatalf("err = %v, want ErrUnsafe", err)
		}
	})
	t.Run("stat failure other than ENOENT", func(t *testing.T) {
		r := New(unaddressableRoot(t))
		_, err := r.Resolve(rrs1601)
		if err == nil || errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "dmlib: stat ") {
			t.Fatalf("err = %v, want stat error", err)
		}
	})
	t.Run("proto dir without slot files is ErrNotFound", func(t *testing.T) {
		root := t.TempDir()
		plantDir(t, filepath.Join(root, "axon", "synapse", "RRS18-1601", "acp1"))
		_, err := New(root).Resolve(rrs1601)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
	t.Run("proto path is a file, not a directory", func(t *testing.T) {
		root := t.TempDir()
		plantFile(t, filepath.Join(root, "axon", "synapse", "RRS18-1601", "acp1"), "{}")
		_, err := New(root).Resolve(rrs1601)
		if err == nil || !strings.Contains(err.Error(), "dmlib: read ") {
			t.Fatalf("err = %v, want read error", err)
		}
	})
	t.Run("corrupt slot file", func(t *testing.T) {
		root := fixture(t)
		plantFile(t, filepath.Join(root, "axon", "synapse", "RRS18-1601", "acp1", "slot_2.json"), "{not json")
		_, err := New(root).Resolve(rrs1601)
		if err == nil || !strings.Contains(err.Error(), "dmlib: decode slot_2.json") {
			t.Fatalf("err = %v, want decode error naming slot_2.json", err)
		}
	})
	t.Run("unreadable slot file", func(t *testing.T) {
		root := fixture(t)
		swapOpenFile(t, func(p string) (*os.File, error) {
			return nil, &os.PathError{Op: "open", Path: p, Err: os.ErrPermission}
		})
		_, err := New(root).Resolve(rrs1601)
		if !errors.Is(err, os.ErrPermission) || !strings.Contains(err.Error(), "dmlib: open slot_1.json") {
			t.Fatalf("err = %v, want open error naming slot_1.json", err)
		}
	})
	t.Run("corrupt product.yaml", func(t *testing.T) {
		root := fixture(t)
		plantFile(t, filepath.Join(root, "axon", "synapse", "RRS18-1601", "product.yaml"), "{not json")
		_, err := New(root).Resolve(rrs1601)
		if err == nil || !strings.Contains(err.Error(), "dmlib: decode ") {
			t.Fatalf("err = %v, want decode error", err)
		}
	})
}

// TestReadSlots_IgnoresForeignFiles: only slot_<n>.json counts as a
// snapshot; notes, sub-directories, and near-miss names sit in the
// same directory without breaking Resolve.
func TestReadSlots_IgnoresForeignFiles(t *testing.T) {
	root := fixture(t)
	dir := filepath.Join(root, "axon", "synapse", "RRS18-1601", "acp1")
	plantFile(t, filepath.Join(dir, "README.md"), "# notes")
	plantFile(t, filepath.Join(dir, "slot_x.json"), "{not json")
	plantFile(t, filepath.Join(dir, "slot_2.json.bak"), "{not json")
	plantDir(t, filepath.Join(dir, "slot_3.json.d"))
	s, err := New(root).Resolve(rrs1601)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(s.Slots) != 1 || s.Slots[1] == nil {
		t.Fatalf("slots = %v, want only slot 1", s.Slots)
	}
}

// TestLookupAlternate_Contract pins the changelog question "which
// other firmware revisions of this model do we hold for this
// protocol?": siblings are matched on the exact <model>- prefix, the
// requested rev is excluded, a sibling without the proto sub-dir does
// not count, stray files are ignored, and the answer is sorted by
// sw_rev.
func TestLookupAlternate_Contract(t *testing.T) {
	root := fixture(t) // RRS18-1601 {acp1, acp2}, RRS18-1602 {acp1}
	product := filepath.Join(root, "axon", "synapse")
	writeAt(t, root, "axon", "synapse", "RRS18-1500", "acp1", 1, makeSnapshot("RRS18", nil))
	plantDir(t, filepath.Join(product, "RRS18-1700", "emberplus")) // no acp1 -> not an alternate
	plantDir(t, filepath.Join(product, "RRS180-1601", "acp1"))     // different model sharing the prefix text
	plantFile(t, filepath.Join(product, "RRS18-1800"), "a file, not a rev dir")

	alts, err := New(root).LookupAlternate(rrs1601)
	if err != nil {
		t.Fatalf("LookupAlternate: %v", err)
	}
	var revs []string
	for _, a := range alts {
		if a.Vendor != "axon" || a.Product != "synapse" || a.Model != "RRS18" || a.Proto != "acp1" {
			t.Fatalf("alternate carries wrong anchor: %+v", a)
		}
		revs = append(revs, a.SwRev)
	}
	if strings.Join(revs, ",") != "1500,1602" {
		t.Fatalf("alternates = %v, want [1500 1602]", revs)
	}
}

// TestLookupAlternate_Errors: a fingerprint without Model/Proto cannot
// be asked about; a vendor/product that cannot be located is ErrInvalid
// / ErrUnsafe; an absent product directory is "no alternates", not an
// error; any other readdir failure is reported.
func TestLookupAlternate_Errors(t *testing.T) {
	cases := []struct {
		name    string
		root    func(t *testing.T) string
		fp      Fingerprint
		wantErr error
		wantNil bool
	}{
		{"model missing", fixture, with(rrs1601, func(f *Fingerprint) { f.Model = "" }), ErrInvalid, true},
		{"proto missing", fixture, with(rrs1601, func(f *Fingerprint) { f.Proto = "" }), ErrInvalid, true},
		{"vendor missing", fixture, with(rrs1601, func(f *Fingerprint) { f.Vendor = "" }), ErrInvalid, true},
		{"vendor unsafe", fixture, with(rrs1601, func(f *Fingerprint) { f.Vendor = "../axon" }), identity.ErrUnsafe, true},
		{"product dir absent is no alternates", fixture, with(rrs1601, func(f *Fingerprint) { f.Product = "nova" }), nil, true},
		{"readdir failure other than ENOENT", unaddressableRoot, rrs1601, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			alts, err := New(tc.root(t)).LookupAlternate(tc.fp)
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.name == "readdir failure other than ENOENT" && (err == nil || !strings.Contains(err.Error(), "dmlib: read ")) {
				t.Fatalf("err = %v, want read error", err)
			}
			if tc.wantErr == nil && !strings.HasPrefix(tc.name, "readdir") && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if tc.wantNil && alts != nil {
				t.Fatalf("alts = %v, want nil", alts)
			}
		})
	}
}

// TestPersist_Errors pins Persist's refusals and I/O failures: nil or
// incomplete schemas are ErrInvalid before any I/O; a root that is a
// file cannot hold the layout; each writeSlot step failure (create,
// encode, close, rename) is reported with no .tmp left behind; and a
// product.yaml that cannot be replaced fails the Persist even though
// the slot files were written.
func TestPersist_Errors(t *testing.T) {
	snap := func() *export.Snapshot {
		return makeSnapshot("RRS18", []consumer.Object{{Slot: 1, ID: 0, Label: "Card Name", Kind: consumer.KindString}})
	}
	schema := func() *Schema {
		return &Schema{Fingerprint: rrs1601, Slots: map[int]*export.Snapshot{1: snap()}}
	}
	protoDir := func(root string) string {
		return filepath.Join(root, "axon", "synapse", "RRS18-1601", "acp1")
	}

	t.Run("nil schema", func(t *testing.T) {
		if err := New(t.TempDir()).Persist(nil); !errors.Is(err, ErrInvalid) {
			t.Fatalf("err = %v, want ErrInvalid", err)
		}
	})
	t.Run("incomplete fingerprint", func(t *testing.T) {
		for _, fp := range []Fingerprint{
			with(rrs1601, func(f *Fingerprint) { f.Model = "" }),
			with(rrs1601, func(f *Fingerprint) { f.SwRev = "" }),
			with(rrs1601, func(f *Fingerprint) { f.Proto = "" }),
		} {
			if err := New(t.TempDir()).Persist(&Schema{Fingerprint: fp}); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Persist(%+v) err = %v, want ErrInvalid", fp, err)
			}
		}
	})
	t.Run("root is a file", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "products")
		plantFile(t, root, "not a dir")
		if err := New(root).Persist(schema()); err == nil || !strings.Contains(err.Error(), "dmlib: mkdir ") {
			t.Fatalf("err = %v, want mkdir error", err)
		}
	})
	t.Run("nil slot snapshot is skipped", func(t *testing.T) {
		root := t.TempDir()
		s := schema()
		s.Slots[2] = nil
		if err := New(root).Persist(s); err != nil {
			t.Fatalf("Persist: %v", err)
		}
		if _, err := os.Stat(filepath.Join(protoDir(root), "slot_2.json")); !os.IsNotExist(err) {
			t.Fatalf("nil snapshot must not produce a file: %v", err)
		}
	})

	closeErr := errors.New("close: I/O error")
	slotFailures := []struct {
		name    string
		arrange func(t *testing.T, root string, s *Schema)
		wantErr string
	}{
		{"create", func(t *testing.T, root string, _ *Schema) {
			plantDir(t, filepath.Join(protoDir(root), "slot_1.json.tmp"))
		}, "dmlib: create "},
		{"encode", func(_ *testing.T, _ string, s *Schema) {
			s.Slots[1].Slots[0].Objects[0].Meta = map[string]any{"handle": make(chan int)}
		}, "dmlib: write "},
		{"close", func(t *testing.T, _ string, _ *Schema) { swapCloseFile(t, closeErr) }, "dmlib: close "},
		{"rename", func(t *testing.T, root string, _ *Schema) {
			plantDir(t, filepath.Join(protoDir(root), "slot_1.json"))
		}, "dmlib: rename "},
	}
	for _, f := range slotFailures {
		t.Run("slot "+f.name, func(t *testing.T) {
			root := t.TempDir()
			s := schema()
			f.arrange(t, root, s)
			err := New(root).Persist(s)
			if err == nil || !strings.Contains(err.Error(), f.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, f.wantErr)
			}
			if f.name == "close" && !errors.Is(err, closeErr) {
				t.Fatalf("close error not wrapped: %v", err)
			}
			tmp := filepath.Join(protoDir(root), "slot_1.json.tmp")
			if st, serr := os.Stat(tmp); serr == nil && !st.IsDir() {
				t.Fatalf("leftover .tmp after %s failure", f.name)
			}
		})
	}

	t.Run("product.yaml cannot be replaced", func(t *testing.T) {
		root := t.TempDir()
		plantDir(t, filepath.Join(root, "axon", "synapse", "RRS18-1601", "product.yaml"))
		s := schema()
		s.Product = ProductMeta{Model: "RRS18", SwRev: "1601"}
		err := New(root).Persist(s)
		if err == nil || !strings.Contains(err.Error(), "dmlib: rename ") {
			t.Fatalf("err = %v, want rename error", err)
		}
		if _, serr := os.Stat(filepath.Join(protoDir(root), "slot_1.json")); serr != nil {
			t.Fatalf("slot file should have been written before product.yaml failed: %v", serr)
		}
	})
}

// TestDiff_NilSchema: a missing side is not a mismatch (nothing to
// compare against), it is an empty diff with PerSlot initialised so
// callers can range over it without a nil check.
func TestDiff_NilSchema(t *testing.T) {
	r := New(t.TempDir())
	s := &Schema{Fingerprint: rrs1601, Slots: map[int]*export.Snapshot{1: makeSnapshot("RRS18", nil)}}
	for name, pair := range map[string][2]*Schema{
		"nil prev": {nil, s},
		"nil cur":  {s, nil},
		"both nil": {nil, nil},
	} {
		t.Run(name, func(t *testing.T) {
			d := r.Diff(pair[0], pair[1])
			if d.Mismatch || d.PerSlot == nil || len(d.PerSlot) != 0 || d.AddedSlots != nil || d.RemovedSlots != nil {
				t.Fatalf("Diff = %+v, want empty diff with initialised PerSlot", d)
			}
		})
	}
}

// TestPersist_SlotFileIsExportJSON pins the deliverable's promise: a
// persisted slot is byte-compatible with `dhs export --format json`
// (hierarchical envelope with device/generator/created_at/slots), so
// the same file feeds the exporter, the producer and the diff.
func TestPersist_SlotFileIsExportJSON(t *testing.T) {
	root := t.TempDir()
	s := &Schema{Fingerprint: rrs1601, Slots: map[int]*export.Snapshot{
		1: makeSnapshot("RRS18", []consumer.Object{{Slot: 1, Path: []string{"control"}, ID: 5, Label: "Gain", Kind: consumer.KindInt}}),
	}}
	if err := New(root).Persist(s); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "axon", "synapse", "RRS18-1601", "acp1", "slot_1.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var doc struct {
		Device    export.DeviceInfo `json:"device"`
		Generator string            `json:"generator"`
		CreatedAt time.Time         `json:"created_at"`
		Slots     []struct {
			Slot    int                       `json:"slot"`
			Objects map[string]map[string]any `json:"objects"`
		} `json:"slots"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("slot file is not the export envelope: %v\n%s", err, raw)
	}
	if doc.Generator != "test" || len(doc.Slots) != 1 || doc.Slots[0].Slot != 1 {
		t.Fatalf("envelope diverged: %+v", doc)
	}
	if _, ok := doc.Slots[0].Objects["control"]["Gain"]; !ok {
		t.Fatalf("objects not nested by path: %v", doc.Slots[0].Objects)
	}
}
