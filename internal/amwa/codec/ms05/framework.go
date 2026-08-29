package ms05

// The MS-05-02 framework ships machine-readable descriptor INSTANCES
// for every standard control class and datatype — the same files the
// spec publishes under models/. They are bundled verbatim under
// testdata/schemas/v1.0.0/ and embedded here so servers that publish
// the device model (IS-12's ClassManager, IS-14's /descriptor and
// ClassManager role path) answer from the spec's own data instead of
// hand-typed copies.

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

//go:embed testdata/schemas/v1.0.0/classes/*.json testdata/schemas/v1.0.0/datatypes/*.json
var frameworkFS embed.FS

var (
	frameworkOnce sync.Once
	classesByKey  map[string]NcClassDescriptor
	datatypesByNm map[string]NcDatatypeDescriptor
	frameworkErr  error
)

// classKey renders a class id as its dotted string form ("1.3.1").
func classKey(id NcClassId) string {
	parts := make([]string, len(id))
	for i, v := range id {
		parts[i] = strconv.Itoa(int(v))
	}
	return strings.Join(parts, ".")
}

// primitiveNames are the MS-05-02 primitive datatypes. The framework
// models only ship files for structs / enums / typedefs, but the
// ClassManager's datatype catalogue includes the primitives too — a
// controller resolving a property's typeName must find NcBoolean.
var primitiveNames = []string{
	"NcBoolean", "NcInt16", "NcInt32", "NcInt64",
	"NcUint16", "NcUint32", "NcUint64",
	"NcFloat32", "NcFloat64", "NcString",
}

func loadFramework() {
	classesByKey = map[string]NcClassDescriptor{}
	datatypesByNm = map[string]NcDatatypeDescriptor{}

	load := func(dir string, into func(name string, raw []byte) error) error {
		entries, err := frameworkFS.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("ms05: framework embed %s: %w", dir, err)
		}
		for _, e := range entries {
			raw, err := frameworkFS.ReadFile(dir + "/" + e.Name())
			if err != nil {
				return fmt.Errorf("ms05: framework read %s: %w", e.Name(), err)
			}
			if err := into(e.Name(), raw); err != nil {
				return err
			}
		}
		return nil
	}

	if err := load("testdata/schemas/v1.0.0/classes", func(name string, raw []byte) error {
		var d NcClassDescriptor
		if err := json.Unmarshal(raw, &d); err != nil {
			return fmt.Errorf("ms05: framework class %s: %w", name, err)
		}
		classesByKey[classKey(d.ClassID)] = d
		return nil
	}); err != nil {
		frameworkErr = err
		return
	}

	if err := load("testdata/schemas/v1.0.0/datatypes", func(name string, raw []byte) error {
		var d NcDatatypeDescriptor
		if err := json.Unmarshal(raw, &d); err != nil {
			return fmt.Errorf("ms05: framework datatype %s: %w", name, err)
		}
		datatypesByNm[d.Name] = d
		return nil
	}); err != nil {
		frameworkErr = err
		return
	}

	for _, n := range primitiveNames {
		desc := n + " primitive"
		datatypesByNm[n] = NcDatatypeDescriptor{
			NcDescriptor: NcDescriptor{Description: &desc},
			Name:         n,
			Type:         NcDatatypeTypePrimitive,
		}
	}
}

func ensureFramework() error {
	frameworkOnce.Do(loadFramework)
	return frameworkErr
}

// StandardClass returns the framework descriptor for one class id
// with its OWN elements only (as the spec models publish them).
func StandardClass(id NcClassId) (NcClassDescriptor, bool) {
	if ensureFramework() != nil {
		return NcClassDescriptor{}, false
	}
	d, ok := classesByKey[classKey(id)]
	return d, ok
}

// FlattenedClass returns the descriptor for one class id INCLUDING
// all inherited elements, own elements first and then each ancestor
// up to NcObject — the order the IS-14 class-descriptor example
// publishes ([2p1 2p2 1p1 … 1p8] for NcBlock).
func FlattenedClass(id NcClassId) (NcClassDescriptor, bool) {
	if ensureFramework() != nil {
		return NcClassDescriptor{}, false
	}
	own, ok := classesByKey[classKey(id)]
	if !ok {
		return NcClassDescriptor{}, false
	}
	out := own
	out.Properties = append([]NcPropertyDescriptor(nil), own.Properties...)
	out.Methods = append([]NcMethodDescriptor(nil), own.Methods...)
	out.Events = append([]NcEventDescriptor(nil), own.Events...)
	for cut := len(id) - 1; cut >= 1; cut-- {
		parent, ok := classesByKey[classKey(id[:cut])]
		if !ok {
			continue // abstract gap — the models ship every level, but stay safe
		}
		out.Properties = append(out.Properties, parent.Properties...)
		out.Methods = append(out.Methods, parent.Methods...)
		out.Events = append(out.Events, parent.Events...)
	}
	return out, true
}

// StandardDatatype returns one framework datatype descriptor by name
// (embedded models plus the ten primitives).
func StandardDatatype(name string) (NcDatatypeDescriptor, bool) {
	if ensureFramework() != nil {
		return NcDatatypeDescriptor{}, false
	}
	d, ok := datatypesByNm[name]
	return d, ok
}

// StandardClasses lists every framework class descriptor, sorted by
// dotted class id.
func StandardClasses() []NcClassDescriptor {
	if ensureFramework() != nil {
		return nil
	}
	keys := make([]string, 0, len(classesByKey))
	for k := range classesByKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]NcClassDescriptor, 0, len(keys))
	for _, k := range keys {
		out = append(out, classesByKey[k])
	}
	return out
}

// StandardDatatypes lists every framework datatype descriptor
// (embedded models + primitives), sorted by name.
func StandardDatatypes() []NcDatatypeDescriptor {
	if ensureFramework() != nil {
		return nil
	}
	names := make([]string, 0, len(datatypesByNm))
	for n := range datatypesByNm {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]NcDatatypeDescriptor, 0, len(names))
	for _, n := range names {
		out = append(out, datatypesByNm[n])
	}
	return out
}
