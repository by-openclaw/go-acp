package datastore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/export"
	"dhs/internal/export/canonical"
)

// swapOpenFile installs an os.Open stand-in for the duration of a
// test; restore is on cleanup so later tests read real files again.
func swapOpenFile(t *testing.T, fn func(string) (*os.File, error)) {
	t.Helper()
	orig := openFile
	openFile = fn
	t.Cleanup(func() { openFile = orig })
}

// writeRaw plants an arbitrary payload at a DM path so the reader's
// shape detection can be driven with hand-written content.
func writeRaw(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// topLevelKeys returns the sorted top-level keys of a DM file so a test
// can assert the on-disk contract by key set, not by decoded struct.
func topLevelKeys(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	keys := make([]string, 0, len(probe))
	for k := range probe {
		keys = append(keys, k)
	}
	return keys
}

// TestSplitIdentity pins the "<Model>@<SwRev>" key: split on the LAST
// '@' so a model name that itself carries '@' survives, and a bare
// model (no '@') yields an empty sw_rev rather than an error.
func TestSplitIdentity(t *testing.T) {
	cases := []struct {
		name, in, model, swRev string
	}{
		{"model at sw_rev", "RRS18@1601", "RRS18", "1601"},
		{"dotted sw_rev with a space in the model", "CONVERT Hybrid@6.7.4", "CONVERT Hybrid", "6.7.4"},
		{"model containing '@' splits on the last one", "a@b@c", "a@b", "c"},
		{"no '@' means no sw_rev", "RRS18", "RRS18", ""},
		{"leading '@' means no model", "@1601", "", "1601"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, r := splitIdentity(tc.in)
			if m != tc.model || r != tc.swRev {
				t.Fatalf("splitIdentity(%q) = (%q, %q), want (%q, %q)", tc.in, m, r, tc.model, tc.swRev)
			}
		})
	}
}

// TestWriteDM_RejectsEmptyKey: proto and identity are the two path
// segments of the DM file; neither may be blank or the file would land
// at .cache/dm//.json and collide across cards.
func TestWriteDM_RejectsEmptyKey(t *testing.T) {
	s := NewTreeStore(t.TempDir())
	if err := s.WriteDM("", "RRS18@1601", DM{}); err == nil || !strings.Contains(err.Error(), "empty proto") {
		t.Fatalf("empty proto: err = %v", err)
	}
	if err := s.WriteDM("acp1", "", DM{}); err == nil || !strings.Contains(err.Error(), "empty identity") {
		t.Fatalf("empty identity: err = %v", err)
	}
	if err := s.SaveByIdentity("", "RRS18@1601", nil); err == nil || !strings.Contains(err.Error(), "empty proto") {
		t.Fatalf("SaveByIdentity empty proto: err = %v", err)
	}
	if err := s.SaveByIdentity("acp1", "", nil); err == nil || !strings.Contains(err.Error(), "empty identity") {
		t.Fatalf("SaveByIdentity empty identity: err = %v", err)
	}
	if entries, _ := os.ReadDir(s.BaseDir()); len(entries) != 0 {
		t.Fatalf("rejected writes must leave the store untouched, found %d entries", len(entries))
	}
}

// TestWriteDM_FillsIdentityFromKey: a caller may hand over only the
// payload; model / sw_rev / protocol are then derived from the key so
// the file is self-describing. Explicit values are never overridden.
func TestWriteDM_FillsIdentityFromKey(t *testing.T) {
	cases := []struct {
		name                          string
		in                            DM
		wantModel, wantRev, wantProto string
	}{
		{"all derived from key", DM{}, "RRS18", "1601", "acp1"},
		{"explicit model kept, sw_rev derived", DM{Model: "Custom"}, "Custom", "1601", "acp1"},
		{"explicit sw_rev kept, model derived", DM{SwRev: "9.9"}, "RRS18", "9.9", "acp1"},
		{"explicit protocol kept", DM{Protocol: "acp1-legacy"}, "RRS18", "1601", "acp1-legacy"},
		{"fully explicit untouched", DM{Model: "M", SwRev: "R", Protocol: "P"}, "M", "R", "P"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewTreeStore(t.TempDir())
			if err := s.WriteDM("acp1", "RRS18@1601", tc.in); err != nil {
				t.Fatalf("WriteDM: %v", err)
			}
			raw, err := os.ReadFile(s.IdentityPath("acp1", "RRS18@1601"))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			var got DM
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.Model != tc.wantModel || got.SwRev != tc.wantRev || got.Protocol != tc.wantProto {
				t.Fatalf("on disk = %s@%s/%s, want %s@%s/%s",
					got.Model, got.SwRev, got.Protocol, tc.wantModel, tc.wantRev, tc.wantProto)
			}
		})
	}
}

