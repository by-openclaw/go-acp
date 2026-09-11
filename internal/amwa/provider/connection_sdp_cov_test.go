package provider

// The SDP a controller copies into a Receiver, across the essence
// shapes a Node can carry. Each media type renders its own rtpmap and
// fmtp, and a parser that meets the wrong form rejects the whole
// description rather than ignoring the line.

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is05"
)

// sdpFor renders the SDP for the bundle's first sender, with the
// bundle's flow replaced by the one the case describes.
func sdpForFlow(t *testing.T, mutate func(*NodeConfig)) string {
	t.Helper()
	b := audioBundle()
	mutate(b)
	cs := NewIS05ConnectionServer(newLogTap().logger(), b, IS05ConnectionConfig{APIVer: "v1.2"})
	cs.Store().setNodeIP("10.0.0.7")
	cs.Store().reresolveActive()

	id := b.Senders[0].ID
	e, err := cs.Store().get("senders", id)
	if err != nil {
		t.Fatalf("get sender: %v", err)
	}
	return cs.sdpForSender(id, e.active)
}

// Each essence shape declares itself: the rtpmap and fmtp a receiver
// reads to know what is arriving.
func TestSDPPerEssenceShape(t *testing.T) {
	for _, tc := range []struct {
		name   string
		flow   func(*is04.Flow)
		want   []string
		absent []string
	}{
		{
			name: "raw video",
			flow: func(f *is04.Flow) {
				f.Format = "urn:x-nmos:format:video"
				f.MediaType = "video/raw"
				f.FrameWidth, f.FrameHeight = 1920, 1080
			},
			want: []string{"m=video ", "raw/90000", "width=1920", "height=1080"},
		},
		{
			name: "JPEG XS",
			flow: func(f *is04.Flow) {
				f.Format = "urn:x-nmos:format:video"
				f.MediaType = "video/jxsv"
				f.FrameWidth, f.FrameHeight = 1920, 1080
				f.Profile, f.Level, f.Sublevel = "High444.12", "2k-1", "Sublev3bpp"
				f.FlowBitRate = 250000
				f.GrainRate = &is04.GrainRate{Numerator: 50}
			},
			want: []string{"jxsv/90000", "packetmode=0", "exactframerate=50", "b=AS:250000"},
		},
		{
			name: "JPEG XS at a fractional rate",
			flow: func(f *is04.Flow) {
				f.Format = "urn:x-nmos:format:video"
				f.MediaType = "video/jxsv"
				f.FrameWidth, f.FrameHeight = 1920, 1080
				f.GrainRate = &is04.GrainRate{Numerator: 30000, Denominator: 1001}
			},
			want: []string{"exactframerate=30000/1001"},
		},
		{
			name: "MPEG transport stream",
			flow: func(f *is04.Flow) {
				f.Format = "urn:x-nmos:format:video"
				f.MediaType = "video/MP2T"
			},
			want: []string{"m=video ", "MP2T/90000"},
		},
		{
			name: "ancillary data",
			flow: func(f *is04.Flow) {
				f.Format = "urn:x-nmos:format:data"
				f.MediaType = "video/smpte291"
			},
			want: []string{"m=application ", "smpte291/90000"},
		},
		{
			name: "a mux",
			flow: func(f *is04.Flow) {
				f.Format = "urn:x-nmos:format:mux"
				f.MediaType = "video/SMPTE2022-6"
			},
			want: []string{"m=video ", "SMPTE2022-6/90000"},
		},
		{
			name: "16-bit audio",
			flow: func(f *is04.Flow) {
				f.Format = "urn:x-nmos:format:audio"
				f.MediaType = "audio/L16"
				f.BitDepth = 16
				f.SampleRate = &is04.GrainRate{Numerator: 96000}
			},
			want: []string{"L16/96000/", "a=ptime:1"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sdp := sdpForFlow(t, func(b *NodeConfig) {
				if len(b.Flows) == 0 {
					t.Fatal("the audio bundle carries a flow")
				}
				tc.flow(&b.Flows[0])
			})
			if sdp == "" {
				t.Fatal("an RTP sender with resolved ACTIVE params must publish an SDP")
			}
			for _, w := range tc.want {
				if !strings.Contains(sdp, w) {
					t.Errorf("missing %q:\n%s", w, sdp)
				}
			}
			for _, a := range tc.absent {
				if strings.Contains(sdp, a) {
					t.Errorf("unexpected %q:\n%s", a, sdp)
				}
			}
		})
	}
}

// BCP-005-02 and -03: an HDCP-protected stream and a PEP-encrypted one
// each carry a session-level marker, and a controller reads its
// presence to know what it is joining.
func TestSDPCarriesProtectionMarkers(t *testing.T) {
	on := true
	sdp := sdpForFlow(t, func(b *NodeConfig) {
		b.Senders[0].Hkep = &on
	})
	if !strings.Contains(sdp, "a=hkep") {
		t.Errorf("an HDCP-protected stream must be marked:\n%s", sdp)
	}
}

