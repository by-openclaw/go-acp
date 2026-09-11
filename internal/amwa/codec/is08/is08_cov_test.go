package is08

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/spec"
)

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

// routedMap is one output routing two channels from one input.
func routedMap() MapEntries {
	return MapEntries{
		"out-1": {
			"0": {Input: strPtr("in-1"), ChannelIndex: intPtr(0)},
			"1": {Input: nil, ChannelIndex: nil}, // unrouted, per spec
		},
	}
}

// The map dictionary's grammar is enforced on both key levels and on
// the routed/unrouted invariant: a half-filled entry names a channel
// on no input, which no device can act on.
func TestValidateMapEntriesGrammar(t *testing.T) {
	if err := ValidateMapEntries(routedMap()); err != nil {
		t.Fatalf("a well-formed map = %v", err)
	}
	if err := ValidateMapEntries(MapEntries{}); err != nil {
		t.Errorf("an empty map is legal: %v", err)
	}
	if err := ValidateMapEntries(nil); err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("a missing map = %v, want it refused", err)
	}

	for name, tc := range map[string]struct {
		m    MapEntries
		want string
	}{
		"an output id outside the grammar": {
			MapEntries{"out 1": {"0": {}}}, "output id",
		},
		"a channel key that is not an index": {
			MapEntries{"out-1": {"first": {}}}, "channel index key",
		},
		"a channel key with a leading zero": {
			MapEntries{"out-1": {"01": {}}}, "channel index key",
		},
		"an entry routed on one side only": {
			MapEntries{"out-1": {"0": {Input: strPtr("in-1")}}}, "both null or both set",
		},
		"an entry indexed on one side only": {
			MapEntries{"out-1": {"0": {ChannelIndex: intPtr(0)}}}, "both null or both set",
		},
		"an input id outside the grammar": {
			MapEntries{"out-1": {"0": {Input: strPtr("in 1"), ChannelIndex: intPtr(0)}}}, "input",
		},
		"a negative channel index": {
			MapEntries{"out-1": {"0": {Input: strPtr("in-1"), ChannelIndex: intPtr(-1)}}}, "must be >= 0",
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := ValidateMapEntries(tc.m)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("= %v, want an error mentioning %q", err, tc.want)
			}
		})
	}
}

// A request always names its mode, and the mode decides whether a
// requested_time belongs: immediate carries none, both scheduled
// modes require one in TAI form.
func TestValidateActivationRequestRules(t *testing.T) {
	if err := ValidateActivation(Activation{Mode: ActivationModeImmediate}); err != nil {
		t.Errorf("immediate = %v", err)
	}
	for _, mode := range []ActivationMode{ActivationModeScheduledRelative, ActivationModeScheduledAbsolute} {
		if err := ValidateActivation(Activation{Mode: mode, RequestedTime: strPtr("1600000000:0")}); err != nil {
			t.Errorf("%s = %v", mode, err)
		}
		if err := ValidateActivation(Activation{Mode: mode}); err == nil ||
			!strings.Contains(err.Error(), "required") {
			t.Errorf("%s without a time = %v", mode, err)
		}
		if err := ValidateActivation(Activation{Mode: mode, RequestedTime: strPtr("")}); err == nil {
			t.Errorf("%s with an empty time was accepted", mode)
		}
		if err := ValidateActivation(Activation{Mode: mode, RequestedTime: strPtr("noon")}); err == nil ||
			!strings.Contains(err.Error(), "TAI") {
			t.Errorf("%s with a non-TAI time = %v", mode, err)
		}
	}
	if err := ValidateActivation(Activation{Mode: ActivationModeImmediate, RequestedTime: strPtr("1:0")}); err == nil ||
		!strings.Contains(err.Error(), "must be null") {
		t.Errorf("immediate with a time = %v", err)
	}
	for _, mode := range []ActivationMode{"", "activate_later", "ACTIVATE_IMMEDIATE"} {
		if err := ValidateActivation(Activation{Mode: mode}); err == nil {
			t.Errorf("mode %q was accepted", mode)
		}
		if IsValidActivationMode(mode) {
			t.Errorf("IsValidActivationMode(%q) = true", mode)
		}
	}
}