// TestWriteDM_CanonicalTreeOnly pins the Ember+ contract (#438): a DM
// built from canonical.Export carries identity + root (+ templates)
// and NOTHING else — no flat objects redundancy — and the root comes
// back as its concrete element type through DM.UnmarshalJSON.
func TestWriteDM_CanonicalTreeOnly(t *testing.T) {
	s := NewTreeStore(t.TempDir())
	root := &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "router", Path: "router", OID: "1", Access: canonical.AccessRead,
		Children: []canonical.Element{
			&canonical.Parameter{Header: canonical.Header{Number: 2, Identifier: "gain", Path: "router.gain", OID: "1.2", Access: canonical.AccessReadWrite}, Type: "integer"},
		},
	}}
	tpl := &canonical.TemplateEntry{}
	if err := s.WriteDM("emberplus", "PowerCore@2.1", DM{Root: root, Templates: []*canonical.TemplateEntry{tpl}}); err != nil {
		t.Fatalf("WriteDM: %v", err)
	}
	path := s.IdentityPath("emberplus", "PowerCore@2.1")

	want := map[string]bool{"model": true, "sw_rev": true, "protocol": true, "root": true, "templates": true}
	keys := topLevelKeys(t, path)
	if len(keys) != len(want) {
		t.Fatalf("top-level keys = %v, want exactly %v", keys, want)
	}
	for _, k := range keys {
		if !want[k] {
			t.Fatalf("forbidden top-level key %q in canonical DM", k)
		}
	}

	raw, _ := os.ReadFile(path)
	var got DM
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	node, ok := got.Root.(*canonical.Node)
	if !ok {
		t.Fatalf("Root decoded as %T, want *canonical.Node", got.Root)
	}
	if node.Identifier != "router" || len(node.Children) != 1 {
		t.Fatalf("root lost content: %+v", node.Header)
	}
	if p, ok := node.Children[0].(*canonical.Parameter); !ok || p.Type != "integer" {
		t.Fatalf("child decoded as %T (%+v), want *canonical.Parameter integer", node.Children[0], node.Children[0])
	}
	if got.Objects != nil {
		t.Fatalf("canonical DM must not carry flat objects, got %d", len(got.Objects))
	}
	if len(got.Templates) != 1 {
		t.Fatalf("templates lost: %v", got.Templates)
	}
	// LoadByIdentity still answers for a Root-only DM: a Snapshot with
	// the protocol and an empty object list (nothing flat to seed).
	snap, err := s.LoadByIdentity("emberplus", "PowerCore@2.1")
	if err != nil || snap == nil || snap.Device.Protocol != "emberplus" || len(snap.Slots[0].Objects) != 0 {
		t.Fatalf("LoadByIdentity(root-only) = %+v, %v", snap, err)
	}
}

// TestWriteDM_FlatObjectsSlotAgnostic pins the ACP1/ACP2 path: the flat
// object list is written with Slot zeroed (DM is per-card, not
// per-frame-position) and no root key appears.
func TestWriteDM_FlatObjectsSlotAgnostic(t *testing.T) {
	s := NewTreeStore(t.TempDir())
	objs := []consumer.Object{
		{Slot: 7, ID: 0, Label: "Card name", Kind: consumer.KindString},
		{Slot: 7, ID: 1, Label: "User label", Kind: consumer.KindString},
	}
	if err := s.WriteDM("acp1", "RRS18@1601", DM{Objects: objs}); err != nil {
		t.Fatalf("WriteDM: %v", err)
	}
	if objs[0].Slot != 7 {
		t.Fatal("WriteDM must not mutate the caller's slice")
	}
	for _, k := range topLevelKeys(t, s.IdentityPath("acp1", "RRS18@1601")) {
		if k == "root" || k == "templates" {
			t.Fatalf("flat DM must not carry %q", k)
		}
	}
	snap, err := s.LoadByIdentity("acp1", "RRS18@1601")
	if err != nil || snap == nil {
		t.Fatalf("LoadByIdentity: %v, %v", snap, err)
	}
	for _, o := range snap.Slots[0].Objects {
		if o.Slot != 0 {
			t.Fatalf("object %q kept Slot=%d on disk; DM is slot-agnostic", o.Label, o.Slot)
		}
	}
}

