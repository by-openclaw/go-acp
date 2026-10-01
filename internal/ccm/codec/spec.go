package codec

import (
	"sort"
	"strconv"
	"strings"
)

// The device's own OpenAPI document, read for what a control system
// needs from it: which resources accept a write, by which method, and
// what every field IS — its type, its enumeration, its bounds, and
// whether it is read-only (CCM 0v1 §14).
//
// A recursive walk of the tree learns the SHAPE of a device — what
// lists what. It cannot learn what may be written, because nothing in
// a GET response says so, and it cannot even learn every readable
// path: on BRIDGE 7.0.3 `/misc` does not list `luts`, and `/io/sdi`
// answers with an array of objects, so a walk stops there and never
// reaches the per-UUID resources underneath. The spec declares all of
// it — 75 GETs and 36 PUTs where a walk finds 41 resources — and its
// schemas carry the types no probe could prove.
//
// The document is read by yaml_subset.go, the subset the device
// generators emit, so the build takes no YAML dependency (ADR-0005,
// ADR-0006). ParseSpec is deliberately strict about finding SOMETHING
// — a document it cannot read at all is an error, not an empty answer
// that would quietly mark every object read-only.

// Method is an HTTP method the spec declares on a path.
type Method string

// The methods CCM §10.1 defines. BRIDGE / CONVERT 7.0.3 declare GET
// and PUT only (every write is a whole-resource PUT, which is why
// writing one field is a read-modify-write); SHUFFLE 2.0.0 declares
// PATCH (mandatory per §11.2) and POST (actions) as well.
const (
	GET    Method = "get"
	PUT    Method = "put"
	PATCH  Method = "patch"
	POST   Method = "post"
	DELETE Method = "delete"
)

var methods = []Method{GET, PUT, PATCH, POST, DELETE}

// Spec is the device's OpenAPI document: the path→methods skeleton
// plus the schemas each operation reads and writes.
type Spec struct {
	// ops maps an API-relative path ("/io/sdi/{uuid}") to its methods.
	ops map[string]map[Method]bool
	// req holds the request-body schema per path and write method.
	req map[string]map[Method]any
	// resp holds the GET 200 response schema per path.
	resp map[string]any
	// schemas is components.schemas, the targets of every $ref.
	schemas map[string]any
}

// ParseSpec reads the document's `paths:` and `components.schemas`.
func ParseSpec(doc []byte) (*Spec, error) {
	root, err := parseYAML(doc)
	if err != nil {
		return nil, err
	}
	rm, _ := root.(map[string]any)
	paths, _ := rm["paths"].(map[string]any)
	s := &Spec{
		ops:  map[string]map[Method]bool{},
		req:  map[string]map[Method]any{},
		resp: map[string]any{},
	}
	for path, item := range paths {
		im, _ := item.(map[string]any)
		s.ops[path] = map[Method]bool{}
		for _, m := range methods {
			op, ok := im[string(m)].(map[string]any)
			if !ok {
				continue
			}
			s.ops[path][m] = true
			if m == GET {
				if sc := dig(op, "responses", "200", "content", "application/json", "schema"); sc != nil {
					s.resp[path] = sc
				}
				continue
			}
			if sc := dig(op, "requestBody", "content", "application/json", "schema"); sc != nil {
				if s.req[path] == nil {
					s.req[path] = map[Method]any{}
				}
				s.req[path][m] = sc
			}
		}
	}
	if comp, ok := rm["components"].(map[string]any); ok {
		s.schemas, _ = comp["schemas"].(map[string]any)
	}
	if len(s.ops) == 0 {
		return nil, errSpec("no paths: section, or no paths in it")
	}
	return s, nil
}

// dig follows nested map keys; nil when any is missing.
func dig(m map[string]any, keys ...string) any {
	var cur any = m
	for _, k := range keys {
		cm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur, ok = cm[k]
		if !ok {
			return nil
		}
	}
	return cur
}

// errSpec keeps the package's errors uniform without importing fmt for
// one call site.
type errSpec string

func (e errSpec) Error() string { return "ccm: api.yml: " + string(e) }

