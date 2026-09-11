package provider

// IS-07 v1.0.1 Event & Tally, the parts the tally-only tests leave
// unread: the version index, the per-source index, every type document
// the spec defines (boolean / number / string / object and their enum
// forms), the WebSocket at .../ws with its subscription -> initial
// state -> health -> change sequence, and the difference between the
// REST view (source-scoped, no flow_id) and the wire view (per-flow).

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is07"
	httpsession "dhs/internal/amwa/session/http"

	// The WebSocket side exists only when an IS-07 codec is REGISTERED,
	// exactly as the shipped binary (cmd/dhs/main.go) registers it.
	// Without this the provider's own test binary served REST only and
	// the wire path a controller actually follows was never exercised.
	_ "dhs/internal/amwa/codec/is07/v10"
)

const (
	evTallyID   = "eeeeeeee-5555-4555-8555-555555555555" // tallyBundle's boolean source
	evTallyFlow = "ffffffff-6666-4666-8666-666666666666" // ... and its flow
	evNumberID  = "11111111-1111-4111-8111-111111111111"
	evStringID  = "22222222-2222-4222-8222-222222222222"
	evObjectID  = "33333333-3333-4333-8333-333333333333"
	evOddID     = "44444444-4444-4444-8444-444444444444"
	evBase      = "/x-nmos/events/v1.0"
)

func evLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// evSource is one IS-04 data Source carrying an event_type -- the rule
// IS-04 uses to mark a Source as an event source.
func evSource(id, eventType, dev string) is04.Source {
	return is04.Source{
		ResourceCore: is04.ResourceCore{
			ID: id, Version: "0:0", Label: "src-" + eventType,
			Description: eventType + " events", Tags: map[string][]string{},
		},
		Caps: map[string]any{}, DeviceID: dev, Parents: []string{},
		Format: formatData, EventType: eventType,
	}
}

// evBundle is tallyBundle plus one source per remaining IS-07 category
// (number, string, object) and one whose event_type names no category
// at all. None of the extra sources has a Flow.
func evBundle() *NodeConfig {
	b := tallyBundle()
	dev := b.Devices[0].ID
	b.Sources = append(b.Sources,
		evSource(evNumberID, "number/fader", dev),
		evSource(evStringID, "string/label", dev),
		evSource(evObjectID, "object/custom", dev),
		evSource(evOddID, "weird", dev),
	)
	return b
}

// evServer mounts the Events API for b over httptest.
func evServer(t *testing.T, b *NodeConfig) (*IS07EventsServer, *httptest.Server) {
	t.Helper()
	logger := evLogger()
	ev := NewIS07EventsServer(logger, b, IS07EventsConfig{APIVer: "v1.0"})
	t.Cleanup(func() { _ = ev.Close() })
	srv := httpsession.NewServer(logger)
	ev.Mount(srv)
	ts := httptest.NewServer(srv.MuxHandler())
	t.Cleanup(ts.Close)
	return ev, ts
}

