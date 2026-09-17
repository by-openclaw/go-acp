package registry

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"
)

// A subscriber's socket is opened at one wire minor, so every grain
// it receives is re-encoded for that minor. A body the codec cannot
// read, and a minor no codec is registered for, both leave the grain
// as it was rather than dropping it.
func TestReencodeChangeForTheSocketsMinor(t *testing.T) {
	bodies := map[is04.ResourceType]any{
		is04.ResourceNode:     validNode(fxNode),
		is04.ResourceDevice:   validDevice(fxDevice, fxNode),
		is04.ResourceSource:   validSource(fxSource, fxDevice),
		is04.ResourceFlow:     validFlow(fxFlow, fxSource, fxDevice),
		is04.ResourceSender:   validSender(fxSender, fxDevice),
		is04.ResourceReceiver: validReceiver(fxReceiver, fxDevice),
	}
	for rt, body := range bodies {
		raw := mustJSONBytes(t, body)
		c := Change{Kind: ChangeUpdated, ResourceType: rt, ID: "x", Pre: raw, Post: raw}

		got := reencodeChange(c, "v1.3")
		if !json.Valid(got.Post) || len(got.Post) == 0 {
			t.Errorf("%s post = %s", rt, got.Post)
		}
		if len(got.Pre) == 0 {
			t.Errorf("%s: both halves are re-encoded", rt)
		}

		// A body that is not this resource is passed through as-is —
		// a subscriber sees what the registry stored, never nothing.
		broken := Change{ResourceType: rt, Post: json.RawMessage(`"not an object"`)}
		if out := reencodeChange(broken, "v1.3"); string(out.Post) != `"not an object"` {
			t.Errorf("%s undecodable body = %s", rt, out.Post)
		}
	}

	// No codec for that minor: the grain is untouched.
	c := Change{ResourceType: is04.ResourceNode, Post: json.RawMessage(`{"id":"x"}`)}
	if got := reencodeChange(c, "v9.9"); string(got.Post) != `{"id":"x"}` {
		t.Errorf("unknown minor re-encoded the grain: %s", got.Post)
	}
	// A type IS-04 does not define has no encoder, and an empty half
	// stays empty.
	odd := Change{ResourceType: is04.ResourceType("gizmo"), Post: json.RawMessage(`{}`)}
	if got := reencodeChange(odd, "v1.3"); string(got.Post) != `{}` || len(got.Pre) != 0 {
		t.Errorf("unknown type = %+v", got)
	}
}

// IS-04 §5.2: a filtered subscription reports the resource's
// transitions in and out of the filter set, not the raw change. A
// resource that enters the set is `created` even though the registry
// called it an update; one that leaves is `deleted`; one outside on
// both sides is not reported at all.
func TestProjectChangeReportsFilterSetTransitions(t *testing.T) {
	inSet := json.RawMessage(`{"id":"a","label":"cam-1"}`)
	outSet := json.RawMessage(`{"id":"a","label":"cam-2"}`)
	filter := map[string][]string{"label": {"cam-1"}}

	// No filter: the change passes through untouched.
	c := Change{Kind: ChangeUpdated, Pre: outSet, Post: outSet}
	if got, ok := projectChange(c, nil); !ok || got.Kind != ChangeUpdated {
		t.Errorf("unfiltered = %+v, %v", got, ok)
	}

	for name, tc := range map[string]struct {
		in       Change
		want     ChangeKind
		reported bool
	}{
		"both sides in the set": {
			Change{Kind: ChangeUpdated, Pre: inSet, Post: inSet}, ChangeUpdated, true,
		},
		"entering the set reads as created": {
			Change{Kind: ChangeUpdated, Pre: outSet, Post: inSet}, ChangeCreated, true,
		},
		"leaving the set reads as deleted": {
			Change{Kind: ChangeUpdated, Pre: inSet, Post: outSet}, ChangeDeleted, true,
		},
		"outside on both sides is not reported": {
			Change{Kind: ChangeUpdated, Pre: outSet, Post: outSet}, "", false,
		},
		"a sync grain stays a sync grain": {
			Change{Kind: ChangeSync, Pre: inSet, Post: inSet}, ChangeSync, true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := projectChange(tc.in, filter)
			if ok != tc.reported {
				t.Fatalf("reported = %v, want %v", ok, tc.reported)
			}
			if !ok {
				return
			}
			if got.Kind != tc.want {
				t.Errorf("kind = %s, want %s", got.Kind, tc.want)
			}
			if tc.want == ChangeCreated && len(got.Pre) != 0 {
				t.Error("a created grain carries no `pre`")
			}
			if tc.want == ChangeDeleted && len(got.Post) != 0 {
				t.Error("a deleted grain carries no `post`")
			}
		})
	}
}

