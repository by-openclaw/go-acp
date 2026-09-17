package v10

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestStripPathsHonoursEveryPathShape pins the drop-table grammar: a
// top-level key, a dotted path, a "[]" fan-out, and the three ways a
// path can point at nothing (missing key, fan-out over a non-array,
// descent into a non-object), none of which may touch anything else.
func TestStripPathsHonoursEveryPathShape(t *testing.T) {
	raw := []byte(`{"top":1,"obj":{"gone":1,"kept":2},"arr":[{"gone":1,"kept":2},{"gone":1}],"notarr":{"x":1},"str":"s","keep":true}`)
	out, err := stripPaths(raw, []string{"top", "obj.gone", "arr[].gone", "missing.x", "notarr[].x", "str.x"})
	if err != nil {
		t.Fatalf("strip: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	want := map[string]any{
		"obj":    map[string]any{"kept": 2.0},
		"arr":    []any{map[string]any{"kept": 2.0}, map[string]any{}},
		"notarr": map[string]any{"x": 1.0},
		"str":    "s",
		"keep":   true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stripped = %v\nwant       %v", got, want)
	}
}

func TestStripPathsWithoutPathsReturnsInputUntouched(t *testing.T) {
	raw := []byte(`{"a":1}`)
	out, err := stripPaths(raw, nil)
	if err != nil || !bytes.Equal(out, raw) {
		t.Fatalf("got %s, %v", out, err)
	}
}

func TestStripPathsRefusesMalformedJSON(t *testing.T) {
	_, err := stripPaths([]byte(`{`), []string{"a"})
	if err == nil || !strings.Contains(err.Error(), "strip") {
		t.Fatalf("want a strip error, got %v", err)
	}
}

// TestRemoveIgnoresNonObjectsAndEmptyPaths: the recursive step must be
// safe to call on whatever a document holds at that path.
func TestRemoveIgnoresNonObjectsAndEmptyPaths(t *testing.T) {
	remove("not an object", []string{"a"})
	m := map[string]any{"a": 1}
	remove(m, nil)
	if len(m) != 1 {
		t.Fatalf("an empty path must remove nothing, got %v", m)
	}
}

// TestDropEmptyRemovesOnlyEmptyValues: "", null, {} and [] go; a
// non-empty value under a named key stays, and keys not named are never
// touched however empty they are.
func TestDropEmptyRemovesOnlyEmptyValues(t *testing.T) {
	raw := []byte(`{"a":"","b":null,"c":{},"d":[],"e":"x","f":{"k":1},"untouched":""}`)
	out, err := dropEmpty(raw, "a", "b", "c", "d", "e", "f")
	if err != nil {
		t.Fatalf("dropEmpty: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	want := map[string]any{"e": "x", "f": map[string]any{"k": 1.0}, "untouched": ""}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// TestDropEmptyLeavesMalformedInputAlone: a body it cannot read is
// returned as-is for the schema check to refuse with a real reason.
func TestDropEmptyLeavesMalformedInputAlone(t *testing.T) {
	raw := []byte(`{`)
	out, err := dropEmpty(raw, "a")
	if err != nil || !bytes.Equal(out, raw) {
		t.Fatalf("got %s, %v", out, err)
	}
}
