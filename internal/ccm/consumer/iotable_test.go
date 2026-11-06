package consumer

import (
	"reflect"
	"strings"
	"testing"

	dhsc "dhs/internal/consumer"
)

func obj(path, val string) dhsc.Object {
	return dhsc.Object{Path: strings.Split(path, "."), Value: dhsc.Value{Kind: dhsc.KindString, Str: val}}
}

// A SHUFFLE-shaped tree: two receivers listed out of router order, one
// sender, a delay bank, a mute, two generator and one analyser channel
// that carry no matrixIndex.
func shuffleTree() []dhsc.Object {
	sdp := "v=0\r\no=- 1 2 IN IP4 10.6.40.60\r\ns=CAM\r\nt=0 0\r\n" +
		"m=audio 30000 RTP/AVP 97\r\nc=IN IP4 239.30.0.1/32\r\na=source-filter: incl IN IP4 239.30.0.1 10.6.40.60\r\n" +
		"a=rtpmap:97 L24/48000/8\r\na=ptime:0.125\r\n" +
		"m=audio 30002 RTP/AVP 97\r\nc=IN IP4 239.94.0.1/32\r\na=source-filter: incl IN IP4 239.94.0.1 10.7.40.58\r\n"
	return []dhsc.Object{
		obj("network.macs.m1.name", "Control Port Mac 1"),
		obj("network.macs.m2.name", "Control Port Mac 2"),
		// "b" sorts before "a2" on the router, so it is input 1.
		obj("io.ip.receivers.audio.a2.name", "Input Audio Stream 2"),
		obj("io.ip.receivers.audio.a2.enable", "true"),
		obj("io.ip.receivers.audio.a2.channels.c3.matrixIndex", "8"),
		obj("io.ip.receivers.audio.a2.channels.c3.name", "IP002-01"),
		obj("io.ip.receivers.audio.a2.channels.c3.number", "1"),
		obj("io.ip.receivers.audio.b.name", "Input Audio Stream 1"),
		obj("io.ip.receivers.audio.b.enable", "true"),
		obj("io.ip.receivers.audio.b.sdp", sdp),
		obj("io.ip.receivers.audio.b.skew", "ClassD"),
		obj("io.ip.receivers.audio.b.primaryLeg.mac", "m1"),
		obj("io.ip.receivers.audio.b.secondaryLeg.mac", "m2"),
		obj("io.ip.receivers.audio.b.nmos.label", "CAM 1"),
		obj("io.ip.receivers.audio.b.channels.c2.matrixIndex", "1"),
		obj("io.ip.receivers.audio.b.channels.c2.name", "IP001-02"),
		obj("io.ip.receivers.audio.b.channels.c2.number", "2"),
		obj("io.ip.receivers.audio.b.channels.c1.matrixIndex", "0"),
		obj("io.ip.receivers.audio.b.channels.c1.name", "IP001-01"),
		obj("io.ip.receivers.audio.b.channels.c1.number", "1"),
		// A receiver with no channels is not an input.
		obj("io.ip.receivers.audio.empty.name", "Spare"),
		obj("io.ip.senders.rtp-payload-types.s2110_30", "97"),
		obj("io.ip.senders.audio.s.name", "Output Audio Stream 1"),
		obj("io.ip.senders.audio.s.mediaType", "s2110-30"),
		obj("io.ip.senders.audio.s.nrChannels", "8"),
		obj("io.ip.senders.audio.s.primaryLeg.ip", "239.30.1.1"),
		obj("io.ip.senders.audio.s.primaryLeg.port", "30000"),
		obj("io.ip.senders.audio.s.primaryLeg.mac", "m1"),
		obj("io.ip.senders.audio.s.channels.d1.matrixIndex", "0"),
		obj("io.ip.senders.audio.s.channels.d1.name", "IP001-01"),
		obj("io.ip.senders.audio.s.channels.d1.number", "1"),
		obj("processing.audio.delay.k.name", "Delay Bank 1"),
		obj("processing.audio.delay.k.channels.k1.matrixIndex", "20"),
		obj("processing.audio.delay.k.channels.k1.name", "DB001-01"),
		obj("processing.audio.delay.k.channels.k1.number", "1"),
		obj("processing.audio.mute.u.matrixIndex", "40"),
		obj("processing.audio.mute.u.name", "MU001-01"),
		obj("processing.audio.mute.u.number", "1"),
		obj("processing.audio.generator.channels.g2.name", "AG001-02"),
		obj("processing.audio.generator.channels.g1.name", "AG001-01"),
		obj("processing.audio.analyser.channels.z1.name", "AA001-01"),
	}
}

