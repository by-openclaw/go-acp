package sdp

// Expected values are written from the SDP text in testdata/, never
// from the parser's own output (repo testing rule). Fixtures use
// documentation multicast (233.252.0.0/24, RFC 5771) and a synthetic
// grandmaster — no customer addressing.

import (
	"os"
	"path/filepath"
	"testing"
)

func load(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(raw)
}

func TestParseVideoRawDup(t *testing.T) {
	s, devs, err := Parse(load(t, "video-raw-dup.sdp"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(devs) != 0 {
		t.Fatalf("clean fixture produced deviations: %+v", devs)
	}
	if s.Version != 0 || s.Name != "dhs-video-raw ST 2110-20" {
		t.Errorf("session header: v=%d s=%q", s.Version, s.Name)
	}
	if s.Origin.Addr != "198.51.100.10" || s.Origin.SessID != "1443716955" {
		t.Errorf("origin = %+v", s.Origin)
	}
	if len(s.Groups) != 1 || s.Groups[0].Semantics != "DUP" ||
		len(s.Groups[0].Tags) != 2 || s.Groups[0].Tags[0] != "primary" || s.Groups[0].Tags[1] != "secondary" {
		t.Fatalf("groups = %+v", s.Groups)
	}
	if len(s.Media) != 2 {
		t.Fatalf("media sections = %d, want 2", len(s.Media))
	}

	m := s.Media[0]
	if m.Type != "video" || m.Port != 5004 || m.Proto != "RTP/AVP" ||
		len(m.Formats) != 1 || m.Formats[0] != "96" {
		t.Errorf("m[0] header = %+v", m)
	}
	if m.Connection == nil || m.Connection.Addr != "233.252.0.10" || m.Connection.TTL != "64" {
		t.Errorf("m[0] c= %+v", m.Connection)
	}
	r, ok := m.RTPMap["96"]
	if !ok || r.Encoding != "raw" || r.ClockRate != 90000 || r.Params != "" {
		t.Errorf("m[0] rtpmap = %+v", m.RTPMap)
	}
	f, ok := m.FMTP["96"]
	if !ok {
		t.Fatal("m[0] fmtp missing")
	}
	// Parameter ORDER is preserved: sampling is first, TP last.
	if f.Params[0].Key != "sampling" || f.Params[0].Value != "YCbCr-4:2:2" ||
		f.Params[len(f.Params)-1].Key != "TP" || f.Params[len(f.Params)-1].Value != "2110TPN" {
		t.Errorf("m[0] fmtp order = %+v", f.Params)
	}
	for _, want := range [][2]string{
		{"width", "1920"}, {"height", "1080"}, {"exactframerate", "50"},
		{"depth", "10"}, {"colorimetry", "BT709"}, {"SSN", "ST2110-20:2017"},
	} {
		if got, ok := f.Get(want[0]); !ok || got != want[1] {
			t.Errorf("fmtp %s = %q (present=%v), want %q", want[0], got, ok, want[1])
		}
	}
	if m.TSRefClk == nil || m.TSRefClk.Version != "IEEE1588-2008" ||
		m.TSRefClk.GMID != "AA-BB-CC-FF-FE-00-00-01" || m.TSRefClk.Domain != 127 {
		t.Errorf("m[0] ts-refclk = %+v", m.TSRefClk)
	}
	if m.MediaClk != "direct=0" || m.Mid != "primary" {
		t.Errorf("m[0] mediaclk=%q mid=%q", m.MediaClk, m.Mid)
	}
	if m.SourceFilt == nil || m.SourceFilt.Mode != "incl" ||
		m.SourceFilt.Dest != "233.252.0.10" ||
		len(m.SourceFilt.Srcs) != 1 || m.SourceFilt.Srcs[0] != "198.51.100.10" {
		t.Errorf("m[0] source-filter = %+v", m.SourceFilt)
	}

	// The 2022-7 helper pairs group tags with sections, group order.
	legs := s.Legs()
	if len(legs) != 2 {
		t.Fatalf("legs = %d, want 2", len(legs))
	}
	if legs[0].Mid != "primary" || legs[0].Dest != "233.252.0.10" ||
		legs[0].Src != "198.51.100.10" || legs[0].Port != 5004 {
		t.Errorf("leg[0] = %+v", legs[0])
	}
	if legs[1].Mid != "secondary" || legs[1].Dest != "233.252.0.138" ||
		legs[1].Src != "198.51.100.138" {
		t.Errorf("leg[1] = %+v", legs[1])
	}
}

func TestParseAudioL24(t *testing.T) {
	s, devs, err := Parse(load(t, "audio-l24.sdp"))
	if err != nil || len(devs) != 0 {
		t.Fatalf("Parse: err=%v devs=%+v", err, devs)
	}
	m := s.Media[0]
	r := m.RTPMap["97"]
	if r.Encoding != "L24" || r.ClockRate != 48000 || r.Params != "8" {
		t.Errorf("audio rtpmap = %+v", r)
	}
	if m.PTime != "0.125" {
		t.Errorf("ptime = %q", m.PTime)
	}
	// The channel-order value carries dots and parentheses — it must
	// survive verbatim.
	if got, ok := m.FMTP["97"].Get("channel-order"); !ok || got != "SMPTE2110.(U08)" {
		t.Errorf("channel-order = %q", got)
	}
	if len(s.Legs()) != 2 {
		t.Errorf("audio legs = %d, want 2", len(s.Legs()))
	}
}

func TestParseAncSmpte291(t *testing.T) {
	s, devs, err := Parse(load(t, "anc-smpte291.sdp"))
	if err != nil || len(devs) != 0 {
		t.Fatalf("Parse: err=%v devs=%+v", err, devs)
	}
	m := s.Media[0]
	if m.RTPMap["100"].Encoding != "smpte291" {
		t.Errorf("anc rtpmap = %+v", m.RTPMap)
	}
	// The DID_SDID value carries braces and a comma inside one param.
	if got, ok := m.FMTP["100"].Get("DID_SDID"); !ok || got != "{0x61,0x02}" {
		t.Errorf("DID_SDID = %q", got)
	}
	// traceable form: GMID "traceable", domain absent = -1.
	if m.TSRefClk == nil || m.TSRefClk.GMID != "traceable" || m.TSRefClk.Domain != -1 {
		t.Errorf("ts-refclk = %+v", m.TSRefClk)
	}
	// b=AS:100 is not modelled — preserved verbatim, never dropped.
	found := false
	for _, x := range m.Extra {
		if x == "b=AS:100" {
			found = true
		}
	}
	if !found {
		t.Errorf("b= line not preserved: %+v", m.Extra)
	}
	// No DUP group: one leg per media section.
	legs := s.Legs()
	if len(legs) != 1 || legs[0].Dest != "233.252.0.30" {
		t.Errorf("single-path legs = %+v", legs)
	}
}

func TestParseMalformedReportsDeviations(t *testing.T) {
	s, devs, err := Parse(load(t, "malformed.sdp"))
	if err != nil {
		t.Fatalf("recoverable defects must not error: %v", err)
	}
	// Three malformed modelled attributes + one non-SDP line.
	wantReasons := map[string]bool{}
	for _, d := range devs {
		wantReasons[d.Text] = true
		if d.Line < 1 {
			t.Errorf("deviation without line number: %+v", d)
		}
	}
	for _, text := range []string{
		"a=rtpmap:96 raw",
		"a=ts-refclk:ptp=IEEE1588-2008",
		"a=source-filter: incl IN",
		"x-not-an-sdp-line",
	} {
		if !wantReasons[text] {
			t.Errorf("expected a deviation for %q, got %+v", text, devs)
		}
	}
	// The malformed attributes are preserved raw, never dropped.
	m := s.Media[0]
	raw := map[string]bool{}
	for _, a := range m.Attributes {
		raw[a.Name] = true
	}
	for _, name := range []string{"rtpmap", "ts-refclk", "source-filter", "custom-flag"} {
		if !raw[name] {
			t.Errorf("malformed/unknown attribute %q not preserved: %+v", name, m.Attributes)
		}
	}
	// The flag attribute is a plain preserve, not a deviation.
	for _, d := range devs {
		if d.Text == "a=custom-flag" {
			t.Errorf("unknown flag attribute wrongly flagged as deviation")
		}
	}
}

func TestParseStructurallyImpossible(t *testing.T) {
	if _, _, err := Parse(""); err == nil {
		t.Error("empty document must error")
	}
	if _, _, err := Parse("s=no-version\n"); err == nil {
		t.Error("document not starting with v= must error")
	}
}

func TestParseCRLF(t *testing.T) {
	s, devs, err := Parse("v=0\r\no=- 1 1 IN IP4 198.51.100.10\r\ns=crlf\r\nt=0 0\r\n")
	if err != nil || len(devs) != 0 {
		t.Fatalf("CRLF document: err=%v devs=%+v", err, devs)
	}
	if s.Name != "crlf" {
		t.Errorf("s= %q", s.Name)
	}
}
