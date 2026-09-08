package ccm

import (
	"bufio"
	"bytes"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// apiOperation is one HTTP operation the OpenAPI document declares on a path:
// the success status it promises and the request-body schema it names.
type apiOperation struct {
	Method  string // upper-case verb
	Success int    // first 2xx response code declared; 0 when none
	Body    string // request body schema name (the last segment of its $ref); "" when none
}

// apiPath is one path of the OpenAPI document with its declared operations,
// in document order. Methods is the verb list the landing's API table shows.
type apiPath struct {
	Path    string
	Methods []string
	Ops     []apiOperation
}

// op returns the operation declared for method, if any.
func (p apiPath) op(method string) (apiOperation, bool) {
	for _, o := range p.Ops {
		if o.Method == method {
			return o, true
		}
	}
	return apiOperation{}, false
}

// httpVerbs are the OpenAPI operation keys, in the order the table lists them.
var httpVerbs = []string{"get", "put", "post", "patch", "delete", "head", "options"}

var statusKeyRe = regexp.MustCompile(`^'?(\d{3})'?:$`)

// specPaths extracts the path -> operations table from an OpenAPI YAML
// document with a line scanner rather than a YAML parser. Every OpenAPI
// generator, and the document the device serves, writes the `paths:` block
// the same way: a path key indented two spaces and starting with "/", its
// operations indented four spaces beneath it, `requestBody:` / `responses:`
// six deep, response codes eight deep and the body schema `$ref` under
// `requestBody`. Reading that shape needs no dependency, and neither the
// landing table nor the write contract is the place to take one. Documents
// that do not follow it yield fewer rows — the raw api.yml is always served
// alongside, so nothing is hidden, and a path the scanner cannot read
// declares no write, which is the safe failure.
func specPaths(spec []byte) []apiPath {
	var out []apiPath
	inPaths := false
	section := "" // "requestBody" | "responses" | "" within the current operation
	sc := bufio.NewScanner(bytes.NewReader(spec))
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		switch {
		case indent == 0:
			// A top-level key: entering or leaving the paths block.
			inPaths = trimmed == "paths:"
		case !inPaths:
			continue
		case indent == 2 && strings.HasPrefix(trimmed, "/") && strings.HasSuffix(trimmed, ":"):
			out = append(out, apiPath{Path: strings.TrimSuffix(trimmed, ":")})
			section = ""
		case indent == 4 && len(out) > 0:
			section = ""
			key := strings.ToLower(strings.TrimSuffix(trimmed, ":"))
			for _, v := range httpVerbs {
				if key == v {
					cur := &out[len(out)-1]
					cur.Methods = append(cur.Methods, strings.ToUpper(v))
					cur.Ops = append(cur.Ops, apiOperation{Method: strings.ToUpper(v)})
					break
				}
			}
		case indent == 6 && len(out) > 0 && len(out[len(out)-1].Ops) > 0:
			section = strings.TrimSuffix(trimmed, ":")
		case indent > 6 && len(out) > 0 && len(out[len(out)-1].Ops) > 0:
			cur := &out[len(out)-1]
			op := &cur.Ops[len(cur.Ops)-1]
			switch section {
			case "responses":
				if m := statusKeyRe.FindStringSubmatch(trimmed); m != nil && indent == 8 && op.Success == 0 {
					if code, _ := strconv.Atoi(m[1]); code >= 200 && code < 300 {
						op.Success = code
					}
				}
			case "requestBody":
				if ref, ok := strings.CutPrefix(trimmed, "$ref:"); ok && op.Body == "" {
					ref = strings.Trim(strings.TrimSpace(ref), `'"`)
					op.Body = ref[strings.LastIndex(ref, "/")+1:]
				}
			}
		}
	}
	return out
}

// contract is the write contract the OpenAPI document declares, indexed for
// request lookup: a write is allowed only where the document has the
// operation, and answers with the status it promises. Path templates
// ("{uuid}") match any single segment, as in the document.
type contract struct {
	paths []apiPath
}

// newContract indexes an OpenAPI document. A nil document yields an empty
// contract that declares nothing — replay is then GET-only.
func newContract(spec []byte) *contract {
	return &contract{paths: specPaths(spec)}
}

// writeMethods lists the non-GET verbs the document declares anywhere, in
// verb order — what the capabilities view reports as supported writes.
func (c *contract) writeMethods() []string {
	seen := map[string]bool{}
	for _, ap := range c.paths {
		for _, o := range ap.Ops {
			if o.Method != http.MethodGet {
				seen[o.Method] = true
			}
		}
	}
	out := []string{}
	for _, v := range httpVerbs {
		if seen[strings.ToUpper(v)] {
			out = append(out, strings.ToUpper(v))
		}
	}
	return out
}

// lookup finds the operation declared for method on the document path p.
func (c *contract) lookup(method, p string) (apiOperation, bool) {
	want := strings.Split(strings.Trim(p, "/"), "/")
	for _, ap := range c.paths {
		segs := strings.Split(strings.Trim(ap.Path, "/"), "/")
		if !matchSegments(segs, want) {
			continue
		}
		if op, ok := ap.op(method); ok {
			return op, true
		}
	}
	return apiOperation{}, false
}

// matchSegments compares a template path to a concrete one segment by
// segment; a "{name}" template segment matches any non-empty segment.
func matchSegments(tmpl, got []string) bool {
	if len(tmpl) != len(got) {
		return false
	}
	for i, s := range tmpl {
		if s == got[i] {
			continue
		}
		if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") && got[i] != "" {
			continue
		}
		return false
	}
	return true
}
