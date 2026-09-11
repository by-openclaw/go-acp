package provider

// IS-08 v1.0.1 Channel Mapping contract, the parts the routing tests do
// not reach: the RAML tree a controller walks (version roots, per-input
// and per-output sub-resources, the per-output active view), the 423
// lock a pending activation holds, the scheduler that promotes due
// activations, and the 4xx answers for malformed or impossible requests.
//
// Scheduled activations are driven with a fixed clock and a direct
// runActivations() call -- the tick in runActivationScheduler is only a
// pump, and a test that waits on it tests the wait.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is05"
	"dhs/internal/amwa/codec/is08"
	// The empty-APIVer mount serves whatever IS-08 minors are
	// registered; production registers v1.0 in cmd/dhs/main.go.
	_ "dhs/internal/amwa/codec/is08/v10"
	httpsession "dhs/internal/amwa/session/http"
)

const cmBase = "/x-nmos/channelmapping/v1.0"

func cmLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// cmServe mounts cm on a fresh session server and returns the live
// HTTP server, so a test can build the mapping server it needs first.
func cmServe(t *testing.T, cm *IS08ChannelMappingServer) *httptest.Server {
	t.Helper()
	srv := httpsession.NewServer(cmLogger())
	cm.Mount(srv)
	ts := httptest.NewServer(srv.MuxHandler())
	t.Cleanup(ts.Close)
	return ts
}

// cmSeededServer builds a Channel Mapping API over constrainedBundle
// after mutate has adjusted its channel_mapping seed. The bundle is
// validated the way config load would, so a seed a device would refuse
// fails the test here rather than passing as a silent no-op.
func cmSeededServer(t *testing.T, mutate func(b *NodeConfig)) (*IS08ChannelMappingServer, *httptest.Server) {
	t.Helper()
	b := constrainedBundle()
	mutate(b)
	if err := validateBundle(b); err != nil {
		t.Fatalf("seeded bundle does not validate: %v", err)
	}
	cm := NewIS08ChannelMappingServer(cmLogger(), b, IS08ChannelMappingConfig{APIVer: "v1.0"})
	return cm, cmServe(t, cm)
}

func cmGetRaw(t *testing.T, ts *httptest.Server, path string) (int, []byte) {
	t.Helper()
	resp, err := ts.Client().Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// cmSameJSON compares a wire body with what the server holds, through
// JSON on both sides so pointer-vs-value and field order do not matter.
func cmSameJSON(t *testing.T, got []byte, want any) bool {
	t.Helper()
	wantRaw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal expectation: %v", err)
	}
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("response is not JSON: %v: %s", err, got)
	}
	_ = json.Unmarshal(wantRaw, &w)
	return reflect.DeepEqual(g, w)
}

func cmRoute(out int, in string, ch int) string {
	return `"` + itoa(out) + `":{"input":"` + in + `","channel_index":` + itoa(ch) + `}`
}

func cmImmediate(outID, entries string) string {
	return `{"activation":{"mode":"activate_immediate"},"action":{"` + outID + `":{` + entries + `}}}`
}

func cmScheduled(mode, when, outID, entries string) string {
	return `{"activation":{"mode":"` + mode + `","requested_time":"` + when + `"},"action":{"` + outID + `":{` + entries + `}}}`
}

// cmErrorBody decodes the IS-08 error envelope a 4xx carries.
func cmErrorBody(t *testing.T, raw string) is08.ErrorBody {
	t.Helper()
	var e is08.ErrorBody
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		t.Fatalf("error body is not the IS-08 envelope: %v: %s", err, raw)
	}
	return e
}

