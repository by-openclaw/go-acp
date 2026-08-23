package sdp_test

import (
	"os"
	"path/filepath"
	"testing"

	"dhs/internal/amwa/codec/sdp"
)

func load(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func parseClean(t *testing.T, name string) *sdp.Session {
	t.Helper()
	s, devs, err := sdp.Parse(load(t, name))
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", name, err)
	}
	if len(devs) != 0 {
		t.Fatalf("%s: unexpected deviations: %v", name, devs)
	}
	return s
}

// Expected values below are read from the fixture TEXT, never from the
// parser's own output (ADR: expected bytes come from the spec, not from
// working code).

func TestVideoRawDualLeg(t *testing.T) {
	s := parseClean(t, "video_2022-7.sdp")

	if s.Version != 0 {
		t.Errorf("version = %d, want 0", s.Version)
	}
	if s.Name != "VTX-01" {
		t.Errorf("name = %q, want VTX-01", s.Name)
	}
	if s.Origin.Address != "192.0.2.42" {
		t.Errorf("origin address = %q, want 192.0.2.42", s.Origin.Address)
	}
	if len(s.Media) != 2 {
		t.Fatalf("media sections = %d, want 2", len(s.Media))
	}

	m := s.Media[0]
	if m.Type != "video" || m.Port != 20000 || m.Proto != "RTP/AVP" {
		t.Errorf("m= parsed as %s/%d/%s, want video/20000/RTP/AVP", m.Type, m.Port, m.Proto)
	}
	if m.Connection == nil || m.Connection.Address != "233.252.0.27" || m.Connection.TTL != 32 {
		t.Errorf("leg 1 connection = %+v, want 233.252.0.27 ttl 32", m.Connection)
	}
	if m.Mid != "primary" {
		t.Errorf("leg 1 mid = %q, want primary", m.Mid)
	}

	r, ok := m.RTPMap["96"]
	if !ok {
		t.Fatal("rtpmap 96 missing")
	}
	if r.Encoding != "raw" || r.ClockRate != 90000 || r.Channels != 0 {
		t.Errorf("rtpmap = %+v, want raw/90000 with no channels", r)
	}

	f, ok := m.FMTP["96"]
	if !ok {
		t.Fatal("fmtp 96 missing")
	}
	for _, tc := range []struct{ key, want string }{
		{"width", "1920"},
		{"height", "1080"},
		{"exactframerate", "50"},
		{"sampling", "YCbCr-4:2:2"},
		{"depth", "10"},
		{"colorimetry", "BT709"},
		{"TCS", "SDR"},
		{"PM", "2110GPM"},
		{"SSN", "ST2110-20:2017"},
		{"TP", "2110TPN"},
	} {
		got, ok := f.Get(tc.key)
		if !ok {
			t.Errorf("fmtp %s missing", tc.key)
			continue
		}
		if got != tc.want {
			t.Errorf("fmtp %s = %q, want %q", tc.key, got, tc.want)
		}
	}
	// sampling contains a colon; splitting on the first '=' must keep it whole.
	if v, _ := f.Get("sampling"); v != "YCbCr-4:2:2" {
		t.Errorf("sampling lost its colons: %q", v)
	}

	if m.TSRefClk == nil {
		t.Fatal("ts-refclk missing")
	}
	if m.TSRefClk.Version != "IEEE1588-2008" ||
		m.TSRefClk.GMID != "00-11-22-FF-FE-33-44-55" ||
		m.TSRefClk.Domain != 127 {
		t.Errorf("ts-refclk = %+v, want IEEE1588-2008 / 00-11-22-FF-FE-33-44-55 / 127", m.TSRefClk)
	}

	if m.SourceFilt == nil {
		t.Fatal("source-filter missing")
	}
	if m.SourceFilt.Mode != "incl" || m.SourceFilt.Dest != "233.252.0.27" ||
		len(m.SourceFilt.Sources) != 1 || m.SourceFilt.Sources[0] != "192.0.2.42" {
		t.Errorf("source-filter = %+v", m.SourceFilt)
	}

	if s.Media[1].Connection.Address != "233.252.1.27" || s.Media[1].Mid != "secondary" {
		t.Errorf("leg 2 = %s / %s, want 233.252.1.27 / secondary",
			s.Media[1].Connection.Address, s.Media[1].Mid)
	}
}

