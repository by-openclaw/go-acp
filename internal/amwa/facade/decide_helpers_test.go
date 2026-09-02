package facade

// Capability-selection helpers for the BCP-006-01-02 / BCP-007-03-02
// Controller suites (issue #954): JPEG XS capability is the Flow's
// media_type (senders) / caps.media_types membership (receivers); MXL
// discovery is the transport URN; MXL compatibility is the suite's
// published rule — both ends MXL, media_type membership, and at least
// one BCP-004-01 constraint set satisfied by the Flow's fields.

import (
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/consumer"
)

func strPtr(s string) *string { return &s }

// mxlSnap builds a registry snapshot shaped like the MXL suite's mock
// plant: one 1080p25 v210 MXL sender, one RTP sender, and receivers of
// varying compatibility.
func mxlSnap() *consumer.CatalogueSnapshot {
	v210caps := func(sets []map[string]any) is04.ReceiverCaps {
		return is04.ReceiverCaps{MediaTypes: []string{"video/v210"}, ConstraintSets: sets, Version: "0:1"}
	}
	matching := []map[string]any{{
		"urn:x-nmos:cap:meta:label":                      "1080p25",
		"urn:x-nmos:cap:format:media_type":               map[string]any{"enum": []any{"video/v210"}},
		"urn:x-nmos:cap:format:frame_width":              map[string]any{"maximum": float64(1920)},
		"urn:x-nmos:cap:format:frame_height":             map[string]any{"minimum": float64(720), "maximum": float64(1080)},
		"urn:x-nmos:cap:format:grain_rate":               map[string]any{"enum": []any{map[string]any{"numerator": float64(25), "denominator": float64(1)}}},
		"urn:x-nmos:cap:format:interlace_mode":           map[string]any{"enum": []any{"progressive"}},
		"urn:x-nmos:cap:format:component_depth":          map[string]any{"maximum": float64(10)},
		"urn:x-nmos:cap:format:transfer_characteristic":  map[string]any{"enum": []any{"SDR"}},
		"urn:x-nmos:cap:format:colorspace":               map[string]any{"enum": []any{"BT709"}},
		"urn:x-nmos:cap:format:some_unknown_cap_is_fine": map[string]any{"enum": []any{"ignored"}},
	}}
	tooNarrow := []map[string]any{{
		"urn:x-nmos:cap:format:frame_width": map[string]any{"maximum": float64(1280)},
	}}
	return &consumer.CatalogueSnapshot{
		Flows: []is04.Flow{
			{ResourceCore: is04.ResourceCore{ID: "flow-jxsv"}, MediaType: "video/jxsv"},
			{ResourceCore: is04.ResourceCore{ID: "flow-mxl"}, MediaType: "video/v210",
				FrameWidth: 1920, FrameHeight: 1080,
				GrainRate: &is04.GrainRate{Numerator: 25, Denominator: 1},
				Interlace: "progressive", ColorSpace: "BT709", TransferChar: "SDR",
				Components: []is04.FlowVideoComponent{{Name: "Y", BitDepth: 10}, {Name: "Cb", BitDepth: 10}}},
		},
		Senders: []is04.Sender{
			{ResourceCore: is04.ResourceCore{ID: "snd-jxsv"}, FlowID: strPtr("flow-jxsv"),
				Transport: "urn:x-nmos:transport:rtp.mcast"},
			{ResourceCore: is04.ResourceCore{ID: "snd-mxl"}, FlowID: strPtr("flow-mxl"),
				Transport: is04.TransportMXL},
		},
		Receivers: []is04.Receiver{
			{ResourceCore: is04.ResourceCore{ID: "rcv-jxsv"}, Transport: "urn:x-nmos:transport:rtp",
				Caps: is04.ReceiverCaps{MediaTypes: []string{"video/jxsv", "video/raw"}}},
			{ResourceCore: is04.ResourceCore{ID: "rcv-raw"}, Transport: "urn:x-nmos:transport:rtp",
				Caps: is04.ReceiverCaps{MediaTypes: []string{"video/raw"}}},
			{ResourceCore: is04.ResourceCore{ID: "rcv-mxl-ok"}, Transport: is04.TransportMXL,
				Caps: v210caps(matching)},
			{ResourceCore: is04.ResourceCore{ID: "rcv-mxl-narrow"}, Transport: is04.TransportMXL,
				Caps: v210caps(tooNarrow)},
			{ResourceCore: is04.ResourceCore{ID: "rcv-mxl-nosets"}, Transport: is04.TransportMXL,
				Caps: is04.ReceiverCaps{MediaTypes: []string{"video/v210"}}},
		},
	}
}

