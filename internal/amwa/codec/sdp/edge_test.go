package sdp_test

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/sdp"
)

const head = "v=0\no=- 1 1 IN IP4 192.0.2.1\ns=x\nt=0 0\n"

func parseOK(t *testing.T, in string) (*sdp.Session, []sdp.Deviation) {
	t.Helper()
	s, devs, err := sdp.Parse([]byte(in))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return s, devs
}

func TestDeviationString(t *testing.T) {
	d := sdp.Deviation{Line: 7, Text: "garbage", Msg: "not a <type>=<value> line"}
	got := d.String()
	for _, want := range []string{"line 7", "not a <type>=<value> line", `"garbage"`} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q, missing %q", got, want)
		}
	}
}

func TestSessionLevelConnectionUsedWhenMediaHasNone(t *testing.T) {
	s, devs := parseOK(t, head+"c=IN IP4 233.252.0.9/32\nm=video 5004 RTP/AVP 96\n")
	if len(devs) != 0 {
		t.Fatalf("deviations: %v", devs)
	}
	if s.Connection == nil || s.Connection.Address != "233.252.0.9" {
		t.Fatalf("session connection = %+v", s.Connection)
	}
	if s.Media[0].Connection != nil {
		t.Errorf("media connection should be nil, got %+v", s.Media[0].Connection)
	}
	// Legs must fall back to the session-level c= rather than reporting no dest.
	legs := s.Legs()
	if len(legs) != 1 || legs[0].Dest != "233.252.0.9" {
		t.Errorf("legs = %+v, want dest 233.252.0.9 from the session-level c=", legs)
	}
}

func TestPropertyAttributeHasNoValue(t *testing.T) {
	s, _ := parseOK(t, head+"m=video 5004 RTP/AVP 96\na=recvonly\n")
	var found bool
	for _, a := range s.Media[0].Attributes {
		if a.Name == "recvonly" {
			found = true
			if a.Value != "" {
				t.Errorf("property attribute got value %q, want empty", a.Value)
			}
		}
	}
	if !found {
		t.Error("a=recvonly was dropped")
	}
}

func TestOtherLineTypesPreserved(t *testing.T) {
	s, devs := parseOK(t, head+"u=http://example.invalid\nb=AS:1000\nm=video 5004 RTP/AVP 96\nb=TIAS:900000\n")
	if len(devs) != 0 {
		t.Fatalf("b=/u= must not be deviations, got %v", devs)
	}
	var sessionB, mediaB bool
	for _, a := range s.Attributes {
		if a.Name == "b" && strings.Contains(a.Raw, "AS:1000") {
			sessionB = true
		}
	}
	for _, a := range s.Media[0].Attributes {
		if a.Name == "b" && strings.Contains(a.Raw, "TIAS:900000") {
			mediaB = true
		}
	}
	if !sessionB || !mediaB {
		t.Errorf("b= lines lost: session=%v media=%v", sessionB, mediaB)
	}
}

func TestTSRefClkNonPTPFormKept(t *testing.T) {
	s, devs := parseOK(t, head+"m=video 5004 RTP/AVP 96\na=ts-refclk:localmac=00-11-22-33-44-55\n")
	if len(devs) != 0 {
		t.Fatalf("a non-PTP ts-refclk is legal, got deviations: %v", devs)
	}
	tc := s.Media[0].TSRefClk
	if tc == nil {
		t.Fatal("ts-refclk missing")
	}
	if tc.GMID != "" || tc.Version != "" {
		t.Errorf("non-PTP form should leave PTP fields empty, got %+v", tc)
	}
	if tc.Domain != -1 {
		t.Errorf("domain = %d, want -1 (absent)", tc.Domain)
	}
	if !strings.Contains(tc.Raw, "localmac") {
		t.Errorf("raw not preserved: %q", tc.Raw)
	}
}

func TestPTPWithoutDomain(t *testing.T) {
	s, devs := parseOK(t, head+"m=video 5004 RTP/AVP 96\na=ts-refclk:ptp=IEEE1588-2008:00-11-22-FF-FE-33-44-55\n")
	if len(devs) != 0 {
		t.Fatalf("deviations: %v", devs)
	}
	tc := s.Media[0].TSRefClk
	if tc.GMID != "00-11-22-FF-FE-33-44-55" {
		t.Errorf("gmid = %q", tc.GMID)
	}
	if tc.Domain != -1 {
		t.Errorf("domain = %d, want -1 when the device omits it", tc.Domain)
	}
}