// TestCmVersionRootsAndIndexes: the RAML tree from the API root down.
// Each index lists its children with trailing slashes, and the version
// roots list every mounted minor -- an empty APIVer mounts them all.
func TestCmVersionRootsAndIndexes(t *testing.T) {
	cm := NewIS08ChannelMappingServer(cmLogger(), audioBundle(), IS08ChannelMappingConfig{})
	ts := cmServe(t, cm)
	vers := is08.SupportedVersions()
	if len(vers) == 0 {
		t.Fatal("no IS-08 codec registered: the empty-APIVer mount has nothing to serve")
	}
	base := "/x-nmos/channelmapping/" + vers[0]

	cases := []struct {
		name string
		path string
		want []string
	}{
		{"API root lists every mounted minor", "/x-nmos/channelmapping", withSlashes(vers)},
		{"API root with trailing slash lists the same", "/x-nmos/channelmapping/", withSlashes(vers)},
		{"version root lists io inputs outputs map", base + "/", []string{"io/", "inputs/", "outputs/", "map/"}},
		{"map index lists active and activations", base + "/map/", []string{"active/", "activations/"}},
		{"inputs index lists every input id", base + "/inputs/", withSlashes(sortedKeys(cm.io.Inputs))},
		{"outputs index lists every output id", base + "/outputs/", withSlashes(sortedKeys(cm.io.Outputs))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			if code := cmGet(t, ts, tc.path, &got); code != stdhttp.StatusOK {
				t.Fatalf("GET %s = %d, want 200", tc.path, code)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("GET %s = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

// TestCmIOSubresourcesMatchAggregate: `io` is an aggregate, not a
// replacement. Every per-input and per-output sub-resource the RAML
// names is served on its own and carries exactly what the aggregate
// shows for it.
func TestCmIOSubresourcesMatchAggregate(t *testing.T) {
	_, ts := cmTestServer(t)

	var agg is08.IO
	if code := cmGet(t, ts, cmBase+"/io/", &agg); code != stdhttp.StatusOK {
		t.Fatalf("io = %d", code)
	}
	if len(agg.Inputs) == 0 || len(agg.Outputs) == 0 {
		t.Fatal("audio bundle derives no input/output pair")
	}

	type sub struct {
		path string
		want any
	}
	var cases []sub
	for id, in := range agg.Inputs {
		p := cmBase + "/inputs/" + id
		cases = append(cases,
			sub{p + "/", []string{"caps/", "channels/", "parent/", "properties/"}},
			sub{p + "/properties/", in.Properties},
			sub{p + "/parent/", in.Parent},
			sub{p + "/channels/", in.Channels},
			sub{p + "/caps/", in.Caps},
		)
	}
	for id, o := range agg.Outputs {
		p := cmBase + "/outputs/" + id
		cases = append(cases,
			sub{p + "/", []string{"caps/", "channels/", "properties/", "sourceid/"}},
			sub{p + "/properties/", o.Properties},
			// `sourceid` in the URL, `source_id` in JSON: the spec's spelling.
			sub{p + "/sourceid/", o.SourceID},
			sub{p + "/channels/", o.Channels},
			sub{p + "/caps/", o.Caps},
		)
	}
	for _, tc := range cases {
		code, raw := cmGetRaw(t, ts, tc.path)
		if code != stdhttp.StatusOK {
			t.Errorf("GET %s = %d, want 200", tc.path, code)
			continue
		}
		if !cmSameJSON(t, raw, tc.want) {
			t.Errorf("GET %s = %s, disagrees with the io aggregate", tc.path, raw)
		}
	}
}

// TestCmFieldHandlersRefuseUnknownIDs: a field handler mounted for an
// id the store no longer holds answers 404 with the id, never an empty
// object that looks like an input with nothing in it.
func TestCmFieldHandlersRefuseUnknownIDs(t *testing.T) {
	cm := NewIS08ChannelMappingServer(cmLogger(), audioBundle(), IS08ChannelMappingConfig{APIVer: "v1.0"})
	req := httptest.NewRequest(stdhttp.MethodGet, "/", nil)

	cases := []struct {
		name string
		h    httpsession.HandlerFunc
		want string
	}{
		{"input field", cm.inputField("no-such-input", func(is08.Input) any { return "picked" }), "Unknown input"},
		{"output field", cm.outputField("no-such-output", func(is08.Output) any { return "picked" }), "Unknown output"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body, err := tc.h(context.Background(), req)
			if err != nil {
				t.Fatalf("handler error: %v", err)
			}
			if code != stdhttp.StatusNotFound {
				t.Errorf("status = %d, want 404", code)
			}
			e, ok := body.(is08.ErrorBody)
			if !ok || e.Error != tc.want || !strings.HasPrefix(e.Debug, "no-such-") {
				t.Errorf("body = %#v, want IS-08 error %q naming the id", body, tc.want)
			}
		})
	}
}

// TestCmPerOutputActiveView: map/active/{outputID}/ keeps the SAME
// {activation, map} shape as the whole map, narrowed to one output; an
// output missing from the active map is 404, and the merge that applies
// an action recreates it rather than dropping the route.
func TestCmPerOutputActiveView(t *testing.T) {
	cm, ts := cmTestServer(t)
	outID, inID := cmFirstPair(t, cm)
	path := cmBase + "/map/active/" + outID + "/"

	var one is08.MapActive
	if code := cmGet(t, ts, path, &one); code != stdhttp.StatusOK {
		t.Fatalf("per-output view = %d, want 200", code)
	}
	if len(one.Map) != 1 {
		t.Fatalf("per-output map carries %d outputs, want exactly the one asked for", len(one.Map))
	}
	if got := len(one.Map[outID]); got != len(cm.io.Outputs[outID].Channels) {
		t.Errorf("per-output view has %d channels, output has %d", got, len(cm.io.Outputs[outID].Channels))
	}
	if one.Activation.Mode != nil {
		t.Error("activation block must be null before the first activation, same as the whole map")
	}

	if code := cmGet(t, ts, cmBase+"/map/active/no-such-output/", nil); code != stdhttp.StatusNotFound {
		t.Errorf("unknown output = %d, want the router's 404", code)
	}

	// An output the IO view lists but the active map has lost is an
	// unknown output, not an empty one.
	cm.mu.Lock()
	delete(cm.active.Map, outID)
	cm.mu.Unlock()
	code, raw := cmGetRaw(t, ts, path)
	if code != stdhttp.StatusNotFound || cmErrorBody(t, string(raw)).Error != "Unknown output" {
		t.Errorf("vanished output = %d %s, want 404 Unknown output", code, raw)
	}

	// applyLocked is a merge: an output absent from the map is created
	// with the routed channels, the rest of the map untouched.
	idx := 0
	cm.mu.Lock()
	cm.applyLocked(is08.MapEntries{outID: {"0": {Input: &inID, ChannelIndex: &idx}}})
	cm.mu.Unlock()
	if code := cmGet(t, ts, path, &one); code != stdhttp.StatusOK {
		t.Fatalf("recreated output = %d, want 200", code)
	}
	if e := one.Map[outID]["0"]; e.Input == nil || *e.Input != inID {
		t.Errorf("merged route lost: channel 0 = %+v", e)
	}
}

// TestCmDeriveIOEdges: the derivation from IS-04 tolerates what IS-04
// permits -- no bundle at all, empty labels, seeds naming ids that do
// not derive -- and defaults rather than dropping the resource.
func TestCmDeriveIOEdges(t *testing.T) {
	t.Run("nil bundle serves an empty io", func(t *testing.T) {
		cm := NewIS08ChannelMappingServer(cmLogger(), nil, IS08ChannelMappingConfig{APIVer: "v1.0"})
		ts := cmServe(t, cm)
		var got is08.IO
		if code := cmGet(t, ts, cmBase+"/io/", &got); code != stdhttp.StatusOK {
			t.Fatalf("io = %d", code)
		}
		if len(got.Inputs) != 0 || len(got.Outputs) != 0 {
			t.Errorf("nil bundle derived %d inputs %d outputs", len(got.Inputs), len(got.Outputs))
		}
		var active is08.MapActive
		if code := cmGet(t, ts, cmBase+"/map/active/", &active); code != stdhttp.StatusOK || len(active.Map) != 0 {
			t.Errorf("map/active = %d with %d outputs, want 200 and none", code, len(active.Map))
		}
	})

	t.Run("empty labels fall back to a name", func(t *testing.T) {
		b := audioBundle()
		for i := range b.Receivers {
			b.Receivers[i].Label = ""
		}
		for i := range b.Sources {
			b.Sources[i].Label = ""
			for j := range b.Sources[i].Channels {
				b.Sources[i].Channels[j].Label = ""
			}
		}
		view := deriveIO(b)
		for id, in := range view.Inputs {
			if in.Properties.Name != "input" {
				t.Errorf("input %s: name = %q, want the fallback \"input\"", id, in.Properties.Name)
			}
		}
		for id, o := range view.Outputs {
			if o.Properties.Name != "output" {
				t.Errorf("output %s: name = %q, want the fallback \"output\"", id, o.Properties.Name)
			}
			for i, c := range o.Channels {
				if c.Label != "Channel" {
					t.Errorf("output %s channel %d: label = %q, want the fallback \"Channel\"", id, i, c.Label)
				}
			}
		}
	})

	t.Run("seeds for ids that do not derive are ignored", func(t *testing.T) {
		b := audioBundle()
		base := deriveIO(b)
		inID := sortedKeys(base.Inputs)[0]
		outID := sortedKeys(base.Outputs)[0]
		f, bs := false, 4
		ghost := "ghost"
		b.ChannelMapping = &ChannelMappingSeed{
			Inputs: map[string]*ChannelMappingInputSeed{
				"ghost-in": {Reordering: &f, BlockSize: &bs},
				inID:       nil,
			},
			Outputs: map[string]*ChannelMappingOutputSeed{
				"ghost-out": {RoutableInputs: []*string{&ghost}},
				outID:       {RoutableInputs: nil},
			},
		}
		view := deriveIO(b)
		if in := view.Inputs[inID]; !in.Caps.Reordering || in.Caps.BlockSize != 1 {
			t.Errorf("nil seed changed input caps: %+v", in.Caps)
		}
		if o := view.Outputs[outID]; len(o.Caps.RoutableInputs) != len(base.Inputs)+1 {
			t.Errorf("seed with no routable_inputs changed output caps: %v", o.Caps.RoutableInputs)
		}
		if _, ok := view.Inputs["ghost-in"]; ok {
			t.Error("a seed must never invent an input the bundle does not have")
		}
	})
}

// TestCmBootMapRejectedAtConstruction: a boot map the running device
// would 400 is refused at construction and LOGGED -- the device comes
// up unrouted with no activation record, not half-routed.
func TestCmBootMapRejectedAtConstruction(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	b := audioBundle()
	view := deriveIO(b)
	outID := sortedKeys(view.Outputs)[0]
	bogus, idx := "no-such-input", 0
	b.ChannelMapping = &ChannelMappingSeed{
		BootMap: map[string]map[string]is08.MapEntry{
			outID: {"0": {Input: &bogus, ChannelIndex: &idx}},
		},
	}
	cm := NewIS08ChannelMappingServer(logger, b, IS08ChannelMappingConfig{APIVer: "v1.0"})

	if len(cm.activations) != 0 {
		t.Errorf("a rejected boot map recorded %d activation(s)", len(cm.activations))
	}
	if e := cm.active.Map[outID]["0"]; e.Input != nil {
		t.Errorf("a rejected boot map routed channel 0 to %q", *e.Input)
	}
	if cm.active.Activation.Mode != nil {
		t.Error("a rejected boot map stamped map/active as activated")
	}
	if !strings.Contains(logs.String(), "boot_map rejected") || !strings.Contains(logs.String(), bogus) {
		t.Errorf("rejection must be logged with the reason; log = %q", logs.String())
	}
}

// cmErrReader is a body the server cannot read.
type cmErrReader struct{}

func (cmErrReader) Read([]byte) (int, error) { return 0, errors.New("cm: body torn mid-read") }

// TestCmActivationPostRejectsMalformedRequests: every way a POST can be
// wrong before it reaches the map answers 400 with the IS-08 error
// envelope naming which stage refused it.
func TestCmActivationPostRejectsMalformedRequests(t *testing.T) {
	cm := NewIS08ChannelMappingServer(cmLogger(), audioBundle(), IS08ChannelMappingConfig{APIVer: "v1.0"})
	outID, inID := cmFirstPair(t, cm)

	cases := []struct {
		name string
		body io.Reader
		want string
	}{
		{"unreadable body", cmErrReader{}, "Unreadable body"},
		{"malformed JSON", strings.NewReader(`{"activation":`), "Invalid activation request"},
		{"unknown activation mode",
			strings.NewReader(`{"activation":{"mode":"activate_now"},"action":{}}`), "Invalid activation request"},
		{"half-set entry (input without channel_index)",
			strings.NewReader(`{"activation":{"mode":"activate_immediate"},"action":{"` + outID + `":{"0":{"input":"` + inID + `","channel_index":null}}}}`),
			"Invalid activation request"},
		{"input channel the input does not have",
			strings.NewReader(cmImmediate(outID, cmRoute(0, inID, 7))), "Invalid action"},
		// Passes the codec's `<sec>:<nsec>` grammar but overflows int64,
		// so it cannot be resolved to an instant.
		{"absolute time beyond int64",
			strings.NewReader(cmScheduled("activate_scheduled_absolute", "99999999999999999999:0", outID, cmRoute(0, inID, 0))),
			"Invalid activation time"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(stdhttp.MethodPost, cmBase+"/map/activations/", tc.body)
			code, body, err := cm.handleActivationPost(req)
			if err != nil {
				t.Fatalf("handler error: %v", err)
			}
			if code != stdhttp.StatusBadRequest {
				t.Errorf("status = %d, want 400", code)
			}
			e, ok := body.(is08.ErrorBody)
			if !ok || e.Code != 400 || e.Error != tc.want || e.Debug == "" {
				t.Errorf("body = %#v, want IS-08 error %q with a debug reason", body, tc.want)
			}
		})
	}
	if len(cm.activations) != 0 || len(cm.lockedOutputs) != 0 {
		t.Error("a refused POST must leave no activation or lock behind")
	}
}

// TestCmDueTimeResolution: the two scheduled modes read requested_time
// differently -- relative is a DURATION from receipt, absolute a TAI
// INSTANT (TAI-UTC = 37 s at v1.0.1). Reading one as the other fires
// a switch the controller wanted later right now.
func TestCmDueTimeResolution(t *testing.T) {
	cm := NewIS08ChannelMappingServer(cmLogger(), audioBundle(), IS08ChannelMappingConfig{APIVer: "v1.0"})
	t0 := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	cm.now = func() time.Time { return t0 }
	str := func(s string) *string { return &s }

	cases := []struct {
		name    string
		a       is08.Activation
		want    time.Time
		wantErr bool
	}{
		{"relative is a duration from now",
			is08.Activation{Mode: is08.ActivationModeScheduledRelative, RequestedTime: str("2:500000000")},
			t0.Add(2500 * time.Millisecond), false},
		{"absolute is a TAI instant (37 s ahead of UTC)",
			is08.Activation{Mode: is08.ActivationModeScheduledAbsolute, RequestedTime: str("37:0")},
			time.Unix(0, 0), false},
		{"missing requested_time cannot be resolved",
			is08.Activation{Mode: is08.ActivationModeScheduledRelative}, time.Time{}, true},
		{"empty requested_time cannot be resolved",
			is08.Activation{Mode: is08.ActivationModeScheduledAbsolute, RequestedTime: str("")}, time.Time{}, true},
		{"requested_time outside <sec>:<nsec> cannot be resolved",
			is08.Activation{Mode: is08.ActivationModeScheduledAbsolute, RequestedTime: str("soon")}, time.Time{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cm.dueTimeLocked(tc.a)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && !got.Equal(tc.want) {
				t.Errorf("due = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCmPendingActivationLocksOutput: IS-08 §5 -- a pending activation
// claims its outputs; a second change on one of them is 423 naming the
// claimant, and deleting the pending activation releases the claim.
func TestCmPendingActivationLocksOutput(t *testing.T) {
	cm, ts := cmTestServer(t)
	outID, inID := cmFirstPair(t, cm)
	far := "4102444800:0"

	code, body := postActivation(t, ts, cmScheduled("activate_scheduled_absolute", far, outID, cmRoute(0, inID, 0)))
	if code != stdhttp.StatusAccepted {
		t.Fatalf("first scheduled = %d %s, want 202", code, body)
	}
	holder, _ := decodeKeyedActivation(t, strings.NewReader(body))

	for _, attempt := range []struct {
		name, body string
	}{
		{"immediate on the claimed output", cmImmediate(outID, cmRoute(1, inID, 1))},
		{"second schedule on the claimed output", cmScheduled("activate_scheduled_relative", "5:0", outID, cmRoute(1, inID, 1))},
	} {
		code, body := postActivation(t, ts, attempt.body)
		if code != stdhttp.StatusLocked {
			t.Errorf("%s = %d %s, want 423", attempt.name, code, body)
			continue
		}
		if e := cmErrorBody(t, body); e.Code != 423 || !strings.Contains(e.Debug, "activation "+holder) {
			t.Errorf("%s: 423 body must name the claimant %q: %+v", attempt.name, holder, e)
		}
	}
	if len(cm.activations) != 1 {
		t.Errorf("a refused POST must not be recorded: %d activation(s)", len(cm.activations))
	}

	del, _ := doJSON(t, stdhttp.MethodDelete, ts.URL+cmBase+"/map/activations/"+holder+"/", "")
	if del != stdhttp.StatusNoContent {
		t.Fatalf("DELETE claimant = %d, want 204", del)
	}
	if code, body := postActivation(t, ts, cmImmediate(outID, cmRoute(1, inID, 1))); code != stdhttp.StatusOK {
		t.Errorf("after release = %d %s, want 200", code, body)
	}
}

// TestCmActivationNotifiesDevice: onActivate fires when a re-map takes
// effect -- once per immediate POST, never for a queued one -- because
// IS-04 device.version is how a controller learns anything changed.
func TestCmActivationNotifiesDevice(t *testing.T) {
	cm, ts := cmTestServer(t)
	outID, inID := cmFirstPair(t, cm)
	fired := 0
	cm.onActivate = func() { fired++ }

	if code, body := postActivation(t, ts, cmImmediate(outID, cmRoute(0, inID, 0))); code != stdhttp.StatusOK {
		t.Fatalf("immediate = %d %s", code, body)
	}
	if fired != 1 {
		t.Errorf("immediate activation notified %d time(s), want 1", fired)
	}
	if code, body := postActivation(t, ts, cmScheduled("activate_scheduled_absolute", "4102444800:0", outID, cmRoute(1, inID, 1))); code != stdhttp.StatusAccepted {
		t.Fatalf("scheduled = %d %s", code, body)
	}
	if fired != 1 {
		t.Errorf("a queued activation changed nothing yet but notified: %d", fired)
	}
}

// TestCmRunActivationsFiresDueOnly: the scheduler promotes exactly the
// activations whose time has come -- skipping ones already done and
// ones still in the future -- stamps activation_time with the firing
// clock, releases the output lock, and notifies once per pass.
func TestCmRunActivationsFiresDueOnly(t *testing.T) {
	cm, ts := cmConstrainedServer(t)
	t0 := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	cm.now = func() time.Time { return t0 }
	fired := 0
	cm.onActivate = func() { fired++ }
	outID, inID := cmFirstPair(t, cm)

	// One already-done activation in the collection, so the scheduler
	// has something to skip.
	if code, body := postActivation(t, ts, cmImmediate(outID, cmRoute(0, inID, 0))); code != stdhttp.StatusOK {
		t.Fatalf("immediate = %d %s", code, body)
	}
	code, body := postActivation(t, ts, cmScheduled("activate_scheduled_relative", "5:0", outID, cmRoute(1, inID, 1)))
	if code != stdhttp.StatusAccepted {
		t.Fatalf("soon = %d %s", code, body)
	}
	soon, _ := decodeKeyedActivation(t, strings.NewReader(body))
	unroute := `"0":{"input":null,"channel_index":null},"1":{"input":null,"channel_index":null},"2":{"input":null,"channel_index":null},"3":{"input":null,"channel_index":null}`
	code, body = postActivation(t, ts, cmScheduled("activate_scheduled_relative", "100:0", wideOutID, unroute))
	if code != stdhttp.StatusAccepted {
		t.Fatalf("later = %d %s", code, body)
	}
	later, _ := decodeKeyedActivation(t, strings.NewReader(body))
	fired = 0

	if n := cm.runActivations(); n != 0 {
		t.Fatalf("nothing is due at t0, yet %d fired", n)
	}
	if fired != 0 {
		t.Error("a pass that fires nothing must not notify")
	}

	cm.now = func() time.Time { return t0.Add(5 * time.Second) }
	if n := cm.runActivations(); n != 1 {
		t.Fatalf("at t0+5s exactly one activation is due, %d fired", n)
	}
	if fired != 1 {
		t.Errorf("one pass with a firing notified %d time(s), want 1", fired)
	}

	var done is08.MapActivationResponse
	if code := cmGet(t, ts, cmBase+"/map/activations/"+soon+"/", &done); code != stdhttp.StatusOK {
		t.Fatalf("fired activation = %d", code)
	}
	wantStamp := is05.FormatTAINow(t0.Add(5 * time.Second))
	if done.Activation.ActivationTime == nil || *done.Activation.ActivationTime != wantStamp {
		t.Errorf("activation_time = %v, want the firing clock %s", done.Activation.ActivationTime, wantStamp)
	}
	var active is08.MapActive
	cmGet(t, ts, cmBase+"/map/active/", &active)
	if e := active.Map[outID]["1"]; e.Input == nil || *e.Input != inID {
		t.Errorf("fired activation not applied: channel 1 = %+v", e)
	}
	if active.Activation.ActivationTime == nil || *active.Activation.ActivationTime != wantStamp {
		t.Errorf("map/active must carry the fired activation block: %+v", active.Activation)
	}
	if _, still := cm.lockedOutputs[outID]; still {
		t.Error("a fired activation must release its output lock")
	}
	if cm.lockedOutputs[wideOutID] != later {
		t.Errorf("the still-pending activation must keep its lock: %q", cm.lockedOutputs[wideOutID])
	}
	var pending is08.MapActivationResponse
	cmGet(t, ts, cmBase+"/map/activations/"+later+"/", &pending)
	if pending.Activation.ActivationTime != nil {
		t.Error("an activation due in 100 s fired at 5 s")
	}

	if n := cm.runActivations(); n != 0 || fired != 1 {
		t.Errorf("second pass re-fired: n=%d notified=%d", n, fired)
	}
	if code, body := postActivation(t, ts, cmImmediate(outID, cmRoute(0, inID, 1))); code != stdhttp.StatusOK {
		t.Errorf("output freed by the firing still refuses: %d %s", code, body)
	}
}

// TestCmActivationPathsBeyondOneSegment: an activation id is exactly
// one path segment. Deeper paths and unknown ids are 404 on GET and
// DELETE alike -- a DELETE that answered 204 for an id it never had
// would tell a controller its activation was cancelled.
func TestCmActivationPathsBeyondOneSegment(t *testing.T) {
	_, ts := cmTestServer(t)
	cases := []struct {
		name, method, path string
	}{
		{"GET below an id", stdhttp.MethodGet, "/1/deeper/"},
		{"DELETE below an id", stdhttp.MethodDelete, "/1/deeper/"},
		{"DELETE unknown id", stdhttp.MethodDelete, "/no-such-activation/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, raw := doJSON(t, tc.method, ts.URL+cmBase+"/map/activations"+tc.path, "")
			if code != stdhttp.StatusNotFound {
				t.Fatalf("%s = %d, want 404", tc.path, code)
			}
			if e := cmErrorBody(t, string(raw)); e.Error != "Unknown activation" {
				t.Errorf("%s: error = %q, want \"Unknown activation\"", tc.path, e.Error)
			}
		})
	}
}

// TestCmTrimPathID: the id is the single segment after the prefix,
// trailing slashes stripped; anything else is not an id.
func TestCmTrimPathID(t *testing.T) {
	const prefix = cmBase + "/map/activations/"
	cases := []struct {
		name, path, want string
	}{
		{"plain id", prefix + "7", "7"},
		{"trailing slash stripped", prefix + "7/", "7"},
		{"several trailing slashes stripped", prefix + "7///", "7"},
		{"path shorter than the prefix", cmBase + "/map/", ""},
		{"path under another prefix", cmBase + "/map/active/7/", ""},
		{"nothing after the prefix", prefix, ""},
		{"only slashes after the prefix", prefix + "//", ""},
		{"deeper than one segment", prefix + "7/x", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := trimPathID(tc.path, prefix); got != tc.want {
				t.Errorf("trimPathID(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

// TestCmValidateActionContract: validateAction against a hand-built IO
// view, for the shapes the wire codec already refuses (half-set entries)
// or the derivation never produces (outputs or inputs without caps, a
// zero block size) -- the boot_map seed reaches this function without
// passing through the codec.
func TestCmValidateActionContract(t *testing.T) {
	four := []is08.Channel{{Label: "1"}, {Label: "2"}, {Label: "3"}, {Label: "4"}}
	str := func(s string) *string { return &s }
	num := func(n int) *int { return &n }
	view := is08.IO{
		Inputs: map[string]is08.Input{
			"in":     {Channels: four, Caps: &is08.InputCaps{Reordering: true, BlockSize: 1}},
			"nocaps": {Channels: four},
			"zero":   {Channels: four, Caps: &is08.InputCaps{Reordering: false, BlockSize: 0}},
		},
		Outputs: map[string]is08.Output{
			"out":  {Channels: four, Caps: &is08.OutputCaps{}},
			"bare": {Channels: four},
		},
	}
	cases := []struct {
		name   string
		action is08.MapEntries
		want   string // substring of the error; "" means accepted
	}{
		{"half-set entry refused", is08.MapEntries{"out": {"0": {Input: str("in")}}},
			"both be set or both null"},
		{"input channel beyond the input refused", is08.MapEntries{"out": {"0": {Input: str("in"), ChannelIndex: num(9)}}},
			`input "in" has no channel 9`},
		{"output without caps accepts any route", is08.MapEntries{"bare": {"3": {Input: str("in"), ChannelIndex: num(0)}}}, ""},
		{"input without caps has no block rules", is08.MapEntries{"out": {"0": {Input: str("nocaps"), ChannelIndex: num(3)}}}, ""},
		{"zero block size routes as single channels", is08.MapEntries{"out": {"1": {Input: str("zero"), ChannelIndex: num(1)}}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAction(view, tc.action)
			switch {
			case tc.want == "" && err != nil:
				t.Errorf("refused a legal action: %v", err)
			case tc.want != "" && err == nil:
				t.Errorf("accepted an action it must refuse (want %q)", tc.want)
			case tc.want != "" && !strings.Contains(err.Error(), tc.want):
				t.Errorf("err = %v, want it to say %q", err, tc.want)
			}
		})
	}
}

// TestCmBlockCapsWithReordering: block_size 2 with reordering=true --
// an output block may take its input block in any order, but must
// take ONE input block and each input channel once (IS-08-01
// test_14/15 shapes on a re-ordering device).
func TestCmBlockCapsWithReordering(t *testing.T) {
	reorderable := func(b *NodeConfig) {
		tr, bs := true, 2
		b.ChannelMapping.Inputs[blockInID] = &ChannelMappingInputSeed{Reordering: &tr, BlockSize: &bs}
	}
	cases := []struct {
		name string
		body string
		want int
		say  string
	}{
		{"output block mixing two input blocks refused",
			cmImmediate(wideOutID, cmRoute(0, blockInID, 0)+","+cmRoute(1, blockInID, 2)), 400, "mixes input blocks"},
		{"input channel routed twice into one block refused",
			cmImmediate(wideOutID, cmRoute(0, blockInID, 0)+","+cmRoute(1, blockInID, 0)), 400, "routed twice"},
		{"legal: one input block, permuted",
			cmImmediate(wideOutID, cmRoute(0, blockInID, 1)+","+cmRoute(1, blockInID, 0)), 200, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ts := cmSeededServer(t, reorderable)
			code, body := postActivation(t, ts, tc.body)
			if code != tc.want {
				t.Fatalf("POST = %d %s, want %d", code, body, tc.want)
			}
			if tc.say != "" && !strings.Contains(cmErrorBody(t, body).Debug, tc.say) {
				t.Errorf("debug = %s, want it to say %q", body, tc.say)
			}
		})
	}
}

// TestCmUnroutingNeedsNullRoutableInput: routable_inputs without a null
// entry is how a device says "this output is never left unrouted"; an
// action that unroutes a channel on it is 400.
func TestCmUnroutingNeedsNullRoutableInput(t *testing.T) {
	_, ts := cmSeededServer(t, func(b *NodeConfig) {
		in := blockInID
		b.ChannelMapping.Outputs[wideOutID] = &ChannelMappingOutputSeed{RoutableInputs: []*string{&in}}
	})
	code, body := postActivation(t, ts, cmImmediate(wideOutID, `"0":{"input":null,"channel_index":null}`))
	if code != stdhttp.StatusBadRequest {
		t.Fatalf("unroute on a never-unrouted output = %d %s, want 400", code, body)
	}
	if e := cmErrorBody(t, body); !strings.Contains(e.Debug, "unrouting is not permitted") {
		t.Errorf("debug must say why: %+v", e)
	}
}
