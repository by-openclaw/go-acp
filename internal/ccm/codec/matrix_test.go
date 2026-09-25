package codec

import (
	"strings"
	"testing"
)

// The two products describe their matrices differently, and a
// connector that understood only one of them would be a connector for
// one product. Both shapes are real: the bridge's is a capture from
// 10.6.255.102, the shuffler's follows its own openapi.yml
// (MatrixProvider{path, group} with a uuid-keyed MatrixState).

// bridgeInfo is the shape the video bridge serves: ordered children
// and a template, so a label resolves by position.
const bridgeInfo = `{
  "description": "Video path matrix",
  "destinations": [
    {"children": [{"id":"ch-a"},{"id":"ch-b"},{"id":"ch-c"}],
     "path": "/api/v1/processing/video/channels",
     "template": "CH{idx}",
     "type": "Video processing channel"}
  ],
  "sources": [
    {"children": [{"id":"rx-0"},{"id":"rx-1"}],
     "path": "/api/v1/io/ip/receivers/video", "template": "IP{idx}", "type": "IP"},
    {"children": [{"id":"sdi-0"}],
     "path": "/api/v1/io/sdi", "template": "SDI{idx}", "type": "SDI"}
  ]
}`

// audioInfo is the bridge's audio matrix: each member carries channels
// the matrix routes separately, so the label has two numbers.
const audioInfo = `{
  "description": "Audio matrix",
  "destinations": [
    {"children": [{"id":"bank-0","subIds":16},{"id":"bank-1","subIds":16}],
     "path": "/api/v1/processing/audio/banks",
     "template": "DB{idx}-{subIdsIdx}", "type": "Delay Bank"}
  ],
  "sources": [
    {"children": [{"id":"tx-0","subIds":16}],
     "path": "/api/v1/io/ip/senders/audio",
     "template": "IP{idx}-{subIdsIdx}", "type": "IP"}
  ]
}`

// shufflerInfo is the audio shuffler's shape, exactly as SHUFFLE 2.0.0
// at 10.44.72.27 serves /matrices/audio/info: no children, no
// template, no type, no group — several providers per axis, and
// `slots: "channels"` saying that a crosspoint key names a channel
// INSIDE a member rather than the member.
//
// Every field here is from that device's own answer (the 17 728
// crosspoint export of 2026-09-25); nothing is supposed.
const shufflerInfo = `{
  "destinations": [
    {"path": "/io/ip/senders/audio", "slots": "channels"},
    {"path": "/processing/audio/analyser/channels"},
    {"path": "/io/madi/outputs", "slots": "channels"},
    {"path": "/processing/audio/delay", "slots": "channels"}
  ],
  "sources": [
    {"path": "/io/ip/receivers/audio", "slots": "channels"},
    {"path": "/io/madi/inputs", "slots": "channels"},
    {"path": "/processing/audio/generator/channels"},
    {"path": "/processing/audio/delay", "slots": "channels"},
    {"path": "/processing/audio/mute"}
  ]
}`

func TestABridgeMatrixResolvesByPosition(t *testing.T) {
	info, err := ParseMatrixInfo([]byte(bridgeInfo))
	if err != nil {
		t.Fatalf("ParseMatrixInfo: %v", err)
	}
	if info.Description != "Video path matrix" {
		t.Errorf("description = %q", info.Description)
	}

	dst := info.ResolveDestination("CH01")
	if !dst.Resolved || dst.ID != "ch-b" ||
		dst.Path != "/api/v1/processing/video/channels/ch-b" {
		t.Errorf("CH01 = %+v", dst)
	}
	if dst.Type != "Video processing channel" {
		t.Errorf("type = %q — the device's own word is what an operator reads", dst.Type)
	}
	if dst.Sub != -1 {
		t.Errorf("sub = %d, want -1: this member is routed whole", dst.Sub)
	}

	// The source axis has two providers, and the label says which.
	if src := info.ResolveSource("IP01"); !src.Resolved ||
		src.Path != "/api/v1/io/ip/receivers/video/rx-1" {
		t.Errorf("IP01 = %+v", src)
	}
	if src := info.ResolveSource("SDI00"); !src.Resolved ||
		src.Path != "/api/v1/io/sdi/sdi-0" {
		t.Errorf("SDI00 = %+v", src)
	}

	// A label past the end of the axis is not invented.
	if got := info.ResolveDestination("CH99"); got.Resolved {
		t.Errorf("CH99 = %+v — there is no ninety-ninth channel", got)
	}
	// Nor is one whose prefix belongs to no provider.
	if got := info.ResolveSource("MADI00"); got.Resolved {
		t.Errorf("MADI00 = %+v", got)
	}
	// An empty key resolves to nothing rather than to the first member.
	if got := info.ResolveSource(""); got.Resolved {
		t.Errorf("empty key = %+v", got)
	}
}