func column(table [][]string, name string) []string {
	i := -1
	for j, h := range table[0] {
		if h == name {
			i = j
		}
	}
	var out []string
	for _, r := range table[1:] {
		out = append(out, r[i])
	}
	return out
}

func TestIOTableInputsInRouterOrder(t *testing.T) {
	tb := BuildIOTables(shuffleTree(), nil, nil)
	if got := column(tb.Inputs, "name"); !reflect.DeepEqual(got, []string{"Input Audio Stream 1", "Input Audio Stream 2"}) {
		t.Fatalf("inputs %v", got)
	}
	row := map[string]string{}
	for i, h := range tb.Inputs[0] {
		row[h] = tb.Inputs[1][i]
	}
	want := map[string]string{
		"input": "1", "uuid": "b", "channel_slots": "2", "channels": "8", "payload_id": "97", "sample_rate": "48000",
		"bit_depth": "24", "packet_time": "125us", "rx_skew": "ClassD",
		"primary_interface": "Control Port Mac 1", "primary_multicast": "239.30.0.1", "primary_port": "30000", "primary_source_ip": "10.6.40.60",
		"secondary_interface": "Control Port Mac 2", "secondary_multicast": "239.94.0.1", "secondary_port": "30002", "secondary_source_ip": "10.7.40.58",
		"nmos_label": "CAM 1", "matrix_first": "0", "matrix_last": "1",
	}
	for k, v := range want {
		if row[k] != v {
			t.Errorf("input 1 %s = %q, want %q", k, row[k], v)
		}
	}
	out := tb.Outputs
	if len(out) != 2 || column(out, "payload_id")[0] != "97" || column(out, "primary_interface")[0] != "Control Port Mac 1" {
		t.Errorf("outputs %v", out)
	}
}

func TestIOTableChannelsByIndex(t *testing.T) {
	tb := BuildIOTables(shuffleTree(), nil, nil)
	var got []string
	for _, r := range tb.Sources {
		got = append(got, strings.Join(r.Record()[:8], "|"))
	}
	want := []string{
		"0|IP input|1|Input Audio Stream 1|b|1|IP001-01|c1",
		"1|IP input|1|Input Audio Stream 1|b|2|IP001-02|c2",
		"8|IP input|2|Input Audio Stream 2|a2|1|IP002-01|c3",
		"20|delay bank|1|Delay Bank 1|k|1|DB001-01|k1",
		"40|mute||MU001-01|u|1|MU001-01|u",
		"|generator||AG001-01|g1|1|AG001-01|g1",
		"|generator||AG001-02|g2|2|AG001-02|g2",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sources\n got %q\nwant %q", got, want)
	}
	if want := []string{"generator: AG001-01", "generator: AG001-02", "analyser: AA001-01"}; !reflect.DeepEqual(tb.NoIndex, want) {
		t.Errorf("no index %v, want %v", tb.NoIndex, want)
	}
}

// The router's labels place a channel CCM gives no index, by name, and
// say whether the router and CCM name each channel the same.
func TestIOTableLabelsPlaceAndCheck(t *testing.T) {
	src := map[int]MatrixLabel{
		0:   {Default: "IP I0001-01", Labels: []string{"", "IP000101", "IP I0001-01", ""}},
		1:   {Default: "CAM 1 R", Labels: []string{"", "CAM1R", "", ""}},
		100: {Default: "AG000101", Labels: []string{"", "AG000101"}},
		101: {Default: "AG000102", Labels: []string{"", "AG000102"}},
	}
	dst := map[int]MatrixLabel{
		50: {Default: "AA000101", Labels: []string{"AA000101"}},
		51: {Default: "AA000101", Labels: []string{"AA000101"}}, // twice: no guess
	}
	tb := BuildIOTables(shuffleTree(), src, dst)
	by := map[string]MatrixRow{}
	for _, r := range append(tb.Sources, tb.Destinations...) {
		by[r.ChannelName+"/"+r.Kind] = r
	}
	cases := []struct {
		key, idx, from, agree, short string
	}{
		{"IP001-01/IP input", "0", "ccm", "yes", "IP000101"},
		{"IP001-02/IP input", "1", "ccm", "no", "CAM1R"},
		{"IP002-01/IP input", "8", "ccm", "", ""},
		{"AG001-01/generator", "100", "labels", "yes", "AG000101"},
		{"AG001-02/generator", "101", "labels", "yes", "AG000102"},
		{"AA001-01/analyser", "", "", "", ""},
	}
	for _, c := range cases {
		r := by[c.key].Record()
		if r[0] != c.idx || r[8] != c.from || r[11] != c.agree || r[10] != c.short {
			t.Errorf("%s: index %q from %q agree %q short %q", c.key, r[0], r[8], r[11], r[10])
		}
	}
	if len(tb.NoIndex) != 3 {
		t.Errorf("a placed channel is still a deviation: %v", tb.NoIndex)
	}
	if got := column(tb.Inputs, "label_first")[0]; got != "IP000101" {
		t.Errorf("label_first %q", got)
	}
	if last := tb.Destinations[len(tb.Destinations)-1]; last.ChannelName != "AA001-01" || last.Index >= 0 {
		t.Errorf("an unplaced channel sorts last: %+v", last)
	}
}

