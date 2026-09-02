package facade

// Capability-selection helpers for the BCP-006-01-02 / BCP-007-03-02
// Controller suites (issue #954): JPEG XS capability is the Flow's
// media_type (senders) / caps.media_types membership (receivers); MXL
// discovery is the transport URN; MXL compatibility is the suite's
// published rule — both ends MXL, media_type membership, and at least
// one BCP-004-01 constraint set satisfied by the Flow's fields.

import (
	"context"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
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

// ---- TR-08 (BCP-006-01-02 test_03/test_04) fixtures --------------------
//
// Modeled verbatim on the tool's fixture book, INCLUDING its trap: the
// Set A/B UHD1 base point (IP 7b) and the Set C UHD1 base point (IP 3c)
// are byte-identical in every REGISTERED flow field — same 3840x2160,
// 60000/1001, profile High444.12, level 4k-2, BT2100, PQ, even the same
// derived bit rate — and differ ONLY in the SDP's sampling
// (YCbCr-4:2:2 vs 4:4:4). That twin is what the second fleet run's
// over-selection receipts scored, and why the matcher reads the SDP.

// jxsvSDP renders an SDP the way the tool's video-jxsv.sdp template
// does.
func jxsvSDP(width, height int, framerate, sampling, colorimetry, tcs, profile, level string, bitrate int) string {
	return fmt.Sprintf(`v=0
o=- 1 1 IN IP4 10.0.0.1
s=Test
t=0 0
m=video 5004 RTP/AVP 97
c=IN IP4 239.1.1.1/32
b=AS:%d
a=rtpmap:97 jxsv/90000
a=fmtp:97 packetmode=0; profile=%s; level=%s; sublevel=Sublev3bpp; depth=10; width=%d; height=%d; exactframerate=%s; sampling=%s; colorimetry=%s; TCS=%s; SSN=ST2110-22:2022; TP=2110TPW
a=mediaclk:direct=0
`, bitrate, profile, level, width, height, framerate, sampling, colorimetry, tcs)
}

// tr08ConstraintSet builds one per-interop-point constraint set the
// way ControllerTest._generate_constraint_set does.
func tr08ConstraintSet(sampling string, w, h float64, grNum, grDen float64, colorimetry, tcs, profile, level string, minBR, maxBR float64) map[string]any {
	return map[string]any{
		"urn:x-nmos:cap:format:color_sampling":              map[string]any{"enum": []any{sampling}},
		"urn:x-nmos:cap:format:frame_width":                 map[string]any{"enum": []any{w}},
		"urn:x-nmos:cap:format:frame_height":                map[string]any{"enum": []any{h}},
		"urn:x-nmos:cap:format:grain_rate":                  map[string]any{"enum": []any{map[string]any{"numerator": grNum, "denominator": grDen}}},
		"urn:x-nmos:cap:format:interlace_mode":              map[string]any{"enum": []any{"progressive"}},
		"urn:x-nmos:cap:format:component_depth":             map[string]any{"enum": []any{float64(10)}},
		"urn:x-nmos:cap:format:colorspace":                  map[string]any{"enum": []any{colorimetry}},
		"urn:x-nmos:cap:format:transfer_characteristic":     map[string]any{"enum": []any{tcs}},
		"urn:x-nmos:cap:format:profile":                     map[string]any{"enum": []any{profile}},
		"urn:x-nmos:cap:format:level":                       map[string]any{"enum": []any{level}},
		"urn:x-nmos:cap:format:sublevel":                    map[string]any{"enum": []any{"Sublev3bpp", "Sublev4bpp"}},
		"urn:x-nmos:cap:transport:packet_transmission_mode": map[string]any{"enum": []any{"codestream"}},
		"urn:x-nmos:cap:format:bit_rate":                    map[string]any{"minimum": minBR, "maximum": maxBR},
		"urn:x-nmos:cap:transport:st2110_21_sender_type":    map[string]any{"enum": []any{"2110TN", "2110TNL", "2110TPW"}},
	}
}

const (
	tr08S30ID = "4db45e3b-0000-4000-8000-000000000030" // s30/collapse — Set C, UHD2, IP 4c
	tr08R1ID  = "89bab259-0000-4000-8000-000000000001" // r1/tb2 — Set A/B, UHD1
)

// tr08Snap builds the snapshot + SDP server. The registered fields are
// deliberately IDENTICAL for the UHD1 A/B and C senders — only their
// SDPs differ.
func tr08Snap(t *testing.T) *consumer.CatalogueSnapshot {
	t.Helper()
	sdps := map[string]string{
		// Set A/B UHD1, IP 7b (PQ) — sampling 4:2:2.
		"/ab-uhd1.sdp": jxsvSDP(3840, 2160, "60000/1001", "YCbCr-4:2:2", "BT2100", "PQ", "High444.12", "4k-2", 995000),
		// Set C UHD1, IP 3c (PQ) — the registered-twin: ONLY sampling differs.
		"/c-uhd1.sdp": jxsvSDP(3840, 2160, "60000/1001", "YCbCr-4:4:4", "BT2100", "PQ", "High444.12", "4k-2", 995000),
		// Set A/B UHD2, IP 9c (HLG) — sampling 4:2:2.
		"/ab-uhd2.sdp": jxsvSDP(7680, 4320, "60000/1001", "YCbCr-4:2:2", "BT2100", "HLG", "High444.12", "8k-2", 3977000),
		// Set C UHD2, IP 4c (HLG) — the receipts' s30/collapse.
		"/s30.sdp": jxsvSDP(7680, 4320, "60000/1001", "YCbCr-4:4:4", "BT2100", "HLG", "High444.12", "8k-2", 3981000),
	}
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		sdp, ok := sdps[r.URL.Path]
		if !ok {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/sdp")
		_, _ = w.Write([]byte(sdp))
	}))
	t.Cleanup(srv.Close)
	href := func(p string) *string { u := srv.URL + p; return &u }

	// Per-point constraint sets (the tool builds each mock receiver's
	// caps from exactly its compatible interop points).
	set7b := tr08ConstraintSet("YCbCr-4:2:2", 3840, 2160, 60000, 1001, "BT2100", "PQ", "High444.12", "4k-2", 746000, 1989000)
	set3c := tr08ConstraintSet("YCbCr-4:4:4", 3840, 2160, 60000, 1001, "BT2100", "PQ", "High444.12", "4k-2", 746000, 1991000)
	set9c := tr08ConstraintSet("YCbCr-4:2:2", 7680, 4320, 60000, 1001, "BT2100", "HLG", "High444.12", "8k-2", 2983000, 7955000)
	set4c := tr08ConstraintSet("YCbCr-4:4:4", 7680, 4320, 60000, 1001, "BT2100", "HLG", "High444.12", "8k-2", 2986000, 7963000)

	return &consumer.CatalogueSnapshot{
		Senders: []is04.Sender{
			{ResourceCore: is04.ResourceCore{ID: "ab0d0000-0000-4000-8000-00000000ud01"},
				Transport: "urn:x-nmos:transport:rtp.mcast", ManifestHref: href("/ab-uhd1.sdp")},
			{ResourceCore: is04.ResourceCore{ID: "c0000000-0000-4000-8000-00000000ud01"},
				Transport: "urn:x-nmos:transport:rtp.mcast", ManifestHref: href("/c-uhd1.sdp")},
			{ResourceCore: is04.ResourceCore{ID: "ab0d0000-0000-4000-8000-00000000ud02"},
				Transport: "urn:x-nmos:transport:rtp.mcast", ManifestHref: href("/ab-uhd2.sdp")},
			{ResourceCore: is04.ResourceCore{ID: tr08S30ID},
				Transport: "urn:x-nmos:transport:rtp.mcast", ManifestHref: href("/s30.sdp")},
			// A sender advertising no manifest can never be judged compatible.
			{ResourceCore: is04.ResourceCore{ID: "0000dead-0000-4000-8000-000000000000"},
				Transport: "urn:x-nmos:transport:rtp.mcast"},
		},
		Receivers: []is04.Receiver{
			// r1/tb2 — Set A/B, UHD1: A/B-UHD1 point sets only.
			{ResourceCore: is04.ResourceCore{ID: tr08R1ID}, Transport: "urn:x-nmos:transport:rtp",
				Caps: is04.ReceiverCaps{MediaTypes: []string{"video/jxsv"}, Version: "0:1",
					ConstraintSets: []map[string]any{set7b}}},
			// Set C, UHD2 receiver: A/B-UHD2 + C-UHD2 point sets.
			{ResourceCore: is04.ResourceCore{ID: "c0000000-0000-4000-8000-00000000ud02"}, Transport: "urn:x-nmos:transport:rtp",
				Caps: is04.ReceiverCaps{MediaTypes: []string{"video/jxsv"}, Version: "0:1",
					ConstraintSets: []map[string]any{set9c, set4c}}},
			// Set A/B, UHD2 receiver: A/B-UHD2 point sets only.
			{ResourceCore: is04.ResourceCore{ID: "ab0d0000-0000-4000-8000-00000000rc02"}, Transport: "urn:x-nmos:transport:rtp",
				Caps: is04.ReceiverCaps{MediaTypes: []string{"video/jxsv"}, Version: "0:1",
					ConstraintSets: []map[string]any{set9c}}},
			// Set C, UHD1 receiver: A/B-UHD1 + C-UHD1 point sets.
			{ResourceCore: is04.ResourceCore{ID: "c0000000-0000-4000-8000-00000000rc01"}, Transport: "urn:x-nmos:transport:rtp",
				Caps: is04.ReceiverCaps{MediaTypes: []string{"video/jxsv"}, Version: "0:1",
					ConstraintSets: []map[string]any{set7b, set3c}}},
			// Raw receiver: no coded-format capability declared.
			{ResourceCore: is04.ResourceCore{ID: "0000fa00-0000-4000-8000-000000000000"}, Transport: "urn:x-nmos:transport:rtp",
				Caps: is04.ReceiverCaps{MediaTypes: []string{"video/raw"}}},
		},
	}
}

