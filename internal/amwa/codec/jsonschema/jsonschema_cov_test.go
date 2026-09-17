package jsonschema

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// mapLoader is a schema set held in memory: the same contract the
// embedded AMWA sets satisfy, without the files.
type mapLoader map[string]string

func (m mapLoader) Load(name string) ([]byte, error) {
	raw, ok := m[name]
	if !ok {
		return nil, fmt.Errorf("no such schema %q", name)
	}
	return []byte(raw), nil
}

// compilerOver builds a Compiler over one inline schema named
// "root.json", plus any extra files the case needs, given as
// name/body pairs.
func compilerOver(t *testing.T, root string, extra ...string) *Compiler {
	t.Helper()
	set := mapLoader{"root.json": root}
	for i := 0; i+1 < len(extra); i += 2 {
		set[extra[i]] = extra[i+1]
	}
	return New(set)
}

// problems runs a document and returns the failures, so a test can
// say what was wrong rather than only that something was.
func problems(t *testing.T, c *Compiler, doc string) []Problem {
	t.Helper()
	err := c.Validate("root.json", []byte(doc))
	if err == nil {
		return nil
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want a *ValidationError, got %T: %v", err, err)
	}
	return ve.Problems
}

// keywords lists the keyword of every problem, in order.
func keywords(ps []Problem) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Keyword
	}
	return out
}

func mustPass(t *testing.T, c *Compiler, doc string) {
	t.Helper()
	if err := c.Validate("root.json", []byte(doc)); err != nil {
		t.Fatalf("%s must conform: %v", doc, err)
	}
}

func mustFail(t *testing.T, c *Compiler, doc, keyword string) {
	t.Helper()
	ps := problems(t, c, doc)
	for _, p := range ps {
		if p.Keyword == keyword {
			return
		}
	}
	t.Fatalf("%s: problems %v, want one on %q", doc, keywords(ps), keyword)
}

// ---------------------------------------------------------------
// type
// ---------------------------------------------------------------

// Every JSON type the AMWA schemas declare is checked, including the
// draft-04 distinction between "number" and "integer" that decides
// whether 1920.5 is a legal frame width.
func TestTypeKeyword(t *testing.T) {
	for _, tc := range []struct {
		typ string
		ok  string
		bad string
	}{
		{"object", `{}`, `[]`},
		{"array", `[]`, `{}`},
		{"string", `"s"`, `1`},
		{"boolean", `true`, `"true"`},
		{"null", `null`, `0`},
		{"number", `1.5`, `"1.5"`},
		{"integer", `7`, `7.5`},
	} {
		c := compilerOver(t, `{"type":"`+tc.typ+`"}`)
		mustPass(t, c, tc.ok)
		mustFail(t, c, tc.bad, "type")
	}
}

// A union type passes when the instance matches any member, and the
// refusal names every member so the operator sees the whole set.
func TestTypeUnion(t *testing.T) {
	c := compilerOver(t, `{"type":["string","null"]}`)
	mustPass(t, c, `"s"`)
	mustPass(t, c, `null`)

	ps := problems(t, c, `1`)
	if len(ps) != 1 || !strings.Contains(ps[0].Detail, "string or null") {
		t.Fatalf("problems = %v, want both members named", ps)
	}
}

// A union member that is not a string names no type this validator
// can check, and it must not silently accept everything.
func TestTypeUnionWithAnUnusableMember(t *testing.T) {
	c := compilerOver(t, `{"type":[7]}`)
	mustFail(t, c, `"s"`, "type")
}

// A type keyword that is neither a string nor a list constrains
// nothing this validator understands; it must still refuse rather
// than pass an unchecked document.
func TestTypeThatIsNeitherStringNorList(t *testing.T) {
	c := compilerOver(t, `{"type":{}}`)
	mustFail(t, c, `"s"`, "type")
}

