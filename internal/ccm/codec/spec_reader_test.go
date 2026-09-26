package codec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The spec reader is what decides which resources exist and which
// accept a write, so it is tested against the device's OWN document
// rather than only against hand-written YAML: a generator that changes
// its indentation would otherwise silently mark every object
// read-only, and nothing downstream could tell.

const miniSpec = `openapi: '3.1.2'
info:
  title: 'Neuron'
paths:
  /v1:
    get:
      tags:
        - 'Root'
  /v1/self:
    get:
      operationId: 'GetSelf'
  /v1/io/sdi/{uuid}:
    get:
      operationId: 'GetSdi'
    put:
      operationId: 'SetSdi'
  /v1/misc/reference:
    get:
      operationId: 'GetReference'
    put:
      operationId: 'SetReference'
  /v1/misc/reference/status:
    get:
      operationId: 'GetReferenceStatus'
components:
  schemas:
    NotAPath:
      put: 'this is a schema field, not an operation'
`

func TestParseSpecReadsPathsAndMethods(t *testing.T) {
	s, err := ParseSpec([]byte(miniSpec))
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}
	if got := len(s.Paths()); got != 5 {
		t.Errorf("paths = %d (%v)", got, s.Paths())
	}
	if got := len(s.With(GET)); got != 5 {
		t.Errorf("GETs = %v", s.With(GET))
	}
	if got := s.With(PUT); len(got) != 2 {
		t.Errorf("PUTs = %v", got)
	}
	// components: is a top-level key, so nothing under it is a path —
	// even a line that looks exactly like an operation.
	for _, p := range s.Paths() {
		if strings.Contains(p, "NotAPath") {
			t.Errorf("a schema leaked into the paths: %v", s.Paths())
		}
	}
}

func TestWritableIsTheSpecsAnswerNotAGuess(t *testing.T) {
	s, err := ParseSpec([]byte(miniSpec))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path  string
		read  bool
		write bool
	}{
		{"/misc/reference", true, true},
		{"/misc/reference/status", true, false}, // a status is a reading
		{"/io/sdi/{uuid}", true, true},
		{"/self", true, false},
		{"/nothing/here", false, false},
	}
	for _, c := range cases {
		if got := s.Readable(c.path); got != c.read {
			t.Errorf("Readable(%s) = %v", c.path, got)
		}
		if got := s.Writable(c.path); got != c.write {
			t.Errorf("Writable(%s) = %v", c.path, got)
		}
	}
	// A nil spec answers no to everything rather than panicking: it is
	// what a connector holds before it has connected.
	var nilSpec *Spec
	if nilSpec.Writable("/misc/reference") || nilSpec.Readable("/misc/reference") ||
		nilSpec.Paths() != nil || nilSpec.With(GET) != nil {
		t.Error("a nil spec must answer no, not panic")
	}
	if _, ok := nilSpec.TemplateFor("/misc/reference"); ok {
		t.Error("a nil spec resolves nothing")
	}
}

func TestTemplateForMatchesAConcreteDevicePath(t *testing.T) {
	s, err := ParseSpec([]byte(miniSpec))
	if err != nil {
		t.Fatal(err)
	}
	// A real UUID resolves to the template that declares it.
	tpl, ok := s.TemplateFor("/io/sdi/8fd150f9-883f-421c-b568-808e5fbf9712")
	if !ok || tpl != "/io/sdi/{uuid}" {
		t.Errorf("template = %q (%v)", tpl, ok)
	}
	// A literal path wins over a template of the same length, so a
	// resource that is BOTH declared literally and covered by a
	// parameter does not inherit the parameter's methods.
	tpl, ok = s.TemplateFor("/misc/reference/status")
	if !ok || tpl != "/misc/reference/status" {
		t.Errorf("template = %q (%v)", tpl, ok)
	}
	// Length has to match: a path one segment longer is not this one.
	if _, ok := s.TemplateFor("/io/sdi/uuid/extra"); ok {
		t.Error("a longer path must not match a shorter template")
	}
	if _, ok := s.TemplateFor("/io"); ok {
		t.Error("a shorter path must not match a longer template")
	}
}