func TestFMTPBareFlagAndMissingKey(t *testing.T) {
	s, _ := parseOK(t, head+"m=video 5004 RTP/AVP 96\na=fmtp:96 interlace; width=1920;\n")
	f := s.Media[0].FMTP["96"]
	if v, ok := f.Get("interlace"); !ok || v != "" {
		t.Errorf("bare flag: value=%q present=%v, want empty/true", v, ok)
	}
	if v, ok := f.Get("WIDTH"); !ok || v != "1920" {
		t.Errorf("Get must be case-insensitive: %q %v", v, ok)
	}
	if _, ok := f.Get("nope"); ok {
		t.Error("Get returned true for an absent key")
	}
	// a trailing ';' must not produce an empty parameter
	for _, p := range f.Params {
		if p.Key == "" {
			t.Errorf("empty parameter parsed from trailing semicolon: %+v", f.Params)
		}
	}
}

func TestMalformedShortLines(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"media too short", head + "m=video\n", "m= expects <media> <port> <proto> [fmt...]"},
		{"group without semantics", head + "a=group:\nm=video 1 RTP/AVP 96\n", "group has no semantics"},
		{"fmtp without params", head + "m=video 1 RTP/AVP 96\na=fmtp:96\n", "fmtp expects <pt> <params>"},
		{"rtpmap without encoding", head + "m=video 1 RTP/AVP 96\na=rtpmap:96\n", "rtpmap expects <pt> <encoding>/<clock>[/<channels>]"},
		{"source-filter too short", head + "m=video 1 RTP/AVP 96\na=source-filter: incl IN IP4\n", "source-filter expects <mode> <nettype> <addrtype> <dest> <src>..."},
		{"ptp without gmid", head + "m=video 1 RTP/AVP 96\na=ts-refclk:ptp=IEEE1588-2008\n", "ptp expects <version>:<gmid>[:<domain>]"},
		{"timing wrong arity", head + "t=0\n", "t= expects <start> <stop>"},
		{"version not a number", "v=x\no=- 1 1 IN IP4 192.0.2.1\ns=x\nt=0 0\n", "v= is not a number"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, devs := parseOK(t, tc.in)
			for _, d := range devs {
				if d.Msg == tc.want {
					return
				}
			}
			t.Errorf("deviations = %v, want one with %q", devs, tc.want)
		})
	}
}

func TestMediaPortCount(t *testing.T) {
	s, devs := parseOK(t, head+"m=video 5004/2 RTP/AVP 96\n")
	if len(devs) != 0 {
		t.Fatalf("deviations: %v", devs)
	}
	if s.Media[0].Port != 5004 || s.Media[0].PortCount != 2 {
		t.Errorf("port/count = %d/%d, want 5004/2", s.Media[0].Port, s.Media[0].PortCount)
	}
}

func TestLegsIgnoreNonDupGroups(t *testing.T) {
	// FID grouping is not redundancy; Legs must not treat it as a DUP pair.
	in := head + "a=group:FID 1 2\nm=video 1 RTP/AVP 96\na=mid:1\nm=video 2 RTP/AVP 96\na=mid:2\n"
	s, _ := parseOK(t, in)
	legs := s.Legs()
	if len(legs) != 2 {
		t.Fatalf("legs = %d, want 2 (fallback: one per media section)", len(legs))
	}
	if legs[0].MediaIdx != 0 || legs[1].MediaIdx != 1 {
		t.Errorf("fallback legs should follow document order, got %+v", legs)
	}
}

func TestFormatsCaptured(t *testing.T) {
	s, _ := parseOK(t, head+"m=video 5004 RTP/AVP 96 97 98\n")
	got := s.Media[0].Formats
	if len(got) != 3 || got[0] != "96" || got[2] != "98" {
		t.Errorf("formats = %v, want [96 97 98]", got)
	}
}

func TestSessionInfoLine(t *testing.T) {
	s, devs := parseOK(t, "v=0\no=- 1 1 IN IP4 192.0.2.1\ns=x\ni=a description\nt=0 0\n")
	if len(devs) != 0 {
		t.Fatalf("deviations: %v", devs)
	}
	if s.Info != "a description" {
		t.Errorf("info = %q, want %q", s.Info, "a description")
	}
}

func TestLegsSkipDanglingGroupTag(t *testing.T) {
	// The group names two tags but only one m= carries a=mid. The dangling tag
	// is reported as a Deviation at parse time; Legs returns the leg that does
	// exist rather than an empty set or a phantom entry.
	in := head + "a=group:DUP primary secondary\nm=video 1 RTP/AVP 96\nc=IN IP4 233.252.0.1/32\na=mid:primary\n"
	s, devs := parseOK(t, in)
	var reported bool
	for _, d := range devs {
		if strings.Contains(d.Msg, "has no matching a=mid") {
			reported = true
		}
	}
	if !reported {
		t.Errorf("dangling group tag was not reported: %v", devs)
	}
	legs := s.Legs()
	if len(legs) != 1 {
		t.Fatalf("legs = %d, want 1", len(legs))
	}
	if legs[0].Mid != "primary" || legs[0].Dest != "233.252.0.1" {
		t.Errorf("leg = %+v, want primary / 233.252.0.1", legs[0])
	}
}
