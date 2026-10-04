package consumer

// The IS-08 half of the Controller: the endpoint comes from the
// Device's cm-ctrl control, a map the Device cannot take is refused
// before it is sent, and an applied map is read back from the Device.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is08"
)

// mapDevice is the Channel Mapping API of one Device: two inputs (the
// second one not routable to out1), one two-channel output.
type mapDevice struct {
	t       *testing.T
	active  map[string]map[string]is08.MapEntry
	posted  []is08.MapActivationRequest
	deleted []string

	ignore   bool   // answer 200 and do not apply the map
	schedule bool   // answer 202
	fail     string // a path that answers 500
}

const mapIO = `{
  "inputs": {
    "in1": {"properties": {"name": "SDI 1", "description": "test"}, "parent": {"id": null, "type": null},
            "channels": [{"label": "L"}, {"label": "R"}], "caps": {"reordering": true, "block_size": 1}},
    "in2": {"properties": {"name": "SDI 2", "description": "test"}, "parent": {"id": null, "type": null},
            "channels": [{"label": "L"}], "caps": {"reordering": true, "block_size": 1}}
  },
  "outputs": {
    "out1": {"properties": {"name": "Tx 1", "description": "test"}, "source_id": null,
             "channels": [{"label": "1"}, {"label": "2"}], "caps": {"routable_inputs": ["in1", null]}},
    "out2": {"properties": {"name": "Tx 2", "description": "test"}, "source_id": null,
             "channels": [{"label": "1"}], "caps": {"routable_inputs": ["in2"]}},
    "out3": {"properties": {"name": "Tx 3", "description": "test"}, "source_id": null,
             "channels": [{"label": "1"}], "caps": {"routable_inputs": null}}
  }
}`

func newMapDevice(t *testing.T) *mapDevice {
	in1, zero := "in1", 0
	return &mapDevice{t: t, active: map[string]map[string]is08.MapEntry{
		"out1": {"0": {Input: &in1, ChannelIndex: &zero}, "1": {}},
	}}
}

func (d *mapDevice) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rest := r.URL.Path[strings.Index(r.URL.Path, "/v1.0/")+len("/v1.0/"):]
		if d.fail != "" && rest == d.fail {
			http.Error(w, "device fault", http.StatusInternalServerError)
			return
		}
		at := "1700000000:0"
		switch {
		case r.Method == http.MethodGet && rest == "io":
			_, _ = io.WriteString(w, mapIO)
		case r.Method == http.MethodGet && rest == "map/active":
			_ = json.NewEncoder(w).Encode(is08.MapActive{
				Activation: is08.ActivationResponse{ActivationTime: &at}, Map: d.active,
			})
		case r.Method == http.MethodGet && rest == "map/activations":
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodPost && rest == "map/activations":
			var req is08.MapActivationRequest
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &req); err != nil {
				d.t.Errorf("POST body: %v", err)
			}
			d.posted = append(d.posted, req)
			mode := req.Activation.Mode
			resp := map[string]is08.MapActivationResponse{"act-1": {
				Activation: is08.ActivationResponse{Mode: &mode, RequestedTime: req.Activation.RequestedTime, ActivationTime: &at},
				Action:     req.Action,
			}}
			if d.schedule {
				w.WriteHeader(http.StatusAccepted)
			} else if !d.ignore {
				for out, chans := range req.Action {
					if d.active[out] == nil {
						d.active[out] = map[string]is08.MapEntry{}
					}
					for ch, e := range chans {
						d.active[out][ch] = e
					}
				}
			}
			_ = json.NewEncoder(w).Encode(resp)
		case r.Method == http.MethodDelete && strings.HasPrefix(rest, "map/activations/"):
			d.deleted = append(d.deleted, strings.TrimPrefix(rest, "map/activations/"))
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}
}

// mapHarness is a catalogue with one Device that advertises the
// Channel Mapping API, and that API behind it.
func mapHarness(t *testing.T) (*harness, *mapDevice, string) {
	t.Helper()
	h := newHarness(t)
	d := newMapDevice(t)
	h.is08 = d.handler()
	dev := deviceWith(uuidN(1), "")
	base := strings.Replace(h.controlHref, "/x-nmos/connection/v1.1", "/x-nmos/channelmapping/", 1)
	dev.Controls = []is04.DeviceControl{
		{Href: base + "v0.9", Type: "urn:x-nmos:control:cm-ctrl/v0.9"},
		{Href: base + "v1.0", Type: "urn:x-nmos:control:cm-ctrl/v1.0"},
		{Href: h.controlHref, Type: "urn:x-nmos:control:sr-ctrl/v1.1"},
	}
	h.cat.devices = []is04.Device{dev}
	return h, d, dev.ID
}

func TestChannelMapReadsTheDevice(t *testing.T) {
	h, d, dev := mapHarness(t)

	res, err := h.ctrl.ChannelMap(context.Background(), ChannelMapRequest{DeviceID: dev})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(res.Endpoint, "/x-nmos/channelmapping/v1.0") {
		t.Errorf("endpoint = %s, want the highest cm-ctrl the Device advertises", res.Endpoint)
	}
	if len(res.IO.Outputs) != 3 || res.Active.Map["out1"]["0"].Input == nil || res.Pending == nil {
		t.Errorf("read = %+v", res)
	}
	if len(d.posted) != 0 {
		t.Error("a read must send no map")
	}
}

