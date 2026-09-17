package audit

// #850: the audit consumes codec/sdp deviations. A conformant SDP is
// silent; a malformed line is a WARN naming the file+line; a
// structurally broken file is an ERROR; a DUP group with one leg is
// its own WARN. Expectations written from the SDP text, not the
// checker's output.

import (
	"strings"
	"testing"
)

const cleanDupSDP = `v=0
o=- 1 1 IN IP4 198.51.100.10
s=clean
t=0 0
a=group:DUP primary secondary
m=video 5004 RTP/AVP 96
c=IN IP4 233.252.0.10/64
a=rtpmap:96 raw/90000
a=mid:primary
m=video 5004 RTP/AVP 96
c=IN IP4 233.252.0.138/64
a=rtpmap:96 raw/90000
a=mid:secondary
`

// one m= section, one a=rtpmap malformed (no clock), plus a DUP group
// that only names one resolvable leg.
const deviantSDP = `v=0
o=- 1 1 IN IP4 198.51.100.10
s=deviant
t=0 0
a=group:DUP primary secondary
m=video 5004 RTP/AVP 96
c=IN IP4 233.252.0.10/64
a=rtpmap:96 raw
a=mid:primary
`

func findingsByCode(fs []Finding) map[string]int {
	m := map[string]int{}
	for _, f := range fs {
		m[f.Code]++
	}
	return m
}

func TestCheckSDPConformance(t *testing.T) {
	h := &Harvest{
		Target: "198.51.100.99:3212", Label: "unit",
		SDP: map[string][]byte{
			"is05/aaaa.sdp": []byte(cleanDupSDP),
			"is05/bbbb.sdp": []byte(deviantSDP),
			"is05/cccc.sdp": []byte("not an sdp at all\n"),
		},
	}
	fs := checkSDPConformance(h)
	by := findingsByCode(fs)

	if by["NMOS-SDP-DEVIATION"] < 1 {
		t.Errorf("deviant SDP (rtpmap without clock) produced no NMOS-SDP-DEVIATION: %+v", fs)
	}
	if by["NMOS-SDP-DUP-INCOMPLETE"] != 1 {
		t.Errorf("DUP with one resolvable leg = %d NMOS-SDP-DUP-INCOMPLETE, want 1", by["NMOS-SDP-DUP-INCOMPLETE"])
	}
	if by["NMOS-SDP-UNPARSEABLE"] != 1 {
		t.Errorf("the non-SDP file = %d NMOS-SDP-UNPARSEABLE, want 1", by["NMOS-SDP-UNPARSEABLE"])
	}

	// The clean two-leg DUP SDP must contribute nothing.
	for _, f := range fs {
		if f.Resource == "sender/aaaa" {
			t.Errorf("clean SDP produced a finding: %+v", f)
		}
	}

	// Findings name the file + line and the resource id from the key.
	var dev Finding
	for _, f := range fs {
		if f.Code == "NMOS-SDP-DEVIATION" {
			dev = f
		}
	}
	if dev.Resource != "sender/bbbb" {
		t.Errorf("deviation resource = %q, want sender/bbbb", dev.Resource)
	}
	if !strings.Contains(dev.Detail, "is05/bbbb.sdp line ") {
		t.Errorf("deviation detail lacks file+line: %q", dev.Detail)
	}
}

func TestCheckSDPConformanceEmpty(t *testing.T) {
	if fs := checkSDPConformance(&Harvest{}); fs != nil {
		t.Errorf("no SDPs must yield no findings, got %+v", fs)
	}
}