// evGetRaw returns status + body, whatever the status.
func evGetRaw(t *testing.T, ts *httptest.Server, path string) (int, []byte) {
	t.Helper()
	resp, err := ts.Client().Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// evPayload reads the REST state of one source and returns its payload.
func evPayload(t *testing.T, ts *httptest.Server, id string) map[string]any {
	t.Helper()
	var state struct {
		MessageType string         `json:"message_type"`
		Payload     map[string]any `json:"payload"`
	}
	if code := evGet(t, ts, evBase+"/sources/"+id+"/state/", &state); code != 200 {
		t.Fatalf("state %s: got %d, want 200", id, code)
	}
	if state.MessageType != "state" {
		t.Errorf("message_type = %q, want state", state.MessageType)
	}
	return state.Payload
}

// TestEvVersionIndexNeedsNoBundle: the version tree is served even
// before there is anything to say, and a Node built from no bundle at
// all still answers the walk a controller makes -- with an empty
// source list, not a 404 halfway down.
func TestEvVersionIndexNeedsNoBundle(t *testing.T) {
	ev := NewIS07EventsServer(evLogger(), nil, IS07EventsConfig{})
	if got := ev.Versions(); len(got) != 1 || got[0] != "v1.0" {
		t.Fatalf("Versions() = %v, want the one registered IS-07 minor", got)
	}
	srv := httpsession.NewServer(evLogger())
	ev.Mount(srv)
	ts := httptest.NewServer(srv.MuxHandler())
	t.Cleanup(ts.Close)

	for _, path := range []string{"/x-nmos/events", "/x-nmos/events/"} {
		var vers []string
		if code := evGet(t, ts, path, &vers); code != 200 {
			t.Fatalf("GET %s: got %d, want 200", path, code)
		}
		if len(vers) != 1 || vers[0] != "v1.0/" {
			t.Errorf("GET %s = %v, want [v1.0/]", path, vers)
		}
	}
	var index []string
	if code := evGet(t, ts, evBase+"/", &index); code != 200 {
		t.Fatalf("GET %s/: got %d, want 200", evBase, code)
	}
	if len(index) != 1 || index[0] != "sources/" {
		t.Errorf("version index = %v, want [sources/]", index)
	}
	var ids []string
	if code := evGet(t, ts, evBase+"/sources/", &ids); code != 200 {
		t.Fatalf("GET sources/: got %d, want 200", code)
	}
	if len(ids) != 0 {
		t.Errorf("sources = %v, want none from a nil bundle", ids)
	}
	// Close tears the WebSocket side down and is idempotent.
	if err := ev.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := ev.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// TestEvSourceIndexIsTypeAndState: IS-07 mandates the per-source
// resource is exactly the two-element array ["type/", "state/"].
func TestEvSourceIndexIsTypeAndState(t *testing.T) {
	_, ts := evServer(t, evBundle())
	var index []string
	if code := evGet(t, ts, evBase+"/sources/"+evTallyID+"/", &index); code != 200 {
		t.Fatalf("source index: got %d, want 200", code)
	}
	if len(index) != 2 || index[0] != "type/" || index[1] != "state/" {
		t.Errorf("source index = %v, want [type/ state/]", index)
	}
}

// TestEvTypeDocumentFollowsEventType: with no operator-declared
// document, `type` is the plain descriptor the event_type category
// implies, and `state` is that category's defined starting value --
// a fader at 0 with scale 1, a label that is "", an object that is {}.
func TestEvTypeDocumentFollowsEventType(t *testing.T) {
	_, ts := evServer(t, evBundle())
	cases := []struct {
		name     string
		id       string
		wantType string
		wantPay  map[string]any
	}{
		{"boolean tally is a boolean that starts off", evTallyID, "boolean", map[string]any{"value": false}},
		{"number fader is a number that starts at zero, scale 1", evNumberID, "number", map[string]any{"value": float64(0), "scale": float64(1)}},
		{"string label is a string that starts empty", evStringID, "string", map[string]any{"value": ""}},
		{"object payload is typed only by name and starts empty", evObjectID, "object", map[string]any{}},
		{"unrecognised category is served as an opaque object", evOddID, "object", map[string]any{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var typ map[string]any
			if code := evGet(t, ts, evBase+"/sources/"+tc.id+"/type/", &typ); code != 200 {
				t.Fatalf("type: got %d, want 200", code)
			}
			if typ["type"] != tc.wantType {
				t.Errorf("type = %v, want %s", typ["type"], tc.wantType)
			}
			got := evPayload(t, ts, tc.id)
			if len(got) != len(tc.wantPay) {
				t.Fatalf("payload = %v, want %v", got, tc.wantPay)
			}
			for k, v := range tc.wantPay {
				if got[k] != v {
					t.Errorf("payload[%s] = %v, want %v", k, got[k], v)
				}
			}
		})
	}
}

// TestEvDeclaredTypeDocumentWins: the bundle's event_types entry is
// the only way to publish an ENUM (IS-04 knows the source emits
// booleans, not that they mean PGM/PVW), and an enum source's starting
// value must be one of its OWN values -- "" is not a member of
// {CAM1, CAM2}. A document that is not an enum, or cannot be read, does
// not change the starting value.
func TestEvDeclaredTypeDocumentWins(t *testing.T) {
	cases := []struct {
		name      string
		id        string
		raw       string
		wantType  map[string]any // nil: the derived document
		wantValue any            // payload.value; absent key when nil
	}{
		{
			"boolean enum starts at its first declared value", evTallyID,
			`{"type":"boolean","values":[{"value":true,"label":"PGM","description":"on air"},{"value":false,"label":"off","description":"idle"}]}`,
			nil, true,
		},
		{
			"string enum starts at its first declared value", evStringID,
			`{"type":"string","values":[{"value":"CAM1","label":"Camera 1","description":""},{"value":"CAM2","label":"Camera 2","description":""}]}`,
			nil, "CAM1",
		},
		{
			"number enum starts at its first declared value with no scale", evNumberID,
			`{"type":"number","values":[{"value":7,"label":"seven","description":""}]}`,
			nil, float64(7),
		},
		{
			"enum value of no supported kind falls back to the category default", evStringID,
			`{"type":"string","values":[{"value":null,"label":"?","description":""}]}`,
			nil, "",
		},
		{
			"empty values list is not an enum", evTallyID,
			`{"type":"boolean","values":[]}`, nil, false,
		},
		{
			"values entry that is not an object is not an enum", evTallyID,
			`{"type":"boolean","values":[true]}`, nil, false,
		},
		{
			"values entry without a value is not an enum", evTallyID,
			`{"type":"boolean","values":[{"label":"x"}]}`, nil, false,
		},
		{
			"declared non-enum document is served verbatim", evTallyID,
			`{"type":"boolean","note":"kept"}`, nil, false,
		},
		{
			"unreadable document falls back to the derived one", evTallyID,
			`{not json`, map[string]any{"type": "boolean"}, false,
		},
		{
			"empty document falls back to the derived one", evTallyID,
			``, map[string]any{"type": "boolean"}, false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := evBundle()
			b.EventTypes = map[string]json.RawMessage{tc.id: json.RawMessage(tc.raw)}
			_, ts := evServer(t, b)

			want := tc.wantType
			if want == nil {
				if err := json.Unmarshal([]byte(tc.raw), &want); err != nil {
					t.Fatalf("case document is not valid JSON: %v", err)
				}
			}
			var typ map[string]any
			if code := evGet(t, ts, evBase+"/sources/"+tc.id+"/type/", &typ); code != 200 {
				t.Fatalf("type: got %d, want 200", code)
			}
			wantJSON, _ := json.Marshal(want)
			gotJSON, _ := json.Marshal(typ)
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("type = %s, want %s", gotJSON, wantJSON)
			}

			pay := evPayload(t, ts, tc.id)
			if pay["value"] != tc.wantValue {
				t.Errorf("payload.value = %v (%T), want %v (%T)", pay["value"], pay["value"], tc.wantValue, tc.wantValue)
			}
			if tc.id == evNumberID {
				if _, has := pay["scale"]; has {
					t.Errorf("payload = %v: an enum member is a plain number, scale claims value/scale", pay)
				}
			}
		})
	}
}

