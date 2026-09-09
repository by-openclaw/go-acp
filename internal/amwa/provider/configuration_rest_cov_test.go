package provider

import (
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/ms05"
)

// propReq drives one properties-subtree request through the dispatch a
// controller reaches over REST.
func propReq(t *testing.T, s *IS14ConfigurationServer, method string, rest []string, body string) (int, any) {
	t.Helper()
	var r *stdhttp.Request
	if body == "" {
		r = httptest.NewRequest(method, "/x-nmos/configuration/v1.0/rolePaths/root/properties", nil)
	} else {
		r = httptest.NewRequest(method, "/x-nmos/configuration/v1.0/rolePaths/root/properties",
			strings.NewReader(body))
	}
	obj := s.objectByOid(1)
	status, out, err := s.dispatchProperties(method, obj, rest, r)
	if err != nil {
		t.Fatalf("dispatchProperties: %v", err)
	}
	return status, out
}

// The properties subtree is a walkable tree: the collection lists its
// property ids, a property lists its two children, and each child
// answers only the verbs it defines.
func TestDispatchPropertiesWalk(t *testing.T) {
	s := configFixture(t)

	status, body := propReq(t, s, stdhttp.MethodGet, nil, "")
	if status != 200 {
		t.Fatalf("the collection = %d (%+v)", status, body)
	}
	ids, ok := body.([]string)
	if !ok || len(ids) == 0 {
		t.Fatalf("the collection body = %T %+v", body, body)
	}
	for _, id := range ids {
		if !strings.HasSuffix(id, "/") {
			t.Errorf("a child listing entry must end in a slash: %q", id)
		}
	}

	status, body = propReq(t, s, stdhttp.MethodGet, []string{"1p6"}, "")
	if status != 200 {
		t.Fatalf("a property index = %d (%+v)", status, body)
	}
	if children, ok := body.([]string); !ok || len(children) != 2 {
		t.Errorf("a property lists descriptor/ and value/: %+v", body)
	}

	if status, _ := propReq(t, s, stdhttp.MethodGet, []string{"1p6", "descriptor"}, ""); status != 200 {
		t.Errorf("the descriptor = %d", status)
	}
	if status, _ := propReq(t, s, stdhttp.MethodGet, []string{"1p6", "value"}, ""); status != 200 {
		t.Errorf("the value = %d", status)
	}

	// A write goes through the same gate as every other property write.
	status, _ = propReq(t, s, stdhttp.MethodPut, []string{"1p6", "value"}, `{"value":"renamed"}`)
	if status != 200 {
		t.Errorf("a value PUT = %d", status)
	}
	_, body = propReq(t, s, stdhttp.MethodGet, []string{"1p6", "value"}, "")
	got, ok := body.(ms05.NcMethodResultPropertyValue)
	if !ok || string(got.Value) != `"renamed"` {
		t.Errorf("read back %+v, want the value just written", body)
	}
}

// Every refusal in the properties subtree names what was wrong: the
// verb, the property, or the path below it.
func TestDispatchPropertiesRefusals(t *testing.T) {
	s := configFixture(t)

	for name, tc := range map[string]struct {
		method string
		rest   []string
		body   string
		want   int
	}{
		"a write to the collection":    {stdhttp.MethodPut, nil, "", 405},
		"a write to a property index":  {stdhttp.MethodPut, []string{"1p6"}, "", 405},
		"a write to a descriptor":      {stdhttp.MethodPut, []string{"1p6", "descriptor"}, "", 405},
		"a verb value does not define": {stdhttp.MethodDelete, []string{"1p6", "value"}, "", 405},
		"a property the object does not have": {
			stdhttp.MethodGet, []string{"9p9"}, "", 404,
		},
		"a path below the property that names nothing": {
			stdhttp.MethodGet, []string{"1p6", "value", "extra"}, "", 404,
		},
		"a PUT whose body is not a value request": {
			stdhttp.MethodPut, []string{"1p6", "value"}, `{`, 400,
		},
		"a PUT of a value the property refuses": {
			stdhttp.MethodPut, []string{"1p6", "value"}, `{"value":42}`, 400,
		},
	} {
		t.Run(name, func(t *testing.T) {
			status, body := propReq(t, s, tc.method, tc.rest, tc.body)
			if status != tc.want {
				t.Errorf("= %d (%+v), want %d", status, body, tc.want)
			}
		})
	}
}
