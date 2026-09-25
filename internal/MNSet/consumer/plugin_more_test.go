package mnset

import (
	"context"
	"encoding/json"
	"errors"
	stdhttp "net/http"
	"strings"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/plugin"
)

func TestSetValueReadBackFailures(t *testing.T) {
	m := newModule(t)
	p := connected(t, m)
	ctx := context.Background()

	// PUT accepted, but the resource is gone on read-back: surfaced, not masked.
	m.drop["refclk"] = true
	if _, err := p.SetValue(ctx, consumer.ValueRequest{Path: "refclk.delay_req"}, consumer.Value{Str: "1"}); err == nil || !strings.Contains(err.Error(), "read-back failed") {
		t.Errorf("read-back err = %v", err)
	}
	// PUT accepted and the resource answers, but the field is gone.
	writable["shrink"] = true
	t.Cleanup(func() { delete(writable, "shrink") })
	m.docs[""] = `["shrink/","absent/","refclk/"]`
	m.docs["shrink"] = `{"a":1}`
	inner := m.ts.Config.Handler
	m.ts.Config.Handler = stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Method == stdhttp.MethodPut && strings.HasSuffix(r.URL.Path, "/shrink") {
			m.docs["shrink"] = `{"b":2}`
			_, _ = w.Write([]byte(`{}`))
			return
		}
		inner.ServeHTTP(w, r)
	})
	if _, err := p.SetValue(ctx, consumer.ValueRequest{Path: "shrink.a"}, consumer.Value{Str: "5"}); err == nil || !strings.Contains(err.Error(), "gone on read-back") {
		t.Errorf("gone on read-back err = %v", err)
	}
	// A listed, writable resource the module does not serve: the GET error is surfaced.
	writable["absent"] = true
	t.Cleanup(func() { delete(writable, "absent") })
	if _, err := p.SetValue(ctx, consumer.ValueRequest{Path: "absent.x"}, consumer.Value{Str: "5"}); err == nil || !strings.Contains(err.Error(), "GET absent") {
		t.Errorf("absent resource err = %v", err)
	}
}

func TestAssignAndLookupCorners(t *testing.T) {
	doc, _ := decodeDoc([]byte(`{"a":{"b":1},"l":[1,2],"s":"x","f":true}`))
	cases := []struct {
		path []string
		val  consumer.Value
		want string
	}{
		{nil, consumer.Value{Str: "1"}, "empty path"},
		{[]string{"nope", "x"}, consumer.Value{Str: "1"}, "parent: "},
		{[]string{"a", "zz"}, consumer.Value{Str: "1"}, "no such field"},
		{[]string{"l", "9"}, consumer.Value{Str: "1"}, "no such index"},
		{[]string{"l", "x"}, consumer.Value{Str: "1"}, "no such index"},
		{[]string{"s", "x"}, consumer.Value{Str: "1"}, "parent is a scalar"},
		{[]string{"f"}, consumer.Value{Str: "maybe"}, "not a boolean"},
		{[]string{"l", "0"}, consumer.Value{Str: "high"}, "not a number"},
	}
	for _, c := range cases {
		if _, err := assign(doc, c.path, c.val); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("assign(%v) err = %v, want %q", c.path, err, c.want)
		}
	}
	for _, missing := range [][]string{{"nope", "x"}, {"a", "zz"}, {"l", "9"}} {
		if _, err := assign(doc, missing, consumer.Value{Str: "1"}); !errors.Is(err, consumer.ErrObjectNotFound) {
			t.Errorf("assign(%v) must wrap ErrObjectNotFound, got %v", missing, err)
		}
	}
	if got, err := assign(doc, []string{"l", "1"}, consumer.Value{Str: "7"}); err != nil || got != json.Number("7") {
		t.Errorf("assign into list = %v, %v", got, err)
	}
	if v, ok := lookup(doc, []string{"l", "1"}); !ok || v != json.Number("7") {
		t.Errorf("list element after assign = %v, %v", v, ok)
	}
	if _, ok := lookup(doc, []string{"s", "deeper"}); ok {
		t.Error("lookup through a scalar must fail")
	}
	if _, ok := lookup(doc, []string{"l", "x"}); ok {
		t.Error("lookup with a non-numeric list index must fail")
	}
}

func TestLeafValueAndCoerceCorners(t *testing.T) {
	if v := leafValue(42); v.Kind != consumer.KindRaw || v.Str != "42" {
		t.Errorf("leafValue(non-json scalar) = %+v", v)
	}
	if v := leafValue(json.Number("1e400")); v.Kind != consumer.KindFloat {
		t.Errorf("leafValue(huge) = %+v", v)
	}
	if got, err := coerce(json.Number("1"), consumer.Value{Kind: consumer.KindInt, Int: -7}); err != nil || got != json.Number("-7") {
		t.Errorf("coerce typed int = %v, %v", got, err)
	}
	if got, err := coerce(json.Number("1"), consumer.Value{Kind: consumer.KindFloat, Float: 0.5}); err != nil || got != json.Number("0.5") {
		t.Errorf("coerce typed float = %v, %v", got, err)
	}
	if got, err := coerce(true, consumer.Value{Str: "YES"}); err != nil || got != true {
		t.Errorf("coerce yes = %v, %v", got, err)
	}
	if got, err := coerce(true, consumer.Value{Str: "no"}); err != nil || got != false {
		t.Errorf("coerce no = %v, %v", got, err)
	}
	if _, err := coerce([]any{}, consumer.Value{Str: "x"}); err == nil || !strings.Contains(err.Error(), "list takes a JSON array") {
		t.Errorf("coerce into list err = %v", err)
	}
	if _, err := coerce([]any{}, consumer.Value{Str: `{"a":1}`}); err == nil || !strings.Contains(err.Error(), "list takes a JSON array, not") {
		t.Errorf("coerce object into list err = %v", err)
	}
	if got, err := coerce([]any{json.Number("1")}, consumer.Value{Str: `[2,3]`}); err != nil || len(got.([]any)) != 2 {
		t.Errorf("coerce list = %v, %v", got, err)
	}
	if _, err := coerce(map[string]any{}, consumer.Value{Str: "x"}); err == nil || !strings.Contains(err.Error(), "node takes a JSON object") {
		t.Errorf("coerce into node err = %v", err)
	}
	if _, err := coerce(map[string]any{}, consumer.Value{Str: `[1]`}); err == nil || !strings.Contains(err.Error(), "node takes a JSON object, not") {
		t.Errorf("coerce list into node err = %v", err)
	}
	if _, err := coerce(42, consumer.Value{Str: "1"}); err == nil || !strings.Contains(err.Error(), "not a settable scalar") {
		t.Errorf("coerce into a non-JSON leaf err = %v", err)
	}
}

