package provider

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is07"
	httpsession "dhs/internal/amwa/session/http"
)

// tallyBundle adds a boolean tally source -- an IS-04 data Source
// carrying an event_type, which is the same rule IS-04 uses to mark a
// Source as an event source.
func tallyBundle() *NodeConfig {
	b := audioBundle()
	dev := b.Devices[0].ID
	src := is04.Source{
		ResourceCore: is04.ResourceCore{
			ID: "eeeeeeee-5555-4555-8555-555555555555", Version: "0:0",
			Label: "src-tally", Description: "camera 1 tally", Tags: map[string][]string{},
		},
		Caps: map[string]any{}, DeviceID: dev, Parents: []string{},
		Format:    formatData,
		EventType: "boolean",
	}
	flow := is04.Flow{
		ResourceCore: is04.ResourceCore{
			ID: "ffffffff-6666-4666-8666-666666666666", Version: "0:0",
			Label: "flow-tally", Description: "tally events", Tags: map[string][]string{},
		},
		SourceID: src.ID, DeviceID: dev, Parents: []string{},
		Format: formatData, MediaType: "application/json", EventType: "boolean",
	}
	b.Sources = append(b.Sources, src)
	b.Flows = append(b.Flows, flow)
	return b
}

func eventsTestServer(t *testing.T) (*IS07EventsServer, *httptest.Server, string) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	b := tallyBundle()
	ev := NewIS07EventsServer(logger, b, IS07EventsConfig{APIVer: "v1.0"})
	srv := httpsession.NewServer(logger)
	ev.Mount(srv)
	ts := httptest.NewServer(srv.MuxHandler())
	t.Cleanup(ts.Close)
	return ev, ts, "eeeeeeee-5555-4555-8555-555555555555"
}

func evGet(t *testing.T, ts *httptest.Server, path string, into any) int {
	t.Helper()
	resp, err := ts.Client().Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if into != nil && resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
			t.Fatalf("GET %s: decode: %v", path, err)
		}
	}
	return resp.StatusCode
}

// TestEventSourcesComeFromDataSources: only IS-04 data Sources with an
// event_type are event sources. An audio Source is not one, however
// many channels it has.
func TestEventSourcesComeFromDataSources(t *testing.T) {
	_, ts, tallyID := eventsTestServer(t)

	var ids []string
	if code := evGet(t, ts, "/x-nmos/events/v1.0/sources/", &ids); code != 200 {
		t.Fatalf("sources: got %d, want 200", code)
	}
	if len(ids) != 1 || ids[0] != tallyID+"/" {
		t.Fatalf("sources = %v, want just the tally source", ids)
	}
}

// TestEventStateIsAlwaysPresent: IS-07 §4.2 makes `state` the CURRENT
// value, and a tally that has never been set is OFF, not unknown. A
// client bootstrapping against "unknown" has to guess.
func TestEventStateIsAlwaysPresent(t *testing.T) {
	_, ts, id := eventsTestServer(t)

	var state is07.EventBoolean
	if code := evGet(t, ts, "/x-nmos/events/v1.0/sources/"+id+"/state/", &state); code != 200 {
		t.Fatalf("state: got %d, want 200", code)
	}
	if state.MessageType != is07.MessageTypeState {
		t.Errorf("message_type = %q, want %q", state.MessageType, is07.MessageTypeState)
	}
	if state.Identity.SourceID != id {
		t.Errorf("identity.source_id = %q, want %q", state.Identity.SourceID, id)
	}
	// flow_id is NOT permitted here. IS-07 §4.2 scopes the REST state
	// to the SOURCE -- one current value, however many flows carry it
	// -- so naming a flow would claim the value belongs to one
	// encoding rather than to the source.
	if state.Identity.FlowID != "" {
		t.Errorf("identity.flow_id = %q, must be absent on the source-scoped REST state", state.Identity.FlowID)
	}
	if state.Timing.CreationTimestamp == "" {
		t.Error("every event carries a creation_timestamp")
	}
	if state.Payload.Value {
		t.Error("a tally that has never been set is OFF")
	}
}

// TestEventTypeDocumentDescribesTheSource: `type` says what the source
// CAN say -- the question a controller must answer before it can
// render a control at all.
func TestEventTypeDocumentDescribesTheSource(t *testing.T) {
	_, ts, id := eventsTestServer(t)

	var typ map[string]any
	if code := evGet(t, ts, "/x-nmos/events/v1.0/sources/"+id+"/type/", &typ); code != 200 {
		t.Fatalf("type: got %d, want 200", code)
	}
	if typ["type"] != "boolean" {
		t.Errorf("type = %v, want boolean for a boolean event_type", typ["type"])
	}
}

// TestSetStatePublishesNewValue: the value comes from outside this API
// -- a GPI pin, a control surface, another protocol's tally -- and the
// REST endpoint must reflect it immediately, because that is what a
// re-syncing client reads.
func TestSetStatePublishesNewValue(t *testing.T) {
	ev, ts, id := eventsTestServer(t)

	if _, ok := ev.SetState(id, true); !ok {
		t.Fatal("SetState refused a known source")
	}
	var state is07.EventBoolean
	evGet(t, ts, "/x-nmos/events/v1.0/sources/"+id+"/state/", &state)
	if !state.Payload.Value {
		t.Error("state must reflect the new value on the next read")
	}

	if _, ok := ev.SetState("no-such-source", true); ok {
		t.Error("SetState accepted a source that does not exist")
	}
}

// TestUnknownEventSourceIs404: a source we do not have gets the
// router's 404, not an invented empty state.
func TestUnknownEventSourceIs404(t *testing.T) {
	_, ts, _ := eventsTestServer(t)
	if code := evGet(t, ts, "/x-nmos/events/v1.0/sources/deadbeef-0000-4000-8000-000000000000/state/", nil); code != 404 {
		t.Errorf("unknown source: got %d, want 404", code)
	}
}
