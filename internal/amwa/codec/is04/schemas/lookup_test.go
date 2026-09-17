package schemas

import (
	"embed"
	"strings"
	"testing"
)

// testFS stands in for the embedded schema set when a test needs a
// schema file that is not valid JSON, something the real set never
// contains and must never contain.
//
//go:embed testdata
var testFS embed.FS

// TestPatchBacksEveryMinor: the wire says "v1.0"; the schema set says
// "v1.0.3". The mapping must exist for every minor we serve and for
// nothing else.
func TestPatchBacksEveryMinor(t *testing.T) {
	for _, m := range Minors() {
		p, ok := Patch(m)
		if !ok || !strings.HasPrefix(p, m+".") {
			t.Errorf("Patch(%s) = %q, %v; want a patch of that minor", m, p, ok)
		}
	}
	if p, ok := Patch("v9.9"); ok {
		t.Errorf("Patch(v9.9) = %q; nothing ships that minor", p)
	}
}

// TestUnknownMinorIsRefusedEverywhere: no entry point silently maps an
// unknown minor onto some other schema set. Each names the minor it
// could not find.
func TestUnknownMinorIsRefusedEverywhere(t *testing.T) {
	cases := map[string]func() error{
		"For": func() error { _, err := For("v9.9"); return err },
		"Validate": func() error {
			return Validate("v9.9", "node", []byte(`{}`))
		},
		"RequiredLeaves": func() error { _, err := RequiredLeaves("v9.9", "node"); return err },
		"Names":          func() error { _, err := Names("v9.9"); return err },
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			err := call()
			if err == nil || !strings.Contains(err.Error(), `"v9.9"`) {
				t.Fatalf("want an error naming v9.9, got %v", err)
			}
		})
	}
}

// TestNamesSurfacesAMissingSchemaDirectory: a patch mapping that points
// nowhere is a packaging bug and must not read as "no schemas".
func TestNamesSurfacesAMissingSchemaDirectory(t *testing.T) {
	patchFor["v9.8"] = "not-shipped"
	t.Cleanup(func() { delete(patchFor, "v9.8") })
	if _, err := Names("v9.8"); err == nil {
		t.Fatal("want an error for a schema directory that does not exist")
	}
}

// TestRequiredLeavesTreatsAMissingFileAsNotOurs: a kind (or $ref) with
// no file behind it contributes nothing and is not an error here. The
// ref-resolution test owns that failure; this walk only collects.
func TestRequiredLeavesTreatsAMissingFileAsNotOurs(t *testing.T) {
	req, err := RequiredLeaves("v1.3", "hologram")
	if err != nil {
		t.Fatalf("a missing schema is not the walk's error to raise: %v", err)
	}
	if len(req) != 0 {
		t.Fatalf("nothing can be required by a file that does not exist, got %v", req)
	}
}

// TestRequiredLeavesRefusesACorruptSchema: a schema file that does not
// parse is reported with its path, whether it is the kind's own file or
// one it reaches through a $ref. A silent skip here would make the
// drop-table guard pass on a schema it never read.
func TestRequiredLeavesRefusesACorruptSchema(t *testing.T) {
	old := files
	files = testFS
	patchFor["v9.7"] = "testdata/corrupt"
	t.Cleanup(func() {
		files = old
		delete(patchFor, "v9.7")
	})

	for _, kind := range []string{"corrupt", "node"} {
		t.Run(kind, func(t *testing.T) {
			_, err := RequiredLeaves("v9.7", kind)
			if err == nil || !strings.Contains(err.Error(), "testdata/corrupt/corrupt.json") {
				t.Fatalf("want the corrupt file named, got %v", err)
			}
		})
	}
}