// A sender that is not RTP has no SDP to publish, and neither does one
// whose ACTIVE parameters have not resolved: an SDP describing legs
// nobody has chosen names addresses that are not listening.
func TestSDPIsNotPublishedWithoutSomethingToDescribe(t *testing.T) {
	b := audioBundle()
	b.Senders[0].Transport = "urn:x-nmos:transport:mqtt"
	cs := NewIS05ConnectionServer(newLogTap().logger(), b, IS05ConnectionConfig{APIVer: "v1.2"})
	if got := cs.sdpForSender(b.Senders[0].ID, is05.StagedSender{}); got != "" {
		t.Errorf("a non-RTP sender = %q, want nothing", got)
	}

	rtp := audioBundle()
	cs2 := NewIS05ConnectionServer(newLogTap().logger(), rtp, IS05ConnectionConfig{APIVer: "v1.2"})
	if got := cs2.sdpForSender(rtp.Senders[0].ID, is05.StagedSender{}); got != "" {
		t.Errorf("a sender with no legs = %q, want nothing", got)
	}
	if got := cs2.sdpForSender("not-a-sender", is05.StagedSender{}); got != "" {
		t.Errorf("a sender that is not there = %q, want nothing", got)
	}
}

// The SDP's a=source-filter line names the sending interface's MAC.
// The line is mandatory, so an unresolvable binding still renders
// something syntactically valid rather than omitting it.
func TestSenderMACFallsBackRatherThanOmitting(t *testing.T) {
	b := audioBundle()
	cs := NewIS05ConnectionServer(newLogTap().logger(), b, IS05ConnectionConfig{APIVer: "v1.2"})

	snd := &b.Senders[0]
	if got := cs.senderLocalMAC(snd); got == "" {
		t.Error("the MAC line is mandatory")
	}

	// A binding that names no declared interface falls back to the
	// Node's first one.
	snd.InterfaceBindings = []string{"eth99"}
	if got := cs.senderLocalMAC(snd); got == "" {
		t.Error("an unmatched binding must still render")
	}

	// With no interfaces at all, and with no bundle, the all-zero MAC.
	b.Node.Interfaces = nil
	if got := cs.senderLocalMAC(snd); got != "00-00-00-00-00-00" {
		t.Errorf("= %q, want the all-zero MAC", got)
	}
	cs.bundle = nil
	if got := cs.senderLocalMAC(snd); got != "00-00-00-00-00-00" {
		t.Errorf("with no bundle = %q, want the all-zero MAC", got)
	}
}

// A sender whose flow_id names nothing yields no flow, and the SDP
// falls back to what it can say rather than dereferencing a nil.
func TestFlowLookupForAFlowThatIsNotThere(t *testing.T) {
	b := audioBundle()
	cs := NewIS05ConnectionServer(newLogTap().logger(), b, IS05ConnectionConfig{APIVer: "v1.2"})

	absent := "aaaaaaaa-9999-4999-8999-999999999999"
	if got := cs.flowByID(&absent); got != nil {
		t.Errorf("= %+v, want nothing", got)
	}
	if got := cs.flowByID(nil); got != nil {
		t.Errorf("a sender with no flow = %+v, want nothing", got)
	}
}

// A multicast destination carries a TTL on the connection line and a
// unicast one must not — an SDP parser meeting the wrong form rejects
// the whole description rather than ignoring the line.
func TestSDPConnectionLineFollowsTheDestination(t *testing.T) {
	b := audioBundle()
	cs := NewIS05ConnectionServer(newLogTap().logger(), b, IS05ConnectionConfig{APIVer: "v1.2"})
	cs.Store().setNodeIP("10.0.0.7")
	cs.Store().reresolveActive()
	id := b.Senders[0].ID

	for _, tc := range []struct {
		dst  string
		want string
	}{
		{"239.10.10.10", "c=IN IP4 239.10.10.10/64"},
		{"10.0.0.9", "c=IN IP4 10.0.0.9\r\n"},
	} {
		e, err := cs.Store().get("senders", id)
		if err != nil {
			t.Fatal(err)
		}
		active := cloneStaged(e.active)
		active.TransportParams[0]["destination_ip"] = tc.dst

		sdp := cs.sdpForSender(id, active)
		if !strings.Contains(sdp, tc.want) {
			t.Errorf("%s: missing %q:\n%s", tc.dst, tc.want, sdp)
		}
	}
}

// The sampling name comes from the component geometry, and a geometry
// none of the standard subsamplings describes still has to render
// something an SDP parser accepts.
func TestSamplingFromComponentGeometry(t *testing.T) {
	comps := func(cbW, cbH int) *is04.Flow {
		return &is04.Flow{Components: []is04.FlowVideoComponent{
			{Name: "Y", Width: 1920, Height: 1080},
			{Name: "Cb", Width: cbW, Height: cbH},
			{Name: "Cr", Width: cbW, Height: cbH},
		}}
	}
	for _, tc := range []struct {
		name string
		flow *is04.Flow
		want string
	}{
		{"4:4:4", comps(1920, 1080), "YCbCr-4:4:4"},
		{"4:2:2", comps(960, 1080), "YCbCr-4:2:2"},
		{"4:2:0", comps(960, 540), "YCbCr-4:2:0"},
		{"a geometry with no standard name", comps(480, 270), "YCbCr-4:2:2"},
		{"no components at all", &is04.Flow{}, "YCbCr-4:2:2"},
	} {
		if got := samplingFromComponents(tc.flow); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, got, tc.want)
		}
	}
}