// Every response field is nullable — an activation the server has not
// scheduled reports null throughout — but a value that is present
// must be well-formed.
func TestValidateActivationResponseRules(t *testing.T) {
	if err := ValidateActivationResponse(ActivationResponse{}); err != nil {
		t.Errorf("an all-null response = %v", err)
	}
	mode := ActivationModeImmediate
	full := ActivationResponse{
		Mode: &mode, RequestedTime: strPtr("1600000000:0"), ActivationTime: strPtr("1600000001:5"),
	}
	if err := ValidateActivationResponse(full); err != nil {
		t.Errorf("a fully-populated response = %v", err)
	}
	// The empty string is treated as absent on both timestamps.
	if err := ValidateActivationResponse(ActivationResponse{
		RequestedTime: strPtr(""), ActivationTime: strPtr(""),
	}); err != nil {
		t.Errorf("empty timestamps = %v", err)
	}

	bad := ActivationMode("activate_later")
	for name, tc := range map[string]ActivationResponse{
		"a mode the spec does not define":    {Mode: &bad},
		"a requested_time that is not TAI":   {RequestedTime: strPtr("noon")},
		"an activation_time that is not TAI": {ActivationTime: strPtr("noon")},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateActivationResponse(tc); err == nil {
				t.Error("was accepted")
			}
		})
	}
}

// The three composite bodies validate their activation half and their
// map half, in that order.
func TestValidateCompositeBodies(t *testing.T) {
	mode := ActivationModeImmediate
	goodResponse := ActivationResponse{Mode: &mode}
	badResponse := ActivationResponse{RequestedTime: strPtr("noon")}

	if err := ValidateMapActive(MapActive{Activation: goodResponse, Map: routedMap()}); err != nil {
		t.Errorf("a valid /map/active = %v", err)
	}
	if err := ValidateMapActive(MapActive{Activation: badResponse, Map: routedMap()}); err == nil {
		t.Error("a bad activation must fail /map/active")
	}
	if err := ValidateMapActive(MapActive{Activation: goodResponse}); err == nil {
		t.Error("a missing map must fail /map/active")
	}

	req := MapActivationRequest{Activation: Activation{Mode: ActivationModeImmediate}, Action: routedMap()}
	if err := ValidateMapActivationRequest(req); err != nil {
		t.Errorf("a valid activation request = %v", err)
	}
	if err := ValidateMapActivationRequest(MapActivationRequest{
		Activation: Activation{Mode: "activate_later"}, Action: routedMap(),
	}); err == nil {
		t.Error("a bad mode must fail the request")
	}
	if err := ValidateMapActivationRequest(MapActivationRequest{
		Activation: Activation{Mode: ActivationModeImmediate},
	}); err == nil {
		t.Error("a missing action must fail the request")
	}

	resp := MapActivationResponse{ID: "act-1", Activation: goodResponse, Action: routedMap()}
	if err := ValidateMapActivationResponse(resp); err != nil {
		t.Errorf("a valid activation response = %v", err)
	}
	if err := ValidateMapActivationResponse(MapActivationResponse{Activation: badResponse}); err == nil {
		t.Error("a bad activation must fail the response")
	}
	if err := ValidateMapActivationResponse(MapActivationResponse{Activation: goodResponse}); err == nil {
		t.Error("a missing action must fail the response")
	}
}