// TestTR08CompatibleFromProse pins the two receipt shapes exactly:
//
//	test_03 — "… compatible for Sender s30/collapse …: Capability Set
//	C, Conformance Level UHD2, IP 4c": expected receivers are the C
//	and D families at UHD2 (here: the C-UHD2 receiver), and the A/B
//	UHD2 receiver must be rejected — its only discriminator against
//	s30 is the SDP's sampling.
//	test_04 — "… compatible for Receiver r1/tb2 …: Capability Set
//	A/B, Conformance Level UHD1": expected senders are A/B-UHD1 only;
//	the C-UHD1 sender is the registered-twin and must be rejected on
//	its SDP sampling alone.
func TestTR08CompatibleFromProse(t *testing.T) {
	snap := tr08Snap(t)
	ctx := context.Background()

	// test_03 direction (sender named in prose).
	q := "select the Receivers that are compatible with the following Sender:\n\ns30/collapse (Mock Sender 30, " + tr08S30ID + ")\n"
	got := tr08CompatibleFromProse(ctx, snap, q)
	if len(got) != 1 || !got["c0000000-0000-4000-8000-00000000ud02"] {
		t.Errorf("s30 receivers = %v, want exactly the C-UHD2 receiver (A/B-UHD2 must fall to sampling)", got)
	}

	// test_04 direction (receiver named in prose).
	q = "select the Senders that are compatible with the following Receiver:\n\nr1/tb2 (Mock Receiver 1, " + tr08R1ID + ")\n"
	got = tr08CompatibleFromProse(ctx, snap, q)
	if len(got) != 1 || !got["ab0d0000-0000-4000-8000-00000000ud01"] {
		t.Errorf("r1 senders = %v, want exactly the A/B-UHD1 sender (the C-UHD1 registered-twin must fall to sampling)", got)
	}

	// No resolvable UUID → nil (the caller's "unable to identify").
	if got = tr08CompatibleFromProse(ctx, snap, "compatible with (x, not-a-uuid)"); got != nil {
		t.Errorf("non-UUID prose must resolve to nil, got %v", got)
	}
	// A named sender whose SDP cannot be read → nil.
	if got = tr08CompatibleFromProse(ctx, snap, "compatible with (x, 0000dead-0000-4000-8000-000000000000)"); got != nil {
		t.Errorf("manifest-less sender must resolve to nil, got %v", got)
	}
}

