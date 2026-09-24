package codec

import (
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

// shufflerInfo is the audio shuffler's shape: no children, no
// template — the crosspoint keys are the flow UUIDs themselves, and
// the provider only says which collection they live in.
const shufflerInfo = `{
  "destinations": [
    {"path": "/api/io/ip/receivers/audio", "slots": "0-63",
     "group": {"uuid": "grp-dst", "name": "Receivers"}}
  ],
  "sources": [
    {"path": "/api/io/ip/senders/audio", "slots": "0-63",
     "group": {"uuid": "grp-src", "name": "Senders"}}
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

func TestAShufflerMatrixResolvesByUUID(t *testing.T) {
	// No children and no template: the crosspoint key IS the flow
	// uuid, and the provider says which collection it lives in.
	info, err := ParseMatrixInfo([]byte(shufflerInfo))
	if err != nil {
		t.Fatal(err)
	}
	dst := info.ResolveDestination("11111111-2222-3333-4444-555555555555")
	if !dst.Resolved {
		t.Fatalf("a uuid-keyed destination did not resolve: %+v", dst)
	}
	if dst.Path != "/api/io/ip/receivers/audio/11111111-2222-3333-4444-555555555555" {
		t.Errorf("path = %q", dst.Path)
	}
	if dst.ID != dst.Key {
		t.Errorf("on this matrix the key IS the id: %+v", dst)
	}
	src := info.ResolveSource("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	if !src.Resolved || src.Path != "/api/io/ip/senders/audio/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Errorf("source = %+v", src)
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
		{"CH{idx}", "CH0004", 4, -1, true},   // padding is the device's business
		{"DB{idx}-{subIdsIdx}", "DB000-05", 0, 5, true},
		{"IP{idx}-{subIdsIdx}", "IP015-00", 15, 0, true},
		{"CH{idx}", "IP04", 0, 0, false},     // another provider's label
		{"CH{idx}", "CH", 0, 0, false},       // no number at all
		{"CH{idx}", "CHxx", 0, 0, false},     // not a number
		{"CH{idx}", "CH04x", 0, 0, false},    // trailing rubbish
		{"CH{idx}-{subIdsIdx}", "CH04", 0, 0, false}, // second number missing
		{"no-markers", "no-markers", -1, -1, true},   // a literal template
		{"CH{idx", "CH04", 0, 0, false},      // a template that never closes
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
