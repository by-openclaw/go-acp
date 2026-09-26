package codec

import (
	"sort"
	"strings"
)

// The device's own OpenAPI document, read for the one thing a control
// system needs from it: which resources accept a write.
//
// A recursive walk of the tree learns the SHAPE of a device — what
// lists what. It cannot learn what may be written, because nothing in
// a GET response says so, and it cannot even learn every readable
// path: on BRIDGE 7.0.3 `/misc` does not list `luts`, and `/io/sdi`
// answers with an array of objects, so a walk stops there and never
// reaches the per-UUID resources underneath. The spec declares all of
// it — 75 GETs and 36 PUTs where a walk finds 41 resources.
//
// Only the path/method skeleton is read here, by structure rather than
// by a YAML parser: this document is emitted by the device's own
// generator with fixed two-space indentation, the two token shapes
// below are all that is needed, and a dependency for the rest would
// enter the build for nothing (ADR-0005). ParseSpec is deliberately
// strict about finding SOMETHING — a document it cannot read at all is
// an error, not an empty answer that would quietly mark every object
// read-only.

// Method is an HTTP method the spec declares on a path.
type Method string

// The methods this API uses. There is no PATCH, POST or DELETE
// anywhere in BRIDGE 7.0.3: every write is a whole-resource PUT, which
// is why writing one field is a read-modify-write.
const (
	GET Method = "get"
	PUT Method = "put"
)

// Spec is the path→methods skeleton of the device's OpenAPI document.
type Spec struct {
	// ops maps an API-relative path ("/io/sdi/{uuid}") to its methods.
	ops map[string]map[Method]bool
}

// ParseSpec reads api.yml's `paths:` section.
func ParseSpec(doc []byte) (*Spec, error) {
	s := &Spec{ops: map[string]map[Method]bool{}}

	var (
		inPaths bool
		path    string
	)
	for _, raw := range strings.Split(string(doc), "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		// A top-level key ends the paths section.
		if !strings.HasPrefix(line, " ") {
			inPaths = strings.HasPrefix(line, "paths:")
			path = ""
			continue
		}
		if !inPaths {
			continue
		}
		switch {
		case strings.HasPrefix(line, "  /"):
			// "  /v1/io/sdi/{uuid}:" — a path item.
			path = strings.TrimSuffix(strings.TrimSpace(line), ":")
			path = strings.Trim(path, `'"`)
			if _, seen := s.ops[path]; !seen {
				s.ops[path] = map[Method]bool{}
			}
		case path != "" && strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "     "):
			// "    get:" — an operation on the current path. Deeper
			// indentation is that operation's own body.
			key := strings.TrimSuffix(strings.TrimSpace(line), ":")
			switch Method(strings.ToLower(key)) {
			case GET:
				s.ops[path][GET] = true
			case PUT:
				s.ops[path][PUT] = true
			}
		}
	}

	if len(s.ops) == 0 {
		return nil, errSpec("no paths: section, or no paths in it")
	}
	return s, nil
}

// errSpec keeps the package's errors uniform without importing fmt for
// one call site.
type errSpec string

func (e errSpec) Error() string { return "ccm: api.yml: " + string(e) }

// Writable reports whether the spec declares PUT on this exact path.
//
// The path is matched with the API version prefix stripped and
// parameters kept as the spec writes them, so a caller asks about
// "/io/sdi/{uuid}" rather than about one device's UUID. TemplateFor
// turns a concrete path into that form.
func (s *Spec) Writable(path string) bool {
	return s.has(path, PUT)
}

// Readable reports whether the spec declares GET on this exact path.
func (s *Spec) Readable(path string) bool {
	return s.has(path, GET)
}

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
