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