// A type name outside the JSON set matches nothing.
func TestTypeNameOutsideTheJSONSet(t *testing.T) {
	c := compilerOver(t, `{"type":"uuid"}`)
	mustFail(t, c, `"s"`, "type")
}

// The report names what it found, and a number with a fraction is
// reported as a number rather than as an integer.
func TestTypeReportNamesWhatItFound(t *testing.T) {
	c := compilerOver(t, `{"type":"string"}`)
	for doc, want := range map[string]string{
		`null`: "value is null",
		`true`: "value is boolean",
		`[]`:   "value is array",
		`{}`:   "value is object",
		`1`:    "value is integer",
		`1.5`:  "value is number",
	} {
		ps := problems(t, c, doc)
		if len(ps) != 1 || !strings.Contains(ps[0].Detail, want) {
			t.Errorf("%s reported %v, want %q", doc, ps, want)
		}
	}
}

// ---------------------------------------------------------------
// enum, string, number
// ---------------------------------------------------------------

func TestEnumKeyword(t *testing.T) {
	c := compilerOver(t, `{"enum":["urn:x-nmos:format:video","urn:x-nmos:format:audio"]}`)
	mustPass(t, c, `"urn:x-nmos:format:audio"`)

	ps := problems(t, c, `"urn:x-nmos:format:data"`)
	if len(ps) != 1 || !strings.Contains(ps[0].Detail, "urn:x-nmos:format:video") {
		t.Fatalf("problems = %v, want the permitted values listed", ps)
	}
}

// An enum whose value is not a list states no constraint, and a
// schema that states none must not manufacture a failure.
func TestEnumThatIsNotAList(t *testing.T) {
	c := compilerOver(t, `{"enum":"video"}`)
	mustPass(t, c, `"anything"`)
}

func TestPatternKeyword(t *testing.T) {
	c := compilerOver(t, `{"pattern":"^urn:x-nmos:"}`)
	mustPass(t, c, `"urn:x-nmos:format:video"`)
	mustPass(t, c, `7`) // pattern applies to strings only
	mustFail(t, c, `"video"`, "pattern")
}

// Go's regexp is RE2. A schema pattern it cannot compile is reported
// as a schema fault, not passed as if everything matched.
func TestPatternThatDoesNotCompile(t *testing.T) {
	c := compilerOver(t, `{"pattern":"(?!x)"}`)
	ps := problems(t, c, `"anything"`)
	if len(ps) != 1 || !strings.Contains(ps[0].Detail, "does not compile") {
		t.Fatalf("problems = %v, want the schema fault reported", ps)
	}
}

// A compiled pattern is cached: the second document must behave
// exactly like the first.
func TestPatternCacheIsTransparent(t *testing.T) {
	c := compilerOver(t, `{"pattern":"^a"}`)
	mustPass(t, c, `"ab"`)
	mustPass(t, c, `"ac"`)
	mustFail(t, c, `"b"`, "pattern")
}

func TestFormatURI(t *testing.T) {
	c := compilerOver(t, `{"format":"uri"}`)
	mustPass(t, c, `"http://node.local/x-nmos"`)
	mustPass(t, c, `"/x-nmos/node/v1.3/"`) // a relative reference is a URI
	mustFail(t, c, `"node.local"`, "format")
	mustFail(t, c, `"http://[::1"`, "format")
}

func TestFormatIPv4AndIPv6(t *testing.T) {
	c4 := compilerOver(t, `{"format":"ipv4"}`)
	mustPass(t, c4, `"192.0.2.1"`)
	mustFail(t, c4, `"2001:db8::1"`, "format")
	mustFail(t, c4, `"not an address"`, "format")

	c6 := compilerOver(t, `{"format":"ipv6"}`)
	mustPass(t, c6, `"2001:db8::1"`)
	mustFail(t, c6, `"192.0.2.1"`, "format")
	mustFail(t, c6, `"nonsense"`, "format")
}

