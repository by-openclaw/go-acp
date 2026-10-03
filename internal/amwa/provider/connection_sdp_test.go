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

// A two-leg SDP (a=group:DUP primary secondary) is paired with a
// receiver's legs in order: a one-leg receiver takes the primary, a
// two-leg receiver takes primary and secondary. The line scanner this
// replaces folded every section into one map, so the last c= won and
// landed on every leg — a receiver joining the secondary group twice
// (issue #1270, found by the plant's SDP-change simulation).
func TestATwoLegSDPIsPairedWithTheReceiversLegsInOrder(t *testing.T) {
	cs := sdpTestServer(t)
	sdp := cs.sdpForSender(cs.bundle.Senders[0].ID, is05.StagedSender{
		MasterEnableField: is05.MasterEnableField{MasterEnable: true},
		TransportParams: []is05.TransportParams{
			{"source_ip": "10.0.0.7", "destination_ip": "239.20.1.1", "destination_port": 5004, "rtp_enabled": true},
			{"source_ip": "10.0.0.8", "destination_ip": "239.22.1.1", "destination_port": 5006, "rtp_enabled": true},
		},
	})

	legs := sdpReceiverLegs(sdp)
	if len(legs) != 2 {
		t.Fatalf("legs = %d, want 2 from a DUP SDP:\n%s", len(legs), sdp)
	}
	if legs[0]["multicast_ip"] != "239.20.1.1" || legs[0]["destination_port"] != 5004 || legs[0]["source_ip"] != "10.0.0.7" {
		t.Errorf("leg 0 = %v, want the primary section", legs[0])
	}
	if legs[1]["multicast_ip"] != "239.22.1.1" || legs[1]["destination_port"] != 5006 || legs[1]["source_ip"] != "10.0.0.8" {
		t.Errorf("leg 1 = %v, want the secondary section", legs[1])
	}
	if got := sdpReceiverParams(sdp); got["multicast_ip"] != "239.20.1.1" {
		t.Errorf("a single-path reading takes the primary, got %v", got["multicast_ip"])
	}
	if got := sdpReceiverLegs("not an sdp"); got != nil {
		t.Errorf("an unparseable SDP yields no legs, got %v", got)
	}
	if got := sdpReceiverParams(""); len(got) != 0 {
		t.Errorf("an empty SDP yields no parameters, got %v", got)
	}
	// A session-level source-filter names the source for every leg
	// that has none of its own; other session attributes are passed
	// over on the way to it.
	const crlf = "\r\n"
	sessionFiltered := strings.Join([]string{
		"v=0",
		"o=- 1 1 IN IP4 10.0.0.1",
		"s=two legs, one filter",
		"t=0 0",
		"a=group:DUP primary secondary",
		"a=ts-refclk:ptp=IEEE1588-2008:traceable",
		"a=source-filter: incl IN IP4 239.30.0.1 10.0.0.9",
		"m=audio 5004 RTP/AVP 96",
		"c=IN IP4 239.30.0.1/64",
		"a=mid:primary",
		"m=audio 5004 RTP/AVP 96",
		"c=IN IP4 239.94.0.1/64",
		"a=mid:secondary",
	}, crlf) + crlf
	filtered := sdpReceiverLegs(sessionFiltered)
	if len(filtered) != 2 || filtered[0]["source_ip"] != "10.0.0.9" || filtered[1]["source_ip"] != "10.0.0.9" {
		t.Errorf("a session-level source-filter applies to every leg, got %v", filtered)
	}
	// A leg the receiver has that the SDP does not describe keeps what
	// it had (covered below with the grown receiver), and a session
	// without legs yields none.
	if got := sdpReceiverLegs(strings.Join([]string{"v=0", "o=- 1 1 IN IP4 10.0.0.1", "s=empty", "t=0 0"}, crlf)); len(got) != 0 {
		t.Errorf("an SDP without media sections yields no legs, got %v", got)
	}

	// Through the PATCH: a one-leg receiver, then the same receiver
	// grown to two legs.
	st := cs.Store()
	rid := cs.bundle.Receivers[0].ID
	e, err := st.get("receivers", rid)
	if err != nil {
		t.Fatal(err)
	}
	typ := "application/sdp"
	patch := is05.StagedSender{TransportFile: &is05.TransportFile{Type: &typ, Data: &sdp}}
	if _, code, err := st.applyPatch("receivers", rid, patch, patchFields{TransportFile: true}); err != nil || code != 200 {
		t.Fatalf("one-leg PATCH = %d, %v", code, err)
	}
	st.mu.RLock()
	one := append([]is05.TransportParams(nil), e.staged.TransportParams...)
	st.mu.RUnlock()
	if len(one) < 1 || one[0]["multicast_ip"] != "239.20.1.1" {
		t.Fatalf("a one-leg receiver takes the primary: %v", one)
	}

	st.mu.Lock()
	e.staged.TransportParams = append(e.staged.TransportParams, is05.TransportParams{})
	for k, v := range one[0] {
		e.staged.TransportParams[1][k] = v
	}
	if len(e.constraints) == 1 {
		e.constraints = append(e.constraints, e.constraints[0])
	}
	st.mu.Unlock()
	if _, code, err := st.applyPatch("receivers", rid, patch, patchFields{TransportFile: true}); err != nil || code != 200 {
		t.Fatalf("two-leg PATCH = %d, %v", code, err)
	}
	st.mu.RLock()
	two := append([]is05.TransportParams(nil), e.staged.TransportParams...)
	st.mu.RUnlock()
	if len(two) != 2 || two[0]["multicast_ip"] != "239.20.1.1" || two[1]["multicast_ip"] != "239.22.1.1" {
		t.Fatalf("a two-leg receiver takes primary then secondary: %v", two)
	}
	// A one-section SDP onto the two-leg receiver: the second leg
	// keeps the secondary it had.
	single := cs.sdpForSender(cs.bundle.Senders[0].ID, is05.StagedSender{
		MasterEnableField: is05.MasterEnableField{MasterEnable: true},
		TransportParams: []is05.TransportParams{
			{"source_ip": "10.0.0.7", "destination_ip": "239.21.1.1", "destination_port": 5004, "rtp_enabled": true},
		},
	})
	patch = is05.StagedSender{TransportFile: &is05.TransportFile{Type: &typ, Data: &single}}
	if _, code, err := st.applyPatch("receivers", rid, patch, patchFields{TransportFile: true}); err != nil || code != 200 {
		t.Fatalf("one-section PATCH = %d, %v", code, err)
	}
	st.mu.RLock()
	kept := append([]is05.TransportParams(nil), e.staged.TransportParams...)
	st.mu.RUnlock()
	if kept[0]["multicast_ip"] != "239.21.1.1" || kept[1]["multicast_ip"] != "239.22.1.1" {
		t.Fatalf("an undescribed leg keeps what it had: %v", kept)
	}
}