// The per-endpoint property bodies each carry one rule worth failing
// on: a block size a device cannot honour, an input id outside the
// grammar, a parent that is neither a source nor a receiver, a
// channel list with no channels.
func TestValidatePerEndpointBodies(t *testing.T) {
	if err := ValidateInputCaps(InputCaps{BlockSize: 1}); err != nil {
		t.Errorf("block_size 1 = %v", err)
	}
	if err := ValidateInputCaps(InputCaps{}); err == nil {
		t.Error("block_size 0 must be refused")
	}

	if err := ValidateOutputCaps(OutputCaps{}); err != nil {
		t.Errorf("unrestricted routable_inputs = %v", err)
	}
	if err := ValidateOutputCaps(OutputCaps{RoutableInputs: []*string{nil, strPtr("in-1")}}); err != nil {
		t.Errorf("an explicit null entry is legal: %v", err)
	}
	if err := ValidateOutputCaps(OutputCaps{RoutableInputs: []*string{strPtr("in 1")}}); err == nil {
		t.Error("an id outside the grammar must be refused")
	}

	const uuid = "11111111-1111-4111-8111-111111111111"
	for name, p := range map[string]InputParent{
		"both null":   {},
		"a source":    {ID: strPtr(uuid), Type: strPtr("source")},
		"a receiver":  {ID: strPtr(uuid), Type: strPtr("receiver")},
		"an empty id": {ID: strPtr("")},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateInputParent(p); err != nil {
				t.Errorf("= %v", err)
			}
		})
	}
	if err := ValidateInputParent(InputParent{ID: strPtr("not-a-uuid")}); err == nil {
		t.Error("a parent id that is not a UUID must be refused")
	}
	if err := ValidateInputParent(InputParent{Type: strPtr("flow")}); err == nil {
		t.Error("a parent type the spec does not define must be refused")
	}

	if err := ValidateChannels([]Channel{{Label: "L"}}); err != nil {
		t.Errorf("one labelled channel = %v", err)
	}
	if err := ValidateChannels(nil); err == nil {
		t.Error("an empty channel list must be refused")
	}
	if err := ValidateChannels([]Channel{{}}); err == nil {
		t.Error("a channel with no label must be refused")
	}

	if err := ValidateInputProperties(InputProperties{Name: "n", Description: "d"}); err != nil {
		t.Errorf("valid properties = %v", err)
	}
	if err := ValidateInputProperties(InputProperties{Description: "d"}); err == nil {
		t.Error("properties with no name must be refused")
	}
	if err := ValidateInputProperties(InputProperties{Name: "n"}); err == nil {
		t.Error("properties with no description must be refused")
	}
}

// fullIO is a /map/io body carrying every optional section, so
// ValidateIO walks all of them.
func fullIO() IO {
	const uuid = "22222222-2222-4222-8222-222222222222"
	return IO{
		Inputs: map[string]Input{
			"in-1": {
				Properties: &InputProperties{Name: "in", Description: "an input"},
				Parent:     &InputParent{ID: strPtr(uuid), Type: strPtr("source")},
				Channels:   []Channel{{Label: "L"}, {Label: "R"}},
				Caps:       &InputCaps{Reordering: true, BlockSize: 2},
			},
		},
		Outputs: map[string]Output{
			"out-1": {
				Properties: &OutputProperties{Name: "out", Description: "an output"},
				SourceID:   strPtr(uuid),
				Channels:   []Channel{{Label: "L"}},
				Caps:       &OutputCaps{RoutableInputs: []*string{strPtr("in-1"), nil}},
			},
		},
	}
}

// The aggregate /map/io view validates every section it carries, and
// both halves must be present even when empty — an absent `outputs`
// is a different statement from a device with none.
func TestValidateIO(t *testing.T) {
	if err := ValidateIO(fullIO()); err != nil {
		t.Fatalf("a full io view = %v", err)
	}
	if err := ValidateIO(IO{Inputs: map[string]Input{}, Outputs: map[string]Output{}}); err != nil {
		t.Errorf("a device with no io = %v", err)
	}
	if err := ValidateIO(IO{Outputs: map[string]Output{}}); err == nil {
		t.Error("a missing inputs section must be refused")
	}
	if err := ValidateIO(IO{Inputs: map[string]Input{}}); err == nil {
		t.Error("a missing outputs section must be refused")
	}

	for name, io := range map[string]IO{
		"an input id outside the grammar": {
			Inputs: map[string]Input{"in 1": {}}, Outputs: map[string]Output{},
		},
		"an output id outside the grammar": {
			Inputs: map[string]Input{}, Outputs: map[string]Output{"out 1": {}},
		},
		"input properties with no name": {
			Inputs:  map[string]Input{"in-1": {Properties: &InputProperties{Description: "d"}}},
			Outputs: map[string]Output{},
		},
		"input caps with no block size": {
			Inputs:  map[string]Input{"in-1": {Caps: &InputCaps{}}},
			Outputs: map[string]Output{},
		},
		"a parent that is neither source nor receiver": {
			Inputs:  map[string]Input{"in-1": {Parent: &InputParent{Type: strPtr("flow")}}},
			Outputs: map[string]Output{},
		},
		"an input channel with no label": {
			Inputs:  map[string]Input{"in-1": {Channels: []Channel{{}}}},
			Outputs: map[string]Output{},
		},
		"output properties with no name": {
			Inputs:  map[string]Input{},
			Outputs: map[string]Output{"out-1": {Properties: &OutputProperties{Description: "d"}}},
		},
		"output caps naming an id outside the grammar": {
			Inputs:  map[string]Input{},
			Outputs: map[string]Output{"out-1": {Caps: &OutputCaps{RoutableInputs: []*string{strPtr("in 1")}}}},
		},
		"a source_id that is not a UUID": {
			Inputs:  map[string]Input{},
			Outputs: map[string]Output{"out-1": {SourceID: strPtr("not-a-uuid")}},
		},
		"an output channel with no label": {
			Inputs:  map[string]Input{},
			Outputs: map[string]Output{"out-1": {Channels: []Channel{{}}}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateIO(io); err == nil {
				t.Error("was accepted")
			}
		})
	}
}

