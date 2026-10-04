package bcp00401

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04"
)

// set decodes a constraint set from the JSON a Receiver would publish,
// so every case is evaluated on exactly what arrives on the wire.
func set(t *testing.T, src string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(src), &out); err != nil {
		t.Fatalf("constraint set %s: %v", src, err)
	}
	return out
}

func constraint(t *testing.T, src string) map[string]any { return set(t, src) }

// The three keywords of a parameter constraint (param_constraint_*.json)
// over the four value kinds: string, number, boolean, rational.
func TestSatisfied(t *testing.T) {
	r50 := is04.GrainRate{Numerator: 50}
	r5994 := is04.GrainRate{Numerator: 60000, Denominator: 1001}
	cases := []struct {
		name       string
		v          any
		constraint string
		want       bool
	}{
		{"string in the enum", "video/jxsv", `{"enum":["video/raw","video/jxsv"]}`, true},
		{"string outside the enum", "video/H264", `{"enum":["video/raw","video/jxsv"]}`, false},
		{"string against a numeric enum", "1080", `{"enum":[1080]}`, false},
		{"number in the enum", 1080.0, `{"enum":[720,1080]}`, true},
		{"number outside the enum", 2160.0, `{"enum":[720,1080]}`, false},
		{"number at the minimum", 1280.0, `{"minimum":1280}`, true},
		{"number below the minimum", 1279.0, `{"minimum":1280}`, false},
		{"number at the maximum", 1920.0, `{"maximum":1920}`, true},
		{"number above the maximum", 3840.0, `{"maximum":1920}`, false},
		{"number inside a range", 10.0, `{"minimum":8,"maximum":12}`, true},
		{"number against a rational bound", 10.0, `{"minimum":{"numerator":8}}`, false},
		{"string against a bound", "10", `{"minimum":8}`, false},
		{"boolean in the enum", true, `{"enum":[true]}`, true},
		{"boolean outside the enum", false, `{"enum":[true]}`, false},
		{"boolean against a string enum", true, `{"enum":["true"]}`, false},
		{"rational in the enum, implicit denominator", r50, `{"enum":[{"numerator":25},{"numerator":50}]}`, true},
		{"rational in the enum, explicit denominator", r5994, `{"enum":[{"numerator":60000,"denominator":1001}]}`, true},
		{"rational equal as a fraction", is04.GrainRate{Numerator: 100, Denominator: 2}, `{"enum":[{"numerator":50}]}`, true},
		{"rational outside the enum", r5994, `{"enum":[{"numerator":50},{"numerator":60}]}`, false},
		{"rational against a numeric enum", r50, `{"enum":[50]}`, false},
		{"rational with no numerator in the bound", r50, `{"minimum":{"denominator":1}}`, false},
		{"rational at the minimum", r50, `{"minimum":{"numerator":50}}`, true},
		{"rational below the minimum", is04.GrainRate{Numerator: 25}, `{"minimum":{"numerator":30000,"denominator":1001}}`, false},
		{"rational above the maximum", is04.GrainRate{Numerator: 120}, `{"maximum":{"numerator":60}}`, false},
		{"rational inside a range", r5994, `{"minimum":{"numerator":50},"maximum":{"numerator":60}}`, true},
		{"rational bound with a zero denominator reads as 1", r50, `{"maximum":{"numerator":50,"denominator":0}}`, true},
		{"an empty constraint constrains nothing", "anything", `{}`, true},
		{"a kind the evaluator does not order", []any{1}, `{"minimum":1}`, false},
	}
	for _, tc := range cases {
		if got := Satisfied(tc.v, constraint(t, tc.constraint)); got != tc.want {
			t.Errorf("%s: Satisfied(%v, %s) = %v, want %v", tc.name, tc.v, tc.constraint, got, tc.want)
		}
	}
}

// What IS-04 states about a stream, by capability URN — absent members
// stay absent, never zero.
func TestFlowParams(t *testing.T) {
	video := &is04.Flow{
		Format: "urn:x-nmos:format:video", MediaType: "video/jxsv",
		GrainRate:  &is04.GrainRate{Numerator: 50},
		FrameWidth: 1920, FrameHeight: 1080,
		Interlace: "progressive", ColorSpace: "BT709", TransferChar: "SDR",
		Components: []is04.FlowVideoComponent{{Name: "Y", BitDepth: 10}, {Name: "Cb", BitDepth: 8}},
		Profile:    "High444.12", Level: "2k-1", Sublevel: "Sublev3bpp", FlowBitRate: 200000,
	}
	want := map[string]any{
		CapMediaType: "video/jxsv", CapGrainRate: is04.GrainRate{Numerator: 50},
		CapFrameWidth: 1920.0, CapFrameHeight: 1080.0,
		CapInterlaceMode: "progressive", CapColorspace: "BT709", CapTransferChar: "SDR",
		CapComponentDepth: 10.0, CapProfile: "High444.12", CapLevel: "2k-1", CapSublevel: "Sublev3bpp",
		CapFormatBitRate: 200000.0,
	}
	if got := FlowParams(video, nil); !reflect.DeepEqual(got, want) {
		t.Errorf("video params =\n%v\nwant\n%v", got, want)
	}

	audio := &is04.Flow{
		Format: "urn:x-nmos:format:audio", MediaType: "audio/L24",
		SampleRate: &is04.GrainRate{Numerator: 48000}, BitDepth: 24,
	}
	src := &is04.Source{Channels: []is04.SourceAudioChannel{{Label: "L"}, {Label: "R"}}}
	wantAudio := map[string]any{
		CapMediaType: "audio/L24", CapSampleRate: is04.GrainRate{Numerator: 48000},
		CapSampleDepth: 24.0, CapChannelCount: 2.0,
	}
	if got := FlowParams(audio, src); !reflect.DeepEqual(got, wantAudio) {
		t.Errorf("audio params = %v, want %v", got, wantAudio)
	}
	// A Flow that states nothing yields nothing — and a Source with no
	// channels states no channel count.
	if got := FlowParams(&is04.Flow{}, &is04.Source{}); len(got) != 0 {
		t.Errorf("an empty flow states %v", got)
	}
}