// Writable reports whether the spec declares a write (PUT or PATCH) on
// this exact path.
//
// The path is matched with the API version prefix stripped and
// parameters kept as the spec writes them, so a caller asks about
// "/io/sdi/{uuid}" rather than about one device's UUID. TemplateFor
// turns a concrete path into that form.
func (s *Spec) Writable(path string) bool {
	return s.has(path, PUT) || s.has(path, PATCH)
}

// WriteMethod is how a field of this path is written: PATCH when the
// device declares it (CCM §11.2: only the fields named are sent, so two
// clients cannot overwrite each other), else PUT of the whole document.
func (s *Spec) WriteMethod(path string) (Method, bool) {
	switch {
	case s.has(path, PATCH):
		return PATCH, true
	case s.has(path, PUT):
		return PUT, true
	}
	return "", false
}

// Readable reports whether the spec declares GET on this exact path.
func (s *Spec) Readable(path string) bool {
	return s.has(path, GET)
}

// Has reports whether the spec declares m on this exact path.
func (s *Spec) Has(path string, m Method) bool { return s.has(path, m) }

func (s *Spec) has(path string, m Method) bool {
	if s == nil {
		return false
	}
	if ops, ok := s.ops[path]; ok && ops[m] {
		return true
	}
	// Documents write paths with the version prefix ("/v1/io/sdi");
	// callers hold them API-relative. Try both.
	if ops, ok := s.ops["/v1"+path]; ok && ops[m] {
		return true
	}
	return false
}

// declared returns the key under which the spec holds this path.
func (s *Spec) declared(path string) (string, bool) {
	if _, ok := s.ops[path]; ok {
		return path, true
	}
	if _, ok := s.ops["/v1"+path]; ok {
		return "/v1" + path, true
	}
	return "", false
}

// Paths returns every path the spec declares, sorted, API-relative.
func (s *Spec) Paths() []string {
	if s == nil {
		return nil
	}
	out := make([]string, 0, len(s.ops))
	for p := range s.ops {
		out = append(out, strings.TrimPrefix(p, "/v1"))
	}
	sort.Strings(out)
	return out
}

// With returns every path declaring m, sorted, API-relative.
func (s *Spec) With(m Method) []string {
	if s == nil {
		return nil
	}
	var out []string
	for p, ops := range s.ops {
		if ops[m] {
			out = append(out, strings.TrimPrefix(p, "/v1"))
		}
	}
	sort.Strings(out)
	return out
}

// TemplateFor turns a concrete device path into the spec's template
// form, so "/io/sdi/8fd150f9-…" is matched against "/io/sdi/{uuid}".
//
// It works by walking the declared paths and accepting a parameter
// segment as a wildcard. The longest literal match wins, so a path
// that is BOTH declared literally and covered by a template — like
// "/processing/video/channels/{id}" beside "/processing/video/general"
// — resolves to the literal one.
func (s *Spec) TemplateFor(path string) (string, bool) {
	if s == nil {
		return "", false
	}
	want := strings.Split(strings.Trim(strings.TrimPrefix(path, "/v1"), "/"), "/")

	best, bestLiterals := "", -1
	for declared := range s.ops {
		got := strings.Split(strings.Trim(strings.TrimPrefix(declared, "/v1"), "/"), "/")
		if len(got) != len(want) {
			continue
		}
		literals := 0
		ok := true
		for i := range got {
			switch {
			case strings.HasPrefix(got[i], "{") && strings.HasSuffix(got[i], "}"):
				// A parameter matches any one segment.
			case got[i] == want[i]:
				literals++
			default:
				ok = false
			}
			if !ok {
				break
			}
		}
		if ok && literals > bestLiterals {
			best, bestLiterals = strings.TrimPrefix(declared, "/v1"), literals
		}
	}
	return best, best != ""
}

// FieldType is what the spec says about one field of a resource (CCM
// §14.3–14.5): its JSON type and format, its enumeration, its numeric
// bounds, its maximum length, and whether it is read-only.
type FieldType struct {
	Type      string // string | integer | number | boolean | array | object
	Format    string // int32 | int64 | float | double | uuid | …
	Enum      []string
	Min, Max  *float64
	MaxLength int
	// ReadOnly: the field is in the GET schema but in neither the PUT
	// nor the PATCH request schema (§14.2), or carries readOnly: true.
	ReadOnly bool
}

