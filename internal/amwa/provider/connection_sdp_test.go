package provider

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is05"
)

func sdpTestServer(t *testing.T) *IS05ConnectionServer {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	b := audioBundle()
	cs := NewIS05ConnectionServer(logger, b, IS05ConnectionConfig{APIVer: "v1.2"})
	cs.Store().setNodeIP("10.0.0.7")
	cs.Store().reresolveActive()
	return cs
}

// TestSenderSDPIsWellFormed: the file a controller copies into a
// Receiver has to parse, and has to describe the addresses ACTIVE
// actually names.
func TestSenderSDPIsWellFormed(t *testing.T) {
	cs := sdpTestServer(t)
	id := cs.bundle.Senders[0].ID
	e, err := cs.Store().get("senders", id)
	if err != nil {
		t.Fatalf("get sender: %v", err)
	}
	sdp := cs.sdpForSender(id, e.active)
	if sdp == "" {
		t.Fatal("an RTP sender with resolved ACTIVE params must publish an SDP")
	}

	// RFC 4566 §5 fixes the order of the leading lines; a parser that
	// sees them out of order rejects the whole description.
	want := []string{"v=0", "o=", "s=", "t=0 0", "m=audio ", "c=IN IP4 ", "a=rtpmap:96 "}
	for _, prefix := range want {
		if !strings.Contains(sdp, prefix) {
			t.Errorf("SDP missing %q:\n%s", prefix, sdp)
		}
	}
	if !strings.Contains(sdp, "L24/48000/2") {
		t.Errorf("rtpmap must describe the Flow (L24 48k stereo):\n%s", sdp)
	}
	// Every line ends CRLF per RFC 4566 §5 -- a bare LF is the classic
	// interop failure with hardware parsers.
	for _, line := range strings.Split(strings.TrimSuffix(sdp, "\r\n"), "\r\n") {
		if strings.Contains(line, "\n") {
			t.Errorf("line not CRLF-terminated: %q", line)
		}
	}
}

// TestSenderSDPLocalMACMatchesInterfaceBinding: ts-refclk names the
// port the stream leaves by, and IS-04 already states that binding
// twice. A constant here contradicts both.
func TestSenderSDPLocalMACMatchesInterfaceBinding(t *testing.T) {
	cs := sdpTestServer(t)
	snd := &cs.bundle.Senders[0]
	var want string
	for _, iface := range cs.bundle.Node.Interfaces {
		if iface.Name == snd.InterfaceBindings[0] {
			want = iface.PortID
		}
	}
	if want == "" {
		t.Skip("test bundle interface has no port_id")
	}
	e, _ := cs.Store().get("senders", snd.ID)
	sdp := cs.sdpForSender(snd.ID, e.active)
	if !strings.Contains(sdp, "a=ts-refclk:localmac="+want) {
		t.Errorf("ts-refclk must carry the bound interface's port_id %q:\n%s", want, sdp)
	}
}

// TestSenderSDPDuplicatesForTwoLegs: an ST 2022-7 pair is ONE stream
// carried twice, and ST 2110-10 §8.3 requires the SDP to say so — a
// session-level `a=group:DUP` and one media section per leg. SDPoker
// rejected a two-leg SDP without the group, and IS-05-01
// test_09_01/25/27/29 reject an SDP whose media-section count
// disagrees with transport_params. Cerebrum additionally keys on the
// exact `primary secondary` spelling.
func TestSenderSDPDuplicatesForTwoLegs(t *testing.T) {
	cs := sdpTestServer(t)
	id := cs.bundle.Senders[0].ID
	active := is05.StagedSender{
		MasterEnableField: is05.MasterEnableField{MasterEnable: true},
		TransportParams: []is05.TransportParams{
			{"source_ip": "10.0.0.7", "destination_ip": "239.20.1.1", "destination_port": 5004, "rtp_enabled": true},
			{"source_ip": "10.0.0.7", "destination_ip": "239.22.1.1", "destination_port": 5004, "rtp_enabled": true},
		},
	}
	sdp := cs.sdpForSender(id, active)
	if !strings.Contains(sdp, "a=group:DUP primary secondary\r\n") {
		t.Errorf("two-leg SDP must carry a=group:DUP primary secondary:\n%s", sdp)
	}
	if got := strings.Count(sdp, "m=audio "); got != 2 {
		t.Errorf("two-leg SDP must carry 2 media sections, got %d:\n%s", got, sdp)
	}
	for _, want := range []string{"c=IN IP4 239.20.1.1/64", "c=IN IP4 239.22.1.1/64", "a=mid:primary", "a=mid:secondary"} {
		if !strings.Contains(sdp, want) {
			t.Errorf("two-leg SDP missing %q:\n%s", want, sdp)
		}
	}

	// And the single-leg form must NOT grow a group — declaring a DUP
	// with one member is exactly the malformed case parsers reject.
	single := is05.StagedSender{
		MasterEnableField: is05.MasterEnableField{MasterEnable: true},
		TransportParams: []is05.TransportParams{
			{"source_ip": "10.0.0.7", "destination_ip": "239.20.1.1", "destination_port": 5004, "rtp_enabled": true},
		},
	}
	sdp = cs.sdpForSender(id, single)
	if strings.Contains(sdp, "a=group:DUP") {
		t.Errorf("single-leg SDP must not declare a DUP group:\n%s", sdp)
	}
	if got := strings.Count(sdp, "m=audio "); got != 1 {
		t.Errorf("single-leg SDP must carry exactly 1 media section, got %d", got)
	}
}

