package provider

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is08"
	httpsession "dhs/internal/amwa/session/http"
)

// audioBundle is a Node with one audio Source feeding a Sender (an
// IS-08 OUTPUT) and one audio Receiver (an IS-08 INPUT) -- the
// smallest device that has anything to channel-map at all.
func audioBundle() *NodeConfig {
	b := validBundle()
	dev := b.Devices[0].ID
	src := is04.Source{
		ResourceCore: is04.ResourceCore{
			ID: "cccccccc-3333-4333-8333-333333333333", Version: "0:0",
			Label: "src-audio", Description: "stereo source", Tags: map[string][]string{},
		},
		Caps: map[string]any{}, DeviceID: dev, Parents: []string{},
		Format:   is04.FormatAudio,
		Channels: []is04.SourceAudioChannel{{Label: "Left", Symbol: "L"}, {Label: "Right", Symbol: "R"}},
	}
	flow := is04.Flow{
		ResourceCore: is04.ResourceCore{
			ID: "dddddddd-4444-4444-8444-444444444444", Version: "0:0",
			Label: "flow-audio", Description: "L24", Tags: map[string][]string{},
		},
		SourceID: src.ID, DeviceID: dev, Parents: []string{},
		Format: is04.FormatAudio, MediaType: "audio/L24",
		SampleRate: &is04.GrainRate{Numerator: 48000}, BitDepth: 24,
	}
	fid := flow.ID
	snd := is04.Sender{
		ResourceCore: is04.ResourceCore{
			ID: "aaaaaaaa-1111-4111-8111-111111111111", Version: "0:0",
			Label: "snd-audio", Description: "test sender", Tags: map[string][]string{},
		},
		FlowID: &fid, Transport: is04.TransportRTP, DeviceID: dev,
		InterfaceBindings: []string{"eth0"},
	}
	rcv := is04.Receiver{
		ResourceCore: is04.ResourceCore{
			ID: "bbbbbbbb-2222-4222-8222-222222222222", Version: "0:0",
			Label: "rcv-audio", Description: "test receiver", Tags: map[string][]string{},
		},
		Transport: is04.TransportRTP, DeviceID: dev, Format: is04.FormatAudio,
		InterfaceBindings: []string{"eth0"},
	}
	b.Sources = append(b.Sources, src)
	b.Flows = append(b.Flows, flow)
	b.Senders = append(b.Senders, snd)
	b.Receivers = append(b.Receivers, rcv)
	b.Devices[0].Senders = []string{snd.ID}
	b.Devices[0].Receivers = []string{rcv.ID}
	return b
}

// cmTestServer builds a Channel Mapping API over an audio bundle and
// returns it plus a live HTTP server.
func cmTestServer(t *testing.T) (*IS08ChannelMappingServer, *httptest.Server) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cm := NewIS08ChannelMappingServer(logger, audioBundle(), IS08ChannelMappingConfig{APIVer: "v1.0"})
	srv := httpsession.NewServer(logger)
	cm.Mount(srv)
	ts := httptest.NewServer(srv.MuxHandler())
	t.Cleanup(ts.Close)
	return cm, ts
}

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

