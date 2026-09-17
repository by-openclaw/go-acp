package sdp

import (
	"strings"
	"testing"
)

// parseOK parses a document that must be structurally acceptable and
// returns the session plus its deviations.
func parseOK(t *testing.T, text string) (*Session, []Deviation) {
	t.Helper()
	s, devs, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return s, devs
}

// hasDeviation reports whether any deviation's reason mentions substr.
func hasDeviation(devs []Deviation, substr string) bool {
	for _, d := range devs {
		if strings.Contains(d.Reason, substr) {
			return true
		}
	}
	return false
}

// Only a document that cannot be read at all is an error; everything
// else is a deviation the caller can report while still holding the
// parsed session.
func TestParseStructuralRefusals(t *testing.T) {
	for name, text := range map[string]string{
		"an empty document":                     "",
		"a document that does not open with v=": "o=- 1 1 IN IP4 10.0.0.1\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Parse(text); err == nil {
				t.Error("was accepted")
			}
		})
	}
}

// Every malformed session-level line is reported and the document
// keeps parsing — a receiver reads what it can and says what it could
// not.
func TestParseSessionLevelDeviations(t *testing.T) {
	doc := strings.Join([]string{
		"v=not-a-number",
		"o=too few fields",
		"s=deviation tour",
		"t=0",
		"c=IN IP4",
		"m=video",
		"", // a blank line inside the document
		"nonsense",
		"a=group",
	}, "\n")

	s, devs := parseOK(t, doc)
	for _, want := range []string{
		"non-numeric version",
		"o= wants 6 fields",
		"t= wants 2 fields",
		"c= wants 3 fields",
		"m= wants at least 4 fields",
		"blank line inside the document",
		"not a <type>=<value> line",
	} {
		if !hasDeviation(devs, want) {
			t.Errorf("no deviation mentioning %q; got %+v", want, devs)
		}
	}
	// A malformed m= still opens a section, so the lines after it have
	// a home rather than being dropped.
	if len(s.Media) != 1 {
		t.Errorf("media sections = %d, want the one the malformed m= opened", len(s.Media))
	}
}

// a=group with no semantics is a deviation; a session-level attribute
// that is not a group is preserved verbatim.
func TestParseSessionAttributes(t *testing.T) {
	s, devs := parseOK(t, strings.Join([]string{
		"v=0",
		"a=group:",
		"a=recvonly",
		"a=tool:dhs",
		"i=a session-level info line",
	}, "\n"))

	if !hasDeviation(devs, "a=group without semantics") {
		t.Errorf("deviations = %+v", devs)
	}
	names := map[string]string{}
	for _, a := range s.Attributes {
		names[a.Name] = a.Value
	}
	if _, ok := names["recvonly"]; !ok {
		t.Errorf("a flag attribute must be preserved: %+v", s.Attributes)
	}
	if names["tool"] != "dhs" {
		t.Errorf("a valued attribute must be preserved: %+v", s.Attributes)
	}
	if len(s.Extra) != 1 || !strings.HasPrefix(s.Extra[0], "i=") {
		t.Errorf("an unmodelled session line must be preserved verbatim: %v", s.Extra)
	}
}

// The media header's own rules: at least four fields, and a numeric
// port.
func TestParseMediaHeaderDeviations(t *testing.T) {
	_, devs := parseOK(t, "v=0\nm=video port RTP/AVP 96\n")
	if !hasDeviation(devs, "m= port is not a number") {
		t.Errorf("deviations = %+v", devs)
	}
}

