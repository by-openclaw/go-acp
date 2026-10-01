package codec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The YAML reader and the schema reader are tested against the device
// documents themselves (BRIDGE / CONVERT 7.0.3, SHUFFLE 2.0.0): the
// types a control system relies on have to come out of the real
// files, not out of a sample shaped to pass.

func TestYAMLSubsetReadsTheShapesTheGeneratorsEmit(t *testing.T) {
	doc := []byte(`openapi: '3.1.2'
info:
  title: 'Neuron'
  description: 'Enter your JWT access token (without the ''Bearer '' prefix).'
paths:
  /v1/misc/reference:
    get:
      tags:
        - 'Reference Settings'
      parameters: []
      responses:
        '200':
          description: "quoted \"inner\" text"
  /hardware/ddr:
    get:
      responses:
        '200':
          content:
            application/json:
              schema:
                allOf:
                - $ref: '#/components/schemas/DDRStatus'
                - required:
                  - memoryBank1
                  - memoryBank2
components:
  schemas:
    Fec:
      enum:
        - Off
        - ReedSolomon
    Port:
      type: integer
      minimum: 1
      maximum: 65535
      flow: [a, 'b', "c"]
      empty:
`)
	v, err := parseYAML(doc)
	if err != nil {
		t.Fatal(err)
	}
	root := v.(map[string]any)
	if root["openapi"] != "3.1.2" || dig(root, "info", "title") != "Neuron" {
		t.Errorf("scalars: %v", root["info"])
	}
	if got := dig(root, "info", "description"); got != "Enter your JWT access token (without the 'Bearer ' prefix)." {
		t.Errorf("single-quote escape: %q", got)
	}
	if got := dig(root, "paths", "/v1/misc/reference", "get", "responses", "200", "description"); got != `quoted "inner" text` {
		t.Errorf("double-quote escape: %q", got)
	}
	if got, ok := dig(root, "paths", "/v1/misc/reference", "get", "parameters").([]any); !ok || len(got) != 0 {
		t.Errorf("empty flow sequence: %v", got)
	}
	if got, ok := dig(root, "paths", "/v1/misc/reference", "get", "tags").([]any); !ok || len(got) != 1 || got[0] != "Reference Settings" {
		t.Errorf("block sequence: %v", got)
	}
	all, ok := dig(root, "paths", "/hardware/ddr", "get", "responses", "200", "content", "application/json", "schema", "allOf").([]any)
	if !ok || len(all) != 2 {
		t.Fatalf("allOf at the key's own indent: %v", all)
	}
	if ref := all[0].(map[string]any)["$ref"]; ref != "#/components/schemas/DDRStatus" {
		t.Errorf("dash-mapping item: %v", all[0])
	}
	if req, ok := all[1].(map[string]any)["required"].([]any); !ok || len(req) != 2 || req[1] != "memoryBank2" {
		t.Errorf("dash-mapping continuation: %v", all[1])
	}
	if e, ok := dig(root, "components", "schemas", "Fec", "enum").([]any); !ok || e[0] != "Off" {
		t.Errorf("a bare Off stays the word Off, not a boolean: %v", e)
	}
	port := dig(root, "components", "schemas", "Port").(map[string]any)
	if port["minimum"] != "1" || port["maximum"] != "65535" || port["type"] != "integer" {
		t.Errorf("numbers stay strings for the caller to parse: %v", port)
	}
	if f, ok := port["flow"].([]any); !ok || len(f) != 3 || f[1] != "b" || f[2] != "c" {
		t.Errorf("flow sequence of scalars: %v", port["flow"])
	}
	if v, has := port["empty"]; !has || v != nil {
		t.Errorf("a key with nothing under it is null: %v %v", v, has)
	}
}

func TestYAMLSubsetRefusesWhatItDoesNotRead(t *testing.T) {
	for _, doc := range []string{"", "a:\n\tb: 1\n", "a:\n  b: 1\n c: 2\n", "- a\nb: 1\n"} {
		if _, err := parseYAML([]byte(doc)); err == nil {
			t.Errorf("%q: want an error, not a guess", doc)
		}
	}
}

func loadSpec(t *testing.T, name string) *Spec {
	t.Helper()
	doc, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Skipf("no %s: %v", name, err)
	}
	s, err := ParseSpec(doc)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return s
}

func f(t *testing.T, s *Spec, path string, field ...string) FieldType {
	t.Helper()
	ft, ok := s.FieldType(path, field)
	if !ok {
		t.Fatalf("%s %v: the spec has no schema for it", path, field)
	}
	return ft
}