// TestEvSetStateAcceptsEveryPayloadKind: the value comes from outside
// this API, in whichever Go kind the caller has -- a JSON number
// (float64), a Go int, a string, an object. Anything else is refused
// rather than published as something it is not.
func TestEvSetStateAcceptsEveryPayloadKind(t *testing.T) {
	ev, ts := evServer(t, evBundle())
	cases := []struct {
		name    string
		id      string
		payload any
		want    any // the REST state's Go form; nil = refused
	}{
		{"json number on a number source", evNumberID, 3.5,
			is07.EventNumber{Payload: is07.Number{Value: 3.5, Scale: 1}}},
		{"go int on a number source", evNumberID, 4,
			is07.EventNumber{Payload: is07.Number{Value: 4, Scale: 1}}},
		{"string on a string source", evStringID, "CAM2",
			is07.EventString{Payload: is07.PayloadString{Value: "CAM2"}}},
		{"object on an object source", evObjectID, map[string]any{"k": "v"},
			is07.EventObject{Payload: is07.PayloadObject{"k": "v"}}},
		{"unsupported kind is refused", evTallyID, []int{1}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ev.SetState(tc.id, tc.payload)
			if tc.want == nil {
				if ok {
					t.Fatalf("SetState accepted %T", tc.payload)
				}
				return
			}
			if !ok {
				t.Fatalf("SetState refused %T", tc.payload)
			}
			// Compare payloads by wire form; the timestamp is the clock's.
			gotJSON, _ := json.Marshal(got)
			var gotMap, wantMap map[string]any
			_ = json.Unmarshal(gotJSON, &gotMap)
			wantJSON, _ := json.Marshal(tc.want)
			_ = json.Unmarshal(wantJSON, &wantMap)
			g, _ := json.Marshal(gotMap["payload"])
			w, _ := json.Marshal(wantMap["payload"])
			if string(g) != string(w) {
				t.Errorf("payload = %s, want %s", g, w)
			}
			// And REST reflects it on the next read.
			rest := evPayload(t, ts, tc.id)
			r, _ := json.Marshal(rest)
			if string(r) != string(w) {
				t.Errorf("REST payload = %s, want %s", r, w)
			}
		})
	}
}

