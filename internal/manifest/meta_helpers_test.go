package manifest

import (
	"reflect"
	"testing"

	"dhs/internal/export/canonical"
)

// The meta* helpers turn the JSON-decoded (or live-walker typed) Meta
// blobs the Ember+ consumer stashes on a DM object back into canonical
// matrix / function fields. These tables pin the tolerant edges: wrong
// container shape -> nil (field absent), malformed entries -> skipped
// (never a partial row), defaults filled per spec p.88.

func TestMetaStringOr(t *testing.T) {
	m := map[string]any{"type": "nToN", "mode": "", "n": float64(1)}
	for _, tc := range []struct {
		name, key, fallback, want string
	}{
		{"present string wins", "type", "oneToN", "nToN"},
		{"empty string falls back", "mode", "linear", "linear"},
		{"missing key falls back", "absent", "linear", "linear"},
		{"non-string falls back", "n", "x", "x"},
	} {
		if got := metaStringOr(m, tc.key, tc.fallback); got != tc.want {
			t.Errorf("%s: metaStringOr = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestMetaInt64(t *testing.T) {
	for _, tc := range []struct {
		name   string
		in     any
		want   int64
		wantOK bool
	}{
		{"int64 (live walker)", int64(4), 4, true},
		{"int (live walker)", 5, 5, true},
		{"int32 (live walker)", int32(6), 6, true},
		{"float64 (json)", float64(7), 7, true},
		{"string is not a number", "8", 0, false},
		{"nil is not a number", nil, 0, false},
	} {
		got, ok := metaInt64(tc.in)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("%s: metaInt64(%#v) = (%d, %v), want (%d, %v)", tc.name, tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestMetaToMatrixLabels(t *testing.T) {
	if got := metaToMatrixLabels("not an array"); got != nil {
		t.Errorf("non-array = %+v, want nil", got)
	}
	got := metaToMatrixLabels([]any{
		map[string]any{"basePath": "1.2.1", "description": "Primary"},
		"garbage",
		map[string]any{"basePath": "1.2.2"},
	})
	if len(got) != 2 {
		t.Fatalf("labels = %+v, want 2 (garbage entry skipped)", got)
	}
	if got[0].BasePath != "1.2.1" || got[0].Description == nil || *got[0].Description != "Primary" {
		t.Errorf("labels[0] = %+v", got[0])
	}
	if got[1].BasePath != "1.2.2" || got[1].Description != nil {
		t.Errorf("labels[1] = %+v, want no description pointer for an empty description", got[1])
	}
}

func TestMetaToMatrixTargetsAndSources(t *testing.T) {
	if metaToMatrixTargets(nil) != nil || metaToMatrixSources(map[string]any{}) != nil {
		t.Error("non-array meta must yield nil")
	}
	in := []any{float64(3), "six", float64(9)}
	if got := metaToMatrixTargets(in); !reflect.DeepEqual(got, []canonical.MatrixTarget{{Number: 3}, {Number: 9}}) {
		t.Errorf("targets = %+v", got)
	}
	if got := metaToMatrixSources(in); !reflect.DeepEqual(got, []canonical.MatrixSource{{Number: 3}, {Number: 9}}) {
		t.Errorf("sources = %+v", got)
	}
}

func TestMetaToMatrixConnections(t *testing.T) {
	if got := metaToMatrixConnections([]any{}); got != nil {
		t.Errorf("non-map = %+v, want nil", got)
	}
	got := metaToMatrixConnections(map[string]any{
		"bad": "not an entry",
		"3": map[string]any{
			"target": float64(3), "sources": []any{float64(6), "x", float64(7)},
			"operation": "connect", "disposition": "locked",
		},
	})
	if len(got) != 1 {
		t.Fatalf("connections = %+v, want 1 (non-map entry skipped)", got)
	}
	want := canonical.MatrixConnection{Target: 3, Sources: []int64{6, 7}, Operation: canonical.ConnOpConnect,
		Disposition: canonical.ConnDispLocked, Locked: true}
	if !reflect.DeepEqual(got[0], want) {
		t.Errorf("connection = %+v, want %+v", got[0], want)
	}
	// Minimal entry: no sources array, no operation/disposition ->
	// spec defaults absolute + tally, not locked.
	got = metaToMatrixConnections(map[string]any{"1": map[string]any{"target": float64(1)}})
	want = canonical.MatrixConnection{Target: 1, Operation: canonical.ConnOpAbsolute, Disposition: canonical.ConnDispTally}
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Errorf("minimal connection = %+v, want %+v", got, want)
	}
}

func TestMetaToLevelMap(t *testing.T) {
	typed := map[string]map[string]string{"Primary": {"3": "AES-T-3"}}
	if got := metaToLevelMap(typed); !reflect.DeepEqual(got, typed) {
		t.Errorf("live-walker typed map must pass through: %+v", got)
	}
	if got := metaToLevelMap("neither"); got != nil {
		t.Errorf("unknown shape = %+v, want nil", got)
	}
	got := metaToLevelMap(map[string]any{
		"Primary": map[string]any{"3": "AES-T-3", "4": float64(4)},
		"Broken":  "not a map",
		"Empty":   map[string]any{"9": float64(9)},
	})
	want := map[string]map[string]string{"Primary": {"3": "AES-T-3"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("level map = %+v, want %+v (non-map level and non-string labels dropped, empty rows absent)", got, want)
	}
}

func TestMetaToTuple(t *testing.T) {
	if got := metaToTuple(map[string]any{}); got != nil {
		t.Errorf("non-array = %+v, want nil", got)
	}
	got := metaToTuple([]any{map[string]any{"name": "a", "type": "integer"}, 42})
	if !reflect.DeepEqual(got, []canonical.TupleItem{{Name: "a", Type: "integer"}}) {
		t.Errorf("tuple = %+v (non-map item must be skipped)", got)
	}
}