// TestDM_UnmarshalJSON_Root pins the dispatch of the interface-typed
// root: absent and null both mean "no canonical tree", a malformed
// root is a decode error naming the field, and the outer envelope's
// own syntax errors are reported as-is.
func TestDM_UnmarshalJSON_Root(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantErr  string
		wantRoot bool
	}{
		{"root absent", `{"model":"M","sw_rev":"1","protocol":"acp1"}`, "", false},
		{"root null", `{"model":"M","sw_rev":"1","protocol":"acp1","root":null}`, "", false},
		{"root node", `{"model":"M","sw_rev":"1","protocol":"emberplus","root":{"number":1,"identifier":"r","children":[]}}`, "", true},
		{"root malformed", `{"model":"M","root":[1]}`, "dm root:", false},
		{"envelope malformed", `{"model":`, "unexpected end", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var dm DM
			err := json.Unmarshal([]byte(tc.raw), &dm)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if (dm.Root != nil) != tc.wantRoot {
				t.Fatalf("Root = %v, want present=%v", dm.Root, tc.wantRoot)
			}
			if dm.Model != "M" {
				t.Fatalf("identity fields lost: %+v", dm)
			}
		})
	}
}

// TestLoadByIdentity_RejectsEmptyKey mirrors the writer: a blank key is
// a programming error, not a cache miss.
func TestLoadByIdentity_RejectsEmptyKey(t *testing.T) {
	s := NewTreeStore(t.TempDir())
	if snap, err := s.LoadByIdentity("", "RRS18@1601"); err == nil || snap != nil || !strings.Contains(err.Error(), "empty proto") {
		t.Fatalf("empty proto: (%v, %v)", snap, err)
	}
	if snap, err := s.LoadByIdentity("acp1", ""); err == nil || snap != nil || !strings.Contains(err.Error(), "empty identity") {
		t.Fatalf("empty identity: (%v, %v)", snap, err)
	}
}