func TestSetValueOnANodeMergesOneAtomicPut(t *testing.T) {
	m := newModule(t)
	m.docs["refclk"] = `{"mode":"0","status":"3","fmt":{"a":1,"b":2,"c":"keep"}}`
	p := connected(t, m)
	got, err := p.SetValue(context.Background(), consumer.ValueRequest{Path: "refclk.fmt"}, consumer.Value{Str: `{"a":10,"b":20}`})
	if err != nil {
		t.Fatalf("set node: %v", err)
	}
	if got.Kind != consumer.KindRaw {
		t.Errorf("read-back of a node is raw, got %+v", got)
	}
	put := m.puts["refclk"]
	if !strings.Contains(put, `"a":10`) || !strings.Contains(put, `"b":20`) || !strings.Contains(put, `"c":"keep"`) || !strings.Contains(put, `"mode":"0"`) {
		t.Errorf("one PUT must carry the merged node inside the whole document: %s", put)
	}
	if strings.Count(put, `"a":`) != 1 {
		t.Errorf("merge must not duplicate keys: %s", put)
	}
}

func TestSubscribeNeedsASession(t *testing.T) {
	p := (&Factory{}).New(plugin.Deps{}).(*Plugin)
	if err := p.Subscribe(consumer.ValueRequest{}, nil); !errors.Is(err, consumer.ErrNotConnected) {
		t.Errorf("Subscribe = %v", err)
	}
	if err := p.Unsubscribe(consumer.ValueRequest{}); err != nil {
		t.Errorf("Unsubscribe of nothing = %v", err)
	}
	p.SetTimeout(time.Second)
	if p.timeout != time.Second {
		t.Error("SetTimeout")
	}
}

func TestGetValueExposesUndecodableBodyAsText(t *testing.T) {
	m := newModule(t)
	p := connected(t, m)
	v, err := p.GetValue(context.Background(), consumer.ValueRequest{Path: "broken"})
	if err != nil || v.Kind != consumer.KindString || v.Str != `{"a":` {
		t.Errorf("v = %+v, %v — a body that is not JSON is the module's text form", v, err)
	}
}

func TestClientPutSurfacesTransportError(t *testing.T) {
	m := newModule(t)
	url := m.ts.URL
	m.ts.Close()
	c := newClient("127.0.0.1", 1, 0, nil, nil, nil)
	c.base = url + apiPrefix
	if err := c.put(context.Background(), "flows", map[string]any{}); err == nil || !strings.Contains(err.Error(), "mnset PUT flows") {
		t.Errorf("err = %v", err)
	}
}

func TestDeviceMessageShapes(t *testing.T) {
	if got := deviceMessage(map[string]any{"message": "nope"}); got != "nope" {
		t.Errorf("got %q", got)
	}
	if got := deviceMessage(map[string]any{"code": 1.0}); got != "map[code:1]" {
		t.Errorf("got %q", got)
	}
	if got := deviceMessage("plain"); got != "plain" {
		t.Errorf("got %q", got)
	}
}

func TestResourceHelpers(t *testing.T) {
	for url, want := range map[string]bool{
		"flows": true, "flows/abc": true, "self/ipconfig": true, "self/information": false,
		"self": false, "sdp/abc": false, "telemetry/node": false, "": false, "route/bulk/sender/x": true,
	} {
		if got := isWritable(url); got != want {
			t.Errorf("isWritable(%q) = %v", url, got)
		}
	}
	if names, ok := listing([]any{"a/", "b/"}); !ok || strings.Join(names, ",") != "a,b" {
		t.Errorf("listing = %v %v", names, ok)
	}
	for _, notListing := range []any{[]any{}, []any{"a/", 1}, []any{"a"}, map[string]any{}, "x"} {
		if _, ok := listing(notListing); ok {
			t.Errorf("listing(%v) must be false", notListing)
		}
	}
	if got := pathElems(""); got != nil {
		t.Errorf("pathElems(root) = %v", got)
	}
	if got := pathElems("self/ipconfig"); len(got) != 2 || got[1] != "ipconfig" {
		t.Errorf("pathElems = %v", got)
	}
	cases := map[string]string{
		"flows.fee338d3.network.1.dst_ip_addr": "flows,fee338d3,network,1,dst_ip_addr",
		"flows.fee338d3.network[1].enable":     "flows,fee338d3,network,1,enable",
		" self.ipconfig.hostname ":             "self,ipconfig,hostname",
		"..":                                   "",
		"":                                     "",
	}
	for in, want := range cases {
		if got := strings.Join(splitPath(in), ","); got != want {
			t.Errorf("splitPath(%q) = %q", in, got)
		}
	}
}