func TestFormatHostname(t *testing.T) {
	c := compilerOver(t, `{"format":"hostname"}`)
	mustPass(t, c, `"node-1.example.com"`)
	mustPass(t, c, `"node-1.example.com."`) // the root dot is legal
	mustFail(t, c, `""`, "format")
	mustFail(t, c, `"-leading-hyphen.example"`, "format")
	mustFail(t, c, `"`+strings.Repeat("a", 254)+`"`, "format")
}

// A format this validator does not implement means the constraint
// went unchecked, which is reported for the same reason an unknown
// keyword is.
func TestFormatOutsideTheImplementedSet(t *testing.T) {
	c := compilerOver(t, `{"format":"date-time"}`)
	ps := problems(t, c, `"2026-01-01T00:00:00Z"`)
	if len(ps) != 1 || !strings.Contains(ps[0].Detail, ErrUnknownKeyword.Error()) {
		t.Fatalf("problems = %v, want the unimplemented format reported", ps)
	}
}

func TestMinimumAndMaximum(t *testing.T) {
	c := compilerOver(t, `{"minimum":0,"maximum":100}`)
	mustPass(t, c, `50`)
	mustPass(t, c, `"not a number"`)
	mustFail(t, c, `-1`, "minimum")
	mustFail(t, c, `101`, "maximum")
}

// ---------------------------------------------------------------
// arrays
// ---------------------------------------------------------------

func TestArrayLengthAndUniqueness(t *testing.T) {
	c := compilerOver(t, `{"minItems":1,"maxItems":2,"uniqueItems":true}`)
	mustPass(t, c, `[1,2]`)
	mustPass(t, c, `"not an array"`)
	mustFail(t, c, `[]`, "minItems")
	mustFail(t, c, `[1,2,3]`, "maxItems")
	mustFail(t, c, `[1,1]`, "uniqueItems")
}

// uniqueItems:false states nothing.
func TestUniqueItemsFalseAllowsDuplicates(t *testing.T) {
	c := compilerOver(t, `{"uniqueItems":false}`)
	mustPass(t, c, `[1,1]`)
}

// items as a single schema applies to every element, and the failure
// is located at the element that caused it.
func TestItemsAsOneSchema(t *testing.T) {
	c := compilerOver(t, `{"items":{"type":"string"}}`)
	mustPass(t, c, `["a","b"]`)

	ps := problems(t, c, `["a",7]`)
	if len(ps) != 1 || ps[0].Path != "/1" {
		t.Fatalf("problems = %+v, want the failure located at /1", ps)
	}
}

// items as a list is the tuple form: element i is checked against
// schema i, and elements past the end of the list are unconstrained.
func TestItemsAsATuple(t *testing.T) {
	c := compilerOver(t, `{"items":[{"type":"string"},{"type":"integer"}]}`)
	mustPass(t, c, `["a",1,"anything"]`)
	mustFail(t, c, `["a","b"]`, "type")
}

// A boolean items schema is draft-06, and false means no element is
// permitted at all.
func TestItemsAsABooleanSchema(t *testing.T) {
	c := compilerOver(t, `{"items":false}`)
	mustPass(t, c, `[]`)
	mustFail(t, c, `[1]`, "schema")
}

// ---------------------------------------------------------------
// objects
// ---------------------------------------------------------------

func TestRequiredKeyword(t *testing.T) {
	c := compilerOver(t, `{"required":["id","version"]}`)
	mustPass(t, c, `{"id":"x","version":"0:0"}`)
	mustPass(t, c, `"not an object"`)

	ps := problems(t, c, `{"id":"x"}`)
	if len(ps) != 1 || !strings.Contains(ps[0].Detail, `"version" is missing`) {
		t.Fatalf("problems = %v, want the missing property named", ps)
	}
}

// A required entry that is not a string names no property; it is
// skipped rather than reported against every document.
func TestRequiredWithANonStringEntry(t *testing.T) {
	c := compilerOver(t, `{"required":["id",7]}`)
	mustPass(t, c, `{"id":"x"}`)
}