// TestEvSetStateLogsWhatTheCodecRefuses: SetState owns HOW a value is
// published, not WHAT it is -- so a string handed to a boolean source
// lands in REST, but the IS-07 codec refuses to put a string event on a
// `boolean` event_type's wire (event_core category rule). That refusal
// is logged, never swallowed: a subscriber not told is a subscriber
// believing the old value.
func TestEvSetStateLogsWhatTheCodecRefuses(t *testing.T) {
	var log bytes.Buffer
	b := evBundle()
	ev := NewIS07EventsServer(slog.New(slog.NewTextHandler(&log, nil)), b, IS07EventsConfig{APIVer: "v1.0"})
	t.Cleanup(func() { _ = ev.Close() })
	if ev.pub == nil {
		t.Fatal("no WebSocket publisher: is the IS-07 codec registered?")
	}
	if _, ok := ev.SetState(evTallyID, "not a boolean"); !ok {
		t.Fatal("SetState refused a string; publication is the codec's call, not SetState's")
	}
	if !strings.Contains(log.String(), "is-07 publish failed") {
		t.Errorf("log = %q, want the refused publish reported", log.String())
	}
	// A Node without a logger still does not panic on the same refusal.
	ev.logger = nil
	if _, ok := ev.SetState(evTallyID, "still not a boolean"); !ok {
		t.Fatal("SetState refused a string")
	}
}