// TestSDPCapParams pins the fmtp → BCP-004-01 parameter mapping on the
// tool's own template shape.
func TestSDPCapParams(t *testing.T) {
	params := sdpCapParams(jxsvSDP(7680, 4320, "60000/1001", "YCbCr-4:4:4", "BT2100", "HLG", "High444.12", "8k-2", 3981000))
	want := map[string]any{
		"urn:x-nmos:cap:format:media_type":                  "video/jxsv",
		"urn:x-nmos:cap:format:profile":                     "High444.12",
		"urn:x-nmos:cap:format:level":                       "8k-2",
		"urn:x-nmos:cap:format:sublevel":                    "Sublev3bpp",
		"urn:x-nmos:cap:format:color_sampling":              "YCbCr-4:4:4",
		"urn:x-nmos:cap:format:colorspace":                  "BT2100",
		"urn:x-nmos:cap:format:transfer_characteristic":     "HLG",
		"urn:x-nmos:cap:transport:st2110_21_sender_type":    "2110TPW",
		"urn:x-nmos:cap:transport:packet_transmission_mode": "codestream",
		"urn:x-nmos:cap:format:component_depth":             float64(10),
		"urn:x-nmos:cap:format:frame_width":                 float64(7680),
		"urn:x-nmos:cap:format:frame_height":                float64(4320),
		"urn:x-nmos:cap:format:interlace_mode":              "progressive",
		"urn:x-nmos:cap:format:bit_rate":                    float64(3981000),
		"urn:x-nmos:cap:format:grain_rate":                  is04.GrainRate{Numerator: 60000, Denominator: 1001},
	}
	for k, v := range want {
		if params[k] != v {
			t.Errorf("params[%s] = %v, want %v", k, params[k], v)
		}
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
