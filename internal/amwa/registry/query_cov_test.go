package registry

import (
	"reflect"
	"testing"

	"dhs/internal/amwa/codec/is04"
)

// The Query API's RQL support is deliberately one predicate wide:
// `eq(field,value)`. Everything else parses to nil and is ignored
// rather than answered with an error, so a Controller that sends a
// richer expression gets the unfiltered collection, not a failure.
func TestParseRQLEq(t *testing.T) {
	for _, tc := range []struct {
		expr         string
		field, value string
		want         bool
	}{
		{"eq(label,cam-1)", "label", "cam-1", true},
		{"  eq( label , cam-1 )  ", "label", "cam-1", true},
		{"eq(label,)", "label", "", true},
		{"eq(,cam-1)", "", "", false},   // no field to compare
		{"eq(label)", "", "", false},    // no comma
		{"ne(label,x)", "", "", false},  // only eq is implemented
		{"eq(label,x", "", "", false},   // unbalanced
		{"", "", "", false},             // empty
		{"and(eq(a,b))", "", "", false}, // composite RQL
	} {
		got := parseRQLEq(tc.expr)
		if (got != nil) != tc.want {
			t.Errorf("parseRQLEq(%q) = %+v, want parsed=%v", tc.expr, got, tc.want)
			continue
		}
		if got != nil && (got.Field != tc.field || got.Value != tc.value) {
			t.Errorf("parseRQLEq(%q) = %+v, want {%q %q}", tc.expr, got, tc.field, tc.value)
		}
	}
}

// rqlMatch reads the predicate's field off the resource itself,
// including through the embedded resource_core.
func TestRQLMatchReadsTheResource(t *testing.T) {
	node := validNode(fxNode)
	node.Label = "cam-1"
	rv := reflect.ValueOf(node)

	if !rqlMatch(rv, &rqlEq{Field: "label", Value: "cam-1"}) {
		t.Error("a matching label must select the resource")
	}
	if rqlMatch(rv, &rqlEq{Field: "label", Value: "cam-2"}) {
		t.Error("a different label must not select it")
	}
	if rqlMatch(rv, &rqlEq{Field: "not_a_field", Value: "x"}) {
		t.Error("a field the resource does not carry reads as no match")
	}
	// A field that is absent reads as the empty string, so
	// `eq(not_a_field,)` selects everything. Absent and empty are the
	// same value to the reflect reader — documented, not asserted as
	// desirable.
	if !rqlMatch(rv, &rqlEq{Field: "not_a_field", Value: ""}) {
		t.Error("an absent field reads as empty")
	}
}

// splitFilterParams keeps the field filters and the one RQL
// predicate, and drops the paging and query control parameters — left
// in, they would be matched against resource JSON and select nothing.
func TestSplitFilterParams(t *testing.T) {
	flat, rql := splitFilterParams(map[string][]string{
		"label":             {"cam-1"},
		"description":       {"studio"},
		"paging.limit":      {"10"},
		"paging.since":      {"0:0"},
		"query.downgrade":   {"v1.0"},
		"query.rql":         {"eq(id," + fxNode + ")"},
		"query.ancestry_id": {fxNode},
	})
	if len(flat) != 2 || flat["label"][0] != "cam-1" || flat["description"][0] != "studio" {
		t.Errorf("flat filters = %v, want label + description only", flat)
	}
	if rql == nil || rql.Field != "id" || rql.Value != fxNode {
		t.Errorf("rql = %+v, want the parsed predicate", rql)
	}

	// A request that carries nothing but control parameters filters
	// nothing — nil, so matchesAll short-circuits.
	flat, rql = splitFilterParams(map[string][]string{
		"paging.limit": {"10"},
		"query.rql":    {"nope"},
	})
	if flat != nil {
		t.Errorf("flat = %v, want nil when no field filter was asked for", flat)
	}
	if rql != nil {
		t.Errorf("rql = %+v, want nil for an expression we do not implement", rql)
	}

	// query.rql present but empty is not a predicate.
	if _, rql := splitFilterParams(map[string][]string{"query.rql": {}}); rql != nil {
		t.Errorf("empty query.rql = %+v, want nil", rql)
	}
}

// matchesField answers on what the resource actually carries: a
// pointer is followed, a non-struct is no match, and any one of the
// requested values selects.
func TestMatchesField(t *testing.T) {
	node := validNode(fxNode)
	node.Label = "cam-1"

	if !matchesField(reflect.ValueOf(&node), "label", []string{"cam-2", "cam-1"}) {
		t.Error("a pointer must be followed, and any one value selects")
	}
	if matchesField(reflect.ValueOf("not-a-struct"), "label", []string{"cam-1"}) {
		t.Error("a non-struct cannot match a field")
	}
	if !matchesAll(reflect.ValueOf(node), nil) {
		t.Error("no filter means everything matches")
	}
	if matchesAll(reflect.ValueOf(node), map[string][]string{"label": {"cam-2"}}) {
		t.Error("a filter that does not match must reject")
	}
}