// The subscription filter reads the same shape as the Query API's:
// top-level equality, plus the one RQL predicate. Anything it cannot
// read is not a match — a grain is never emitted on a guess.
func TestJSONMatchesFilter(t *testing.T) {
	body := json.RawMessage(`{"id":"a","label":"cam-1","description":"studio","port":8080}`)

	for name, tc := range map[string]struct {
		data json.RawMessage
		q    map[string][]string
		want bool
	}{
		"no filter matches":            {body, map[string][]string{}, true},
		"equality":                     {body, map[string][]string{"label": {"cam-1"}}, true},
		"any one of several values":    {body, map[string][]string{"label": {"cam-2", "cam-1"}}, true},
		"a value that is not there":    {body, map[string][]string{"label": {"cam-9"}}, false},
		"a field that is not there":    {body, map[string][]string{"nope": {"x"}}, false},
		"a field that is not a string": {body, map[string][]string{"port": {"8080"}}, false},
		"every clause must hold": {body, map[string][]string{
			"label": {"cam-1"}, "description": {"elsewhere"},
		}, false},
		"paging parameters are not filters":  {body, map[string][]string{"paging.limit": {"10"}}, true},
		"query controls are not filters":     {body, map[string][]string{"query.downgrade": {"v1.0"}}, true},
		"an RQL predicate that holds":        {body, map[string][]string{"query.rql": {"eq(label,cam-1)"}}, true},
		"an RQL predicate that does not":     {body, map[string][]string{"query.rql": {"eq(label,cam-9)"}}, false},
		"RQL against a non-string field":     {body, map[string][]string{"query.rql": {"eq(port,8080)"}}, false},
		"an expression we do not implement":  {body, map[string][]string{"query.rql": {"and(a,b)"}}, true},
		"an empty body matches nothing":      {json.RawMessage(``), map[string][]string{"label": {"cam-1"}}, false},
		"a body that is not JSON matches no": {json.RawMessage(`{not json`), map[string][]string{"label": {"cam-1"}}, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := jsonMatchesFilter(tc.data, tc.q); got != tc.want {
				t.Errorf("jsonMatchesFilter = %v, want %v", got, tc.want)
			}
		})
	}
}

// The subscription's `params` object round-trips: flattened into the
// filter map on the way in, rendered back into JSON on the way out.
func TestSubscriptionParamsRoundTrip(t *testing.T) {
	q := paramsAsQuery(map[string]any{
		"label":       "cam-1",
		"query.rql":   "eq(label,cam-1)",
		"media_types": []any{"video/raw", "video/jxsv", 7}, // the 7 is not a filter value
		"port":        8080,                                // nor is a number
	})
	if got := q["label"]; len(got) != 1 || got[0] != "cam-1" {
		t.Errorf("label = %v", got)
	}
	if got := q["media_types"]; len(got) != 2 {
		t.Errorf("media_types = %v, want the two strings", got)
	}
	if _, ok := q["port"]; ok {
		t.Error("a non-string parameter is not a filter")
	}

	if got := paramsAsQuery(nil); got != nil {
		t.Errorf("no params = %v, want nil", got)
	}
	if got := paramsAsQuery(map[string]any{"port": 8080}); got != nil {
		t.Errorf("params with nothing usable = %v, want nil", got)
	}
	if got := paramsAsQuery("not an object"); got != nil {
		t.Errorf("params that are not an object = %v, want nil", got)
	}

	back, ok := queryAsParams(map[string][]string{
		"label":       {"cam-1"},
		"media_types": {"video/raw", "video/jxsv"},
		"empty":       {},
	}).(map[string]any)
	if !ok {
		t.Fatal("queryAsParams must render a JSON object")
	}
	if back["label"] != "cam-1" {
		t.Errorf("label = %v, want the single value unwrapped", back["label"])
	}
	if vs, ok := back["media_types"].([]any); !ok || len(vs) != 2 {
		t.Errorf("media_types = %v, want the array shape", back["media_types"])
	}
	if _, ok := back["empty"]; ok {
		t.Error("a key with no values is not echoed back")
	}
	if got := queryAsParams(nil); got != nil {
		t.Errorf("queryAsParams(nil) = %v, want nil", got)
	}
}

