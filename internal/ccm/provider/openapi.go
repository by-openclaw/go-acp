package ccm

import (
	"bufio"
	"bytes"
	"strings"
)

// apiPath is one row of the landing's API table: a path and the HTTP
// methods the OpenAPI document declares on it.
type apiPath struct {
	Path    string
	Methods []string
}

// httpVerbs are the OpenAPI operation keys, in the order the table lists them.
var httpVerbs = []string{"get", "put", "post", "patch", "delete", "head", "options"}

// specPaths extracts the path -> methods table from an OpenAPI YAML document
// with a line scanner rather than a YAML parser. Every OpenAPI generator, and
// the document the device serves, writes the `paths:` block the same way: a
// path key indented two spaces and starting with "/", its operations indented
// four spaces beneath it. Reading that shape needs no dependency, and a
// landing-page table is not the place to take one. Documents that do not
// follow it simply yield fewer rows — the raw api.yml is always linked
// alongside, so nothing is hidden.
func specPaths(spec []byte) []apiPath {
	var out []apiPath
	inPaths := false
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
		case indent == 4 && len(out) > 0:
			key := strings.ToLower(strings.TrimSuffix(trimmed, ":"))
			for _, v := range httpVerbs {
				if key == v {
					cur := &out[len(out)-1]
					cur.Methods = append(cur.Methods, strings.ToUpper(v))
					break
				}
			}
		}
	}
	return out
}
