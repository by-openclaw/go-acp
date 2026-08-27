// Package schemas ships AMWA's normative IS-04 JSON Schemas and hands
// out one validator per wire minor.
//
// The files under v1.0.3/ v1.1.3/ v1.2.2/ v1.3.3/ are copied verbatim
// from github.com/AMWA-TV/is-04 at those tags (APIs/schemas/). They are
// not edited, ever. When AMWA publishes a new patch, the fix is to copy
// the new set in — not to adjust Go code.
//
// This mirrors sony/nmos-cpp, which embeds the same files under
// Development/nmos/is04_schemas/ and validates against them rather than
// against hand-written rules.
//
// Each minor gets its OWN [jsonschema.Compiler] over its OWN directory.
// A v1.0 validator physically cannot load a v1.3 schema, so a rule can
// never leak across versions — the failure mode that had us checking a
// v1.0 Flow for `frame_width`, a property v1.0 does not define.
package schemas

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sync"

	"dhs/internal/amwa/codec/jsonschema"
)

//go:embed v1.0.3 v1.1.3 v1.2.2 v1.3.3
var files embed.FS

// patchFor maps a wire minor to the latest patch release we ship for
// it. The wire carries "v1.0"; the schemas are published per patch.
var patchFor = map[string]string{
	"v1.0": "v1.0.3",
	"v1.1": "v1.1.3",
	"v1.2": "v1.2.2",
	"v1.3": "v1.3.3",
}

// Minors lists every wire minor with a schema set on disk, oldest
// first.
func Minors() []string { return []string{"v1.0", "v1.1", "v1.2", "v1.3"} }

// Patch returns the schema patch release backing a wire minor.
func Patch(apiVer string) (string, bool) {
	p, ok := patchFor[apiVer]
	return p, ok
}

// dirLoader serves one version's directory and nothing else.
type dirLoader struct{ dir string }

func (d dirLoader) Load(name string) ([]byte, error) {
	b, err := files.ReadFile(path.Join(d.dir, name))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", d.dir, err)
	}
	return b, nil
}

var (
	mu       sync.Mutex
	compiled = map[string]*jsonschema.Compiler{}
)

// For returns the validator for one IS-04 wire minor ("v1.0" … "v1.3").
// Compilers are cached and safe for concurrent use.
func For(apiVer string) (*jsonschema.Compiler, error) {
	patch, ok := patchFor[apiVer]
	if !ok {
		return nil, fmt.Errorf("is04/schemas: no schema set for %q", apiVer)
	}
	mu.Lock()
	defer mu.Unlock()
	if c, ok := compiled[patch]; ok {
		return c, nil
	}
	c := jsonschema.New(dirLoader{dir: patch})
	compiled[patch] = c
	return c, nil
}

// Validate checks a raw resource payload against AMWA's schema for
// that resource at that minor. kind is the resource name as it appears
// in the schema filename: node · device · source · flow · sender ·
// receiver.
func Validate(apiVer, kind string, raw []byte) error {
	c, err := For(apiVer)
	if err != nil {
		return err
	}
	return c.Validate(kind+".json", raw)
}

// Names lists the schema files shipped for a minor. Used by tests to
// prove every file parses and every $ref resolves.
func Names(apiVer string) ([]string, error) {
	patch, ok := patchFor[apiVer]
	if !ok {
		return nil, fmt.Errorf("is04/schemas: no schema set for %q", apiVer)
	}
	ents, err := fs.ReadDir(files, patch)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ents))
	for _, e := range ents {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out, nil
}