func TestAnAudioMatrixResolvesAChannelWithinAMember(t *testing.T) {
	// 4352 crosspoints over 16 delay banks: the resource is the bank,
	// and the crosspoint is one channel of it. Both halves matter — a
	// UI that opened the bank without knowing the channel would show
	// the wrong fader.
	info, err := ParseMatrixInfo([]byte(audioInfo))
	if err != nil {
		t.Fatal(err)
	}
	dst := info.ResolveDestination("DB001-05")
	if !dst.Resolved || dst.ID != "bank-1" ||
		dst.Path != "/api/v1/processing/audio/banks/bank-1" {
		t.Fatalf("DB001-05 = %+v", dst)
	}
	if dst.Sub != 5 {
		t.Errorf("channel = %d, want 5", dst.Sub)
	}
	if src := info.ResolveSource("IP000-15"); !src.Resolved || src.Sub != 15 {
		t.Errorf("IP000-15 = %+v", src)
	}
	// A label missing its second number does not fit the template.
	if got := info.ResolveDestination("DB001"); got.Resolved {
		t.Errorf("DB001 = %+v — the template has two numbers", got)
	}
}

func TestAShufflerMatrixCannotBeResolvedFromItsInfoAlone(t *testing.T) {
	// Four destination providers and five source providers, all
	// UUID-keyed, and the UUIDs are channels one level below the
	// collections named here. Nothing in this body says which stream
	// owns which channel, so guessing a provider would be inventing an
	// answer: the honest result is unresolved, until the device is
	// asked. See SetIndex and the consumer's link pass.
	info, err := ParseMatrixInfo([]byte(shufflerInfo))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.ResolveDestination("11111111-2222-3333-4444-555555555555"); got.Resolved {
		t.Errorf("resolved to %+v with nothing to resolve it by", got)
	}
	if !info.Destinations[0].RoutesChannels() {
		t.Error("slots: channels means the key is a channel, not a stream")
	}
	if info.Destinations[1].RoutesChannels() {
		t.Error("a provider with no slots routes its members whole")
	}
}

func TestAnIndexFromTheDeviceResolvesWhatTheInfoCannot(t *testing.T) {
	// What the link pass hands back: the channel UUID, the stream that
	// owns it, and the position within that stream. With it, a
	// crosspoint names a resource an operator can open.
	info, err := ParseMatrixInfo([]byte(shufflerInfo))
	if err != nil {
		t.Fatal(err)
	}
	const ch = "86940bd5-6e6c-5931-977a-c852f6e91826"
	info.SetIndex(Index{ch: {
		Key: ch, ID: "0001ac17-cabf-53cf-a7f6-580b6a3e91c0", Sub: 0,
		Path:     "/io/ip/receivers/audio/0001ac17-cabf-53cf-a7f6-580b6a3e91c0/channels/" + ch,
		Type:     "IP",
		Resolved: true,
	}}, nil)

	src := info.ResolveSource(ch)
	if !src.Resolved || src.Sub != 0 ||
		src.ID != "0001ac17-cabf-53cf-a7f6-580b6a3e91c0" {
		t.Fatalf("source = %+v", src)
	}
	if !strings.HasSuffix(src.Path, "/channels/"+ch) {
		t.Errorf("path = %q — a channel crosspoint must name the channel", src.Path)
	}
	// The other axis was given no index and still answers honestly.
	if got := info.ResolveDestination(ch); got.Resolved {
		t.Errorf("destination = %+v", got)
	}
	// A key the index does not hold is not invented either.
	if got := info.ResolveSource("no-such-channel"); got.Resolved {
		t.Errorf("unknown key = %+v", got)
	}
}