// A media-level connection overrides the session's, and carries the
// multicast TTL when the sender wrote one.
func TestParseConnectionTTLAndScope(t *testing.T) {
	s, _ := parseOK(t, strings.Join([]string{
		"v=0",
		"c=IN IP4 239.0.0.1/32",
		"m=video 5004 RTP/AVP 96",
		"c=IN IP4 239.0.0.2/16",
		"b=AS:1000",
	}, "\n"))

	if s.Connection == nil || s.Connection.Addr != "239.0.0.1" || s.Connection.TTL != "32" {
		t.Errorf("session connection = %+v", s.Connection)
	}
	if len(s.Media) != 1 || s.Media[0].Connection == nil {
		t.Fatalf("media = %+v", s.Media)
	}
	if s.Media[0].Connection.Addr != "239.0.0.2" || s.Media[0].Connection.TTL != "16" {
		t.Errorf("media connection = %+v", s.Media[0].Connection)
	}
	if len(s.Media[0].Extra) != 1 {
		t.Errorf("an unmodelled media line must be preserved: %v", s.Media[0].Extra)
	}
}

// Every modelled media attribute reports its own malformed shape, and
// the raw line survives regardless — a malformed attribute is
// reported AND kept.
func TestParseMediaAttributeDeviations(t *testing.T) {
	doc := strings.Join([]string{
		"v=0",
		"m=video 5004 RTP/AVP 96",
		"a=rtpmap:96",                        // one field
		"a=rtpmap:97 raw",                    // no clock rate
		"a=rtpmap:98 raw/not-a-number",       // clock rate is not numeric
		"a=fmtp:96",                          // no params
		"a=ts-refclk:ptp=IEEE1588-2008",      // too few parts
		"a=ts-refclk:ptp=IEEE1588-2008:gm:x", // domain is not numeric
	}, "\n")

	s, devs := parseOK(t, doc)
	for _, want := range []string{
		"a=rtpmap wants",
		"a=rtpmap encoding wants",
		"a=rtpmap clock rate is not a number",
		"a=fmtp wants",
		"a=ts-refclk ptp= wants",
		"a=ts-refclk ptp domain is not a number",
	} {
		if !hasDeviation(devs, want) {
			t.Errorf("no deviation mentioning %q; got %+v", want, devs)
		}
	}
	if len(s.Media) != 1 {
		t.Fatalf("media = %+v", s.Media)
	}
	if len(s.Media[0].Attributes) != 6 {
		t.Errorf("every malformed attribute must be preserved: %+v", s.Media[0].Attributes)
	}
}

// The modelled attributes decode into their slots: an rtpmap with
// parameters, an fmtp whose empty segments are skipped, and both ptp
// forms of ts-refclk.
func TestParseMediaAttributesDecode(t *testing.T) {
	s, devs := parseOK(t, strings.Join([]string{
		"v=0",
		"m=video 5004 RTP/AVP 96",
		"a=mid:primary",
		"a=rtpmap:96 raw/90000/2",
		"a=fmtp:96 sampling=YCbCr-4:2:2; width=1920;; depth=10",
		"a=ts-refclk:ptp=IEEE1588-2008:traceable",
		"a=mediaclk:direct=0",
		"m=audio 5006 RTP/AVP 97",
		"a=ts-refclk:ptp=IEEE1588-2008:39-A7-94-FF-FE-07-CB-D0:0",
		"a=ts-refclk:localmac=00-11-22-33-44-55",
	}, "\n"))
	if len(devs) != 0 {
		t.Fatalf("a well-formed document must report nothing: %+v", devs)
	}
	if len(s.Media) != 2 {
		t.Fatalf("media = %d sections", len(s.Media))
	}

	video := s.Media[0]
	if video.Mid != "primary" {
		t.Errorf("mid = %q", video.Mid)
	}
	rtp, ok := video.RTPMap["96"]
	if !ok || rtp.Encoding != "raw" || rtp.ClockRate != 90000 || rtp.Params != "2" {
		t.Errorf("rtpmap = %+v, %v", rtp, ok)
	}
	fmtp, ok := video.FMTP["96"]
	if !ok {
		t.Fatalf("fmtp = %+v", video.FMTP)
	}
	if len(fmtp.Params) != 3 {
		t.Errorf("fmtp params = %+v, want the empty segment skipped", fmtp.Params)
	}
	if v, ok := fmtp.Get("width"); !ok || v != "1920" {
		t.Errorf("fmtp.Get(width) = %q, %v", v, ok)
	}
	if _, ok := fmtp.Get("colorimetry"); ok {
		t.Error("fmtp.Get must report a key the sender did not write")
	}
	if video.TSRefClk == nil || video.TSRefClk.GMID != "traceable" || video.TSRefClk.Domain != -1 {
		t.Errorf("ts-refclk = %+v, want the traceable form with no domain", video.TSRefClk)
	}
	if video.MediaClk != "direct=0" {
		t.Errorf("mediaclk = %q", video.MediaClk)
	}

	audio := s.Media[1]
	if audio.TSRefClk == nil || audio.TSRefClk.Domain != 0 ||
		audio.TSRefClk.GMID != "39-A7-94-FF-FE-07-CB-D0" {
		t.Errorf("ts-refclk = %+v, want the gmid:domain form", audio.TSRefClk)
	}
	// localmac= is not modelled, but it is kept.
	found := false
	for _, a := range audio.Attributes {
		if a.Name == "ts-refclk" && strings.HasPrefix(a.Value, "localmac=") {
			found = true
		}
	}
	if !found {
		t.Errorf("an unmodelled ts-refclk form must be preserved: %+v", audio.Attributes)
	}
}

