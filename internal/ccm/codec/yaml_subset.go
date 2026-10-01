package codec

import (
	"fmt"
	"strconv"
	"strings"
)

// A reader for the YAML the device generators emit (BRIDGE / CONVERT
// 7.0.3 "api.yml", SHUFFLE 2.0.0 "openapi.yml"): block mappings and
// sequences by indentation, scalars bare or quoted, the empty flow
// sequence `[]` and a flow sequence of scalars `[a, b]`. Measured on
// both documents: no anchors, no block scalars (| or >), no tabs, no
// multi-document streams — so none of that is read, and a document
// using it is an error rather than a guess. Stdlib only (ADR-0006).
//
// Every scalar stays a string ("5000", "true"); the caller turns it
// into a number where it means one. That keeps `enum: [Off, On]` as
// the words the device compares against — a YAML 1.1 reader would make
// Off a boolean.

type yamlLine struct {
	n      int // 1-based line number, for errors
	indent int
	text   string
}

func yamlLines(doc []byte) []yamlLine {
	var out []yamlLine
	for i, raw := range strings.Split(string(doc), "\n") {
		line := strings.TrimRight(raw, "\r")
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") || trim == "---" {
			continue
		}
		out = append(out, yamlLine{n: i + 1, indent: len(line) - len(strings.TrimLeft(line, " ")), text: trim})
	}
	return out
}

type yamlParser struct {
	ls []yamlLine
	i  int
}

// parseYAML reads the whole document into map[string]any / []any /
// string / nil.
func parseYAML(doc []byte) (any, error) {
	if strings.Contains(string(doc), "\t") {
		return nil, errSpec("tab in the document: not the YAML the device generators emit")
	}
	p := &yamlParser{ls: yamlLines(doc)}
	if len(p.ls) == 0 {
		return nil, errSpec("empty document")
	}
	v, err := p.block(p.ls[0].indent)
	if err != nil {
		return nil, err
	}
	if p.i < len(p.ls) {
		return nil, errSpec(fmt.Sprintf("line %d: unexpected %q", p.ls[p.i].n, p.ls[p.i].text))
	}
	return v, nil
}

func isDash(text string) bool { return text == "-" || strings.HasPrefix(text, "- ") }

// block reads the node that starts at the current line: a sequence
// when the line is a dash item, a mapping otherwise.
// Every caller has checked that a line is there.
func (p *yamlParser) block(indent int) (any, error) {
	if isDash(p.ls[p.i].text) {
		return p.seq(indent)
	}
	return p.mapping(indent)
}

func (p *yamlParser) mapping(indent int) (map[string]any, error) {
	m := map[string]any{}
	for p.i < len(p.ls) {
		l := p.ls[p.i]
		if l.indent < indent || (l.indent == indent && isDash(l.text)) {
			break
		}
		if l.indent > indent {
			return nil, errSpec(fmt.Sprintf("line %d: indented past its mapping: %q", l.n, l.text))
		}
		key, val, ok := splitKV(l.text)
		if !ok {
			return nil, errSpec(fmt.Sprintf("line %d: not a key: %q", l.n, l.text))
		}
		p.i++
		if val != "" {
			m[key] = scalar(val)
			continue
		}
		// `key:` with the value on the following lines: a deeper block,
		// or a sequence whose dashes sit at the key's own indent.
		if p.i < len(p.ls) {
			next := p.ls[p.i]
			switch {
			case next.indent > indent:
				v, err := p.block(next.indent)
				if err != nil {
					return nil, err
				}
				m[key] = v
				continue
			case next.indent == indent && isDash(next.text):
				v, err := p.seq(indent)
				if err != nil {
					return nil, err
				}
				m[key] = v
				continue
			}
		}
		m[key] = nil
	}
	return m, nil
}

func (p *yamlParser) seq(indent int) ([]any, error) {
	out := []any{}
	for p.i < len(p.ls) {
		l := p.ls[p.i]
		if l.indent != indent || !isDash(l.text) {
			break
		}
		rest := strings.TrimSpace(strings.TrimPrefix(l.text, "-"))
		switch {
		case rest == "":
			p.i++
			if p.i < len(p.ls) && p.ls[p.i].indent > indent {
				v, err := p.block(p.ls[p.i].indent)
				if err != nil {
					return nil, err
				}
				out = append(out, v)
			} else {
				out = append(out, nil)
			}
		case isKV(rest):
			// "- key: value" opens a mapping whose first entry sits on
			// the dash line and whose next entries are indented two
			// past the dash. The dash line is re-read as that entry.
			p.ls[p.i] = yamlLine{n: l.n, indent: indent + 2, text: rest}
			v, err := p.mapping(indent + 2)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		default:
			p.i++
			out = append(out, scalar(rest))
		}
	}
	return out, nil
}

// isKV reports whether text is "key: value" or "key:" rather than a
// scalar. A quoted scalar is never a key, and a bare scalar is one only
// with ": " or a trailing colon.
func isKV(text string) bool {
	_, _, ok := splitKV(text)
	return ok
}

func splitKV(text string) (key, val string, ok bool) {
	if strings.HasPrefix(text, "'") || strings.HasPrefix(text, `"`) {
		q := text[:1]
		end := closingQuote(text, q)
		if end < 0 || end+1 >= len(text) || text[end+1] != ':' {
			return "", "", false
		}
		rest := strings.TrimSpace(text[end+2:])
		if rest != "" && !strings.HasPrefix(text[end+1:], ": ") {
			return "", "", false
		}
		return unquote(text[:end+1]), rest, true
	}
	if i := strings.Index(text, ": "); i > 0 {
		return text[:i], strings.TrimSpace(text[i+2:]), true
	}
	if strings.HasSuffix(text, ":") && len(text) > 1 && !strings.ContainsAny(text, " ") {
		return strings.TrimSuffix(text, ":"), "", true
	}
	return "", "", false
}

// closingQuote returns the index of the quote that closes a scalar
// opened at text[0] ('' escapes inside single quotes, \ inside double).
func closingQuote(text, q string) int {
	for i := 1; i < len(text); i++ {
		switch {
		case q == `"` && text[i] == '\\':
			i++
		case text[i:i+1] == q:
			if q == "'" && i+1 < len(text) && text[i+1] == '\'' {
				i++
				continue
			}
			return i
		}
	}
	return -1
}

func unquote(s string) string {
	switch {
	case len(s) >= 2 && strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'"):
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'")
	case len(s) >= 2 && strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`):
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
		return s[1 : len(s)-1]
	}
	return s
}

// scalar reads an inline value: a flow sequence of scalars, or one
// scalar (quoted or bare). Numbers and booleans stay strings.
func scalar(val string) any {
	if strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]") {
		inner := strings.TrimSpace(val[1 : len(val)-1])
		out := []any{}
		if inner == "" {
			return out
		}
		for _, item := range strings.Split(inner, ",") {
			out = append(out, unquote(strings.TrimSpace(item)))
		}
		return out
	}
	return unquote(val)
}
