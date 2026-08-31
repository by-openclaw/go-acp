package provider

// IS-08 routing constraints: declared caps (channel_mapping seed) are
// ENFORCED on activations — the attacks IS-08-01 test_13/14/15 run,
// plus the tool's own legal route shapes which must keep working.

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is08"
	httpsession "dhs/internal/amwa/session/http"
)

// constrainedBundle extends audioBundle with a 4-channel block input
// (reordering=false, block_size=2) and a 4-channel output restricted
// to that input.
func constrainedBundle() *NodeConfig {
	b := audioBundle()
	dev := b.Devices[0].ID
	fourCh := []is04.SourceAudioChannel{
		{Label: "Ch1"}, {Label: "Ch2"}, {Label: "Ch3"}, {Label: "Ch4"},
	}
	blockIn := is04.Source{
		ResourceCore: is04.ResourceCore{
			ID: "eeeeeeee-5555-4555-8555-555555555555", Version: "0:0",
			Label: "src-block-in", Description: "block input", Tags: map[string][]string{},
		},
		Caps: map[string]any{}, DeviceID: dev, Parents: []string{},
		Format: is04.FormatAudio, Channels: fourCh,
	}
	wideSrc := is04.Source{
		ResourceCore: is04.ResourceCore{
			ID: "ffffffff-6666-4666-8666-666666666666", Version: "0:0",
			Label: "src-wide-out", Description: "wide output", Tags: map[string][]string{},
		},
		Caps: map[string]any{}, DeviceID: dev, Parents: []string{},
		Format: is04.FormatAudio, Channels: fourCh,
	}
	wideFlow := is04.Flow{
		ResourceCore: is04.ResourceCore{
			ID: "abababab-7777-4777-8777-777777777777", Version: "0:0",
			Label: "flow-wide", Description: "L24", Tags: map[string][]string{},
		},
		SourceID: wideSrc.ID, DeviceID: dev, Parents: []string{},
		Format: is04.FormatAudio, MediaType: "audio/L24",
		SampleRate: &is04.GrainRate{Numerator: 48000}, BitDepth: 24,
	}
	wfid := wideFlow.ID
	wideSnd := is04.Sender{
		ResourceCore: is04.ResourceCore{
			ID: "cdcdcdcd-8888-4888-8888-888888888888", Version: "0:0",
			Label: "snd-wide", Description: "wide sender", Tags: map[string][]string{},
		},
		FlowID: &wfid, Transport: is04.TransportRTP, DeviceID: dev,
		InterfaceBindings: []string{"eth0"},
	}
	b.Sources = append(b.Sources, blockIn, wideSrc)
	b.Flows = append(b.Flows, wideFlow)
	b.Senders = append(b.Senders, wideSnd)
	b.Devices[0].Senders = append(b.Devices[0].Senders, wideSnd.ID)

	f, bs := false, 2
	blockInID := blockIn.ID
	b.ChannelMapping = &ChannelMappingSeed{
		Inputs: map[string]*ChannelMappingInputSeed{
			blockIn.ID: {Reordering: &f, BlockSize: &bs},
		},
		Outputs: map[string]*ChannelMappingOutputSeed{
			wideSrc.ID: {RoutableInputs: []*string{&blockInID, nil}},
		},
	}
	return b
}

const (
	blockInID  = "eeeeeeee-5555-4555-8555-555555555555"
	wideOutID  = "ffffffff-6666-4666-8666-666666666666"
	stereoInID = "bbbbbbbb-2222-4222-8222-222222222222" // audioBundle receiver
)

func cmConstrainedServer(t *testing.T) (*IS08ChannelMappingServer, *httptest.Server) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	b := constrainedBundle()
	if err := validateBundle(b); err != nil {
		t.Fatalf("constrained bundle does not validate: %v", err)
	}
	cm := NewIS08ChannelMappingServer(logger, b, IS08ChannelMappingConfig{APIVer: "v1.0"})
	srv := httpsession.NewServer(logger)
	cm.Mount(srv)
	ts := httptest.NewServer(srv.MuxHandler())
	t.Cleanup(ts.Close)
	return cm, ts
}

func postActivation(t *testing.T, ts *httptest.Server, body string) (int, string) {
	t.Helper()
	resp, err := ts.Client().Post(ts.URL+"/x-nmos/channelmapping/v1.0/map/activations/",
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST activation: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func TestChannelMappingSeededCaps(t *testing.T) {
	_, ts := cmConstrainedServer(t)

	var caps is08.InputCaps
	if st := cmGet(t, ts, "/x-nmos/channelmapping/v1.0/inputs/"+blockInID+"/caps/", &caps); st != 200 {
		t.Fatalf("input caps = %d", st)
	}
	if caps.Reordering || caps.BlockSize != 2 {
		t.Errorf("seeded input caps = %+v, want reordering=false block_size=2", caps)
	}
	var ocaps is08.OutputCaps
	if st := cmGet(t, ts, "/x-nmos/channelmapping/v1.0/outputs/"+wideOutID+"/caps/", &ocaps); st != 200 {
		t.Fatalf("output caps = %d", st)
	}
	if len(ocaps.RoutableInputs) != 2 || ocaps.RoutableInputs[0] == nil || *ocaps.RoutableInputs[0] != blockInID || ocaps.RoutableInputs[1] != nil {
		t.Errorf("seeded routable_inputs = %v, want [block-in, null]", ocaps.RoutableInputs)
	}
}

func TestChannelMappingConstraintEnforcement(t *testing.T) {
	act := func(entries string) string {
		return `{"activation":{"mode":"activate_immediate"},"action":{"` + wideOutID + `":{` + entries + `}}}`
	}
	route := func(out int, in string, ch int) string {
		return `"` + itoa(out) + `":{"input":"` + in + `","channel_index":` + itoa(ch) + `}`
	}

	cases := []struct {
		name string
		body string
		want int
	}{
		{"forbidden input (test_13)", act(route(0, stereoInID, 0)), 400},
		{"cross-block swap, order kept (test_14)",
			act(route(0, blockInID, 2) + "," + route(1, blockInID, 3) + "," +
				route(2, blockInID, 0) + "," + route(3, blockInID, 1)), 400},
		{"out-of-block duplicate (test_15)",
			act(route(0, blockInID, 0) + "," + route(1, blockInID, 0)), 400},
		{"partial block", act(route(0, blockInID, 0)), 400},
		{"legal: block 0 repeated (tool route builder shape)",
			act(route(0, blockInID, 0) + "," + route(1, blockInID, 1) + "," +
				route(2, blockInID, 0) + "," + route(3, blockInID, 1)), 200},
		{"legal: block 1 repeated",
			act(route(0, blockInID, 2) + "," + route(1, blockInID, 3) + "," +
				route(2, blockInID, 2) + "," + route(3, blockInID, 3)), 200},
		{"legal: unroute all",
			act(`"0":{"input":null,"channel_index":null},"1":{"input":null,"channel_index":null},"2":{"input":null,"channel_index":null},"3":{"input":null,"channel_index":null}`), 200},
	}
	for _, tc := range cases {
		// Fresh server per case: immediate activations mutate the map.
		_, ts := cmConstrainedServer(t)
		st, body := postActivation(t, ts, tc.body)
		if st != tc.want {
			t.Errorf("%s: POST = %d %s, want %d", tc.name, st, body, tc.want)
		}
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