// TestSDPRoundTripsIntoReceiverParams: what a Sender publishes is
// exactly what a Receiver is given, so the two halves must agree --
// the controller copies the file verbatim and translates nothing.
func TestSDPRoundTripsIntoReceiverParams(t *testing.T) {
	cs := sdpTestServer(t)
	id := cs.bundle.Senders[0].ID
	e, _ := cs.Store().get("senders", id)
	sdp := cs.sdpForSender(id, e.active)

	got := sdpReceiverParams(sdp)
	sent := e.active.TransportParams[0]

	if got["source_ip"] != sent["source_ip"] {
		t.Errorf("source_ip: receiver derived %v, sender transmits %v", got["source_ip"], sent["source_ip"])
	}
	if got["destination_port"] != sent["destination_port"] {
		t.Errorf("destination_port: receiver derived %v, sender transmits %v",
			got["destination_port"], sent["destination_port"])
	}
	if got["multicast_ip"] != sent["destination_ip"] {
		t.Errorf("multicast_ip: receiver derived %v, sender sends to %v",
			got["multicast_ip"], sent["destination_ip"])
	}
	if got["rtp_enabled"] != true {
		t.Error("an SDP arriving at all means the far end is transmitting RTP")
	}
}

// TestSDPUnicastDoesNotSetMulticastIP: a unicast connection address is
// where the stream lands, not a group to join.
func TestSDPUnicastDoesNotSetMulticastIP(t *testing.T) {
	const sdp = "v=0\r\n" +
		"o=- 1 1 IN IP4 10.0.0.7\r\n" +
		"s=unicast\r\nt=0 0\r\n" +
		"m=audio 5004 RTP/AVP 96\r\n" +
		"c=IN IP4 10.0.0.9\r\n" +
		"a=rtpmap:96 L24/48000/2\r\n"
	got := sdpReceiverParams(sdp)
	if got["multicast_ip"] != nil {
		t.Errorf("multicast_ip = %v, want nil for a unicast stream", got["multicast_ip"])
	}
	if got["interface_ip"] != "10.0.0.9" {
		t.Errorf("interface_ip = %v, want the unicast destination", got["interface_ip"])
	}
	if got["source_ip"] != "10.0.0.7" {
		t.Errorf("source_ip = %v, want the o= address when no source-filter is present", got["source_ip"])
	}
}

// TestActivationModeMarshalsNullWhenUnset: every IS-05 schema types
// mode as a nullable enum, and "" is not a member.
func TestActivationModeMarshalsNullWhenUnset(t *testing.T) {
	raw, err := is05.Activation{}.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"mode":null`) {
		t.Errorf("unset mode must serialise as null, got %s", raw)
	}
	set := is05.Activation{Mode: is05.ActivationModeImmediate}
	raw, _ = set.MarshalJSON()
	if !strings.Contains(string(raw), `"mode":"activate_immediate"`) {
		t.Errorf("a set mode must survive round-trip, got %s", raw)
	}
}
