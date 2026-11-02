package codec

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "testdata", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func num(s string) *json.Number {
	n := json.Number(s)
	return &n
}

func TestDecodeEnvelope(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantErr  error
		hasInfo  bool
		status   string
		deviceEr bool
	}{
		{name: "ok", body: `{"data":{"a":1},"info":{"status":"ok","errors":[],"warnings":[],"notes":[]}}`, hasInfo: true, status: "ok"},
		{name: "failed", body: `{"info":{"status":"failed","errors":[{"status":-1,"source":"a","detail":"b"}]}}`, hasInfo: true, status: "failed", deviceEr: true},
		{name: "other status", body: `{"info":{"status":"busy"}}`, hasInfo: true, status: "busy", deviceEr: true},
		{name: "empty status", body: `{"info":{}}`, hasInfo: true, status: "", deviceEr: true},
		{name: "no info", body: `{"data":{}}`},
		{name: "html", body: `<html>`, wantErr: ErrNotJSON},
		{name: "empty body", body: ``, wantErr: ErrNotJSON},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, err := DecodeEnvelope([]byte(tt.body))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if env.HasInfo != tt.hasInfo || env.Info.Status != tt.status {
				t.Fatalf("env = %+v", env)
			}
			if got := env.Err() != nil; got != tt.deviceEr {
				t.Fatalf("Err() = %v, want error %v", env.Err(), tt.deviceEr)
			}
		})
	}
}

func TestStatusErrorText(t *testing.T) {
	env, _ := DecodeEnvelope([]byte(`{"info":{"status":"failed","errors":[{"status":-1,"source":"auth","detail":"denied"},"plain",7]}}`))
	var se *StatusError
	if !errors.As(env.Err(), &se) {
		t.Fatal("no StatusError")
	}
	if se.Errors[0].Status != "-1" {
		t.Errorf("status %q", se.Errors[0].Status)
	}
	got := se.Error()
	for _, want := range []string{`"failed"`, "auth: denied", "plain", "7"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q lacks %q", got, want)
		}
	}
	bare := (&StatusError{Status: "failed"}).Error()
	if !strings.Contains(bare, "no error text") {
		t.Errorf("bare: %q", bare)
	}
}

func TestLookup(t *testing.T) {
	env, _ := DecodeEnvelope([]byte(`{"data":{"dev":{"product_info":{"devname":"x"}}},"info":{"status":"ok"}}`))
	raw, ok := env.Lookup([]string{"dev", "product_info", "devname"})
	if !ok || string(raw) != `"x"` {
		t.Fatalf("%s %v", raw, ok)
	}
	for _, path := range [][]string{{"dev", "nope"}, {"dev", "product_info", "devname", "deeper"}} {
		if _, ok := env.Lookup(path); ok {
			t.Errorf("%v found", path)
		}
	}
	if _, ok := (Envelope{}).Lookup(nil); ok {
		t.Error("empty document found")
	}
}