func TestADocumentWithNoPathsIsAnError(t *testing.T) {
	// Answering "no paths" quietly would mark every object read-only
	// and make a device look like it has nothing to set.
	for _, doc := range []string{
		"", "openapi: '3.1.2'\ninfo:\n  title: 'x'\n", "# just a comment\n",
	} {
		if _, err := ParseSpec([]byte(doc)); err == nil {
			t.Errorf("ParseSpec(%q) must fail", doc)
		}
	}
	if got := (errSpec("why")).Error(); !strings.Contains(got, "api.yml") {
		t.Errorf("error = %q", got)
	}
}

func TestParseSpecAgainstTheDevicesOwnDocument(t *testing.T) {
	// The committed capture from BRIDGE 7.0.3. If EVS's generator
	// changes shape, this fails here rather than in the field.
	doc, err := os.ReadFile(filepath.Join("testdata", "BRIDGE@7.0.3-api.yml"))
	if err != nil {
		t.Skipf("no committed api.yml: %v", err)
	}
	s, err := ParseSpec(doc)
	if err != nil {
		t.Fatalf("the device's own document must parse: %v", err)
	}
	// Counted from the document by hand (awk over `paths:`), so this
	// asserts the reader against the file rather than against itself.
	if got := len(s.With(GET)); got != 75 {
		t.Errorf("GETs = %d, want 75", got)
	}
	if got := len(s.With(PUT)); got != 36 {
		t.Errorf("PUTs = %d, want 36", got)
	}
	// The three that matter for the connector's behaviour.
	if !s.Writable("/matrix/video/path/main") {
		t.Error("the video routing table must be writable")
	}
	if s.Writable("/io/ip/senders/video/{uuid}/status") {
		t.Error("a status sub-resource is a reading, not a setting")
	}
	if !s.Readable("/misc/luts") {
		t.Error("/misc/luts is declared — it is the one a tree walk cannot find")
	}
}

func TestParseSpecAgainstTheShufflersOwnDocument(t *testing.T) {
	// A second product, a second generator: 3.1.1 where the bridge is
	// 3.1.2, no version segment in its paths, and its own model. The
	// reader has to take both, because one connector serves both
	// fleets — and if EVS changes the shape again, this fails here.
	doc, err := os.ReadFile(filepath.Join("testdata", "SHUFFLE@2.0.0-openapi.yml"))
	if err != nil {
		t.Skipf("no committed shuffler spec: %v", err)
	}
	s, err := ParseSpec(doc)
	if err != nil {
		t.Fatalf("the shuffler's document must parse: %v", err)
	}
	// Counted from the file, so this asserts the reader against the
	// document rather than against itself.
	if got := len(s.With(GET)); got != 55 {
		t.Errorf("GETs = %d, want 55", got)
	}
	if got := len(s.With(PUT)); got != 20 {
		t.Errorf("PUTs = %d, want 20", got)
	}
	// Its paths carry no /v1, and its matrix is spelled differently
	// from the bridge's — which is the whole reason the model is read
	// from the device rather than hardcoded.
	if !s.Readable("/matrices/audio/info") {
		t.Error("the shuffler's matrix info must be readable")
	}
	if s.Readable("/matrix/audio/info") {
		t.Error("that is the BRIDGE's spelling — this device does not serve it")
	}
	if !s.Writable("/matrices/audio/state/main") {
		t.Error("the shuffler's crosspoint map must be writable")
	}
	// And a per-channel resource the bridge does not have at all.
	if !s.Readable("/io/ip/receivers/audio/{uuid}/channels/{channelUuid}") {
		t.Error("the shuffler routes audio per channel")
	}
}