func cmGet(t *testing.T, ts *httptest.Server, path string, into any) int {
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

// TestChannelMappingIODerivedFromBundle: the IO view is derived from
// the IS-04 resources, not restated by hand. A Source with a Sender is
// an output; a Receiver is an input.
func TestChannelMappingIODerivedFromBundle(t *testing.T) {
	_, ts := cmTestServer(t)

	var io is08.IO
	if code := cmGet(t, ts, "/x-nmos/channelmapping/v1.0/io/", &io); code != 200 {
		t.Fatalf("io: got %d, want 200", code)
	}
	if len(io.Outputs) == 0 {
		t.Fatal("no outputs derived: an audio Source feeding a Sender is an output")
	}
	for id, o := range io.Outputs {
		if o.SourceID == nil || *o.SourceID == "" {
			t.Errorf("output %s: source_id must name the IS-04 Source it carries", id)
		}
		if len(o.Channels) == 0 {
			t.Errorf("output %s: an output with no channels cannot be mapped to", id)
		}
		if o.Caps == nil || len(o.Caps.RoutableInputs) == 0 {
			t.Errorf("output %s: routable_inputs must at least carry the null entry that permits unrouting", id)
		}
	}
	for id, in := range io.Inputs {
		if in.Parent == nil || in.Parent.Type == nil {
			t.Errorf("input %s: parent type must say whether it is a source or a receiver", id)
			continue
		}
		if *in.Parent.Type != "source" && *in.Parent.Type != "receiver" {
			t.Errorf("input %s: parent type %q is not a spec value", id, *in.Parent.Type)
		}
	}
	if err := is08.ValidateIO(io); err != nil {
		t.Errorf("derived IO fails its own validator: %v", err)
	}
}

// TestChannelMappingStartsUnrouted: every output channel is PRESENT
// and unrouted, because an absent entry reads as an output the device
// does not have.
func TestChannelMappingStartsUnrouted(t *testing.T) {
	cm, ts := cmTestServer(t)

	var active is08.MapActive
	if code := cmGet(t, ts, "/x-nmos/channelmapping/v1.0/map/active/", &active); code != 200 {
		t.Fatalf("map/active: got %d, want 200", code)
	}
	if len(active.Map) != len(cm.io.Outputs) {
		t.Fatalf("map covers %d output(s), device has %d", len(active.Map), len(cm.io.Outputs))
	}
	for outID, chans := range active.Map {
		want := len(cm.io.Outputs[outID].Channels)
		if len(chans) != want {
			t.Errorf("output %s: map has %d channel(s), output has %d", outID, len(chans), want)
		}
		for idx, e := range chans {
			if e.Input != nil || e.ChannelIndex != nil {
				t.Errorf("output %s channel %s: a fresh device routes nothing", outID, idx)
			}
		}
	}
	// No activation has happened, so all three fields are null.
	if active.Activation.Mode != nil || active.Activation.ActivationTime != nil {
		t.Error("activation block must be all-null before the first activation")
	}
}

// TestChannelMappingImmediateActivation: an immediate POST applies the
// route and answers 200 with what it did -- not 202 with an id to
// poll, because there is nothing left to wait for.
func TestChannelMappingImmediateActivation(t *testing.T) {
	cm, ts := cmTestServer(t)

	outID, inID := cmFirstPair(t, cm)
	idx := 0
	body := is08.MapActivationRequest{
		Activation: is08.Activation{Mode: is08.ActivationModeImmediate},
		Action: is08.MapEntries{
			outID: {"0": {Input: &inID, ChannelIndex: &idx}},
		},
	}
	raw, _ := json.Marshal(body)
	resp, err := ts.Client().Post(
		ts.URL+"/x-nmos/channelmapping/v1.0/map/activations/",
		"application/json", bytesReader(raw))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != stdhttp.StatusOK {
		t.Fatalf("immediate activation: got %d, want 200", resp.StatusCode)
	}
	var out is08.MapActivationResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Activation.ActivationTime == nil {
		t.Error("an activation that HAS happened must report when")
	}

	var active is08.MapActive
	cmGet(t, ts, "/x-nmos/channelmapping/v1.0/map/active/", &active)
	got := active.Map[outID]["0"]
	if got.Input == nil || *got.Input != inID {
		t.Errorf("channel 0 input = %v, want %q", got.Input, inID)
	}
}

// TestChannelMappingRejectsUnknownIDs: a device that accepts a route
// it cannot make reports a mapping that is not happening.
func TestChannelMappingRejectsUnknownIDs(t *testing.T) {
	cm, ts := cmTestServer(t)
	outID, _ := cmFirstPair(t, cm)
	bogus := "no-such-input"
	idx := 0

	cases := []struct {
		name   string
		action is08.MapEntries
	}{
		{"unknown output", is08.MapEntries{
			"no-such-output": {"0": {Input: &bogus, ChannelIndex: &idx}}}},
		{"unknown input", is08.MapEntries{
			outID: {"0": {Input: &bogus, ChannelIndex: &idx}}}},
		{"channel out of range", is08.MapEntries{
			outID: {"99": {}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(is08.MapActivationRequest{
				Activation: is08.Activation{Mode: is08.ActivationModeImmediate},
				Action:     tc.action,
			})
			resp, err := ts.Client().Post(
				ts.URL+"/x-nmos/channelmapping/v1.0/map/activations/",
				"application/json", bytesReader(raw))
			if err != nil {
				t.Fatalf("POST: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != stdhttp.StatusBadRequest {
				t.Errorf("got %d, want 400", resp.StatusCode)
			}
		})
	}
}

// TestChannelMappingScheduledIsQueued: a scheduled re-map answers 202
// with an id and does NOT take effect yet.
func TestChannelMappingScheduledIsQueued(t *testing.T) {
	cm, ts := cmTestServer(t)
	outID, inID := cmFirstPair(t, cm)
	idx := 0
	when := "4102444800:0" // far future

	raw, _ := json.Marshal(is08.MapActivationRequest{
		Activation: is08.Activation{
			Mode:          is08.ActivationModeScheduledAbsolute,
			RequestedTime: &when,
		},
		Action: is08.MapEntries{outID: {"0": {Input: &inID, ChannelIndex: &idx}}},
	})
	resp, err := ts.Client().Post(
		ts.URL+"/x-nmos/channelmapping/v1.0/map/activations/",
		"application/json", bytesReader(raw))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != stdhttp.StatusAccepted {
		t.Fatalf("scheduled activation: got %d, want 202", resp.StatusCode)
	}
	var out is08.MapActivationResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.ID == "" {
		t.Fatal("a queued activation needs an id to poll or cancel")
	}

	var active is08.MapActive
	cmGet(t, ts, "/x-nmos/channelmapping/v1.0/map/active/", &active)
	if e := active.Map[outID]["0"]; e.Input != nil {
		t.Error("a scheduled activation must not take effect on POST")
	}

	// It is listed, readable, and cancellable.
	var ids []string
	cmGet(t, ts, "/x-nmos/channelmapping/v1.0/map/activations/", &ids)
	if len(ids) != 1 {
		t.Errorf("activations list = %v, want one entry", ids)
	}
	if code := cmGet(t, ts, "/x-nmos/channelmapping/v1.0/map/activations/"+out.ID+"/", nil); code != 200 {
		t.Errorf("GET activation: got %d, want 200", code)
	}

	req, _ := stdhttp.NewRequest(stdhttp.MethodDelete,
		ts.URL+"/x-nmos/channelmapping/v1.0/map/activations/"+out.ID+"/", nil)
	del, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	_ = del.Body.Close()
	if del.StatusCode != stdhttp.StatusNoContent {
		t.Errorf("DELETE activation: got %d, want 204", del.StatusCode)
	}
	if code := cmGet(t, ts, "/x-nmos/channelmapping/v1.0/map/activations/"+out.ID+"/", nil); code != 404 {
		t.Errorf("cancelled activation still readable: got %d, want 404", code)
	}
}

// cmFirstPair picks one output and one input that can legally be
// joined, so the routing tests do not depend on map iteration order.
func cmFirstPair(t *testing.T, cm *IS08ChannelMappingServer) (outID, inID string) {
	t.Helper()
	for id := range cm.io.Outputs {
		if outID == "" || id < outID {
			outID = id
		}
	}
	for id := range cm.io.Inputs {
		if inID == "" || id < inID {
			inID = id
		}
	}
	if outID == "" || inID == "" {
		t.Skip("test bundle has no audio input/output pair to route")
	}
	return outID, inID
}
