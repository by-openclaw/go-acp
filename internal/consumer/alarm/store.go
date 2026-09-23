package alarm

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Templates live beside the DM they describe (ADR-0020 bucket 4):
//
//	.cache/alarm/<proto>/<Model@SwRev>.json   this card type
//	.cache/alarm/<proto>/_default.json        every card of this protocol
//
// A plant with no rules yet has neither file, and that is not an
// error — it is a plant whose thresholds nobody has written down.

// DefaultName is the protocol-wide fallback template's file name.
const DefaultName = "_default"

// CatchAll is the row every template ends with: the objects no rule
// names. They are tracked and shown as info, never alarmed.
const CatchAll = "**"

// Everything is the template a device gets when a plant has written
// none: every object its model defines is in the view, carrying its
// value, with no severity. A monitoring system that shows only what
// someone remembered to configure is how an unwatched object goes
// unnoticed for a year.
func Everything() *Template {
	return &Template{
		Model: DefaultName,
		Rows: []Row{{
			Match: CatchAll,
			Kind:  KindInfo,
			Text:  "no rule — reported, never alarmed",
			Source: "every object the device's model defines; " +
				"a plant that writes rules replaces this row's scope, it does not remove it",
		}},
	}
}

// Dir is where one protocol's templates live under a cache root.
func Dir(cacheRoot, proto string) string {
	return filepath.Join(cacheRoot, "alarm", sanitizeSeg(proto))
}

// Path is the file one template lives in. name is a DM identity
// ("FusioN6@0x68cd783f") or DefaultName.
func Path(cacheRoot, proto, name string) string {
	return filepath.Join(Dir(cacheRoot, proto), sanitizeSeg(name)+".json")
}

// Resolve loads the template that governs one device: its own model
// first, then the protocol default. It returns the template and the
// file it came from, or (nil, "", nil) when neither exists.
func Resolve(cacheRoot, proto, identity string) (*Template, string, error) {
	names := []string{DefaultName}
	if identity != "" {
		names = []string{identity, DefaultName}
	}
	for _, n := range names {
		p := Path(cacheRoot, proto, n)
		t, err := LoadFile(p)
		switch {
		case err == nil:
			return t, p, nil
		case errors.Is(err, fs.ErrNotExist):
			continue
		default:
			return nil, p, err
		}
	}
	return nil, "", nil
}

// LoadFile reads and validates one template file. A missing file is
// reported as fs.ErrNotExist so a caller can tell "no rules" from "bad
// rules".
func LoadFile(path string) (*Template, error) {
	data, err := os.ReadFile(path) //nolint:gosec // operator-supplied path, by design
	if err != nil {
		return nil, err
	}
	t, err := Load(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return t, nil
}

// Save writes a template, creating the directory if needed, and
// reports whether the file changed. Writing the same rules twice is a
// no-op — an import is idempotent, like every other converge step in
// this repo. The write is atomic (tmp + rename).
func Save(path string, t *Template) (bool, error) {
	var buf bytes.Buffer
	// A bytes.Buffer never fails to accept bytes, and a Template always
	// marshals; Encode's error belongs to the caller's writer, not here.
	_ = t.Encode(&buf)
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, buf.Bytes()) { //nolint:gosec // same path we are about to write
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("alarm: create %s: %w", filepath.Dir(path), err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil { //nolint:gosec // a template is not a secret
		return false, fmt.Errorf("alarm: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return false, fmt.Errorf("alarm: install %s: %w", path, err)
	}
	return true, nil
}

// sanitizeSeg makes one path segment safe on every OS, the same way
// the DM cache does — an identity may carry "/", ":" or "*".
func sanitizeSeg(s string) string {
	for _, ch := range []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"} {
		s = strings.ReplaceAll(s, ch, "_")
	}
	return s
}