func TestASingleProviderUUIDMatrixStillResolvesFromItsInfo(t *testing.T) {
	// One provider, no children, no slots: the key can only be a member
	// of that one collection, and saying so beats saying nothing.
	info, err := ParseMatrixInfo([]byte(`{
	  "destinations": [{"path": "/api/io/ip/receivers/audio", "type": "IP"}],
	  "sources": [{"path": "/api/io/ip/senders/audio", "type": "IP"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	dst := info.ResolveDestination("11111111-2222-3333-4444-555555555555")
	if !dst.Resolved ||
		dst.Path != "/api/io/ip/receivers/audio/11111111-2222-3333-4444-555555555555" {
		t.Fatalf("destination = %+v", dst)
	}
	if dst.ID != dst.Key {
		t.Errorf("on this matrix the key IS the id: %+v", dst)
	}
}

func TestResolveStateGivesBothSidesOfEveryCrosspoint(t *testing.T) {
	info, err := ParseMatrixInfo([]byte(bridgeInfo))
	if err != nil {
		t.Fatal(err)
	}
	points, err := info.ResolveState([]byte(`{"CH02":"SDI00","CH00":"IP01","CH01":"NOPE00"}`))
	if err != nil {
		t.Fatalf("ResolveState: %v", err)
	}
	if len(points) != 3 {
		t.Fatalf("crosspoints = %d", len(points))
	}
	// Sorted by destination, so two captures of one device diff.
	if points[0].Destination.Key != "CH00" || points[2].Destination.Key != "CH02" {
		t.Errorf("order = %s %s %s", points[0].Destination.Key,
			points[1].Destination.Key, points[2].Destination.Key)
	}
	if points[0].Source.Path != "/api/v1/io/ip/receivers/video/rx-1" {
		t.Errorf("CH00 source = %+v", points[0].Source)
	}
	// A source no provider accounts for is REPORTED, not dropped: the
	// routing is still real, and an unresolvable name is a fact about
	// the device.
	if points[1].Source.Resolved {
		t.Errorf("NOPE00 resolved to %+v", points[1].Source)
	}
	if points[1].Destination.Path == "" {
		t.Error("the destination must still resolve when the source does not")
	}
}

func TestMatrixRefusalsAreNamed(t *testing.T) {
	if _, err := ParseMatrixInfo([]byte(`not json`)); err == nil {
		t.Error("a body that is not JSON must be an error")
	}
	if _, err := ParseMatrixInfo([]byte(`{"description":"empty"}`)); err == nil {
		t.Error("a matrix with neither axis is not a matrix")
	}
	info, err := ParseMatrixInfo([]byte(bridgeInfo))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := info.ResolveState([]byte(`["not","a","map"]`)); err == nil {
		t.Error("a crosspoint map that is not a map must be an error")
	}
}

func TestIndexesFromTemplate(t *testing.T) {
	cases := []struct {
		template, key string
		idx, sub      int
		ok            bool
	}{
		{"CH{idx}", "CH04", 4, -1, true},
		{"CH{idx}", "CH0004", 4, -1, true}, // padding is the device's business
		{"DB{idx}-{subIdsIdx}", "DB000-05", 0, 5, true},
		{"IP{idx}-{subIdsIdx}", "IP015-00", 15, 0, true},
		{"CH{idx}", "IP04", 0, 0, false},             // another provider's label
		{"CH{idx}", "CH", 0, 0, false},               // no number at all
		{"CH{idx}", "CHxx", 0, 0, false},             // not a number
		{"CH{idx}", "CH04x", 0, 0, false},            // trailing rubbish
		{"CH{idx}-{subIdsIdx}", "CH04", 0, 0, false}, // second number missing
		{"no-markers", "no-markers", -1, -1, true},   // a literal template
		{"CH{idx", "CH04", 0, 0, false},              // a template that never closes
	}
	for _, c := range cases {
		idx, sub, ok := indexesFromTemplate(c.template, c.key)
		if ok != c.ok {
			t.Errorf("%s / %s: ok = %v, want %v", c.template, c.key, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if idx != c.idx || sub != c.sub {
			t.Errorf("%s / %s = (%d,%d), want (%d,%d)", c.template, c.key, idx, sub, c.idx, c.sub)
		}
	}
}