// v1.0 predates `secure`, so a subscription served on that tree is
// rendered without it; every later minor takes the canonical struct.
// `params` is always an object, never null.
func TestSubscriptionForVersion(t *testing.T) {
	res := SubscriptionResource{
		ID: "sub-1", WSHref: "ws://host/ws", ResourcePath: "/nodes",
	}
	v10, ok := subscriptionForVersion(res, "v1.0").(map[string]any)
	if !ok {
		t.Fatal("v1.0 renders a bare object")
	}
	if _, has := v10["secure"]; has {
		t.Error("v1.0 has no `secure` field")
	}
	if params, isMap := v10["params"].(map[string]any); !isMap || params == nil {
		t.Errorf("params = %v, want an empty object", v10["params"])
	}

	v13, ok := subscriptionForVersion(res, "v1.3").(SubscriptionResource)
	if !ok {
		t.Fatal("v1.1+ take the canonical struct")
	}
	if v13.Params == nil {
		t.Error("params must never be null on the wire")
	}
}

// Ancestry is defined only where `parents` exists, and the parents of
// a body the registry cannot read are simply none.
func TestAncestryHelpers(t *testing.T) {
	for path, want := range map[string]is04.ResourceType{
		"sources":  is04.ResourceSource,
		"/sources": is04.ResourceSource,
		"/flows/":  is04.ResourceFlow,
	} {
		got, ok := ancestryTypeForPath(path)
		if !ok || got != want {
			t.Errorf("ancestryTypeForPath(%q) = %q, %v", path, got, ok)
		}
	}
	for _, path := range []string{"/nodes", "/senders", "/receivers", "/devices", ""} {
		if _, ok := ancestryTypeForPath(path); ok {
			t.Errorf("ancestry must not be defined for %q", path)
		}
	}

	if got := parentsFromBody(json.RawMessage(`{"parents":["a","b"]}`)); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("parents = %v", got)
	}
	if got := parentsFromBody(nil); got != nil {
		t.Errorf("no body = %v, want no parents", got)
	}
	if got := parentsFromBody(json.RawMessage(`{"parents":"not an array"}`)); got != nil {
		t.Errorf("a malformed parents field = %v, want no parents", got)
	}
}

// Subscription ids are opaque, but they keep the v4 UUID shape every
// IS-04 id pattern check expects.
func TestNewUUIDLikeShape(t *testing.T) {
	id, err := newUUIDLike()
	if err != nil {
		t.Fatal(err)
	}
	if !is04.IsValidUUID(id) {
		t.Errorf("newUUIDLike = %q, which is not a v1-5 UUID", id)
	}
	other, _ := newUUIDLike()
	if id == other {
		t.Error("two subscriptions must not share an id")
	}
}

// Keep-alive is one setting with a footgun: a one-way Query WS is
// silent by design, so reaping without pinging would evict every
// healthy Controller. Turning pings off turns reaping off with it.
func TestWSKeepAliveResolution(t *testing.T) {
	m := &SubscriptionManager{}
	ping, idle := m.wsKeepAlive()
	if ping != DefaultWSPingInterval || idle != DefaultWSIdleTimeout {
		t.Errorf("unset = (%v, %v), want the defaults", ping, idle)
	}

	m.SetWSKeepAlive(5*time.Second, 20*time.Second)
	if ping, idle = m.wsKeepAlive(); ping != 5*time.Second || idle != 20*time.Second {
		t.Errorf("configured = (%v, %v)", ping, idle)
	}

	m.SetWSKeepAlive(-1, 20*time.Second)
	if ping, idle = m.wsKeepAlive(); ping != 0 || idle != 0 {
		t.Errorf("pings off = (%v, %v), want reaping off too", ping, idle)
	}

	m.SetWSKeepAlive(5*time.Second, -1)
	if ping, idle = m.wsKeepAlive(); ping != 5*time.Second || idle != 0 {
		t.Errorf("reaping off = (%v, %v)", ping, idle)
	}
}