// Every body round-trips through its own encoder and decoder, and
// neither half lets an invalid document past: encoding validates
// before marshalling, decoding validates after — and an unknown field
// is a decode error, so a device's extension never arrives silently.
func TestEncodeDecodeRoundTrips(t *testing.T) {
	mode := ActivationModeImmediate
	active := MapActive{Activation: ActivationResponse{Mode: &mode}, Map: routedMap()}
	raw, err := EncodeMapActive(active)
	if err != nil {
		t.Fatalf("EncodeMapActive: %v", err)
	}
	back, err := DecodeMapActive(raw)
	if err != nil {
		t.Fatalf("DecodeMapActive: %v", err)
	}
	if len(back.Map["out-1"]) != 2 || *back.Map["out-1"]["0"].Input != "in-1" {
		t.Errorf("round-tripped map = %+v", back.Map)
	}

	req := MapActivationRequest{
		Activation: Activation{Mode: ActivationModeScheduledAbsolute, RequestedTime: strPtr("1600000000:0")},
		Action:     routedMap(),
	}
	raw, err = EncodeMapActivationRequest(req)
	if err != nil {
		t.Fatalf("EncodeMapActivationRequest: %v", err)
	}
	gotReq, err := DecodeMapActivationRequest(raw)
	if err != nil {
		t.Fatalf("DecodeMapActivationRequest: %v", err)
	}
	if gotReq.Activation.Mode != ActivationModeScheduledAbsolute {
		t.Errorf("round-tripped request = %+v", gotReq.Activation)
	}

	raw, err = EncodeIO(fullIO())
	if err != nil {
		t.Fatalf("EncodeIO: %v", err)
	}
	gotIO, err := DecodeIO(raw)
	if err != nil {
		t.Fatalf("DecodeIO: %v", err)
	}
	if len(gotIO.Inputs) != 1 || len(gotIO.Outputs) != 1 {
		t.Errorf("round-tripped io = %+v", gotIO)
	}

	// Encoding refuses an invalid document rather than emitting it.
	if _, err := EncodeMapActive(MapActive{}); err == nil {
		t.Error("EncodeMapActive accepted a body with no map")
	}
	if _, err := EncodeMapActivationRequest(MapActivationRequest{}); err == nil {
		t.Error("EncodeMapActivationRequest accepted a body with no mode")
	}
	if _, err := EncodeIO(IO{}); err == nil {
		t.Error("EncodeIO accepted a body with no sections")
	}

	// Decoding refuses malformed JSON, an unknown field, trailing
	// content, and a document that decodes but does not validate.
	for name, tc := range map[string]string{
		"malformed JSON": `{`,
		"an unknown field": `{"activation":{"mode":null,"requested_time":null,"activation_time":null},
			"map":{},"extra":1}`,
		"trailing content": `{"activation":{"mode":null,"requested_time":null,"activation_time":null},"map":{}} {}`,
		"a document that does not validate": `{"activation":{"mode":"activate_later","requested_time":null,
			"activation_time":null},"map":{}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeMapActive([]byte(tc)); err == nil {
				t.Error("was accepted")
			}
		})
	}
	if _, err := DecodeMapActivationRequest([]byte(`{`)); err == nil {
		t.Error("DecodeMapActivationRequest accepted malformed JSON")
	}
	if _, err := DecodeMapActivationRequest([]byte(`{"activation":{"mode":"activate_later"},"action":{}}`)); err == nil {
		t.Error("DecodeMapActivationRequest accepted an invalid document")
	}
	if _, err := DecodeIO([]byte(`{`)); err == nil {
		t.Error("DecodeIO accepted malformed JSON")
	}
	if _, err := DecodeIO([]byte(`{"inputs":{},"outputs":{"out 1":{}}}`)); err == nil {
		t.Error("DecodeIO accepted an invalid document")
	}
}

// stubCodec is a second IS-08 codec, used to exercise the registry
// without disturbing the v1.0 one the process registers.
type stubCodec struct{ ver string }

func (c stubCodec) SpecID() string    { return SpecID }
func (c stubCodec) APIVer() string    { return c.ver }
func (c stubCodec) SpecPatch() string { return c.ver + ".0" }

func (stubCodec) EncodeMapActive(m MapActive) ([]byte, error)   { return EncodeMapActive(m) }
func (stubCodec) DecodeMapActive(raw []byte) (MapActive, error) { return DecodeMapActive(raw) }
func (stubCodec) ValidateMapActive(m MapActive) error           { return ValidateMapActive(m) }

func (stubCodec) EncodeMapActivationRequest(r MapActivationRequest) ([]byte, error) {
	return EncodeMapActivationRequest(r)
}

func (stubCodec) DecodeMapActivationRequest(raw []byte) (MapActivationRequest, error) {
	return DecodeMapActivationRequest(raw)
}
func (stubCodec) ValidateMapActivationRequest(r MapActivationRequest) error {
	return ValidateMapActivationRequest(r)
}
func (stubCodec) EncodeIO(io IO) ([]byte, error)  { return EncodeIO(io) }
func (stubCodec) DecodeIO(raw []byte) (IO, error) { return DecodeIO(raw) }
func (stubCodec) ValidateIO(io IO) error          { return ValidateIO(io) }

var _ Codec = stubCodec{}

// wrongSpec is a codec that claims another spec — the registry's own
// guard against a mis-wired init.
type wrongSpec struct{ stubCodec }

func (wrongSpec) SpecID() string { return "is-05" }

// The per-minor registry answers by version, offers the whole set,
// negotiates the highest mutual minor with a peer, and refuses a
// codec registered under the wrong spec.
func TestCodecRegistry(t *testing.T) {
	Register(stubCodec{ver: "v1.1"})
	Register(stubCodec{ver: "v1.1"}) // idempotent for the same version

	if c, ok := Get("v1.1"); !ok || c.APIVer() != "v1.1" {
		t.Errorf("Get(v1.1) = %v, %v", c, ok)
	}
	if _, ok := Get("v9.9"); ok {
		t.Error("Get answered for a minor nobody registered")
	}
	if len(AllCodecs()) == 0 {
		t.Error("AllCodecs must list what is registered")
	}
	vers := SupportedVersions()
	if len(vers) == 0 || vers[len(vers)-1] != "v1.1" {
		t.Errorf("SupportedVersions = %v", vers)
	}
	if Default().APIVer() != vers[len(vers)-1] {
		t.Errorf("Default = %q, want the newest minor", Default().APIVer())
	}

	c, err := SelectHighest([]string{"v1.0", "v1.1", "v9.9"})
	if err != nil || c.APIVer() != "v1.1" {
		t.Errorf("SelectHighest = %v, %v; want the highest mutual minor", c, err)
	}
	if _, err := SelectHighest([]string{"v9.9"}); err == nil {
		t.Error("a peer with no mutual minor must be reported")
	}

	defer func() {
		if recover() == nil {
			t.Error("registering a codec for another spec must panic")
		}
	}()
	Register(wrongSpec{})
}

var _ spec.Versioned = stubCodec{}

// A channel index too large for the platform's int is refused rather
// than silently truncated — the key matched the grammar, and the
// parse is the second gate.
func TestValidateMapEntriesRejectsAnUnparsableIndex(t *testing.T) {
	err := ValidateMapEntries(MapEntries{"out-1": {"99999999999999999999": {}}})
	if err == nil || !strings.Contains(err.Error(), "channel index parse") {
		t.Errorf("an out-of-range channel key = %v", err)
	}
}

// Default is the convenience every caller reaches for; with nothing
// registered it names the missing blank import rather than returning
// a nil codec that fails later at the call site.
func TestDefaultWithoutACodec(t *testing.T) {
	prev := versions
	versions = spec.NewRegistry[Codec]()
	defer func() {
		versions = prev
		r := recover()
		if r == nil {
			t.Fatal("Default with no codec registered must panic")
		}
		if !strings.Contains(r.(string), "blank-import") {
			t.Errorf("panic = %v, want it to name the missing import", r)
		}
	}()
	_ = Default()
}