func TestPropertiesAndAdditionalProperties(t *testing.T) {
	c := compilerOver(t, `{"properties":{"id":{"type":"string"}},"additionalProperties":false}`)
	mustPass(t, c, `{"id":"x"}`)
	mustFail(t, c, `{"id":7}`, "type")

	ps := problems(t, c, `{"id":"x","extra":1}`)
	if len(ps) != 1 || ps[0].Keyword != "additionalProperties" {
		t.Fatalf("problems = %+v, want the extra property refused", ps)
	}
}

// additionalProperties:true permits anything; as a schema, it
// constrains every property the named ones did not cover.
func TestAdditionalPropertiesAsTrueAndAsASchema(t *testing.T) {
	mustPass(t, compilerOver(t, `{"additionalProperties":true}`), `{"anything":1}`)

	c := compilerOver(t, `{"properties":{"id":{}},"additionalProperties":{"type":"string"}}`)
	mustPass(t, c, `{"id":7,"label":"x"}`)
	mustFail(t, c, `{"label":7}`, "type")
}

// patternProperties constrain by name, and a property they match is
// not an additional property.
func TestPatternProperties(t *testing.T) {
	c := compilerOver(t, `{"patternProperties":{"^urn:":{"type":"string"}},"additionalProperties":false}`)
	mustPass(t, c, `{"urn:x":"ok"}`)
	mustFail(t, c, `{"urn:x":7}`, "type")
	mustFail(t, c, `{"other":1}`, "additionalProperties")
}

// A patternProperties key that does not compile is a schema fault,
// reported rather than swallowed.
func TestPatternPropertiesThatDoNotCompile(t *testing.T) {
	c := compilerOver(t, `{"patternProperties":{"(?!x)":{}}}`)
	ps := problems(t, c, `{"a":1}`)
	if len(ps) != 1 || ps[0].Keyword != "patternProperties" {
		t.Fatalf("problems = %+v, want the schema fault reported", ps)
	}
}

// Problem paths are JSON Pointers, so the two characters a pointer
// gives meaning to are escaped in property names.
func TestProblemPathsEscapePointerCharacters(t *testing.T) {
	c := compilerOver(t, `{"properties":{"a/b":{"type":"string"},"c~d":{"type":"string"}}}`)
	ps := problems(t, c, `{"a/b":1,"c~d":2}`)
	if len(ps) != 2 || ps[0].Path != "/a~1b" || ps[1].Path != "/c~0d" {
		t.Fatalf("paths = %+v, want /a~1b and /c~0d", ps)
	}
}

// Two runs over the same document report identically: the property
// walk is ordered, not map-ordered.
func TestProblemOrderIsDeterministic(t *testing.T) {
	c := compilerOver(t, `{"additionalProperties":false}`)
	doc := `{"z":1,"a":2,"m":3}`
	for i := 0; i < 20; i++ {
		ps := problems(t, c, doc)
		if len(ps) != 3 || !strings.Contains(ps[0].Detail, `"a"`) ||
			!strings.Contains(ps[1].Detail, `"m"`) ||
			!strings.Contains(ps[2].Detail, `"z"`) {
			t.Fatalf("run %d = %+v, want the properties in sorted order", i, ps)
		}
	}
}

// ---------------------------------------------------------------
// combinators
// ---------------------------------------------------------------

func TestAllOf(t *testing.T) {
	c := compilerOver(t, `{"allOf":[{"type":"object"},{"required":["id"]}]}`)
	mustPass(t, c, `{"id":"x"}`)
	mustFail(t, c, `{}`, "required")
}

func TestAnyOf(t *testing.T) {
	c := compilerOver(t, `{"anyOf":[{"type":"string"},{"type":"integer"}]}`)
	mustPass(t, c, `"s"`)
	mustPass(t, c, `1`)

	ps := problems(t, c, `1.5`)
	if len(ps) != 1 || ps[0].Keyword != "anyOf" {
		t.Fatalf("problems = %+v, want one anyOf failure, not the members'", ps)
	}
}