func TestChannelMapAppliesAndReadsBack(t *testing.T) {
	h, d, dev := mapHarness(t)

	res, err := h.ctrl.ChannelMap(context.Background(), ChannelMapRequest{DeviceID: dev, Routes: []ChannelRoute{
		{Output: "out1", Channel: 1, Input: "in1", InputChannel: 1},
		{Output: "out1", Channel: 0}, // unrouted
	}})
	if err != nil {
		t.Fatal(err)
	}
	if res.ActivationID != "act-1" || res.Scheduled {
		t.Errorf("result = %+v", res)
	}
	sent := d.posted[0]
	if sent.Activation.Mode != is08.ActivationModeImmediate || sent.Activation.RequestedTime != nil {
		t.Errorf("activation sent = %+v", sent.Activation)
	}
	if e := sent.Action["out1"]["1"]; e.Input == nil || *e.Input != "in1" || *e.ChannelIndex != 1 {
		t.Errorf("out1/1 sent = %+v", e)
	}
	if e := sent.Action["out1"]["0"]; e.Input != nil || e.ChannelIndex != nil {
		t.Errorf("out1/0 must be sent unrouted, got %+v", e)
	}
	if got := res.Active.Map["out1"]["1"]; got.Input == nil || *got.ChannelIndex != 1 {
		t.Errorf("the result must carry the map read back from the Device, got %+v", got)
	}
	if evs := h.rep.Snapshot(); len(evs) != 0 {
		t.Errorf("an applied map fires nothing, got %+v", evs)
	}
}

// A Device that answers 200 and does not apply the map is reported:
// the operator would otherwise hear the old routing behind a success.
func TestChannelMapReportsAMapTheDeviceDidNotApply(t *testing.T) {
	h, d, dev := mapHarness(t)
	d.ignore = true

	_, err := h.ctrl.ChannelMap(context.Background(), ChannelMapRequest{DeviceID: dev, Routes: []ChannelRoute{
		{Output: "out1", Channel: 1, Input: "in1", InputChannel: 1}, // stays unrouted on the Device
		{Output: "out1", Channel: 0},                                // stays routed on the Device
		{Output: "out3", Channel: 0, Input: "in2"},                  // not in the active map at all
	}})
	if err != nil {
		t.Fatal(err)
	}
	evs := h.rep.Snapshot()
	if len(evs) != 3 {
		t.Fatalf("%d event(s), want one per route the Device did not apply: %+v", len(evs), evs)
	}
	for _, ev := range evs {
		if ev.Code != "nmos_is08_map_not_applied" {
			t.Errorf("event = %+v", ev)
		}
	}
	if !strings.Contains(evs[0].Detail, "from input in1 channel 1") || !strings.Contains(evs[1].Detail, "unrouted") {
		t.Errorf("details = %q / %q", evs[0].Detail, evs[1].Detail)
	}
}