// IS-04 §6.1.5 version isolation rests on comparing `vMAJOR.MINOR`
// strings. A string that is not one fails permissively — the registry
// serves the resource rather than 404-ing on a parse.
func TestAPIVersionOrdering(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"v1.0", "v1.3", true},
		{"v1.3", "v1.3", true},
		{"v1.3", "v1.0", false},
		{"v1.3", "v2.0", true},
		{"v2.0", "v1.3", false},
		{"", "v1.3", true},         // unparseable: permissive
		{"1.3", "v1.3", true},      // no leading v
		{"vx.3", "v1.3", true},     // non-numeric major
		{"v1.x", "v1.3", true},     // non-numeric minor
		{"v13", "v1.3", true},      // no dot
		{"v.3", "v1.3", true},      // empty major
		{"v1.3", "nonsense", true}, // unparseable on the right
	} {
		if got := apiVerLE(tc.a, tc.b); got != tc.want {
			t.Errorf("apiVerLE(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}

	for _, tc := range []struct {
		resourceVer, urlVer, downgrade string
		want                           bool
	}{
		{"v1.3", "v1.3", "", true},  // exact
		{"v1.0", "v1.3", "", false}, // no implicit downgrade
		{"v1.0", "v1.3", "v1.0", true},
		{"v1.0", "v1.3", "v1.2", false}, // below the opt-in floor
		{"v1.3", "v1.2", "v1.0", false}, // above the URL's minor
		{"", "v1.3", "", true},          // unstamped: any version
		{"v1.0", "", "", true},          // no URL minor: any version
	} {
		if got := versionAllowed(tc.resourceVer, tc.urlVer, tc.downgrade); got != tc.want {
			t.Errorf("versionAllowed(%q, %q, %q) = %v, want %v",
				tc.resourceVer, tc.urlVer, tc.downgrade, got, tc.want)
		}
	}
}

// encodeForVersion renders a resource in the URL minor's wire shape,
// and reports (nil, false) — the caller's cue to fall back to the
// canonical marshal — when there is no codec for that minor or the
// value is not the type the caller claimed.
func TestEncodeForVersion(t *testing.T) {
	bodies := map[is04.ResourceType]any{
		is04.ResourceNode:     validNode(fxNode),
		is04.ResourceDevice:   validDevice(fxDevice, fxNode),
		is04.ResourceSource:   validSource(fxSource, fxDevice),
		is04.ResourceFlow:     validFlow(fxFlow, fxSource, fxDevice),
		is04.ResourceSender:   validSender(fxSender, fxDevice),
		is04.ResourceReceiver: validReceiver(fxReceiver, fxDevice),
	}
	for rt, body := range bodies {
		raw, ok := encodeForVersion(rt, body, "v1.3")
		if !ok || len(raw) == 0 {
			t.Errorf("encodeForVersion(%s, v1.3) = %q, %v", rt, raw, ok)
		}
		// The declared type and the value must agree: a Node handed
		// in as a Flow is refused rather than mis-encoded.
		if _, ok := encodeForVersion(rt, "not a resource", "v1.3"); ok {
			t.Errorf("encodeForVersion(%s) accepted a string body", rt)
		}
		// No codec is registered for a minor that does not exist.
		if _, ok := encodeForVersion(rt, body, "v9.9"); ok {
			t.Errorf("encodeForVersion(%s, v9.9) reported success", rt)
		}
	}

	// A type IS-04 does not define has no encoder at all.
	if _, ok := encodeForVersion(is04.ResourceType("gizmo"), validNode(fxNode), "v1.3"); ok {
		t.Error("encodeForVersion accepted an unknown resource type")
	}
}

// getResource is the per-id GET's typed lookup: it answers for every
// resource type the registry holds, and reports a miss for an id it
// does not — or a type IS-04 never defined.
func TestGetResourceEveryType(t *testing.T) {
	s := populated(t)
	for rt, id := range map[is04.ResourceType]string{
		is04.ResourceNode:     fxNode,
		is04.ResourceDevice:   fxDevice,
		is04.ResourceSource:   fxSource,
		is04.ResourceFlow:     fxFlow,
		is04.ResourceSender:   fxSender,
		is04.ResourceReceiver: fxReceiver,
	} {
		body, ok := getResource(s, rt, id)
		if !ok || body == nil {
			t.Errorf("getResource(%s, %s) = %v, %v", rt, id, body, ok)
		}
		if _, ok := getResource(s, rt, fxAbsent); ok {
			t.Errorf("getResource(%s, absent) reported a hit", rt)
		}
	}
	if _, ok := getResource(s, is04.ResourceType("gizmo"), fxNode); ok {
		t.Error("getResource accepted an unknown resource type")
	}
}

// singularFromPlural maps a URL collection back to its type, and
// refuses a segment that names no IS-04 collection.
func TestSingularFromPlural(t *testing.T) {
	for plural, want := range map[string]is04.ResourceType{
		"nodes":     is04.ResourceNode,
		"devices":   is04.ResourceDevice,
		"sources":   is04.ResourceSource,
		"flows":     is04.ResourceFlow,
		"senders":   is04.ResourceSender,
		"receivers": is04.ResourceReceiver,
	} {
		got, ok := singularFromPlural(plural)
		if !ok || got != want {
			t.Errorf("singularFromPlural(%q) = %q, %v; want %q", plural, got, ok, want)
		}
	}
	if _, ok := singularFromPlural("subscriptions"); ok {
		t.Error("subscriptions is not an IS-04 resource collection")
	}
}

// defaultHostname is the seam registry.New resolves its advertised
// name through; on any host it answers with a name or an error, never
// both empty and nil.
func TestDefaultHostname(t *testing.T) {
	name, err := defaultHostname()
	if err == nil && name == "" {
		t.Error("defaultHostname returned neither a name nor an error")
	}
}
