// Package jsonschema is a JSON Schema draft-04 validator covering
// exactly the keyword set the AMWA NMOS schemas use, and nothing more.
//
// It exists so that NO NMOS validation rule is hand-written. AMWA
// publishes the normative schema for every resource at every version;
// those files are the specification. A hand-written validator is a
// paraphrase of them, and a paraphrase drifts — ours failed a real EVS
// Neuron on `label`/`description` checks no schema ever asked for, and
// failed a v1.0 Flow for `frame_width`, a field v1.0 does not have.
// sony/nmos-cpp avoids that whole class of bug by embedding the AMWA
// schemas (Development/nmos/is04_schemas/ et al) and validating
// against them. This is the same move.
//
// Stdlib only, per ADR-0006 — no external schema library.
//
// Keywords implemented, which is precisely what a scan of the IS-04
// schema sets at v1.0.3 / v1.1.3 / v1.2.2 / v1.3.3 reports:
//
//	$ref  type  properties  required  additionalProperties
//	patternProperties  items  enum  pattern  format  minItems
//	maxItems  uniqueItems  minimum  maximum  allOf  anyOf  oneOf
//	not  definitions
//
// A keyword outside that set is a schema we have not seen. Rather
// than ignore it silently — which would quietly weaken validation —
// [Compiler.Validate] reports it through [ErrUnknownKeyword].
package jsonschema

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Loader resolves a schema file name — "flow_video_raw.json" — to its
// bytes. One Loader serves one version's schema set, which is what
// keeps versions from bleeding into each other: a v1.0 Compiler can
// only ever see v1.0 schemas.
type Loader interface {
	Load(name string) ([]byte, error)
}

// ErrUnknownKeyword reports a schema keyword this validator does not
// implement. It is deliberately an error and not a silent skip: an
// unimplemented keyword means the document went unchecked in a way
// the spec required, and nobody would notice.
var ErrUnknownKeyword = errors.New("jsonschema: unimplemented keyword")

// known lists every keyword we either enforce or may safely ignore.
// Annotations carry no validation meaning, so ignoring them is
// correct rather than a gap.
var known = map[string]bool{
	// enforced
	"$ref": true, "type": true, "properties": true, "required": true,
	"additionalProperties": true, "patternProperties": true, "items": true,
	"enum": true, "pattern": true, "format": true, "minItems": true,
	"maxItems": true, "uniqueItems": true, "minimum": true, "maximum": true,
	"allOf": true, "anyOf": true, "oneOf": true, "not": true,
	// structural
	"definitions": true,
	// annotations — no validation meaning
	"$schema": true, "title": true, "description": true, "default": true,
	"id": true, "$id": true, "example": true, "examples": true,
	"deprecated": true, "readOnly": true, "writeOnly": true,
}

// Problem is one validation failure, located by JSON Pointer.
type Problem struct {
	Path    string // JSON Pointer into the instance, "" for the root
	Keyword string
	Detail  string
}

func (p Problem) String() string {
	at := p.Path
	if at == "" {
		at = "(root)"
	}
	return fmt.Sprintf("%s: %s: %s", at, p.Keyword, p.Detail)
}

// ValidationError collects every Problem found. Validation does not
// stop at the first failure: an operator debugging a device wants the
// whole list, not a game of whack-a-mole.
type ValidationError struct {
	Schema   string
	Problems []Problem
}

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Problems))
	for _, p := range e.Problems {
		parts = append(parts, p.String())
	}
	return fmt.Sprintf("%s: %s", e.Schema, strings.Join(parts, "; "))
}

// Compiler validates instances against one version's schema set.
// Safe for concurrent use once built; schemas are parsed on first use
// and cached.
type Compiler struct {
	loader Loader

	mu       sync.RWMutex
	parsed   map[string]any
	patterns map[string]*regexp.Regexp
}

// New returns a Compiler over one schema set.
func New(l Loader) *Compiler {
	return &Compiler{
		loader:   l,
		parsed:   map[string]any{},
		patterns: map[string]*regexp.Regexp{},
	}
}

