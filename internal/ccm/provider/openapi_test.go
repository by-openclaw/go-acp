package ccm

import (
	"os"
	"reflect"
	"testing"
)

// The scanner reads the shape every OpenAPI generator — and the device — emits:
// paths indented two spaces under `paths:`, operations four spaces beneath.
// It must stop at the next top-level key, skip comments and blanks, ignore
// non-path keys at the path indent, and only count real HTTP verbs.
func TestSpecPathsReadsTheDeviceShape(t *testing.T) {
	spec := []byte(`openapi: '3.1.2'
info:
  title: 'Neuron'
paths:
  # the root lists its children
  /v1:
    get:
      tags: ['Root']
  /v1/self:
    get:
      operationId: GetSelf
    put:
      operationId: PutSelf

  /v1/io/ip/senders/video/{uuid}:
    GET:
    patch:
    parameters:
      - name: uuid
components:
  schemas:
    /not-a-path:
      get:
`)
	got := specPaths(spec)
	want := []apiPath{
		{Path: "/v1", Methods: []string{"GET"}, Ops: []apiOperation{{Method: "GET"}}},
		{Path: "/v1/self", Methods: []string{"GET", "PUT"}, Ops: []apiOperation{{Method: "GET"}, {Method: "PUT"}}},
		{Path: "/v1/io/ip/senders/video/{uuid}", Methods: []string{"GET", "PATCH"}, Ops: []apiOperation{{Method: "GET"}, {Method: "PATCH"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("specPaths =\n%+v\nwant\n%+v", got, want)
	}
}

// A verb line before any path, or a document with no paths block, yields
// nothing rather than a panic on an empty table.
func TestSpecPathsEdges(t *testing.T) {
	if got := specPaths([]byte("paths:\n    get:\n")); len(got) != 0 {
		t.Errorf("verb before any path produced %+v", got)
	}
	if got := specPaths([]byte("openapi: '3.1.2'\ninfo:\n  title: x\n")); len(got) != 0 {
		t.Errorf("no paths block produced %+v", got)
	}
	if got := specPaths(nil); len(got) != 0 {
		t.Errorf("nil spec produced %+v", got)
	}
}

// The contract is read from the REAL device document: PUT where the device
// declares it (200 + the resource; MatrixState on the matrix levels), no
// PATCH anywhere, GET-only on current/info/status, and no main/backup on the
// single-level output matrices. This pins the emulator to the api.yml, not
// to the 0v1 paper.
func TestContractFromTheDeviceDocument(t *testing.T) {
	spec, err := os.ReadFile("../codec/testdata/neuron-api-1.0.0.yml")
	if err != nil {
		t.Fatal(err)
	}
	c := newContract(spec)
	if got := c.writeMethods(); !reflect.DeepEqual(got, []string{"PUT"}) {
		t.Errorf("writeMethods = %v, want [PUT] (the device declares no PATCH)", got)
	}
	puts := 0
	for _, ap := range c.paths {
		if op, ok := ap.op("PUT"); ok {
			puts++
			if op.Success != 200 || op.Body == "" {
				t.Errorf("%s PUT = %+v, want 200 with a named body schema", ap.Path, op)
			}
		}
	}
	if puts != 35 {
		t.Errorf("PUT operations = %d, want 35 as in the document", puts)
	}
	op, ok := c.lookup("PUT", "/v1/matrix/audio/main")
	if !ok || op.Body != "MatrixState" || op.Success != 200 {
		t.Errorf("PUT matrix/audio/main = %+v %v, want 200 MatrixState", op, ok)
	}
	op, ok = c.lookup("PUT", "/v1/io/ip/receivers/audio/cd17dc08-f637-4b0b-a221-4e6a9d0c9530")
	if !ok || op.Body != "IpReceiverAudioPut" || op.Success != 200 {
		t.Errorf("PUT receivers/audio/{uuid} = %+v %v, want 200 IpReceiverAudioPut", op, ok)
	}
	for _, p := range []string{
		"/v1/matrix/audio/current", "/v1/matrix/audio/info", "/v1/matrix/data/output/main",
		"/v1/matrix/video/output/backup", "/v1/self", "/v1/io/ip/receivers/audio",
	} {
		if _, ok := c.lookup("PUT", p); ok {
			t.Errorf("PUT %s is declared; the device document has no such write", p)
		}
	}
	if _, ok := c.lookup("PATCH", "/v1/matrix/audio/main"); ok {
		t.Error("PATCH declared; the device document has none")
	}
	if _, ok := c.lookup("GET", "/v1/matrix/audio/main/extra"); ok {
		t.Error("a longer path must not match a shorter template")
	}
	if _, ok := c.lookup("PUT", "/v1/io/ip/receivers/audio//"); ok {
		t.Error("an empty segment must not satisfy a {uuid} template")
	}
}

// Response codes and body refs are read only in their own sections: a code
// under requestBody or a $ref under responses is ignored, a second 2xx does
// not override the first, and keys at the operation depth that are not verbs
// (parameters) open no operation.
func TestContractScannerSections(t *testing.T) {
	spec := []byte(`paths:
  /v1/x:
    parameters: []
    put:
      requestBody:
        '201':
        content:
          schema:
            $ref: "#/components/schemas/First"
            $ref: '#/components/schemas/Second'
      responses:
        $ref: '#/components/schemas/NotABody'
        '404':
        '202':
        '200':
`)
	c := newContract(spec)
	op, ok := c.lookup("PUT", "/v1/x")
	if !ok || op.Success != 202 || op.Body != "First" {
		t.Errorf("op = %+v %v, want 202 First", op, ok)
	}
	if _, ok := c.lookup("PUT", "/v1"); ok {
		t.Error("a shorter path must not match a longer template")
	}
}