// FieldType describes field (segments inside the resource document,
// array indices as digits) of the resource at path (template form).
// false when the spec has no schema for it.
func (s *Spec) FieldType(path string, field []string) (FieldType, bool) {
	if s == nil {
		return FieldType{}, false
	}
	key, ok := s.declared(path)
	if !ok {
		return FieldType{}, false
	}
	// The constraints a write must respect are the WRITE schema's: the
	// BRIDGE declares misc/nmos twice, and only the PUT body carries
	// addressOverride's maxLength. The read schema fills in what the
	// write schema leaves out (the type of a read-only field).
	var ft FieldType
	found := false
	hasWrite, inWrite := false, false
	for _, m := range []Method{PUT, PATCH} {
		sc, ok := s.req[key][m]
		if !ok {
			continue
		}
		hasWrite = true
		if leaf, ok := s.walk(sc, field); ok {
			inWrite = true
			if !found {
				ft, found = s.describe(leaf), true
			}
		}
	}
	readLeaf, inRead := s.walk(s.resp[key], field)
	if inRead {
		rt := s.describe(readLeaf)
		if !found {
			ft, found = rt, true
		} else {
			if ft.Type == "" {
				ft.Type = rt.Type
			}
			if ft.Format == "" {
				ft.Format = rt.Format
			}
			if len(ft.Enum) == 0 {
				ft.Enum = rt.Enum
			}
			ft.ReadOnly = ft.ReadOnly || rt.ReadOnly
		}
	}
	if !found {
		return FieldType{}, false
	}
	if inRead && hasWrite && !inWrite {
		ft.ReadOnly = true
	}
	return ft, true
}

// walk follows field through a schema: properties by name, items by
// index. $ref and allOf are resolved at every step.
func (s *Spec) walk(schema any, field []string) (map[string]any, bool) {
	node := s.resolve(schema, 0)
	if node == nil {
		return nil, false
	}
	for _, seg := range field {
		var next any
		if _, err := strconv.Atoi(seg); err == nil {
			next = node["items"]
		} else {
			props, _ := node["properties"].(map[string]any)
			next = props[seg]
		}
		if node = s.resolve(next, 0); node == nil {
			return nil, false
		}
	}
	return node, true
}

// resolve dereferences $ref and folds allOf into one schema map.
func (s *Spec) resolve(schema any, depth int) map[string]any {
	m, ok := schema.(map[string]any)
	if !ok || depth > 32 {
		return nil
	}
	if ref, ok := m["$ref"].(string); ok {
		name := ref[strings.LastIndex(ref, "/")+1:]
		return s.resolve(s.schemas[name], depth+1)
	}
	all, ok := m["allOf"].([]any)
	if !ok {
		return m
	}
	merged := map[string]any{}
	props := map[string]any{}
	for k, v := range m {
		if k != "allOf" {
			merged[k] = v
		}
	}
	for _, part := range all {
		pm := s.resolve(part, depth+1)
		for k, v := range pm {
			if k == "properties" {
				if pp, ok := v.(map[string]any); ok {
					for pk, pv := range pp {
						props[pk] = pv
					}
				}
				continue
			}
			if _, have := merged[k]; !have {
				merged[k] = v
			}
		}
	}
	if len(props) > 0 {
		merged["properties"] = props
	}
	return merged
}

// describe reads the type keywords of one resolved schema.
func (s *Spec) describe(m map[string]any) FieldType {
	ft := FieldType{}
	ft.Type, _ = m["type"].(string)
	ft.Format, _ = m["format"].(string)
	if items, ok := m["enum"].([]any); ok {
		for _, it := range items {
			if str, ok := it.(string); ok {
				ft.Enum = append(ft.Enum, str)
			}
		}
	}
	if v, ok := m["minimum"].(string); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			ft.Min = &f
		}
	}
	if v, ok := m["maximum"].(string); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			ft.Max = &f
		}
	}
	if v, ok := m["maxLength"].(string); ok {
		ft.MaxLength, _ = strconv.Atoi(v)
	}
	if v, ok := m["readOnly"].(string); ok && v == "true" {
		ft.ReadOnly = true
	}
	return ft
}