func TestJPEGXSCapabilitySelection(t *testing.T) {
	snap := mxlSnap()
	if got := jxsvCapableSenders(snap); !got["snd-jxsv"] || got["snd-mxl"] || len(got) != 1 {
		t.Errorf("jxsvCapableSenders = %v, want exactly snd-jxsv", got)
	}
	if got := jxsvCapableReceivers(snap); !got["rcv-jxsv"] || got["rcv-raw"] || len(got) != 1 {
		t.Errorf("jxsvCapableReceivers = %v, want exactly rcv-jxsv", got)
	}
}

func TestMXLDiscoverySelection(t *testing.T) {
	snap := mxlSnap()
	if got := mxlSenders(snap); !got["snd-mxl"] || got["snd-jxsv"] || len(got) != 1 {
		t.Errorf("mxlSenders = %v, want exactly snd-mxl", got)
	}
	got := mxlReceivers(snap)
	if len(got) != 3 || !got["rcv-mxl-ok"] || !got["rcv-mxl-narrow"] || !got["rcv-mxl-nosets"] {
		t.Errorf("mxlReceivers = %v, want the three MXL-transport receivers", got)
	}
}

// TestMXLCompatibility pins the suite's rule: the fully matching
// constraint set is compatible; a narrower frame_width maximum is not;
// NO constraint sets at all is not (the capability declaration is
// required); a non-MXL sender selects nothing.
func TestMXLCompatibility(t *testing.T) {
	snap := mxlSnap()
	got := compatibleReceiversForSender(snap, "snd-mxl")
	if len(got) != 1 || !got["rcv-mxl-ok"] {
		t.Errorf("compatibleReceiversForSender(snd-mxl) = %v, want exactly rcv-mxl-ok", got)
	}
	if got := compatibleReceiversForSender(snap, "snd-jxsv"); len(got) != 0 {
		t.Errorf("non-MXL sender must select nothing, got %v", got)
	}
	if got := compatibleReceiversForSender(snap, "missing"); len(got) != 0 {
		t.Errorf("unknown sender must select nothing, got %v", got)
	}
}

// tr08Snap models the BCP-006-01-02 mock plant: JPEG XS senders whose
// flows carry the TR-08 discriminators (profile ⇔ capability set,
// level ⇔ conformance level — the tool registers them via
// flow_params), a video/raw sender, and receivers whose BCP-004-01
// constraint sets enumerate exactly their compatible interop points'
// profile/level pairs (how ControllerTest builds mock caps).
func tr08Snap() *consumer.CatalogueSnapshot {
	profLevel := func(profile, level string) map[string]any {
		return map[string]any{
			"urn:x-nmos:cap:meta:label":         profile + " " + level,
			"urn:x-nmos:cap:format:media_type":  map[string]any{"enum": []any{"video/jxsv"}},
			"urn:x-nmos:cap:format:profile":     map[string]any{"enum": []any{profile}},
			"urn:x-nmos:cap:format:level":       map[string]any{"enum": []any{level}},
			"urn:x-nmos:cap:format:frame_width": map[string]any{"enum": []any{float64(1920)}},
		}
	}
	return &consumer.CatalogueSnapshot{
		Flows: []is04.Flow{
			{ResourceCore: is04.ResourceCore{ID: "flow-ab"}, MediaType: "video/jxsv",
				Profile: "Main422.10", Level: "2k-1", FrameWidth: 1920},
			{ResourceCore: is04.ResourceCore{ID: "flow-c"}, MediaType: "video/jxsv",
				Profile: "High444.12", Level: "2k-1", FrameWidth: 1920},
			{ResourceCore: is04.ResourceCore{ID: "flow-raw"}, MediaType: "video/raw",
				FrameWidth: 1920},
		},
		Senders: []is04.Sender{
			{ResourceCore: is04.ResourceCore{ID: "b0b00000-0000-4000-8000-00000000ab01"},
				FlowID: strPtr("flow-ab"), Transport: "urn:x-nmos:transport:rtp.mcast"},
			{ResourceCore: is04.ResourceCore{ID: "b0b00000-0000-4000-8000-00000000c001"},
				FlowID: strPtr("flow-c"), Transport: "urn:x-nmos:transport:rtp.mcast"},
			{ResourceCore: is04.ResourceCore{ID: "b0b00000-0000-4000-8000-0000000faw01"},
				FlowID: strPtr("flow-raw"), Transport: "urn:x-nmos:transport:rtp.mcast"},
		},
		Receivers: []is04.Receiver{
			// Set A/B receiver: compatible with A/B senders only.
			{ResourceCore: is04.ResourceCore{ID: "rcv-ab"}, Transport: "urn:x-nmos:transport:rtp",
				Caps: is04.ReceiverCaps{MediaTypes: []string{"video/jxsv"}, Version: "0:1",
					ConstraintSets: []map[string]any{profLevel("Main422.10", "2k-1")}}},
			// Set D receiver: compatible with A/B AND C senders (its
			// constraint sets enumerate both points).
			{ResourceCore: is04.ResourceCore{ID: "rcv-d"}, Transport: "urn:x-nmos:transport:rtp",
				Caps: is04.ReceiverCaps{MediaTypes: []string{"video/jxsv"}, Version: "0:1",
					ConstraintSets: []map[string]any{
						profLevel("Main422.10", "2k-1"), profLevel("High444.12", "2k-1")}}},
			// Raw receiver: no constraint sets, no JPEG XS capability.
			{ResourceCore: is04.ResourceCore{ID: "rcv-raw"}, Transport: "urn:x-nmos:transport:rtp",
				Caps: is04.ReceiverCaps{MediaTypes: []string{"video/raw"}}},
		},
	}
}