// oneOf is the shape IS-04 uses for its Flow union: exactly one
// member, and matching two is as much a failure as matching none.
func TestOneOf(t *testing.T) {
	c := compilerOver(t, `{"oneOf":[{"required":["a"]},{"required":["b"]}]}`)
	mustPass(t, c, `{"a":1}`)

	for _, doc := range []string{`{}`, `{"a":1,"b":2}`} {
		ps := problems(t, c, doc)
		if len(ps) != 1 || ps[0].Keyword != "oneOf" {
			t.Fatalf("%s = %+v, want the oneOf count reported", doc, ps)
		}
	}
}

func TestNot(t *testing.T) {
	c := compilerOver(t, `{"not":{"type":"string"}}`)
	mustPass(t, c, `1`)
	mustFail(t, c, `"s"`, "not")
}

// ---------------------------------------------------------------
// $ref
// ---------------------------------------------------------------

// A local pointer, a whole other file, and a pointer into another
// file — the three shapes the AMWA sets use.
func TestRefResolvesEveryShape(t *testing.T) {
	c := compilerOver(t,
		`{"properties":{
			"local":{"$ref":"#/definitions/str"},
			"whole":{"$ref":"other.json"},
			"into":{"$ref":"other.json#/definitions/num"}
		 },
		 "definitions":{"str":{"type":"string"}}}`,
		"other.json", `{"type":"object","definitions":{"num":{"type":"integer"}}}`)

	mustPass(t, c, `{"local":"s","whole":{},"into":7}`)
	mustFail(t, c, `{"local":1}`, "type")
	mustFail(t, c, `{"whole":"not an object"}`, "type")
	mustFail(t, c, `{"into":"not an integer"}`, "type")
}

// $ref replaces the schema in draft-04: its siblings are ignored,
// which is why an unknown keyword beside one is not reported.
func TestRefIgnoresItsSiblings(t *testing.T) {
	c := compilerOver(t, `{"$ref":"#/definitions/str","nonsense":true,
		"definitions":{"str":{"type":"string"}}}`)
	mustPass(t, c, `"s"`)
	mustFail(t, c, `1`, "type")
}

// A $ref to a file the loader does not have is reported against the
// document rather than ending the validation run.
func TestRefToAFileThatIsNotThere(t *testing.T) {
	c := compilerOver(t, `{"$ref":"absent.json"}`)
	ps := problems(t, c, `{}`)
	if len(ps) != 1 || ps[0].Keyword != "$ref" ||
		!strings.Contains(ps[0].Detail, "absent.json") {
		t.Fatalf("problems = %+v, want the unresolved file named", ps)
	}
}

// A $ref into the current file whose own body will not parse is
// reported the same way.
func TestRefIntoAFileThatWillNotParse(t *testing.T) {
	c := New(mapLoader{
		"root.json":  `{"properties":{"a":{"$ref":"#/definitions/x"}}}`,
		"other.json": `{`,
	})
	// The local ref resolves; the point is the cross-file one below.
	c2 := New(mapLoader{
		"root.json":  `{"$ref":"other.json"}`,
		"other.json": `{`,
	})
	if err := c.Validate("root.json", []byte(`{}`)); err != nil {
		t.Fatalf("the local schema must load: %v", err)
	}
	err := c2.Validate("root.json", []byte(`{}`))
	var ve *ValidationError
	if !errors.As(err, &ve) || len(ve.Problems) != 1 ||
		!strings.Contains(ve.Problems[0].Detail, "parse other.json") {
		t.Fatalf("= %v, want the unparsable referent reported", err)
	}
}

// A pointer that walks off the schema is reported with the reference
// that was written, so the fix is obvious.
func TestRefToAPointerThatDoesNotResolve(t *testing.T) {
	c := compilerOver(t, `{"$ref":"#/definitions/absent","definitions":{}}`)
	ps := problems(t, c, `{}`)
	if len(ps) != 1 || !strings.Contains(ps[0].Detail, "does not resolve") {
		t.Fatalf("problems = %+v, want the unresolved pointer reported", ps)
	}
}

