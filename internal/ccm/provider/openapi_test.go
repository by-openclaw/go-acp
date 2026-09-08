package ccm

import (
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
		{Path: "/v1", Methods: []string{"GET"}},
		{Path: "/v1/self", Methods: []string{"GET", "PUT"}},
		{Path: "/v1/io/ip/senders/video/{uuid}", Methods: []string{"GET", "PATCH"}},
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
