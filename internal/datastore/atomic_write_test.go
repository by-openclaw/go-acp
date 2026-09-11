package datastore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/consumer"
)

// swapCloseFile installs a Close stand-in that really closes the file
// (so Windows lets the writer unlink its .tmp) and then reports the
// injected failure — the only way to reach the close-error branch on
// both OSes.
func swapCloseFile(t *testing.T, err error) {
	t.Helper()
	orig := closeFile
	closeFile = func(f *os.File) error {
		_ = f.Close()
		return err
	}
	t.Cleanup(func() { closeFile = orig })
}

// unencodable is an object whose Meta cannot be JSON-encoded; every
// writer merges Meta into the payload, so it drives the encode-failure
// branch honestly on any OS.
var unencodable = []consumer.Object{{
	ID: 1, Label: "Gain", Path: []string{"control"}, Kind: consumer.KindInt,
	Meta: map[string]any{"handle": make(chan int)},
}}

// TestAtomicWriters_FailureLeavesNoTmp is the atomic-write contract for
// all three writers (Save, SaveByIdentity, WriteDM): whichever step
// fails — creating the directory, creating the .tmp, encoding,
// closing, or the final rename — the error names the step, the final
// path is never half-written, and no .tmp file is left behind.
func TestAtomicWriters_FailureLeavesNoTmp(t *testing.T) {
	plain := []consumer.Object{{ID: 1, Label: "Gain", Path: []string{"control"}, Kind: consumer.KindInt}}
	closeErr := errors.New("close: I/O error")

	writers := []struct {
		name  string
		dst   func(s *TreeStore) string
		write func(s *TreeStore, objs []consumer.Object) error
	}{
		{
			"Save",
			func(s *TreeStore) string { return s.slotPath("10.0.0.1", 0) },
			func(s *TreeStore, objs []consumer.Object) error { return s.Save("10.0.0.1", "acp1", 0, objs) },
		},
		{
			"SaveByIdentity",
			func(s *TreeStore) string { return s.IdentityPath("acp1", "RRS18@1601") },
			func(s *TreeStore, objs []consumer.Object) error { return s.SaveByIdentity("acp1", "RRS18@1601", objs) },
		},
		{
			"WriteDM",
			func(s *TreeStore) string { return s.IdentityPath("acp1", "RRS18@1601") },
			func(s *TreeStore, objs []consumer.Object) error {
				return s.WriteDM("acp1", "RRS18@1601", DM{Objects: objs})
			},
		},
	}
	failures := []struct {
		name    string
		arrange func(t *testing.T, dst string)
		objs    []consumer.Object
		wantErr string
	}{
		{
			// A regular file where the parent directory must go.
			"mkdir", func(t *testing.T, dst string) { plantFile(t, filepath.Dir(filepath.Dir(dst))) }, plain, ": mkdir ",
		},
		{
			// A directory squatting on the .tmp name.
			"create", func(t *testing.T, dst string) { plantDir(t, dst+".tmp") }, plain, ": create ",
		},
		{
			"encode", func(*testing.T, string) {}, unencodable, ": ",
		},
		{
			"close", func(t *testing.T, _ string) { swapCloseFile(t, closeErr) }, plain, ": close: ",
		},
		{
			// A directory squatting on the final name.
			"rename", func(t *testing.T, dst string) { plantDir(t, dst) }, plain, ": rename: ",
		},
	}
	for _, w := range writers {
		for _, f := range failures {
			t.Run(w.name+"/"+f.name, func(t *testing.T) {
				s := NewTreeStore(t.TempDir())
				dst := w.dst(s)
				f.arrange(t, dst)
				err := w.write(s, f.objs)
				if err == nil {
					t.Fatal("write succeeded, want failure")
				}
				if !strings.HasPrefix(err.Error(), "storage: ") || !strings.Contains(err.Error(), f.wantErr) {
					t.Fatalf("err = %q, want storage-prefixed error containing %q", err, f.wantErr)
				}
				if f.name == "close" && !errors.Is(err, closeErr) {
					t.Fatalf("close error not wrapped: %v", err)
				}
				var ute *json.UnsupportedTypeError
				if f.name == "encode" && !errors.As(err, &ute) {
					t.Fatalf("encode error does not carry the JSON cause: %v", err)
				}
				if st, serr := os.Stat(dst + ".tmp"); serr == nil && !st.IsDir() {
					t.Fatalf("leftover .tmp after %s failure", f.name)
				}
				if st, serr := os.Stat(dst); serr == nil && !st.IsDir() {
					t.Fatalf("final file exists after %s failure", f.name)
				}
			})
		}
	}
}

func plantFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("squatter"), 0o644); err != nil {
		t.Fatalf("plant file %s: %v", path, err)
	}
}

func plantDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("plant dir %s: %v", path, err)
	}
}