// TestTR08CompatibleFromProse: the counterpart UUID embedded in the
// question prose (the tool's display_answer format) drives both
// directions — a sender selects its compatible receivers (test_03), a
// receiver its compatible senders (test_04); no resolvable UUID means
// nil (the caller's "unable to identify" branch).
func TestTR08CompatibleFromProse(t *testing.T) {
	snap := tr08Snap()

	// test_03 shape: given the A/B sender, both jxsv receivers admit
	// its profile/level; the raw receiver never does.
	q := "select the Receivers compatible with:\n\ns0/rush (Mock Sender 0, b0b00000-0000-4000-8000-00000000ab01)\n"
	got := tr08CompatibleFromProse(snap, q)
	if len(got) != 2 || !got["rcv-ab"] || !got["rcv-d"] {
		t.Errorf("A/B sender receivers = %v, want rcv-ab + rcv-d", got)
	}
	// Given the C sender, only the D receiver's sets admit it.
	q = "compatible with (x, b0b00000-0000-4000-8000-00000000c001)"
	got = tr08CompatibleFromProse(snap, q)
	if len(got) != 1 || !got["rcv-d"] {
		t.Errorf("C sender receivers = %v, want exactly rcv-d", got)
	}
	// test_04 shape: given the D receiver, both jxsv senders match,
	// the raw sender (no profile/level) never.
	q = "select the Senders compatible with (x, rcv-d)" // not a UUID — resolve failure first
	if got = tr08CompatibleFromProse(snap, q); got != nil {
		t.Errorf("non-UUID prose must resolve to nil, got %v", got)
	}
	snap.Receivers[1].ID = "d0d00000-0000-4000-8000-000000000d01"
	q = "select the Senders compatible with (x, d0d00000-0000-4000-8000-000000000d01)"
	got = tr08CompatibleFromProse(snap, q)
	if len(got) != 2 || !got["b0b00000-0000-4000-8000-00000000ab01"] || !got["b0b00000-0000-4000-8000-00000000c001"] {
		t.Errorf("D receiver senders = %v, want the two jxsv senders", got)
	}
}

// TestConstraintSatisfied covers the three BCP-004-01 keywords over the
// value shapes the MXL profiles use.
func TestConstraintSatisfied(t *testing.T) {
	gr := is04.GrainRate{Numerator: 50} // denominator omitted = 1
	cases := []struct {
		name string
		v    any
		c    map[string]any
		want bool
	}{
		{"enum string hit", "BT709", map[string]any{"enum": []any{"BT709", "BT2020"}}, true},
		{"enum string miss", "BT601", map[string]any{"enum": []any{"BT709"}}, false},
		{"min/max inside", float64(1080), map[string]any{"minimum": float64(720), "maximum": float64(2160)}, true},
		{"below minimum", float64(576), map[string]any{"minimum": float64(720)}, false},
		{"above maximum", float64(4320), map[string]any{"maximum": float64(2160)}, false},
		{"grain rate default denominator", gr,
			map[string]any{"enum": []any{map[string]any{"numerator": float64(50), "denominator": float64(1)}}}, true},
		{"grain rate mismatch", gr,
			map[string]any{"enum": []any{map[string]any{"numerator": float64(25)}}}, false},
	}
	for _, tc := range cases {
		if got := constraintSatisfied(tc.v, tc.c); got != tc.want {
			t.Errorf("%s: constraintSatisfied = %v, want %v", tc.name, got, tc.want)
		}
	}
}