// One set: every constraint whose parameter the stream states must
// hold; meta keys and unstated parameters are passed over; the refusal
// names the first violated parameter in URN order.
func TestSetSatisfied(t *testing.T) {
	params := map[string]any{
		CapMediaType: "video/raw", CapFrameWidth: 3840.0, CapFrameHeight: 2160.0,
		CapGrainRate: is04.GrainRate{Numerator: 50},
	}
	hd := set(t, `{
		"urn:x-nmos:cap:meta:label": "HD",
		"urn:x-nmos:cap:meta:preference": 10,
		"urn:x-nmos:cap:format:media_type": {"enum": ["video/raw"]},
		"urn:x-nmos:cap:format:frame_width": {"maximum": 1920},
		"urn:x-nmos:cap:format:frame_height": {"maximum": 1080},
		"urn:x-nmos:cap:transport:packet_time": {"enum": [1]},
		"urn:x-vendor:cap:oddity": "not a constraint object"
	}`)
	ok, why := SetSatisfied(params, hd)
	if ok || why != "format:frame_height is 2160, outside maximum 1080" {
		t.Errorf("UHD against the HD set = %v %q", ok, why)
	}
	params[CapFrameWidth], params[CapFrameHeight] = 1920.0, 1080.0
	if ok, why := SetSatisfied(params, hd); !ok || why != "" {
		t.Errorf("HD against the HD set = %v %q, want satisfied", ok, why)
	}
	rate := set(t, `{"urn:x-nmos:cap:format:grain_rate": {"enum": [{"numerator": 60000, "denominator": 1001}], "minimum": {"numerator": 59}, "maximum": {"numerator": 60}}}`)
	if ok, why := SetSatisfied(params, rate); ok || !strings.Contains(why, "format:grain_rate is 50, outside enum") ||
		!strings.Contains(why, "minimum") || !strings.Contains(why, "maximum") {
		t.Errorf("50 Hz against a 59.94-only set = %v %q", ok, why)
	}
	params[CapGrainRate] = is04.GrainRate{Numerator: 30000, Denominator: 1001}
	if _, why := SetSatisfied(params, rate); !strings.Contains(why, "is 30000/1001,") {
		t.Errorf("a fractional rate is shown as %q", why)
	}
}

// The Receiver-level rule: one enabled set satisfied is enough; a set
// switched off admits nothing; with none satisfied every enabled set
// says why; and a Receiver with no enabled set has declared nothing.
func TestAnySetSatisfied(t *testing.T) {
	uhd := map[string]any{CapMediaType: "video/raw", CapFrameHeight: 2160.0}
	hd := set(t, `{"urn:x-nmos:cap:meta:label": "HD", "urn:x-nmos:cap:format:frame_height": {"maximum": 1080}}`)
	sd := set(t, `{"urn:x-nmos:cap:format:frame_height": {"maximum": 576}}`)
	any2160 := set(t, `{"urn:x-nmos:cap:format:frame_height": {"enum": [2160]}}`)
	off := set(t, `{"urn:x-nmos:cap:meta:enabled": false, "urn:x-nmos:cap:format:frame_height": {"enum": [2160]}}`)
	on := set(t, `{"urn:x-nmos:cap:meta:enabled": true, "urn:x-nmos:cap:format:frame_height": {"enum": [2160]}}`)

	if ok, declared, why := AnySetSatisfied(uhd, []map[string]any{hd, any2160}); !ok || !declared || why != nil {
		t.Errorf("a stream the second set admits = %v %v %v", ok, declared, why)
	}
	ok, declared, why := AnySetSatisfied(uhd, []map[string]any{hd, sd, off})
	if ok || !declared || len(why) != 2 ||
		why[0] != "HD: format:frame_height is 2160, outside maximum 1080" ||
		why[1] != "constraint set 1: format:frame_height is 2160, outside maximum 576" {
		t.Errorf("a stream no enabled set admits = %v %v %v", ok, declared, why)
	}
	if ok, declared, _ := AnySetSatisfied(uhd, []map[string]any{on}); !ok || !declared {
		t.Errorf("an explicitly enabled set = %v %v", ok, declared)
	}
	if ok, declared, why := AnySetSatisfied(uhd, []map[string]any{off}); !ok || declared || why != nil {
		t.Errorf("only a disabled set = %v %v %v, want nothing declared", ok, declared, why)
	}
	if ok, declared, _ := AnySetSatisfied(uhd, nil); !ok || declared {
		t.Errorf("no sets = %v %v, want nothing declared", ok, declared)
	}
	if !Enabled(hd) || Enabled(off) {
		t.Error("meta:enabled defaults to true and false switches a set off")
	}
}
