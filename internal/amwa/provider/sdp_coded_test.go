package provider

// SDP generation for coded media types (BCP-006-01 JPEG XS,
// BCP-006-04 MPEG-TS): rtpmap/fmtp/b=AS come from the Flow's register
// attributes so manifest and IS-04 body agree.

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04"
)

func TestMediaLinesForJXSV(t *testing.T) {
	f := &is04.Flow{
		Format: "urn:x-nmos:format:video", MediaType: "video/jxsv",
		FrameWidth: 1920, FrameHeight: 1080,
		Profile: "High444.12", Level: "2k-1", Sublevel: "Sublev3bpp",
	}
	media, rtpmap, extra := mediaLinesFor(f, 0)
	if media != "video" || rtpmap != "jxsv/90000" {
		t.Fatalf("media=%q rtpmap=%q", media, rtpmap)
	}
	if len(extra) != 1 {
		t.Fatalf("extra=%v", extra)
	}
	for _, want := range []string{"profile=High444.12", "level=2k-1", "sublevel=Sublev3bpp", "width=1920", "height=1080", "packetmode=0", "SSN=ST2110-22:2019"} {
		if !strings.Contains(extra[0], want) {
			t.Errorf("fmtp missing %s: %s", want, extra[0])
		}
	}
}

func TestMediaLinesForMP2T(t *testing.T) {
	f := &is04.Flow{Format: "urn:x-nmos:format:mux", MediaType: "video/MP2T"}
	media, rtpmap, _ := mediaLinesFor(f, 0)
	if media != "video" || rtpmap != "MP2T/90000" {
		t.Fatalf("media=%q rtpmap=%q", media, rtpmap)
	}
}

func TestSDPCarriesBandwidthForCodedFlow(t *testing.T) {
	cs := sdpTestServer(t)
	// Retarget the first sender's flow to a jxsv flow with a bit_rate.
	snd := &cs.bundle.Senders[0]
	flowID := *snd.FlowID
	for i := range cs.bundle.Flows {
		if cs.bundle.Flows[i].ID == flowID {
			cs.bundle.Flows[i].MediaType = "video/jxsv"
			cs.bundle.Flows[i].Format = "urn:x-nmos:format:video"
			cs.bundle.Flows[i].FrameWidth = 1920
			cs.bundle.Flows[i].FrameHeight = 1080
			cs.bundle.Flows[i].Profile = "High444.12"
			cs.bundle.Flows[i].Level = "2k-1"
			cs.bundle.Flows[i].Sublevel = "Sublev3bpp"
			cs.bundle.Flows[i].FlowBitRate = 497664
		}
	}
	e, _ := cs.Store().get("senders", snd.ID)
	sdp := cs.sdpForSender(snd.ID, e.active)
	if !strings.Contains(sdp, "b=AS:497664\r\n") {
		t.Errorf("SDP missing b=AS bandwidth:\n%s", sdp)
	}
	if !strings.Contains(sdp, "a=rtpmap:96 jxsv/90000") {
		t.Errorf("SDP missing jxsv rtpmap:\n%s", sdp)
	}
}