// TestEvWireMessagesCarryFlowIDRestDoesNot: a WebSocket message arrives
// on a connection reached through ONE Sender, so it names the flow it
// came by; the REST state is scoped to the SOURCE and must not.
// withFlowID is that stamping, and it applies to state messages only.
func TestEvWireMessagesCarryFlowIDRestDoesNot(t *testing.T) {
	ev, ts := evServer(t, evBundle())

	m, found := ev.StateMessage(evTallyID)
	if !found {
		t.Fatal("StateMessage: known source not found")
	}
	if e, isBool := m.(is07.EventBoolean); !isBool || e.Identity.FlowID != evTallyFlow {
		t.Errorf("wire message = %#v, want an EventBoolean carrying flow %s", m, evTallyFlow)
	}
	if _, found := ev.StateMessage("no-such-source"); found {
		t.Error("StateMessage answered for an unknown source")
	}
	var rest is07.EventBoolean
	evGet(t, ts, evBase+"/sources/"+evTallyID+"/state/", &rest)
	if rest.Identity.FlowID != "" {
		t.Errorf("REST flow_id = %q, must be absent on the source-scoped view", rest.Identity.FlowID)
	}
	// A source without a Flow publishes with no flow_id at all.
	if m, _ := ev.StateMessage(evNumberID); m.(is07.EventNumber).Identity.FlowID != "" {
		t.Errorf("flow-less source carries flow_id %q", m.(is07.EventNumber).Identity.FlowID)
	}

	common := is07.EventCommon{Identity: is07.Identity{SourceID: evTallyID}}
	cases := []struct {
		name  string
		state any
		want  string // flow_id on the result; "" with isMsg=false
		isMsg bool
	}{
		{"boolean event is stamped", is07.EventBoolean{EventCommon: common}, "f", true},
		{"number event is stamped", is07.EventNumber{EventCommon: common}, "f", true},
		{"string event is stamped", is07.EventString{EventCommon: common}, "f", true},
		{"object event is stamped", is07.EventObject{EventCommon: common}, "f", true},
		{"health is not a state message", is07.MessageHealth{}, "", false},
		{"nil is not a state message", nil, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, isMsg := withFlowID(tc.state, "f")
			if isMsg != tc.isMsg {
				t.Fatalf("isMsg = %v, want %v", isMsg, tc.isMsg)
			}
			if !isMsg {
				if got != nil {
					t.Errorf("got %#v, want nil", got)
				}
				return
			}
			raw, _ := json.Marshal(got)
			var env struct {
				Identity is07.Identity `json:"identity"`
			}
			_ = json.Unmarshal(raw, &env)
			if env.Identity.FlowID != tc.want {
				t.Errorf("flow_id = %q, want %q", env.Identity.FlowID, tc.want)
			}
		})
	}
}