func TestLegsResolveDupGroup(t *testing.T) {
	s := parseClean(t, "video_2022-7.sdp")
	legs := s.Legs()
	if len(legs) != 2 {
		t.Fatalf("legs = %d, want 2", len(legs))
	}
	want := []struct {
		mid, dest, src string
		port           int
	}{
		{"primary", "233.252.0.27", "192.0.2.42", 20000},
		{"secondary", "233.252.1.27", "198.51.100.42", 20000},
	}
	for i, w := range want {
		if legs[i].Mid != w.mid || legs[i].Dest != w.dest || legs[i].Port != w.port {
			t.Errorf("leg %d = %+v, want %s %s :%d", i, legs[i], w.mid, w.dest, w.port)
		}
		if len(legs[i].Sources) != 1 || legs[i].Sources[0] != w.src {
			t.Errorf("leg %d sources = %v, want [%s]", i, legs[i].Sources, w.src)
		}
	}
}

func TestAudioL24Channels(t *testing.T) {
	s := parseClean(t, "audio_l24.sdp")
	m := s.Media[0]
	if m.Type != "audio" || m.Port != 30000 {
		t.Errorf("m= = %s/%d, want audio/30000", m.Type, m.Port)
	}
	r := m.RTPMap["97"]
	if r.Encoding != "L24" || r.ClockRate != 48000 || r.Channels != 8 {
		t.Errorf("rtpmap = %+v, want L24/48000/8", r)
	}
	if m.PTime != "0.125" {
		t.Errorf("ptime = %q, want 0.125", m.PTime)
	}
	if v, ok := m.FMTP["97"].Get("channel-order"); !ok || v != "SMPTE2110.(U08)" {
		t.Errorf("channel-order = %q (present=%v), want SMPTE2110.(U08)", v, ok)
	}
	if m.MediaClk != "direct=0" {
		t.Errorf("mediaclk = %q, want direct=0", m.MediaClk)
	}
	if m.TSRefClk.Domain != 100 {
		t.Errorf("ptp domain = %d, want 100", m.TSRefClk.Domain)
	}
}

func TestAncillarySMPTE291(t *testing.T) {
	s := parseClean(t, "anc_smpte291.sdp")
	m := s.Media[0]
	// ANC is carried in an m=video section — the media type alone does not
	// identify the essence; the rtpmap encoding does.
	if m.Type != "video" {
		t.Errorf("media type = %q, want video", m.Type)
	}
	if got := m.RTPMap["100"].Encoding; got != "smpte291" {
		t.Errorf("encoding = %q, want smpte291", got)
	}
	if m.SourceFilt != nil {
		t.Errorf("source-filter should be absent, got %+v", m.SourceFilt)
	}
	if len(s.Legs()) != 2 {
		t.Errorf("legs = %d, want 2", len(s.Legs()))
	}
}

func TestSingleLegWithoutGroup(t *testing.T) {
	s := parseClean(t, "single_leg.sdp")
	if len(s.Groups) != 0 {
		t.Fatalf("groups = %d, want 0", len(s.Groups))
	}
	legs := s.Legs()
	if len(legs) != 1 {
		t.Fatalf("legs = %d, want 1 (a stream with no DUP group is still one leg)", len(legs))
	}
	if legs[0].Dest != "233.252.0.5" || legs[0].Port != 5004 {
		t.Errorf("leg = %+v, want 233.252.0.5:5004", legs[0])
	}
	c := s.Media[0].Connection
	if c.TTL != 64 || c.Count != 2 {
		t.Errorf("connection ttl/count = %d/%d, want 64/2", c.TTL, c.Count)
	}
}