func TestDecodeProductInfo(t *testing.T) {
	env, err := DecodeEnvelope(fixture(t, "manager_data.json"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := DecodeProductInfo(env)
	if err != nil {
		t.Fatal(err)
	}
	if p.Model != "40" || p.Serial != "1" || p.Prodname != "Virtuoso MI" || p.SwVer != "0.0.1" || p.BoardSerial != "7" {
		t.Fatalf("%+v", p)
	}
	f := p.Fields()
	if f["serialfull"] != "TST0000.0001" {
		t.Errorf("fields %v", f)
	}
	if _, has := f["inventory_id"]; has {
		t.Error("empty field kept")
	}

	none, _ := DecodeEnvelope([]byte(`{"data":{},"info":{"status":"ok"}}`))
	if _, err := DecodeProductInfo(none); err == nil {
		t.Error("no product_info accepted")
	}
	bad, _ := DecodeEnvelope([]byte(`{"data":{"dev":{"product_info":[1]}},"info":{"status":"ok"}}`))
	if _, err := DecodeProductInfo(bad); err == nil {
		t.Error("array accepted")
	}
}

func TestTargets(t *testing.T) {
	tests := []struct{ got, want string }{
		{SchemaTarget(0), "/api/schema.json"},
		{SchemaTarget(3), "/slot/3/api/schema.json"},
		{ProductInfoTarget(0), "/api/data/dev/product_info.json"},
		{ProductInfoTarget(8), "/slot/8/api/data/dev/product_info.json"},
		{DataTarget(0, nil), "/api/data"},
		{DataTarget(1, []string{"elements", "ipoutputts_coll", "500000"}), "/slot/1/api/data/elements/ipoutputts_coll/500000"},
		{DataTarget(0, []string{"network", "routev4_static_coll", "0.0.0.0:0:10"}), "/api/data/network/routev4_static_coll/0.0.0.0:0:10"},
		{DataTarget(0, []string{"a b/c"}), "/api/data/a%20b%2Fc"},
		// The guide's PATCH examples, byte for byte.
		{PatchTarget(0, []string{"network", "routev4_static_coll", "0.0.0.0:0:10", "label"}, "office network"),
			"/api/data/network/routev4_static_coll/0.0.0.0:0:10?label=office%20network"},
		{PatchTarget(1, []string{"elements", "ipoutputts_coll", "500000", "dst_coll", "0", "enable"}, "true"),
			"/slot/1/api/data/elements/ipoutputts_coll/500000/dst_coll/0?enable=true"},
		{PatchTarget(0, []string{"top"}, "a&b=c"), "/api/data?top=a%26b%3Dc"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("got %s, want %s", tt.got, tt.want)
		}
	}
	for slot, want := range map[int]bool{-1: false, 0: true, 8: true, 9: false} {
		if ValidSlot(slot) != want {
			t.Errorf("ValidSlot(%d)", slot)
		}
	}
}

func TestSplitPath(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{" / ", []string{" ", " "}[:0]},
		{"dev/product_info/devname", []string{"dev", "product_info", "devname"}},
		{"/dev/product_info/", []string{"dev", "product_info"}},
		{"dev.product_info.devname", []string{"dev", "product_info", "devname"}},
		{"network/routev4_static_coll/0.0.0.0:0:10/gw", []string{"network", "routev4_static_coll", "0.0.0.0:0:10", "gw"}},
	}
	for _, tt := range tests {
		got := SplitPath(tt.in)
		if len(got) == 0 && len(tt.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("SplitPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if JoinPath([]string{"a", "b"}) != "a/b" {
		t.Error("JoinPath")
	}
}

func TestDecodeSchemaFixtureShape(t *testing.T) {
	s, err := DecodeSchema(fixture(t, "manager_schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	coll := s.Definitions["network-routev4_static_coll"]
	if coll.Item == nil || coll.Item.Ref == "" || !coll.AllowInsert {
		t.Fatalf("collection: %+v", coll)
	}
	if ip := s.Definitions["network-ipinterface_coll"]; !ip.AutoKey {
		t.Fatalf("autoKey: %+v", ip)
	}
	reset := s.Definitions["device-rebootreq"]
	if !reset.RPC || reset.IsLeaf() {
		t.Fatalf("rpc: %+v", reset)
	}
	if got := reset.Args(); !reflect.DeepEqual(got, []string{"delay", "details", "dev"}) {
		t.Fatalf("args %v", got)
	}
	if got := s.Definitions["snmp-setcommunity"].Args(); len(got) != 0 {
		t.Fatalf("result listed as an argument: %v", got)
	}
	// A device field named "type" must be a property, not the keyword.
	if n := s.Definitions["network-ipinterface"]; n.Properties["type"] == nil || n.Type != "object" {
		t.Fatalf("type field: %+v", n)
	}
}

func TestDecodeSchemaErrors(t *testing.T) {
	if _, err := DecodeSchema([]byte(`<html>`)); !errors.Is(err, ErrNotJSON) {
		t.Errorf("html: %v", err)
	}
	if _, err := DecodeSchema([]byte(`{}`)); err == nil {
		t.Error("empty schema accepted")
	}
	if _, err := DecodeSchema([]byte(`{"properties":{"a":{"title":7}}}`)); err == nil {
		t.Error("bad node accepted")
	}
}

func TestNodeCollectionSpellings(t *testing.T) {
	tests := []struct {
		body string
		item bool
	}{
		{`{"type":"object","additionalProperties":{"type":"string"}}`, true},
		{`{"type":"array","allowInsert":true,"items":{"type":"string"}}`, true},
		{`{"type":"object","additionalProperties":false}`, false},
		{`{"type":"object","additionalProperties":{"title":7}}`, false},
		{`{"type":"string"}`, false},
	}
	for _, tt := range tests {
		var n Node
		if err := json.Unmarshal([]byte(tt.body), &n); err != nil {
			t.Fatal(err)
		}
		if (n.Item != nil) != tt.item {
			t.Errorf("%s: item %v", tt.body, n.Item)
		}
	}
	var n Node
	if err := json.Unmarshal([]byte(`{"type":{"type":"string"},"required":true}`), &n); err != nil {
		t.Fatal(err)
	}
	if n.Type != "" || n.Required != nil {
		t.Errorf("%+v", n)
	}
}

func TestResolve(t *testing.T) {
	s := &Schema{Definitions: map[string]*Node{
		"a":    {Type: "string"},
		"b":    {Ref: "#/definitions/a"},
		"loop": {Ref: "#/definitions/loop"},
	}}
	tests := []struct {
		ref string
		ok  bool
	}{
		{"#/definitions/a", true},
		{"#/a", true}, // the guide's spelling
		{"#/definitions/b", true},
		{"#/definitions/nope", false},
		{"#/definitions/loop", false},
	}
	for _, tt := range tests {
		n, _, ok := s.Resolve(&Node{Ref: tt.ref})
		if ok != tt.ok || (ok && n.Type != "string") {
			t.Errorf("%s: %+v %v", tt.ref, n, ok)
		}
	}
	if _, ref, ok := s.Resolve(&Node{Type: "string"}); !ok || ref != "" {
		t.Error("plain node")
	}
}

func TestFind(t *testing.T) {
	s, err := DecodeSchema(fixture(t, "manager_schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path string
		typ  string
		key  bool
		rpc  bool
		err  error
	}{
		{path: "dev/product_info/devname", typ: "string"},
		{path: "network/routev4_static_coll/0.0.0.0:0:10/gw", typ: "string"},
		{path: "network/routev4_static_coll/0.0.0.0:0:10/dest", typ: "string", key: true},
		{path: "network/ipinterface_coll/1/enable", typ: "boolean"},
		{path: "network/ipinterface_coll/1/stats/rx_rate", typ: "number"},
		{path: "dev/system/do_reset", typ: "object", rpc: true},
		{path: "dev/system/do_reset/delay", typ: "object", rpc: true},
		{path: "dev/nope", err: ErrNotInSchema},
		{path: "nope/x", err: ErrNotInSchema},
	}
	for _, tt := range tests {
		got, err := s.Find(SplitPath(tt.path))
		if !errors.Is(err, tt.err) {
			t.Errorf("%s: %v", tt.path, err)
			continue
		}
		if err != nil {
			continue
		}
		if got.Node.Type != tt.typ || got.Key != tt.key || got.Node.RPC != tt.rpc {
			t.Errorf("%s: %+v", tt.path, got)
		}
	}

	broken := &Schema{Properties: map[string]*Node{
		"a": {Ref: "#/definitions/gone"},
		"b": {Properties: map[string]*Node{"c": {Ref: "#/definitions/gone"}}},
	}}
	for _, p := range []string{"a", "a/x"} {
		if _, err := broken.Find(SplitPath(p)); !errors.Is(err, ErrUnresolvedRef) {
			t.Errorf("%s: %v", p, err)
		}
	}
}

func leafByPath(f Flat, path string) (Leaf, bool) {
	for _, l := range f.Leaves {
		if JoinPath(l.Path) == path {
			return l, true
		}
	}
	return Leaf{}, false
}

func TestFlattenManagerFixture(t *testing.T) {
	s, _ := DecodeSchema(fixture(t, "manager_schema.json"))
	env, _ := DecodeEnvelope(fixture(t, "manager_data.json"))
	f, err := s.Flatten(env.Data)
	if err != nil {
		t.Fatal(err)
	}
	if f.Undeclared != 0 || f.Unresolved != 0 {
		t.Fatalf("%+v", f)
	}
	if v, ok := leafByPath(f, "network/ipinterface_coll/1/mtu"); !ok || v.Value != json.Number("9000") || !v.HasValue {
		t.Errorf("collection value: %+v", v)
	}
	if v, ok := leafByPath(f, "dev/system/do_reset"); !ok || !v.RPC {
		t.Errorf("rpc: %+v", v)
	}
	if v, ok := leafByPath(f, "alarms/log_note"); !ok || v.HasValue {
		t.Errorf("declared, no data: %+v", v)
	}
	if _, ok := leafByPath(f, "network/routev4_static_coll/0.0.0.0:0:10/dest"); ok {
		t.Error("a key field absent from the item must not be listed")
	}
	if v, ok := leafByPath(f, "network/ipinterface_coll/0/gid"); !ok || !v.Key {
		t.Errorf("a key the data repeats: %+v", v)
	}
}

func TestFlattenStraysAndBrokenRefs(t *testing.T) {
	s := &Schema{
		Properties: map[string]*Node{
			"coll":   {Item: &Node{Properties: map[string]*Node{"a": {Type: "string"}}}},
			"obj":    {Properties: map[string]*Node{"a": {Type: "string"}}},
			"gone":   {Ref: "#/definitions/nope"},
			"self":   {Ref: "#/definitions/self"},
			"scalar": {Type: "string"},
		},
		Definitions: map[string]*Node{
			"self": {Properties: map[string]*Node{"next": {Ref: "#/definitions/self"}, "v": {Type: "string"}}},
		},
	}
	data := `{"coll":"not a map","obj":7,"extra":{"x":1},"self":{"next":{"v":"deep"},"v":"top"},"scalar":"s"}`
	f, err := s.Flatten([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if f.Undeclared != 3 || f.Unresolved != 1 {
		t.Fatalf("undeclared %d unresolved %d: %+v", f.Undeclared, f.Unresolved, f.Leaves)
	}
	for _, p := range []string{"coll", "obj", "extra"} {
		if l, ok := leafByPath(f, p); !ok || !l.Undeclared {
			t.Errorf("%s: %+v", p, l)
		}
	}
	if l, ok := leafByPath(f, "self/next/v"); !ok || l.Value != "deep" {
		t.Errorf("a self-reference follows the data: %+v", l)
	}
	if _, ok := leafByPath(f, "self/next/next/v"); ok {
		t.Error("a self-reference ends with the data")
	}

	if _, err := s.Flatten([]byte(`[1]`)); !errors.Is(err, ErrNotJSON) {
		t.Errorf("array document: %v", err)
	}
	if f, err := s.Flatten(nil); err != nil || len(f.Leaves) == 0 {
		t.Errorf("empty data: %+v %v", f, err)
	}
}

func TestEncodeAttr(t *testing.T) {
	enum := &Node{Type: "integer", Enum: []json.RawMessage{json.RawMessage("1"), json.RawMessage("2")}, Labels: []string{"TRAP", "INFORM"}}
	strEnum := &Node{Type: "string", Enum: []json.RawMessage{json.RawMessage(`"a"`)}}
	integer := &Node{Type: "integer", Minimum: num("1"), Maximum: num("65535")}
	float := &Node{Type: "number", Minimum: num("-12"), Maximum: num("12.5")}
	bad := &Node{Type: "number", Minimum: num("x")}
	tests := []struct {
		name string
		node *Node
		in   string
		want string
		err  error
	}{
		{"enum label", enum, "TRAP", "1", nil},
		{"enum number", enum, "2", "2", nil},
		{"enum unknown", enum, "3", "", ErrNotInEnum},
		{"string enum", strEnum, "a", "a", nil},
		{"bool true", &Node{Type: "boolean"}, "true", "true", nil},
		{"bool 1", &Node{Type: "boolean"}, "1", "true", nil},
		{"bool FALSE", &Node{Type: "boolean"}, "FALSE", "false", nil},
		{"bool 0", &Node{Type: "boolean"}, "0", "false", nil},
		{"bool bad", &Node{Type: "boolean"}, "yes", "", ErrBadValue},
		{"int", integer, "12000", "12000", nil},
		{"int low", integer, "0", "", ErrBelowMinimum},
		{"int high", integer, "65536", "", ErrAboveMaximum},
		{"int bad", integer, "1.5", "", ErrBadValue},
		{"int no limits", &Node{Type: "integer"}, "-7", "-7", nil},
		{"float", float, "1.25", "1.25", nil},
		{"float low", float, "-12.5", "", ErrBelowMinimum},
		{"float high", float, "13", "", ErrAboveMaximum},
		{"float bad", float, "abc", "", ErrBadValue},
		{"float NaN", float, "NaN", "", ErrBadValue},
		{"float Inf", float, "Inf", "", ErrBadValue},
		{"float unparsable minimum", bad, "1", "1", nil},
		{"string", &Node{Type: "string"}, "office network", "office network", nil},
		{"string empty ip", &Node{Type: "string", Format: "ipv4"}, "", "", nil},
		{"ipv4", &Node{Type: "string", Format: "ipv4"}, "192.0.2.1", "192.0.2.1", nil},
		{"ipv4 bad", &Node{Type: "string", Format: "ipv4"}, "192.0.2", "", ErrBadValue},
		{"ipv4 given v6", &Node{Type: "string", Format: "ipv4"}, "2001:db8::1", "", ErrBadValue},
		{"ipv6", &Node{Type: "string", Format: "ipv6"}, "2001:db8::1", "2001:db8::1", nil},
		{"ipv6 given v4", &Node{Type: "string", Format: "ipv6"}, "192.0.2.1", "", ErrBadValue},
		{"pattern ok", &Node{Type: "string", Pattern: `^\d+$`}, "12", "12", nil},
		{"pattern bad", &Node{Type: "string", Pattern: `^\d+$`}, "x", "", ErrBadValue},
		{"pattern go cannot compile", &Node{Type: "string", Pattern: `(?=x)`}, "y", "y", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EncodeAttr(tt.node, tt.in)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if err == nil && got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSameValue(t *testing.T) {
	tests := []struct {
		wire string
		got  any
		want bool
	}{
		{"a", "a", true},
		{"a", "b", false},
		{"true", true, true},
		{"true", false, false},
		{"1.50", json.Number("1.5"), true},
		{"2", json.Number("3"), false},
		{"x", json.Number("3"), false},
		{"1", []any{}, false},
	}
	for _, tt := range tests {
		if SameValue(tt.wire, tt.got) != tt.want {
			t.Errorf("%q %v", tt.wire, tt.got)
		}
	}
}