func TestNameKeys(t *testing.T) {
	for name, want := range map[string]string{"IP001-01": "IP:101", "MA004-59": "MA:459", "IP1130-15": "IP:113015", "x": "", "AB1-100": ""} {
		if got := nameKey(name); got != want {
			t.Errorf("nameKey(%q) = %q, want %q", name, got, want)
		}
	}
	for label, want := range map[string]string{"IP000101": "IP:101", "ma000459": "MA:459", "IP I0001-01": "", "": ""} {
		if got := labelKey(label); got != want {
			t.Errorf("labelKey(%q) = %q, want %q", label, got, want)
		}
	}
}

// An SDP with its connection line above the media sections gives it to
// both legs; one with no channel count in the rtpmap is mono.
func TestParseSDPSessionLevelLines(t *testing.T) {
	s := parseSDP("v=0\nc=IN IP4 239.1.1.1/32\na=source-filter: incl IN IP4 239.1.1.1 10.0.0.1\nm=audio 5004 RTP/AVP 96\na=rtpmap:96 L16/48000\na=ptime:1\nm=audio 5006 RTP/AVP 96\n")
	if s.leg(0).mcast != "239.1.1.1" || s.leg(1).mcast != "239.1.1.1" || s.leg(1).source != "10.0.0.1" || s.leg(1).port != "5006" {
		t.Errorf("legs %+v", s.legs)
	}
	if s.channels != "1" || s.ptime != "1ms" || s.depth != "16" {
		t.Errorf("facts %+v", s)
	}
	if (parseSDP("").leg(2) != sdpLeg{}) {
		t.Error("an empty SDP has legs")
	}
}

// What a tree holds besides the plain case: an object with no field, a
// channel folder with only a deeper resource, an interface with no
// name, a stream whose channels carry no index, labels with no short
// form.
func TestIOTableEdges(t *testing.T) {
	objs := append(shuffleTree(),
		obj("orphan", "x"),
		obj("network.macs.m3.enable", "true"),
		obj("io.ip.receivers.audio.b.channels.cx.meta.deep", "1"),
		obj("io.ip.senders.audio.t.name", "Output Audio Stream 0"),
		obj("io.ip.senders.audio.t.primaryLeg.mac", "m3"),
		obj("io.ip.senders.audio.t.channels.e1.name", "odd"),
	)
	tb := BuildIOTables(objs, nil, nil)
	if got := column(tb.Inputs, "channel_slots")[0]; got != "2" {
		t.Errorf("a folder with no fields counted as a channel: %s", got)
	}
	out := tb.Outputs
	if n := column(out, "name"); len(n) != 2 || n[1] != "Output Audio Stream 0" {
		t.Errorf("a stream with no index sorts last: %v", n)
	}
	if got := column(out, "primary_interface")[1]; got != "m3" {
		t.Errorf("an unnamed interface is its uuid: %q", got)
	}
	if got := column(out, "matrix_first")[1]; got != "" {
		t.Errorf("no index printed as %q", got)
	}

	cs := []channel{{index: -1, number: 1}, {index: 4}, {index: -1, number: 0}, {index: 2}}
	sortChannels(cs)
	if cs[0].index != 2 || cs[1].index != 4 || cs[2].number != 0 || cs[3].number != 1 {
		t.Errorf("sorted %+v", cs)
	}
	if _, _, ok := chanParts("odd"); ok {
		t.Error("a name of another shape parsed")
	}
	if labelKey("IP99999999999999999999999") != "" {
		t.Error("an overlong number gave a key")
	}
	if got := labelAt(map[int]MatrixLabel{3: {Default: "LONG NAME"}}, 3); got != "LONG NAME" {
		t.Errorf("labelAt fell back to %q", got)
	}
}