// Validate checks doc against the named schema file. It returns nil
// when the document conforms, and a *ValidationError listing every
// failure otherwise.
func (c *Compiler) Validate(schemaName string, doc []byte) error {
	root, err := c.schema(schemaName)
	if err != nil {
		return err
	}
	var inst any
	if err := json.Unmarshal(doc, &inst); err != nil {
		return fmt.Errorf("jsonschema: %s: instance is not JSON: %w", schemaName, err)
	}
	v := &validator{c: c, base: schemaName}
	v.apply(root, inst, "")
	if len(v.problems) == 0 {
		return nil
	}
	return &ValidationError{Schema: schemaName, Problems: v.problems}
}

// schema parses and caches one schema file.
func (c *Compiler) schema(name string) (any, error) {
	c.mu.RLock()
	s, ok := c.parsed[name]
	c.mu.RUnlock()
	if ok {
		return s, nil
	}
	raw, err := c.loader.Load(name)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: load %s: %w", name, err)
	}
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("jsonschema: parse %s: %w", name, err)
	}
	c.mu.Lock()
	c.parsed[name] = parsed
	c.mu.Unlock()
	return parsed, nil
}

// compile caches compiled patterns. Go's regexp is RE2, which rejects
// backreferences and lookaround; if an AMWA pattern ever needs them
// the error surfaces here rather than silently passing everything.
func (c *Compiler) compile(pat string) (*regexp.Regexp, error) {
	c.mu.RLock()
	re, ok := c.patterns[pat]
	c.mu.RUnlock()
	if ok {
		return re, nil
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.patterns[pat] = re
	c.mu.Unlock()
	return re, nil
}

type validator struct {
	c        *Compiler
	base     string // schema file the current subschema came from
	problems []Problem
}

func (v *validator) fail(path, keyword, format string, args ...any) {
	v.problems = append(v.problems, Problem{
		Path: path, Keyword: keyword, Detail: fmt.Sprintf(format, args...),
	})
}

// apply validates inst against one subschema.
func (v *validator) apply(sch, inst any, path string) {
	switch s := sch.(type) {
	case bool: // draft-06 boolean schema; harmless to support
		if !s {
			v.fail(path, "schema", "schema is false — nothing validates here")
		}
		return
	case map[string]any:
		v.applyObject(s, inst, path)
	default:
		v.fail(path, "schema", "subschema is %T, want object", sch)
	}
}

func (v *validator) applyObject(s map[string]any, inst any, path string) {
	// $ref replaces the schema entirely in draft-04; siblings are
	// ignored by the spec, so resolving first is correct.
	if ref, ok := s["$ref"].(string); ok {
		v.applyRef(ref, inst, path)
		return
	}

	for kw := range s {
		if !known[kw] {
			v.fail(path, kw, "%v — this validator does not implement it, so the "+
				"constraint went unchecked", ErrUnknownKeyword)
		}
	}

	v.checkType(s, inst, path)
	v.checkEnum(s, inst, path)
	v.checkString(s, inst, path)
	v.checkNumber(s, inst, path)
	v.checkArray(s, inst, path)
	v.checkObjectKeywords(s, inst, path)
	v.checkCombinators(s, inst, path)
}

// applyRef resolves "file.json", "#/definitions/x", or
// "file.json#/definitions/x" and validates against the target.
func (v *validator) applyRef(ref string, inst any, path string) {
	file, frag, _ := strings.Cut(ref, "#")
	base := v.base
	root := any(nil)

	if file == "" {
		var err error
		root, err = v.c.schema(base)
		if err != nil {
			v.fail(path, "$ref", "%v", err)
			return
		}
	} else {
		var err error
		root, err = v.c.schema(file)
		if err != nil {
			v.fail(path, "$ref", "%v", err)
			return
		}
		base = file
	}

	target := root
	if frag != "" {
		var ok bool
		target, ok = pointer(root, frag)
		if !ok {
			v.fail(path, "$ref", "%q does not resolve", ref)
			return
		}
	}

	saved := v.base
	v.base = base
	v.apply(target, inst, path)
	v.base = saved
}

// pointer walks a JSON Pointer fragment ("/definitions/foo").
func pointer(doc any, frag string) (any, bool) {
	cur := doc
	for _, seg := range strings.Split(strings.TrimPrefix(frag, "/"), "/") {
		if seg == "" {
			continue
		}
		seg = strings.ReplaceAll(seg, "~1", "/")
		seg = strings.ReplaceAll(seg, "~0", "~")
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[seg]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func (v *validator) checkType(s map[string]any, inst any, path string) {
	raw, present := s["type"]
	if !present {
		return
	}
	var want []string
	switch t := raw.(type) {
	case string:
		want = []string{t}
	case []any:
		for _, e := range t {
			if str, ok := e.(string); ok {
				want = append(want, str)
			}
		}
	}
	for _, w := range want {
		if typeMatches(w, inst) {
			return
		}
	}
	v.fail(path, "type", "value is %s, want %s", jsonTypeOf(inst), strings.Join(want, " or "))
}

func typeMatches(want string, inst any) bool {
	switch want {
	case "object":
		_, ok := inst.(map[string]any)
		return ok
	case "array":
		_, ok := inst.([]any)
		return ok
	case "string":
		_, ok := inst.(string)
		return ok
	case "boolean":
		_, ok := inst.(bool)
		return ok
	case "null":
		return inst == nil
	case "number":
		_, ok := inst.(float64)
		return ok
	case "integer":
		f, ok := inst.(float64)
		return ok && f == math.Trunc(f) && !math.IsInf(f, 0)
	}
	return false
}

func jsonTypeOf(inst any) string {
	switch t := inst.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case float64:
		if t == math.Trunc(t) {
			return "integer"
		}
		return "number"
	}
	return fmt.Sprintf("%T", inst)
}

func (v *validator) checkEnum(s map[string]any, inst any, path string) {
	raw, present := s["enum"]
	if !present {
		return
	}
	allowed, ok := raw.([]any)
	if !ok {
		return
	}
	for _, a := range allowed {
		if reflect.DeepEqual(a, inst) {
			return
		}
	}
	shown := make([]string, 0, len(allowed))
	for _, a := range allowed {
		shown = append(shown, fmt.Sprintf("%v", a))
	}
	v.fail(path, "enum", "%v is not one of [%s]", inst, strings.Join(shown, ", "))
}

func (v *validator) checkString(s map[string]any, inst any, path string) {
	str, ok := inst.(string)
	if !ok {
		return
	}
	if pat, ok := s["pattern"].(string); ok {
		re, err := v.c.compile(pat)
		if err != nil {
			v.fail(path, "pattern", "schema pattern %q does not compile: %v", pat, err)
		} else if !re.MatchString(str) {
			v.fail(path, "pattern", "%q does not match %s", str, pat)
		}
	}
	if f, ok := s["format"].(string); ok {
		if err := checkFormat(f, str); err != nil {
			v.fail(path, "format", "%v", err)
		}
	}
}

// checkFormat implements the four formats the AMWA IS-04 schemas use.
// An unrecognised format is reported rather than ignored, for the same
// reason as ErrUnknownKeyword.
func checkFormat(format, s string) error {
	switch format {
	case "uri":
		u, err := url.Parse(s)
		if err != nil {
			return fmt.Errorf("%q is not a URI: %w", s, err)
		}
		if u.Scheme == "" && !strings.HasPrefix(s, "/") {
			return fmt.Errorf("%q is not a URI: no scheme", s)
		}
		return nil
	case "ipv4":
		ip := net.ParseIP(s)
		if ip == nil || ip.To4() == nil {
			return fmt.Errorf("%q is not an IPv4 address", s)
		}
		return nil
	case "ipv6":
		ip := net.ParseIP(s)
		if ip == nil || strings.Count(s, ":") < 2 {
			return fmt.Errorf("%q is not an IPv6 address", s)
		}
		return nil
	case "hostname":
		if s == "" || len(s) > 253 {
			return fmt.Errorf("%q is not a hostname", s)
		}
		for _, label := range strings.Split(strings.TrimSuffix(s, "."), ".") {
			if !hostLabel.MatchString(label) {
				return fmt.Errorf("%q is not a hostname: bad label %q", s, label)
			}
		}
		return nil
	}
	return fmt.Errorf("%w: format %q", ErrUnknownKeyword, format)
}

var hostLabel = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

func (v *validator) checkNumber(s map[string]any, inst any, path string) {
	n, ok := inst.(float64)
	if !ok {
		return
	}
	if min, ok := s["minimum"].(float64); ok && n < min {
		v.fail(path, "minimum", "%v is below %v", n, min)
	}
	if max, ok := s["maximum"].(float64); ok && n > max {
		v.fail(path, "maximum", "%v is above %v", n, max)
	}
}

func (v *validator) checkArray(s map[string]any, inst any, path string) {
	arr, ok := inst.([]any)
	if !ok {
		return
	}
	if min, ok := s["minItems"].(float64); ok && float64(len(arr)) < min {
		v.fail(path, "minItems", "%d items, want at least %v", len(arr), min)
	}
	if max, ok := s["maxItems"].(float64); ok && float64(len(arr)) > max {
		v.fail(path, "maxItems", "%d items, want at most %v", len(arr), max)
	}
	if uniq, ok := s["uniqueItems"].(bool); ok && uniq {
		for i := 0; i < len(arr); i++ {
			for j := i + 1; j < len(arr); j++ {
				if reflect.DeepEqual(arr[i], arr[j]) {
					v.fail(path, "uniqueItems", "items %d and %d are equal", i, j)
				}
			}
		}
	}
	switch items := s["items"].(type) {
	case map[string]any, bool:
		for i, el := range arr {
			v.apply(items, el, fmt.Sprintf("%s/%d", path, i))
		}
	case []any: // tuple form
		for i, el := range arr {
			if i < len(items) {
				v.apply(items[i], el, fmt.Sprintf("%s/%d", path, i))
			}
		}
	}
}

func (v *validator) checkObjectKeywords(s map[string]any, inst any, path string) {
	obj, ok := inst.(map[string]any)
	if !ok {
		return
	}

	if req, ok := s["required"].([]any); ok {
		for _, r := range req {
			name, ok := r.(string)
			if !ok {
				continue
			}
			if _, present := obj[name]; !present {
				v.fail(path, "required", "%q is missing", name)
			}
		}
	}

	props, _ := s["properties"].(map[string]any)
	patProps, _ := s["patternProperties"].(map[string]any)

	// Deterministic order so two runs report identically.
	names := make([]string, 0, len(obj))
	for k := range obj {
		names = append(names, k)
	}
	sort.Strings(names)

	for _, name := range names {
		val := obj[name]
		matched := false
		if sub, ok := props[name]; ok {
			v.apply(sub, val, path+"/"+escape(name))
			matched = true
		}
		for pat, sub := range patProps {
			re, err := v.c.compile(pat)
			if err != nil {
				v.fail(path, "patternProperties", "schema pattern %q does not compile: %v", pat, err)
				continue
			}
			if re.MatchString(name) {
				v.apply(sub, val, path+"/"+escape(name))
				matched = true
			}
		}
		if matched {
			continue
		}
		switch ap := s["additionalProperties"].(type) {
		case bool:
			if !ap {
				v.fail(path, "additionalProperties", "%q is not permitted here", name)
			}
		case map[string]any:
			v.apply(ap, val, path+"/"+escape(name))
		}
	}
}

func escape(s string) string {
	s = strings.ReplaceAll(s, "~", "~0")
	return strings.ReplaceAll(s, "/", "~1")
}

func (v *validator) checkCombinators(s map[string]any, inst any, path string) {
	if all, ok := s["allOf"].([]any); ok {
		for _, sub := range all {
			v.apply(sub, inst, path)
		}
	}
	if any_, ok := s["anyOf"].([]any); ok {
		if !v.matchesAny(any_, inst, path) {
			v.fail(path, "anyOf", "matches none of the %d permitted shapes", len(any_))
		}
	}
	if one, ok := s["oneOf"].([]any); ok {
		n := 0
		for _, sub := range one {
			if v.matches(sub, inst, path) {
				n++
			}
		}
		if n != 1 {
			v.fail(path, "oneOf", "matches %d of the %d permitted shapes, want exactly 1", n, len(one))
		}
	}
	if not, ok := s["not"]; ok {
		if v.matches(not, inst, path) {
			v.fail(path, "not", "matches a shape the schema forbids")
		}
	}
}

func (v *validator) matchesAny(subs []any, inst any, path string) bool {
	for _, sub := range subs {
		if v.matches(sub, inst, path) {
			return true
		}
	}
	return false
}

// matches runs a subschema in isolation, discarding its problems —
// combinators need a yes/no, not a running tally.
func (v *validator) matches(sub, inst any, path string) bool {
	probe := &validator{c: v.c, base: v.base}
	probe.apply(sub, inst, path)
	return len(probe.problems) == 0
}