// evDial opens the IS-07 WebSocket of ts with a bounded read deadline.
func evDial(t *testing.T, ts *httptest.Server) *httpsession.WebSocket {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, err := httpsession.DialWebSocket(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+evBase+"/ws", nil)
	if err != nil {
		t.Fatalf("dial events ws: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	return ws
}

func evSendCommand(t *testing.T, ws *httpsession.WebSocket, c is07.Command) {
	t.Helper()
	raw, err := is07.EncodeCommand(c)
	if err != nil {
		t.Fatalf("encode %T: %v", c, err)
	}
	if err := ws.SendText(raw); err != nil {
		t.Fatalf("send %T: %v", c, err)
	}
}

func evReadMessage(t *testing.T, ws *httpsession.WebSocket) is07.Message {
	t.Helper()
	raw, err := ws.ReadText()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	m, err := is07.DecodeMessage(raw)
	if err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return m
}

// TestEvWebSocketFollowsTheSpecSequence walks IS-07 section 5 on the
// socket a Sender's connection_uri names: a subscription command is
// answered with each subscribed source's CURRENT state (5.2), a health
// command with exactly one health message echoing its timestamp
// (5.2), and a change made through SetState reaches the subscriber
// unasked. Closing the Node closes the socket.
func TestEvWebSocketFollowsTheSpecSequence(t *testing.T) {
	ev, ts := evServer(t, evBundle())
	ws := evDial(t, ts)

	evSendCommand(t, ws, is07.CommandSubscription{Sources: []string{evTallyID}})
	initial, isBool := evReadMessage(t, ws).(is07.EventBoolean)
	if !isBool {
		t.Fatal("subscription must be answered with the source's current state")
	}
	if initial.Identity.SourceID != evTallyID || initial.Identity.FlowID != evTallyFlow {
		t.Errorf("initial identity = %+v, want source %s via flow %s", initial.Identity, evTallyID, evTallyFlow)
	}
	if initial.Payload.Value {
		t.Error("a tally never set is OFF")
	}

	evSendCommand(t, ws, is07.CommandHealth{Timestamp: "1:0"})
	health, isHealth := evReadMessage(t, ws).(is07.MessageHealth)
	if !isHealth {
		t.Fatal("health command must be answered with a health message")
	}
	if health.Timing.OriginTimestamp != "1:0" || health.Timing.CreationTimestamp == "" {
		t.Errorf("health timing = %+v, want origin 1:0 and a creation timestamp", health.Timing)
	}

	if _, ok := ev.SetState(evTallyID, true); !ok {
		t.Fatal("SetState refused the tally")
	}
	changed, isBool := evReadMessage(t, ws).(is07.EventBoolean)
	if !isBool || !changed.Payload.Value || changed.Identity.FlowID != evTallyFlow {
		t.Errorf("change = %#v, want the tally ON via flow %s", changed, evTallyFlow)
	}

	if err := ev.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := ws.ReadText(); err == nil {
		t.Error("the socket survived the Node closing")
	}
}

// TestEvUnknownSourceAnswersErrorBody: the routes are minted per known
// source, so the in-handler lookup only misses when the source set
// changes underneath a mounted server. When it does, the answer is the
// IS-07 error body (code / error / debug), not an invented state.
func TestEvUnknownSourceAnswersErrorBody(t *testing.T) {
	ev, ts := evServer(t, evBundle())
	ev.mu.Lock()
	delete(ev.sources, evOddID)
	ev.mu.Unlock()

	for _, leaf := range []string{"type/", "state/"} {
		code, body := evGetRaw(t, ts, evBase+"/sources/"+evOddID+"/"+leaf)
		if code != stdhttp.StatusNotFound {
			t.Errorf("%s: got %d, want 404", leaf, code)
		}
		var e is07.ErrorBody
		if err := json.Unmarshal(body, &e); err != nil {
			t.Fatalf("%s: body %s: %v", leaf, body, err)
		}
		if e.Code != 404 || e.Error != "Unknown source" || e.Debug != evOddID {
			t.Errorf("%s: error body = %+v", leaf, e)
		}
	}
}

// TestEvDeviceAdvertisesEventsControl: a controller finds IS-07 the
// way it finds every API -- through device.controls[] with the
// urn:x-nmos:control:events/{ver} type -- and the href it is given
// must answer. With the API switched off no such control is minted.
func TestEvDeviceAdvertisesEventsControl(t *testing.T) {
	addr := serveNCPBundleNode(t, tallyBundle())
	var devices []map[string]any
	if code := getJSON(t, "http://"+addr+"/x-nmos/node/v1.3/devices", &devices); code != 200 || len(devices) == 0 {
		t.Fatalf("devices = %d (%d devices)", code, len(devices))
	}
	href := ""
	for _, c := range devices[0]["controls"].([]any) {
		cm, _ := c.(map[string]any)
		if cm["type"] == controlTypeEvents+"v1.0" {
			href, _ = cm["href"].(string)
		}
	}
	if !strings.HasPrefix(href, "http://") || !strings.HasSuffix(href, evBase+"/") {
		t.Fatalf("events control href = %q, want http://<host>%s/", href, evBase)
	}
	var ids []string
	if code := getJSON(t, "http://"+addr+evBase+"/sources/", &ids); code != 200 || len(ids) != 1 || ids[0] != evTallyID+"/" {
		t.Errorf("advertised API: sources = %d %v, want the tally", code, ids)
	}

	off, err := NewIS04NodeServer(nil, tallyBundle(), IS04NodeConfig{
		Bind: freeAddr(t), DiscoveryMode: "static", NoEventsAPI: true,
	})
	if err != nil {
		t.Fatalf("NewIS04NodeServer: %v", err)
	}
	off.attachEventsAPI(httpsession.NewServer(evLogger()))
	for _, d := range off.bundle.Devices {
		for _, c := range d.Controls {
			if strings.HasPrefix(c.Type, controlTypeEvents) {
				t.Errorf("device %s advertises %s with the Events API disabled", d.ID, c.Type)
			}
		}
	}
}
