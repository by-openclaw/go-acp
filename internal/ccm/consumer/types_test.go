package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"dhs/internal/ccm/codec"
	dhsc "dhs/internal/consumer"
)

// A device whose openapi.yml carries schemas: an enumeration, bounds,
// a length, a read-only field (in the GET schema, in no write schema),
// a resource written by PATCH and one whose field sits inside an array.
const specWithSchemas = `openapi: 3.1.1
paths:
  /self:
    get:
      operationId: GetSelf
  /thing:
    get:
      responses:
        '200':
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ThingRead'
    put:
      requestBody:
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/ThingWrite'
  /patchy:
    get:
      responses:
        '200':
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ThingRead'
    put:
      requestBody:
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/ThingWrite'
    patch:
      requestBody:
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/ThingWrite'
  /arr:
    get:
      responses:
        '200':
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Arr'
    patch:
      requestBody:
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Arr'
components:
  schemas:
    ThingWrite:
      type: object
      properties:
        mode:
          type: string
          enum:
            - A
            - B
        level:
          type: integer
          minimum: 0
          maximum: 10
        label:
          type: string
          maxLength: 4
        ratio:
          type: number
          minimum: 0
          maximum: 1
        nested:
          type: object
          properties:
            depth:
              type: integer
              minimum: -5
              maximum: 5
    ThingRead:
      allOf:
        - $ref: '#/components/schemas/ThingWrite'
        - type: object
          properties:
            serial:
              type: string
    Arr:
      type: object
      properties:
        legs:
          type: array
          items:
            type: object
            properties:
              ip:
                type: string
`

type typedDevice struct {
	srv    *httptest.Server
	mu     sync.Mutex
	docs   map[string]string
	writes []string // "METHOD path body"
	gets   []string // every path read
	status int      // non-zero: every write answers this status with the body below
	answer string
}

func newTypedDevice(t *testing.T) *typedDevice {
	t.Helper()
	d := &typedDevice{docs: map[string]string{
		"/self":   `{"app":{"productName":"X","productVersion":"1"}}`,
		"/thing":  `{"mode":"A","level":3,"label":"ab","ratio":0.5,"nested":{"depth":1},"serial":"S1"}`,
		"/patchy": `{"mode":"A","level":3,"label":"ab","ratio":0.5,"nested":{"depth":1},"serial":"S1"}`,
		"/arr":    `{"legs":[{"ip":"1.1.1.1"},{"ip":"2.2.2.2"}]}`,
	}}
	d.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			d.gets = append(d.gets, r.URL.Path)
			if r.URL.Path == "/docs/api.yml" {
				_, _ = w.Write([]byte(specWithSchemas))
				return
			}
			body, ok := d.docs[r.URL.Path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(body))
		case http.MethodPut, http.MethodPatch:
			b, _ := io.ReadAll(r.Body)
			d.writes = append(d.writes, r.Method+" "+r.URL.Path+" "+string(b))
			if d.status != 0 {
				w.WriteHeader(d.status)
				_, _ = w.Write([]byte(d.answer))
				return
			}
			if r.Method == http.MethodPut {
				d.docs[r.URL.Path] = string(b)
			} else {
				var cur, part map[string]any
				_ = json.Unmarshal([]byte(d.docs[r.URL.Path]), &cur)
				_ = json.Unmarshal(b, &part)
				for k, v := range part {
					cur[k] = v
				}
				merged, _ := json.Marshal(cur)
				d.docs[r.URL.Path] = string(merged)
			}
			_, _ = w.Write([]byte(d.docs[r.URL.Path]))
		}
	}))
	t.Cleanup(d.srv.Close)
	restore := dialClient
	dialClient = func(string) *Client { return testClient(d.srv) }
	t.Cleanup(func() { dialClient = restore })
	return d
}

// requests is every path the device was asked to read.
func (d *typedDevice) requests() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.gets...)
}

func (d *typedDevice) written() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.writes...)
}