func TestCRLFAccepted(t *testing.T) {
	s := parseClean(t, "crlf.sdp")
	if s.Name != "crlf" {
		t.Errorf("name = %q, want crlf (CR must not survive into values)", s.Name)
	}
	if s.Media[0].RTPMap["97"].Channels != 2 {
		t.Errorf("channels = %d, want 2", s.Media[0].RTPMap["97"].Channels)
	}
}

func TestUnknownAttributesPreserved(t *testing.T) {
	in := []byte("v=0\no=- 1 1 IN IP4 192.0.2.1\ns=x\nt=0 0\na=x-vendor:whatever\nm=video 1 RTP/AVP 96\na=x-media-thing:42\n")
	s, devs, err := sdp.Parse(in)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(devs) != 0 {
		t.Fatalf("unexpected deviations: %v", devs)
	}
	var found bool
	for _, a := range s.Attributes {
		if a.Name == "x-vendor" && a.Value == "whatever" {
			found = true
		}
	}
	if !found {
		t.Error("session-level unknown attribute was dropped")
	}
	found = false
	for _, a := range s.Media[0].Attributes {
		if a.Name == "x-media-thing" && a.Value == "42" {
			found = true
		}
	}
	if !found {
		t.Error("media-level unknown attribute was dropped")
	}
}

func TestDeviations(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"not a type=value line", "v=0\no=- 1 1 IN IP4 192.0.2.1\ns=x\nt=0 0\ngarbage\n", "not a <type>=<value> line"},
		{"short origin", "v=0\no=- 1 1 IN IP4\ns=x\nt=0 0\n", "o= expects 6 fields"},
		{"bad connection", "v=0\no=- 1 1 IN IP4 192.0.2.1\ns=x\nt=0 0\nc=IN IP4\n", "c= expects <nettype> <addrtype> <address>"},
		{"bad media port", "v=0\no=- 1 1 IN IP4 192.0.2.1\ns=x\nt=0 0\nm=video xyz RTP/AVP 96\n", "m= port is not a number"},
		{"bad rtpmap clock", "v=0\no=- 1 1 IN IP4 192.0.2.1\ns=x\nt=0 0\nm=video 1 RTP/AVP 96\na=rtpmap:96 raw/abc\n", "clock rate is not a number"},
		{"bad ptp domain", "v=0\no=- 1 1 IN IP4 192.0.2.1\ns=x\nt=0 0\nm=video 1 RTP/AVP 96\na=ts-refclk:ptp=IEEE1588-2008:00-11:xyz\n", "ptp domain is not a number"},
		{"dangling group tag", "v=0\no=- 1 1 IN IP4 192.0.2.1\ns=x\nt=0 0\na=group:DUP primary secondary\nm=video 1 RTP/AVP 96\na=mid:primary\n", "group tag secondary has no matching a=mid"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, devs, err := sdp.Parse([]byte(tc.in))
			if err != nil {
				t.Fatalf("parse returned error %v; a recoverable defect must be a Deviation", err)
			}
			var got bool
			for _, d := range devs {
				if d.Msg == tc.want {
					got = true
				}
			}
			if !got {
				t.Errorf("deviations = %v, want one with %q", devs, tc.want)
			}
		})
	}
}

func TestEmptyAndNonSDP(t *testing.T) {
	if _, _, err := sdp.Parse(nil); err != sdp.ErrEmpty {
		t.Errorf("nil input: err = %v, want ErrEmpty", err)
	}
	if _, _, err := sdp.Parse([]byte("   \n\n")); err != sdp.ErrEmpty {
		t.Errorf("blank input: err = %v, want ErrEmpty", err)
	}
	if _, _, err := sdp.Parse([]byte("s=no version here\n")); err != sdp.ErrNoVersion {
		t.Errorf("missing v=: err = %v, want ErrNoVersion", err)
	}
}
