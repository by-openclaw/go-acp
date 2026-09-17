package ms05

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

const (
	classDirV1  = "testdata/schemas/v1.0.0/classes"
	classDirCfg = "testdata/schemas/v1.0.0/featuresets/device-configuration/classes"
	classDirMon = "testdata/schemas/v1.0.0/featuresets/monitoring/classes"
	typeDirV1   = "testdata/schemas/v1.0.0/datatypes"
	typeDirCfg  = "testdata/schemas/v1.0.0/featuresets/device-configuration/datatypes"
	typeDirMon  = "testdata/schemas/v1.0.0/featuresets/monitoring/datatypes"
)

// loadFrom runs the loader over a substitute file system and restores
// the real catalogue afterwards, so a failure injected here cannot
// leak into another test's view of the framework.
func loadFrom(t *testing.T, fsys fs.FS) error {
	t.Helper()
	prev := frameworkFS
	t.Cleanup(func() {
		frameworkFS = prev
		loadFramework()
		if frameworkErr != nil {
			t.Fatalf("restoring the embedded framework failed: %v", frameworkErr)
		}
	})
	frameworkFS = fsys
	loadFramework()
	return frameworkErr
}

// okClass and okType are the smallest descriptor files the loader
// accepts, so a test can make every directory but the one under
// examination load cleanly.
func okClass() *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(`{"classId":[1],"name":"NcObject"}`)}
}

func okType() *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(`{"name":"NcBoolean","type":0}`)}
}

// wholeFramework is a substitute that loads without complaint; each
// test below breaks exactly one thing in it.
func wholeFramework() fstest.MapFS {
	return fstest.MapFS{
		classDirV1 + "/nc-object.json":   okClass(),
		classDirCfg + "/nc-bulk.json":    okClass(),
		classDirMon + "/nc-monitor.json": okClass(),
		typeDirV1 + "/nc-boolean.json":   okType(),
		typeDirCfg + "/nc-bulk.json":     okType(),
		typeDirMon + "/nc-status.json":   okType(),
	}
}

// A catalogue the loader cannot read is an error the caller sees, not
// a device that answers /descriptor with whatever loaded before the
// failure. ensureFramework's callers all return empty on error, so a
// half-built catalogue would look to a controller exactly like a
// device that legitimately has no such class.
func TestLoadFrameworkReportsWhatItCannotRead(t *testing.T) {
	for _, tc := range []struct {
		name   string
		break_ func(fstest.MapFS)
		want   string
	}{
		{"a classes directory that is not there", func(m fstest.MapFS) {
			delete(m, classDirMon+"/nc-monitor.json")
		}, "framework embed " + classDirMon},

		{"a datatypes directory that is not there", func(m fstest.MapFS) {
			delete(m, typeDirMon+"/nc-status.json")
		}, "framework embed " + typeDirMon},

		{"an entry that is a directory, not a file", func(m fstest.MapFS) {
			m[classDirV1+"/nested/deeper.json"] = okClass()
		}, "framework read nested"},

		{"a class file that is not a descriptor", func(m fstest.MapFS) {
			m[classDirV1+"/broken.json"] = &fstest.MapFile{Data: []byte(`{`)}
		}, "framework class broken.json"},

		{"a datatype file that is not a descriptor", func(m fstest.MapFS) {
			m[typeDirV1+"/broken.json"] = &fstest.MapFile{Data: []byte(`{`)}
		}, "framework datatype broken.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := wholeFramework()
			tc.break_(m)
			err := loadFrom(t, m)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("= %v, want %q reported", err, tc.want)
			}
		})
	}
}

// The substitute itself must load, or every case above would pass for
// the wrong reason.
func TestTheSubstituteFrameworkLoads(t *testing.T) {
	if err := loadFrom(t, wholeFramework()); err != nil {
		t.Fatalf("the unbroken substitute must load: %v", err)
	}
}

// A struct whose declared parent is not in the catalogue flattens to
// the fields it has. The alternative — following a nil parent — is a
// panic on a device model a peer authored, and the fields we do know
// are still the truthful answer.
func TestFlattenedDatatypeStopsAtAParentThatIsNotThere(t *testing.T) {
	if err := ensureFramework(); err != nil {
		t.Fatal(err)
	}
	missing := "NcNotInTheCatalogue"
	frameworkMu.Lock()
	datatypesByNm["NcOrphan"] = NcDatatypeDescriptor{
		Name:       "NcOrphan",
		Type:       NcDatatypeTypeStruct,
		ParentType: &missing,
		Fields:     []NcFieldDescriptor{{Name: "own"}},
	}
	frameworkMu.Unlock()
	t.Cleanup(func() {
		frameworkMu.Lock()
		delete(datatypesByNm, "NcOrphan")
		frameworkMu.Unlock()
	})

	got, ok := FlattenedDatatype("NcOrphan")
	if !ok {
		t.Fatal("the datatype itself is present and must be returned")
	}
	if len(got.Fields) != 1 || got.Fields[0].Name != "own" {
		t.Fatalf("fields = %+v, want only the type's own field", got.Fields)
	}
}