func TestTheWalkCarriesWhatTheSchemaSaysAboutEachLeaf(t *testing.T) {
	d := newTypedDevice(t)
	p := testPluginConnected(t, d.srv)
	objs, err := p.Walk(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]dhsc.Object{}
	for _, o := range objs {
		by[strings.Join(o.Path, ".")] = o
	}
	if o := by["thing.mode"]; strings.Join(o.EnumItems, ",") != "A,B" || o.Access&accessWrite == 0 {
		t.Errorf("thing.mode = %+v", o)
	}
	if o := by["thing.level"]; o.Min != int64(0) || o.Max != int64(10) {
		t.Errorf("thing.level bounds = %v..%v (%T)", o.Min, o.Max, o.Min)
	}
	if o := by["thing.ratio"]; o.Min != 0.0 || o.Max != 1.0 {
		t.Errorf("thing.ratio bounds = %v..%v (%T)", o.Min, o.Max, o.Min)
	}
	if o := by["thing.label"]; o.MaxLen != 4 {
		t.Errorf("thing.label = %+v", o)
	}
	if o := by["thing.nested.depth"]; o.Min != int64(-5) {
		t.Errorf("thing.nested.depth = %+v", o)
	}
	if o := by["thing.serial"]; o.Access&accessWrite != 0 {
		t.Errorf("serial is in the GET schema only: read-only (CCM §14.2): %+v", o)
	}
	if o := by["arr.legs.1.ip"]; o.Access&accessWrite == 0 {
		t.Errorf("arr.legs.1.ip = %+v", o)
	}
	v, err := p.GetValue(context.Background(), dhsc.ValueRequest{Path: "thing.level"})
	if err != nil || v.Int != 3 {
		t.Errorf("get = %+v, %v", v, err)
	}
}

func TestAWriteTheSchemaForbidsNeverReachesTheWire(t *testing.T) {
	d := newTypedDevice(t)
	p := testPluginConnected(t, d.srv)
	ctx := context.Background()
	cases := []struct {
		path, value, want string
		is                error
	}{
		{"thing.serial", "S2", "read-only", dhsc.ErrValidationFailed},
		{"thing.mode", "C", "not one of A, B", dhsc.ErrValidationFailed},
		{"thing.level", "11", "11 > 10", dhsc.ErrOutOfRangeHigh},
		{"thing.level", "-1", "-1 < 0", dhsc.ErrOutOfRangeLow},
		{"thing.label", "abcde", "longer than 4", dhsc.ErrValidationFailed},
		{"thing.level", "x", "not a number", dhsc.ErrValidationFailed},
		{"thing.nested.depth", "6", "6 > 5", dhsc.ErrOutOfRangeHigh},
	}
	for _, c := range cases {
		_, err := p.SetValue(ctx, dhsc.ValueRequest{Path: c.path}, dhsc.Value{Str: c.value})
		if err == nil || !strings.Contains(err.Error(), c.want) || !errors.Is(err, c.is) {
			t.Errorf("set %s=%s: err = %v, want %q wrapping %v", c.path, c.value, err, c.want, c.is)
		}
	}
	if w := d.written(); len(w) != 0 {
		t.Errorf("nothing may be sent for a refused value, got %v", w)
	}
	// A value the schema allows goes through.
	v, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "thing.level"}, dhsc.Value{Str: "7"})
	if err != nil || v.Int != 7 {
		t.Errorf("set within bounds = %+v, %v", v, err)
	}
	if w := d.written(); len(w) != 1 || !strings.HasPrefix(w[0], "PUT /thing ") {
		t.Errorf("BRIDGE-style resource is PUT whole: %v", w)
	}
}

func TestAResourceDeclaringPATCHGetsOnlyTheFieldsNamed(t *testing.T) {
	d := newTypedDevice(t)
	p := testPluginConnected(t, d.srv)
	ctx := context.Background()
	v, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "patchy.level"}, dhsc.Value{Str: "5"})
	if err != nil || v.Int != 5 {
		t.Fatalf("set = %+v, %v", v, err)
	}
	vals, err := p.SetValues(ctx,
		[]dhsc.ValueRequest{{Path: "patchy.nested.depth"}, {Path: "patchy.mode"}},
		[]dhsc.Value{{Str: "-2"}, {Str: "B"}})
	if err != nil || vals[0].Int != -2 || vals[1].Str != "B" {
		t.Fatalf("set values = %+v, %v", vals, err)
	}
	// A field inside an array has no partial form: PUT of the whole document.
	if _, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "arr.legs.0.ip"}, dhsc.Value{Str: "3.3.3.3"}); err != nil {
		t.Fatalf("array set: %v", err)
	}
	w := d.written()
	if len(w) != 3 {
		t.Fatalf("writes = %v", w)
	}
	if w[0] != `PATCH /patchy {"level":5}` {
		t.Errorf("one field: %s", w[0])
	}
	if w[1] != `PATCH /patchy {"mode":"B","nested":{"depth":-2}}` {
		t.Errorf("two fields, nested as the document nests them: %s", w[1])
	}
	if !strings.HasPrefix(w[2], "PUT /arr ") || !strings.Contains(w[2], `"3.3.3.3"`) || !strings.Contains(w[2], `"2.2.2.2"`) {
		t.Errorf("array field falls back to the whole document: %s", w[2])
	}
}