// ST 2022-7 legs come from the DUP group when the sender declared
// one, and from the media sections themselves otherwise. Each leg's
// destination falls back to the session connection.
func TestLegs(t *testing.T) {
	dup := strings.Join([]string{
		"v=0",
		"c=IN IP4 239.0.0.9/32",
		"a=group:DUP primary secondary",
		"m=video 5004 RTP/AVP 96",
		"a=mid:primary",
		"c=IN IP4 239.0.0.1/32",
		"a=source-filter: incl IN IP4 239.0.0.1 10.0.0.1",
		"m=video 5006 RTP/AVP 96",
		"a=mid:secondary",
	}, "\n")
	s, _ := parseOK(t, dup)
	legs := s.Legs()
	if len(legs) != 2 {
		t.Fatalf("legs = %+v, want one per DUP tag", legs)
	}
	if legs[0].Mid != "primary" || legs[0].Dest != "239.0.0.1" || legs[0].Port != 5004 {
		t.Errorf("first leg = %+v", legs[0])
	}
	if legs[0].Src != "10.0.0.1" {
		t.Errorf("first leg source = %q, want the source-filter's own", legs[0].Src)
	}
	// The second section has no c= of its own, so it takes the
	// session's.
	if legs[1].Dest != "239.0.0.9" {
		t.Errorf("second leg destination = %q, want the session connection", legs[1].Dest)
	}

	// A group whose semantics are not DUP is not a leg grouping.
	other, _ := parseOK(t, strings.Join([]string{
		"v=0",
		"a=group:FID one",
		"m=video 5004 RTP/AVP 96",
		"a=mid:one",
	}, "\n"))
	if legs := other.Legs(); len(legs) != 1 || legs[0].Mid != "one" {
		t.Errorf("a non-DUP group must fall back to the media sections: %+v", legs)
	}

	// A DUP group naming tags no media section carries falls back too,
	// rather than reporting no legs at all.
	dangling, _ := parseOK(t, strings.Join([]string{
		"v=0",
		"a=group:DUP nowhere",
		"m=video 5004 RTP/AVP 96",
		"a=mid:one",
	}, "\n"))
	if legs := dangling.Legs(); len(legs) != 1 || legs[0].Mid != "one" {
		t.Errorf("a DUP group naming nothing must fall back: %+v", legs)
	}

	// With no connection anywhere a leg simply has no destination.
	bare, _ := parseOK(t, "v=0\nm=video 5004 RTP/AVP 96\n")
	if legs := bare.Legs(); len(legs) != 1 || legs[0].Dest != "" {
		t.Errorf("legs = %+v, want one with no destination", legs)
	}
}