func TestFieldTypeReadsTheBridgeSchemas(t *testing.T) {
	s := loadSpec(t, "BRIDGE@7.0.3-api.yml")
	if ft := f(t, s, "/misc/reference", "ptp", "domain"); ft.Type != "integer" || ft.Format != "int32" || ft.Min == nil || *ft.Min != 0 || ft.Max == nil || *ft.Max != 5000 || ft.ReadOnly {
		t.Errorf("ptp.domain = %+v", ft)
	}
	if ft := f(t, s, "/misc/reference", "refSelection"); strings.Join(ft.Enum, ",") != "Freerun,ptp-l3-e2e,Reference1,SDI01" {
		t.Errorf("refSelection = %+v", ft)
	}
	if ft := f(t, s, "/misc/reference", "timeSelection"); strings.Join(ft.Enum, ",") != "Auto,PTP,NTP" {
		t.Errorf("timeSelection = %+v", ft)
	}
	if ft := f(t, s, "/misc/macs/{uuid}", "fec"); strings.Join(ft.Enum, ",") != "Off,ReedSolomon" {
		t.Errorf("macs fec = %+v", ft)
	}
	if ft := f(t, s, "/misc/macs/{uuid}", "igmpV3RefreshRate"); ft.Min == nil || *ft.Min != 100 || ft.Max == nil || *ft.Max != 18000 {
		t.Errorf("igmpV3RefreshRate = %+v", ft)
	}
	if ft := f(t, s, "/misc/nmos", "registry", "portOverride"); ft.Min == nil || *ft.Min != 1 || ft.Max == nil || *ft.Max != 65535 {
		t.Errorf("registry.portOverride = %+v", ft)
	}
	if ft := f(t, s, "/misc/nmos", "registry", "addressOverride"); ft.Type != "string" || ft.MaxLength != 23 {
		t.Errorf("registry.addressOverride = %+v", ft)
	}
	if ft := f(t, s, "/misc/nmos", "registry", "discoveryMode"); strings.Join(ft.Enum, ",") != "None,Manual,mDNS,Unicast" {
		t.Errorf("discoveryMode = %+v", ft)
	}
	// An array item is reached by its index.
	if ft := f(t, s, "/io/ip/senders/video/{uuid}", "legs", "0", "port"); ft.Type != "integer" || ft.Max == nil || *ft.Max != 65535 {
		t.Errorf("legs.0.port = %+v", ft)
	}
	if ft := f(t, s, "/io/ip/senders/video/{uuid}", "legs", "1", "ip"); ft.Type != "string" {
		t.Errorf("legs.1.ip = %+v", ft)
	}
	if ft := f(t, s, "/io/ip/senders/video/{uuid}", "enable"); ft.Type != "boolean" {
		t.Errorf("enable = %+v", ft)
	}
	// The version prefix the document writes is not the caller's concern.
	if _, ok := s.FieldType("/v1/misc/reference", []string{"ptp", "domain"}); !ok {
		t.Error("a /v1-prefixed path must resolve too")
	}
	if _, ok := s.FieldType("/misc/reference", []string{"nosuch"}); ok {
		t.Error("a field the schema does not have is not typed")
	}
	if _, ok := s.FieldType("/nosuch", []string{"x"}); ok {
		t.Error("a path the spec does not have is not typed")
	}
	if m, ok := s.WriteMethod("/misc/reference"); !ok || m != PUT {
		t.Errorf("BRIDGE writes by PUT: %v %v", m, ok)
	}
	if _, ok := s.WriteMethod("/misc/reference/status"); ok {
		t.Error("a GET-only path has no write method")
	}
}