// A pointer that descends through something that is not an object
// does not resolve either.
func TestRefPointerThroughANonObject(t *testing.T) {
	c := compilerOver(t, `{"$ref":"#/definitions/str/deeper",
		"definitions":{"str":"a string, not a subschema"}}`)
	mustFail(t, c, `{}`, "$ref")
}

// A bare "#" means the whole document, and so does a fragment that is
// nothing but separators: empty pointer segments are skipped rather
// than looked up as a property named "".
func TestRefToTheWholeDocument(t *testing.T) {
	for _, ref := range []string{"#", "#/"} {
		c := compilerOver(t,
			`{"properties":{"nest":{"$ref":"`+ref+`"}},"additionalProperties":false}`)
		mustPass(t, c, `{"nest":{"nest":{}}}`)
		mustFail(t, c, `{"nest":{"other":1}}`, "additionalProperties")
	}
}

// jsonTypeOf names the six kinds json.Unmarshal produces into an
// `any`. Anything else is a decoder that changed under us, and the
// name it gives is more use than a wrong one from the list above.
func TestJSONTypeOfAnythingElse(t *testing.T) {
	if got := jsonTypeOf(struct{ A int }{}); got != "struct { A int }" {
		t.Fatalf("= %q, want the Go type named", got)
	}
}

// Pointer segments carry the ~0 / ~1 escapes, so a definition whose
// name holds a slash is still reachable.
func TestRefPointerUnescapesSegments(t *testing.T) {
	c := compilerOver(t, `{"$ref":"#/definitions/a~1b","definitions":{"a/b":{"type":"string"}}}`)
	mustPass(t, c, `"s"`)
	mustFail(t, c, `1`, "type")
}

// Following a $ref into another file moves the base for the refs
// inside it, and moves it back afterwards.
func TestRefRestoresTheBaseFile(t *testing.T) {
	c := compilerOver(t,
		`{"properties":{
			"far":{"$ref":"other.json"},
			"near":{"$ref":"#/definitions/here"}
		 },
		 "definitions":{"here":{"type":"integer"}}}`,
		"other.json", `{"$ref":"#/definitions/there","definitions":{"there":{"type":"string"}}}`)

	mustPass(t, c, `{"far":"a string","near":7}`)
	mustFail(t, c, `{"far":7}`, "type")
	mustFail(t, c, `{"near":"not an integer"}`, "type")
}

// ---------------------------------------------------------------
// schema-level faults
// ---------------------------------------------------------------

// A keyword this validator does not implement means the constraint
// went unchecked. Reporting it is the whole point: a silent skip is
// how a validator claims conformance it never tested.
func TestUnknownKeywordIsReported(t *testing.T) {
	c := compilerOver(t, `{"multipleOf":2}`)
	ps := problems(t, c, `4`)
	if len(ps) != 1 || ps[0].Keyword != "multipleOf" {
		t.Fatalf("problems = %+v, want multipleOf reported", ps)
	}
	if !strings.Contains(ps[0].Detail, "went unchecked") ||
		!strings.Contains(ps[0].Detail, ErrUnknownKeyword.Error()) {
		t.Fatalf("detail = %q, want it to say the constraint went unchecked", ps[0].Detail)
	}
}

// Annotations carry no validation meaning, so ignoring them is
// correct rather than a gap.
func TestAnnotationsAreNotUnknownKeywords(t *testing.T) {
	c := compilerOver(t, `{"$schema":"http://json-schema.org/draft-04/schema#",
		"title":"t","description":"d","default":1,"id":"x","$id":"y",
		"example":1,"examples":[1],"deprecated":true,"readOnly":true,
		"writeOnly":true,"type":"integer"}`)
	mustPass(t, c, `1`)
}