func TestARefusedPATCHIsReportedAsTheDeviceSaidIt(t *testing.T) {
	d := newTypedDevice(t)
	p := testPluginConnected(t, d.srv)
	ctx := context.Background()
	d.mu.Lock()
	d.status, d.answer = 400, `{"message":"licenses not loaded yet"}`
	d.mu.Unlock()
	if _, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "patchy.level"}, dhsc.Value{Str: "5"}); err == nil || !strings.Contains(err.Error(), "400: licenses not loaded yet") {
		t.Errorf("device message: %v", err)
	}
	d.mu.Lock()
	d.status, d.answer = 500, "boom"
	d.mu.Unlock()
	if _, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "patchy.level"}, dhsc.Value{Str: "5"}); err == nil || !strings.Contains(err.Error(), "answered 500") {
		t.Errorf("bare status: %v", err)
	}
	c := testClient(d.srv)
	d.srv.Close()
	if err := c.patch(ctx, "/patchy", map[string]any{"level": 1}); err == nil || !strings.Contains(err.Error(), "PATCH /patchy") {
		t.Errorf("transport failure: %v", err)
	}
}

func TestValueTextAndPartialEdges(t *testing.T) {
	cases := []struct {
		v    dhsc.Value
		want string
	}{
		{dhsc.Value{Kind: dhsc.KindBool, Bool: true}, "true"},
		{dhsc.Value{Kind: dhsc.KindInt, Int: -3}, "-3"},
		{dhsc.Value{Kind: dhsc.KindUint, Uint: 9}, "9"},
		{dhsc.Value{Kind: dhsc.KindFloat, Float: 0.25}, "0.25"},
		{dhsc.Value{Kind: dhsc.KindInt, Int: 4, Str: "4 "}, "4 "},
		{dhsc.Value{Str: "B"}, "B"},
	}
	for _, c := range cases {
		if got := valueText(c.v); got != c.want {
			t.Errorf("valueText(%+v) = %q, want %q", c.v, got, c.want)
		}
	}
	doc := map[string]any{"a": map[string]any{"b": 1}, "s": "x"}
	if _, ok := partial(doc, [][]string{{"missing"}}); ok {
		t.Error("a field the document lacks has no partial")
	}
	if _, ok := partial(doc, [][]string{{"s", "deeper"}}); ok {
		t.Error("a path through a scalar has no partial")
	}
	if part, ok := partial(doc, [][]string{{"a", "b"}}); !ok || part["a"].(map[string]any)["b"] != 1 {
		t.Errorf("partial = %v %v", part, ok)
	}
}

func TestTypingSkipsWhatTheSpecDoesNotDeclare(t *testing.T) {
	spec, err := codec.ParseSpec([]byte(specWithSchemas))
	if err != nil {
		t.Fatal(err)
	}
	// The resource's own leaf (a text body) and a leaf of a resource
	// the spec does not declare keep what the JSON said.
	objs := []dhsc.Object{{Path: []string{"thing"}}, {Path: []string{"thing", "mode"}}}
	typeObjects(spec, "/thing", objs)
	if len(objs[0].EnumItems) != 0 || strings.Join(objs[1].EnumItems, ",") != "A,B" {
		t.Errorf("typeObjects = %+v", objs)
	}
	other := []dhsc.Object{{Path: []string{"nope", "x"}}}
	typeObjects(spec, "/nope", other)
	if len(other[0].EnumItems) != 0 {
		t.Errorf("an undeclared resource is not typed: %+v", other)
	}
	if err := checkWrite(spec, "/nope", []string{"x"}, dhsc.Value{Str: "1"}); err != nil {
		t.Errorf("an undeclared resource has nothing to check against: %v", err)
	}
}