func TestFieldTypeReadsTheShuffleSchemas(t *testing.T) {
	s := loadSpec(t, "SHUFFLE@2.0.0-openapi.yml")
	if m, ok := s.WriteMethod("/reference"); !ok || m != PATCH {
		t.Errorf("SHUFFLE declares PATCH (CCM §11.2): %v %v", m, ok)
	}
	if !s.Writable("/network/macs/{uuid}") || !s.Has("/network/macs/{uuid}", PATCH) || !s.Has("/network/macs/{uuid}", PUT) {
		t.Error("network/macs/{uuid} declares PUT and PATCH")
	}
	if !s.Has("/io/ip/receivers/audio/metrics/reset", POST) {
		t.Error("an action is a POST")
	}
	if ft := f(t, s, "/reference", "ptp", "domain"); ft.Type != "integer" || ft.Max == nil || *ft.Max != 5000 {
		t.Errorf("ptp.domain = %+v", ft)
	}
	// The enum is read as the words the device compares against.
	if ft := f(t, s, "/network/macs/{uuid}", "fec"); strings.Join(ft.Enum, ",") != "Off,ReedSolomon" {
		t.Errorf("fec = %+v", ft)
	}
	// readOnly: true on the property (through the allOf the request
	// and response schemas share).
	if ft := f(t, s, "/network/macs/{uuid}", "name"); !ft.ReadOnly || ft.Type != "string" {
		t.Errorf("macs name = %+v", ft)
	}
	if ft := f(t, s, "/network/macs/{uuid}", "ipAddrMethod"); ft.ReadOnly || strings.Join(ft.Enum, ",") != "Manual,DHCP" {
		t.Errorf("ipAddrMethod = %+v", ft)
	}
	if ft := f(t, s, "/io/ip/senders/audio/{uuid}", "primaryLeg", "port"); ft.Type != "integer" || ft.Max == nil || *ft.Max != 65535 {
		t.Errorf("primaryLeg.port = %+v", ft)
	}
}

func TestReadOnlyIsAFieldTheWriteSchemasLackOrFlag(t *testing.T) {
	doc := []byte(`openapi: 3.1.1
paths:
  /thing:
    get:
      responses:
        '200':
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ThingRead'
    put:
      requestBody:
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/ThingWrite'
  /status:
    get:
      responses:
        '200':
          content:
            application/json:
              schema:
                type: object
                properties:
                  temp:
                    type: number
components:
  schemas:
    ThingWrite:
      type: object
      properties:
        label:
          type: string
          maxLength: 8
    ThingRead:
      allOf:
        - $ref: '#/components/schemas/ThingWrite'
        - type: object
          properties:
            serial:
              type: string
            flag:
              type: boolean
              readOnly: true
`)
	s, err := ParseSpec(doc)
	if err != nil {
		t.Fatal(err)
	}
	if ft := f(t, s, "/thing", "label"); ft.ReadOnly || ft.MaxLength != 8 {
		t.Errorf("label is in the write schema: %+v", ft)
	}
	if ft := f(t, s, "/thing", "serial"); !ft.ReadOnly {
		t.Errorf("serial is read-only: in GET, in no write schema (CCM §14.2): %+v", ft)
	}
	if ft := f(t, s, "/thing", "flag"); !ft.ReadOnly {
		t.Errorf("flag carries readOnly: true: %+v", ft)
	}
	// A GET-only resource: its fields are typed, and the resource-level
	// answer (Writable) says it cannot be written.
	if ft := f(t, s, "/status", "temp"); ft.Type != "number" || ft.ReadOnly {
		t.Errorf("status.temp = %+v", ft)
	}
	if s.Writable("/status") {
		t.Error("/status declares no write")
	}
}