// A subschema that is neither an object nor a boolean is a broken
// schema, and the report says so rather than passing the instance.
func TestSubschemaOfTheWrongShape(t *testing.T) {
	c := compilerOver(t, `{"properties":{"a":"not a subschema"}}`)
	ps := problems(t, c, `{"a":1}`)
	if len(ps) != 1 || !strings.Contains(ps[0].Detail, "want object") {
		t.Fatalf("problems = %+v, want the broken subschema reported", ps)
	}
}

// A boolean schema of true accepts anything.
func TestBooleanSchemaTrue(t *testing.T) {
	mustPass(t, compilerOver(t, `{"properties":{"a":true}}`), `{"a":1}`)
}

// ---------------------------------------------------------------
// Compiler plumbing
// ---------------------------------------------------------------

// A schema file the loader cannot produce is a load error, not a
// validation failure: nothing about the document is known yet.
func TestValidateReportsALoadFailure(t *testing.T) {
	err := New(mapLoader{}).Validate("root.json", []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "load root.json") {
		t.Fatalf("= %v, want the load failure reported", err)
	}
	var ve *ValidationError
	if errors.As(err, &ve) {
		t.Fatal("a load failure is not a validation failure")
	}
}

// A schema file that is not JSON is a schema fault reported as one.
func TestValidateReportsASchemaThatIsNotJSON(t *testing.T) {
	err := compilerOver(t, `{`).Validate("root.json", []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "parse root.json") {
		t.Fatalf("= %v, want the parse failure reported", err)
	}
}

// An instance that is not JSON is refused before any keyword runs.
func TestValidateReportsAnInstanceThatIsNotJSON(t *testing.T) {
	err := compilerOver(t, `{}`).Validate("root.json", []byte(`{`))
	if err == nil || !strings.Contains(err.Error(), "instance is not JSON") {
		t.Fatalf("= %v, want the instance parse failure reported", err)
	}
}

// A parsed schema is cached, so the loader is asked once however many
// documents are validated against it.
func TestSchemasAreLoadedOnce(t *testing.T) {
	counted := &countingLoader{inner: mapLoader{"root.json": `{"type":"integer"}`}}
	c := New(counted)
	for i := 0; i < 5; i++ {
		if err := c.Validate("root.json", []byte(`1`)); err != nil {
			t.Fatal(err)
		}
	}
	if counted.n != 1 {
		t.Fatalf("the loader was asked %d times, want 1", counted.n)
	}
}

type countingLoader struct {
	inner Loader
	n     int
}

func (l *countingLoader) Load(name string) ([]byte, error) {
	l.n++
	return l.inner.Load(name)
}

// ---------------------------------------------------------------
// reporting
// ---------------------------------------------------------------

// Validation does not stop at the first failure: an operator
// debugging a device wants the whole list.
func TestEveryFailureIsReported(t *testing.T) {
	c := compilerOver(t, `{"required":["a","b","c"]}`)
	if ps := problems(t, c, `{}`); len(ps) != 3 {
		t.Fatalf("problems = %+v, want all three", ps)
	}
}

// A problem at the root says so rather than pointing at an empty
// path, and the error text carries every problem.
func TestProblemAndErrorText(t *testing.T) {
	root := Problem{Keyword: "type", Detail: "value is null, want object"}
	if got := root.String(); !strings.HasPrefix(got, "(root): type: ") {
		t.Errorf("root problem = %q", got)
	}
	nested := Problem{Path: "/id", Keyword: "type", Detail: "d"}
	if got := nested.String(); got != "/id: type: d" {
		t.Errorf("nested problem = %q", got)
	}

	ve := &ValidationError{Schema: "node.json", Problems: []Problem{root, nested}}
	got := ve.Error()
	if !strings.HasPrefix(got, "node.json: ") || !strings.Contains(got, "; ") {
		t.Errorf("error text = %q, want the schema named and both problems listed", got)
	}
}