// TestLoadByIdentity_ShapeErrors pins the reader's content-based shape
// detection: unparsable bytes, a DM envelope whose payload is the
// wrong type, and a legacy Snapshot envelope with a wrong-typed
// field are each a decode error that names the file — never a
// silent cache miss — and a file the OS lets us open but not read
// (a directory at the DM path) is a read error.
func TestLoadByIdentity_ShapeErrors(t *testing.T) {
	cases := []struct {
		name, content, wantErr string
	}{
		{"not JSON", `{"model": "RRS18",`, "storage: decode "},
		{"DM shape with wrong-typed objects", `{"model":"RRS18","objects":"nope"}`, "storage: decode dm "},
		{"legacy shape with wrong-typed slots", `{"device":{},"slots":"nope"}`, "storage: decode legacy "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewTreeStore(t.TempDir())
			writeRaw(t, s.IdentityPath("acp1", "RRS18@1601"), tc.content)
			snap, err := s.LoadByIdentity("acp1", "RRS18@1601")
			if err == nil || snap != nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("LoadByIdentity = (%v, %v), want error containing %q", snap, err, tc.wantErr)
			}
		})
	}
	t.Run("directory at the DM path is a read error", func(t *testing.T) {
		s := NewTreeStore(t.TempDir())
		if err := os.MkdirAll(s.IdentityPath("acp1", "RRS18@1601"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		snap, err := s.LoadByIdentity("acp1", "RRS18@1601")
		if err == nil || snap != nil || !strings.Contains(err.Error(), "storage: read ") {
			t.Fatalf("LoadByIdentity = (%v, %v), want read error", snap, err)
		}
	})
}

// TestLoadByIdentity_OpenFailureIsNotAMiss: only ENOENT means "no
// cache"; any other open failure on either the per-proto path or the
// legacy flat path surfaces as an error so a permission problem is
// never mistaken for a cold cache and silently re-walked.
func TestLoadByIdentity_OpenFailureIsNotAMiss(t *testing.T) {
	s := NewTreeStore(t.TempDir())
	newPath := s.IdentityPath("acp1", "RRS18@1601")
	legacyPath := s.legacyIdentityPath("RRS18@1601")
	denied := func(p string) *os.PathError { return &os.PathError{Op: "open", Path: p, Err: os.ErrPermission} }

	t.Run("per-proto path unreadable", func(t *testing.T) {
		swapOpenFile(t, func(p string) (*os.File, error) { return nil, denied(p) })
		snap, err := s.LoadByIdentity("acp1", "RRS18@1601")
		if snap != nil || !errors.Is(err, os.ErrPermission) || !strings.Contains(err.Error(), newPath) {
			t.Fatalf("LoadByIdentity = (%v, %v), want permission error on %s", snap, err, newPath)
		}
	})
	t.Run("legacy path unreadable", func(t *testing.T) {
		swapOpenFile(t, func(p string) (*os.File, error) {
			if p == legacyPath {
				return nil, denied(p)
			}
			return os.Open(p)
		})
		snap, err := s.LoadByIdentity("acp1", "RRS18@1601")
		if snap != nil || !errors.Is(err, os.ErrPermission) || !strings.Contains(err.Error(), legacyPath) {
			t.Fatalf("LoadByIdentity = (%v, %v), want permission error on %s", snap, err, legacyPath)
		}
	})
	t.Run("neither path present is a miss", func(t *testing.T) {
		snap, err := s.LoadByIdentity("acp1", "RRS18@1601")
		if snap != nil || err != nil {
			t.Fatalf("LoadByIdentity = (%v, %v), want (nil, nil)", snap, err)
		}
	})
}

// TestLoad_Errors pins the slot cache reader: a non-ENOENT open failure
// and unparsable content are errors, a missing file is (nil, nil).
func TestLoad_Errors(t *testing.T) {
	t.Run("corrupt slot file", func(t *testing.T) {
		s := NewTreeStore(t.TempDir())
		writeRaw(t, s.slotPath("10.0.0.1", 0), "{not json")
		snap, err := s.Load("10.0.0.1", 0)
		if err == nil || snap != nil || !strings.Contains(err.Error(), "storage: decode ") {
			t.Fatalf("Load = (%v, %v), want decode error", snap, err)
		}
	})
	t.Run("unreadable slot file", func(t *testing.T) {
		s := NewTreeStore(t.TempDir())
		swapOpenFile(t, func(p string) (*os.File, error) {
			return nil, &os.PathError{Op: "open", Path: p, Err: os.ErrPermission}
		})
		snap, err := s.Load("10.0.0.1", 0)
		if snap != nil || !errors.Is(err, os.ErrPermission) {
			t.Fatalf("Load = (%v, %v), want permission error", snap, err)
		}
	})
}

// TestDelete pins Delete's idempotence (a missing file is success) and
// that a slot path the OS refuses to remove — here a non-empty
// directory squatting on it — is reported, not swallowed.
func TestDelete(t *testing.T) {
	s := NewTreeStore(t.TempDir())
	if err := s.Delete("10.0.0.1", 0); err != nil {
		t.Fatalf("Delete(missing) = %v, want nil", err)
	}
	squat := s.slotPath("10.0.0.1", 0)
	writeRaw(t, filepath.Join(squat, "inner"), "x")
	err := s.Delete("10.0.0.1", 0)
	if err == nil || !strings.Contains(err.Error(), "storage: remove ") {
		t.Fatalf("Delete(non-empty dir) = %v, want remove error", err)
	}
}

// TestFindCardName_NoMatch: only a string-valued "Card Name" object
// counts; a label match with a non-string value or no match at all
// answers "".
func TestFindCardName_NoMatch(t *testing.T) {
	cases := []struct {
		name string
		objs []consumer.Object
	}{
		{"no objects", nil},
		{"label present but not a string value", []consumer.Object{{Label: "Card Name", Value: consumer.Value{Kind: consumer.KindInt, Int: 5}}}},
		{"other labels only", []consumer.Object{{Label: "Serial", Value: consumer.Value{Kind: consumer.KindString, Str: "1"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FindCardName(tc.objs); got != "" {
				t.Fatalf("FindCardName = %q, want empty", got)
			}
		})
	}
}

// TestValidate_Table pins the cache-validity rule: a cache is trusted
// only when both it and the live device name the same card; every
// missing piece of evidence votes "invalid", never "probably fine".
func TestValidate_Table(t *testing.T) {
	withCard := func(name string) *export.Snapshot {
		objs := []consumer.Object{{Label: "Card Name", Value: consumer.Value{Kind: consumer.KindString, Str: name}}}
		if name == "" {
			objs = nil
		}
		return &export.Snapshot{Slots: []export.SlotDump{{Objects: objs}}}
	}
	cases := []struct {
		name string
		snap *export.Snapshot
		live string
		want bool
	}{
		{"nil snapshot", nil, "SHPRM1", false},
		{"snapshot without slots", &export.Snapshot{}, "SHPRM1", false},
		{"cache without a card name", withCard(""), "SHPRM1", false},
		{"live device without a card name", withCard("SHPRM1"), "", false},
		{"card swapped", withCard("SHPRM1"), "RRS18", false},
		{"same card", withCard("SHPRM1"), "SHPRM1", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Validate(tc.snap, tc.live); got != tc.want {
				t.Fatalf("Validate = %v, want %v", got, tc.want)
			}
		})
	}
}