func TestYAMLSubsetEdges(t *testing.T) {
	// A quoted key with a value, a quoted key with nothing under it, a
	// dash with nothing on it followed by a deeper block, a dash with
	// nothing at all, a double-quoted scalar with an escaped quote and
	// a broken one, a quoted scalar that is not a key.
	doc := []byte(`'200': "ok"
"x y": 'v'
'empty':
list:
  -
    a: 1
  -
  - "esc \" aped"
  - 'not: a key'
  - "unterminated
  - 'tail' z
`)
	v, err := parseYAML(doc)
	if err != nil {
		t.Fatal(err)
	}
	root := v.(map[string]any)
	if root["200"] != "ok" || root["x y"] != "v" {
		t.Errorf("quoted keys: %v", root)
	}
	if e, has := root["empty"]; !has || e != nil {
		t.Errorf("quoted key with nothing under it: %v %v", e, has)
	}
	list, _ := root["list"].([]any)
	if len(list) != 6 {
		t.Fatalf("list = %v", list)
	}
	if m, ok := list[0].(map[string]any); !ok || m["a"] != "1" {
		t.Errorf("dash then deeper block: %v", list[0])
	}
	if list[1] != nil {
		t.Errorf("bare dash is null: %v", list[1])
	}
	if list[2] != `esc " aped` {
		t.Errorf("double-quote escape: %q", list[2])
	}
	if list[3] != "not: a key" {
		t.Errorf("a quoted scalar is a scalar: %q", list[3])
	}
	if list[4] != `"unterminated` {
		t.Errorf("an unterminated quote stays as written: %q", list[4])
	}
	if list[5] != "'tail' z" {
		t.Errorf("a quoted scalar followed by text is not a key: %q", list[5])
	}
	// A dash item in single quotes with a doubled quote inside, a quoted
	// key glued to its value, a double-quoted key with an escaped quote.
	more, err := parseYAML([]byte("- 'it''s'\n- 'a':x\n- \"k\\\"q\": v\n"))
	if err != nil {
		t.Fatal(err)
	}
	ml := more.([]any)
	if ml[0] != "it's" || ml[1] != "'a':x" {
		t.Errorf("quoted dash items: %v", ml)
	}
	if m, ok := ml[2].(map[string]any); !ok || m[`k"q`] != "v" {
		t.Errorf("double-quoted key with an escaped quote: %v", ml[2])
	}
	if got := unquote(`"\x"`); got != `\x` {
		t.Errorf("a double-quoted scalar Go cannot unquote keeps its inside: %q", got)
	}
	// Error paths: a line that is not a key, an item indented past its
	// mapping inside a sequence item.
	for _, bad := range []string{"a: 1\nnot a key\n", "a:\n  - b: 1\n      c: 2\n   d: 3\n", "a:\n  - x\n  - b:\n   - c\n     - d\n", "a:\n- b: 1\n   c: 2\n", "-\n  a: 1\n   b: 2\n"} {
		if _, err := parseYAML([]byte(bad)); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}

func TestSpecEdges(t *testing.T) {
	var none *Spec
	if _, ok := none.FieldType("/x", nil); ok {
		t.Error("a nil spec types nothing")
	}
	if m, ok := none.WriteMethod("/x"); ok || m != "" {
		t.Error("a nil spec writes nothing")
	}
	if dig(map[string]any{"a": "s"}, "a", "b") != nil {
		t.Error("dig through a scalar is nil")
	}
	s := &Spec{schemas: map[string]any{}}
	if s.resolve("not a map", 0) != nil || s.resolve(map[string]any{"$ref": "#/components/schemas/Missing"}, 0) != nil {
		t.Error("resolve of a non-schema or a dangling $ref is nil")
	}
	loop := map[string]any{"$ref": "#/components/schemas/Loop"}
	s.schemas["Loop"] = loop
	if s.resolve(loop, 0) != nil {
		t.Error("a $ref cycle ends at the depth guard")
	}
	if _, ok := s.walk(nil, []string{"a"}); ok {
		t.Error("walking nothing finds nothing")
	}
	// allOf keeps the outer keywords and merges members' properties;
	// a member without properties contributes its other keywords once.
	s.schemas["A"] = map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "string"}}}
	merged := s.resolve(map[string]any{"allOf": []any{map[string]any{"$ref": "#/components/schemas/A"}, map[string]any{"readOnly": "true", "type": "object"}}, "description": "d"}, 0)
	if merged["description"] != "d" || merged["readOnly"] != "true" || merged["type"] != "object" {
		t.Errorf("allOf merge: %v", merged)
	}
	if props, _ := merged["properties"].(map[string]any); props["x"] == nil {
		t.Errorf("allOf properties: %v", merged)
	}
	// describe: bounds that are not numbers are ignored.
	ft := s.describe(map[string]any{"type": "integer", "minimum": "low", "maximum": "high", "maxLength": "x", "enum": []any{"a", 1}})
	if ft.Min != nil || ft.Max != nil || ft.MaxLength != 0 || strings.Join(ft.Enum, ",") != "a" {
		t.Errorf("describe with bad keywords = %+v", ft)
	}
	// A field in the write schema only: its type comes from there, the
	// read schema adds nothing, and it is writable.
	doc := []byte("openapi: 3.1.1\npaths:\n  /t:\n    get:\n      responses:\n        '200':\n          content:\n            application/json:\n              schema:\n                type: object\n                properties:\n                  a:\n                    type: string\n                    format: uuid\n                    enum:\n                      - x\n    put:\n      requestBody:\n        content:\n          application/json:\n            schema:\n              type: object\n              properties:\n                a:\n                  type: ''\n                b:\n                  type: integer\n")
	sp, err := ParseSpec(doc)
	if err != nil {
		t.Fatal(err)
	}
	if ft := f(t, sp, "/t", "a"); ft.Type != "string" || ft.Format != "uuid" || strings.Join(ft.Enum, ",") != "x" || ft.ReadOnly {
		t.Errorf("read schema fills what the write schema leaves out: %+v", ft)
	}
	if ft := f(t, sp, "/t", "b"); ft.Type != "integer" || ft.ReadOnly {
		t.Errorf("write-only field: %+v", ft)
	}
	if !sp.Has("/t", PUT) || sp.Has("/t", DELETE) {
		t.Error("Has")
	}
}