func TestChannelMapSchedulesAndCancels(t *testing.T) {
	h, d, dev := mapHarness(t)
	d.schedule = true

	res, err := h.ctrl.ChannelMap(context.Background(), ChannelMapRequest{
		DeviceID: dev, Mode: is08.ActivationModeScheduledRelative, When: "2:0",
		Routes: []ChannelRoute{{Output: "out3", Channel: 0, Input: "in1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Scheduled || res.ActivationID != "act-1" {
		t.Errorf("result = %+v", res)
	}
	if got := d.posted[0].Activation.RequestedTime; got == nil || *got != "2:0" {
		t.Errorf("requested_time sent = %v", got)
	}

	res, err = h.ctrl.ChannelMap(context.Background(), ChannelMapRequest{DeviceID: dev, Cancel: "act-1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Cancelled != "act-1" || len(d.deleted) != 1 {
		t.Errorf("cancel = %+v, device saw %v", res, d.deleted)
	}
}

func TestChannelMapDryRunSendsNothing(t *testing.T) {
	h, d, dev := mapHarness(t)

	res, err := h.ctrl.ChannelMap(context.Background(), ChannelMapRequest{
		DeviceID: dev, DryRun: true, Routes: []ChannelRoute{{Output: "out1", Channel: 1, Input: "in1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || res.Request == nil || res.ActivationID != "" {
		t.Errorf("dry run = %+v", res)
	}
	res, err = h.ctrl.ChannelMap(context.Background(), ChannelMapRequest{DeviceID: dev, DryRun: true, Cancel: "act-1"})
	if err != nil || res.Cancelled != "" {
		t.Errorf("dry-run cancel = %+v, %v", res, err)
	}
	if len(d.posted) != 0 || len(d.deleted) != 0 {
		t.Errorf("a dry run reached the Device: %v / %v", d.posted, d.deleted)
	}
}

// A map the Device cannot take is refused before it is sent, with
// every reason at once.
func TestChannelMapRefusesWhatTheDeviceCannotTake(t *testing.T) {
	h, d, dev := mapHarness(t)

	_, err := h.ctrl.ChannelMap(context.Background(), ChannelMapRequest{DeviceID: dev, Routes: []ChannelRoute{
		{Output: "nope", Channel: 0, Input: "in1"},
		{Output: "out1", Channel: 7, Input: "in1"},
		{Output: "out1", Channel: 0, Input: "nope"},
		{Output: "out1", Channel: 0, Input: "in1", InputChannel: 9},
		{Output: "out1", Channel: 0, Input: "in2"}, // out1 takes in1 or nothing
		{Output: "out2", Channel: 0},               // out2 cannot be left unrouted
		{Output: "out3", Channel: 0, Input: "in2"}, // fine: no restriction declared
	}})
	if err == nil {
		t.Fatal("the map must be refused")
	}
	for _, want := range []string{
		`no output "nope"`, "output out1 has 2 channel(s), not a channel 7", `no input "nope"`,
		"input in1 has 2 channel(s), not a channel 9", "output out1 does not take input in2",
		"output out2 cannot be left unrouted",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not say %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "out3") {
		t.Errorf("out3 declares no restriction and must not be refused: %v", err)
	}
	if len(d.posted) != 0 {
		t.Error("a refused map reached the Device")
	}
}

func TestChannelMapRequestIsCheckedBeforeAnythingIsWalked(t *testing.T) {
	h, _, dev := mapHarness(t)
	ctx := context.Background()
	route := []ChannelRoute{{Output: "out1", Channel: 0, Input: "in1"}}

	for name, req := range map[string]ChannelMapRequest{
		"no device":              {Routes: route},
		"cancel with routes":     {DeviceID: dev, Cancel: "act-1", Routes: route},
		"an unknown mode":        {DeviceID: dev, Mode: "sometime", Routes: route},
		"scheduled without time": {DeviceID: dev, Mode: is08.ActivationModeScheduledAbsolute, Routes: route},
		"a device not listed":    {DeviceID: uuidN(9), Routes: route},
	} {
		if _, err := h.ctrl.ChannelMap(ctx, req); err == nil {
			t.Errorf("%s: must be refused", name)
		}
	}

	// A Device with no cm-ctrl control has no IS-08 endpoint; one whose
	// control href is not a Channel Mapping URL cannot be driven.
	h.cat.devices = append(h.cat.devices, deviceWith(uuidN(2), h.controlHref))
	if _, err := h.ctrl.ChannelMap(ctx, ChannelMapRequest{DeviceID: uuidN(2)}); err == nil ||
		!strings.Contains(err.Error(), "cm-ctrl") {
		t.Errorf("a Device without IS-08: %v", err)
	}
	odd := deviceWith(uuidN(3), "")
	odd.Controls = []is04.DeviceControl{{Href: "http://h/x-nmos/channelmapping", Type: "urn:x-nmos:control:cm-ctrl/v1.0"}}
	h.cat.devices = append(h.cat.devices, odd)
	if _, err := h.ctrl.ChannelMap(ctx, ChannelMapRequest{DeviceID: uuidN(3)}); err == nil {
		t.Error("a control href with no version must be refused")
	}
}

// Every call to the Device can fail; each failure is the caller's to
// see, in the Device's words.
func TestChannelMapCarriesTheDevicesFailures(t *testing.T) {
	ctx := context.Background()
	route := []ChannelRoute{{Output: "out1", Channel: 1, Input: "in1"}}

	for _, tc := range []struct {
		fail string
		req  func(dev string) ChannelMapRequest
	}{
		{"io", func(dev string) ChannelMapRequest { return ChannelMapRequest{DeviceID: dev} }},
		{"map/active", func(dev string) ChannelMapRequest { return ChannelMapRequest{DeviceID: dev} }},
		{"map/activations", func(dev string) ChannelMapRequest { return ChannelMapRequest{DeviceID: dev} }},
		{"map/activations", func(dev string) ChannelMapRequest { return ChannelMapRequest{DeviceID: dev, Routes: route} }},
		{"map/activations/act-1", func(dev string) ChannelMapRequest {
			return ChannelMapRequest{DeviceID: dev, Cancel: "act-1"}
		}},
	} {
		h, d, dev := mapHarness(t)
		d.fail = tc.fail
		if _, err := h.ctrl.ChannelMap(ctx, tc.req(dev)); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
			t.Errorf("%s failing: %v", tc.fail, err)
		}
	}

	// The read-back after an applied map can fail too.
	h, d, dev := mapHarness(t)
	reads := 0
	inner := d.handler()
	h.is08 = func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "map/active") {
			if reads++; reads == 2 {
				http.Error(w, "device fault", http.StatusInternalServerError)
				return
			}
		}
		inner(w, r)
	}
	if _, err := h.ctrl.ChannelMap(ctx, ChannelMapRequest{DeviceID: dev, Routes: route}); err == nil {
		t.Error("a failed read-back must fail the call")
	}
}
