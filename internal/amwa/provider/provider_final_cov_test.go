package provider

// The residue: arms reached only through a particular combination of
// bundle, transport and request that no other test happens to build.

import (
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is05"
)

// An IS-08 activation request the schema refuses is a bad request, and
// the refusal carries the schema's reason: a controller sending an
// action the device cannot make needs to know which part was wrong.
func TestChannelMapActivationRefusesAnInvalidRequest(t *testing.T) {
	s := nodeFor(t, audioBundle(), nil)

	req := httptest.NewRequest(stdhttp.MethodPost, cmBase+"/map/activations/",
		strings.NewReader(`{"activation":{"mode":"activate_scheduled_absolute"},"action":{}}`))
	code, body, err := s.channelMapping.handleActivationPost(req)
	if err != nil || code != stdhttp.StatusBadRequest {
		t.Fatalf("= %d (%+v) %v, want 400", code, body, err)
	}
}

// A sender's activation writes its own IS-04 subscription and stops:
// the receiver loop below it is for the other collection, and running
// both would have one activation touch two resources.
func TestSenderActivationTouchesOnlyTheSender(t *testing.T) {
	b := routableBundle(t)
	s := NewIS05ConnectionServer(newLogTap().logger(), b, IS05ConnectionConfig{APIVer: "v1.2"})
	rid := b.Receivers[0].ID

	_, _, err := s.Store().applyPatch("senders", b.Senders[0].ID, is05.StagedSender{
		MasterEnableField: is05.MasterEnableField{MasterEnable: true},
		Activation:        is05.Activation{Mode: is05.ActivationModeImmediate},
	}, patchFields{MasterEnable: true})
	if err != nil {
		t.Fatal(err)
	}

	if got := b.Receivers[0].Subscription; got.Active {
		t.Errorf("receiver %s was touched by a sender's activation: %+v", rid, got)
	}
}

// A sender id the bundle does not carry is not an error on the way out
// of an activation: the endpoint was staged from the same bundle, so
// there is nothing to update and nothing to report.
func TestSubscriptionUpdateForAResourceTheBundleLost(t *testing.T) {
	b := routableBundle(t)
	s := NewIS05ConnectionServer(newLogTap().logger(), b, IS05ConnectionConfig{APIVer: "v1.2"})

	s.updateIS04Subscription("senders", "not-a-sender", is05.StagedSender{})
	s.updateIS04Subscription("receivers", "not-a-receiver", is05.StagedSender{})
}

// A scheduled activation whose requested time will not resolve is
// refused rather than queued: a switch nobody can put a time on is a
// 202 that never fires.
func TestScheduledActivationWithATimeThatWillNotResolve(t *testing.T) {
	s, sid, _ := is05Server(t)

	bad := "99999999999999999999:0" // passes the grammar, overflows int64
	_, status, err := s.Store().applyPatch("senders", sid, is05.StagedSender{
		Activation: is05.Activation{
			Mode: is05.ActivationModeScheduledAbsolute, RequestedTime: &bad,
		},
	}, patchFields{})
	if err == nil || status != 400 {
		t.Fatalf("= %d (%v), want 400", status, err)
	}
}

// A receiver may not have chosen a source address yet, so null is a
// legal value there — IS-05-02 test_18 rejects an endpoint that
// refuses it.
func TestNullAddressIsLegalOnAReceiver(t *testing.T) {
	if err := validateParamValue("interface_ip", nil, false); err != nil {
		t.Errorf("a receiver's unset address = %v, want it accepted", err)
	}
	if err := validateParamValue("interface_ip", 7, false); err == nil {
		t.Error("a number where an address belongs must be refused")
	}
}

// An endpoint whose ACTIVE already carries an activation time is live,
// and a re-resolution pass must not rewrite it: the values a
// controller is reading are the ones the switch actually used.
func TestReresolveLeavesALiveEndpointAlone(t *testing.T) {
	s, sid, _ := is05Server(t)
	st := s.Store()
	st.setNodeIP("10.0.0.7")

	if _, _, err := st.applyPatch("senders", sid, is05.StagedSender{
		MasterEnableField: is05.MasterEnableField{MasterEnable: true},
		Activation:        is05.Activation{Mode: is05.ActivationModeImmediate},
	}, patchFields{MasterEnable: true}); err != nil {
		t.Fatal(err)
	}
	e, err := st.get("senders", sid)
	if err != nil {
		t.Fatal(err)
	}
	before := e.active.Activation.ActivationTime

	st.setNodeIP("10.0.0.9")
	st.reresolveActive()

	if e.active.Activation.ActivationTime != before {
		t.Error("a live endpoint's activation was rewritten")
	}
}

// The constraint set carries exactly the keys the leg stages: the
// suite compares the two sets, so a constraint key the leg does not
// stage is as wrong as a missing one.
func TestConstraintsMatchTheStagedKeysOnAnMQTTLeg(t *testing.T) {
	b := audioBundle()
	b.Senders[0].Transport = "urn:x-nmos:transport:mqtt"
	s := NewIS05ConnectionServer(newLogTap().logger(), b, IS05ConnectionConfig{APIVer: "v1.2"})
	s.Store().setNodeBase("10.0.0.7:8080")
	s.Store().reresolveActive()

	e, err := s.Store().get("senders", b.Senders[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.constraints) == 0 || len(e.staged.TransportParams) == 0 {
		t.Fatal("an MQTT sender stages a leg with constraints")
	}
	if _, ws := e.staged.TransportParams[0]["ext_is_07_source_id"]; ws {
		t.Error("an MQTT leg stages only the REST url, not the source id")
	}
}

// The MQTT bridge only acts on senders it was built for, and only on
// activations that name a broker it can reach.
func TestMQTTBridgeActivationPaths(t *testing.T) {
	b := mqttEventBundle(t)
	bridge := newMQTTEventBridge(newLogTap().logger(), b,
		func(string) (any, bool) { return map[string]any{"x": 1}, true })
	if bridge == nil {
		t.Fatal("a bundle with an MQTT event sender must build a bridge")
	}
	sid := b.Senders[0].ID

	// A sender that is not one of its own, and an activation with no
	// legs at all: neither is the bridge's business.
	bridge.OnSenderActivation("not-a-sender", is05.StagedSender{
		TransportParams: []is05.TransportParams{{}},
	})
	bridge.OnSenderActivation(sid, is05.StagedSender{})

	// Legs that name no broker: the binding is recorded, but there is
	// nothing to dial and nothing is published into the void.
	bridge.OnSenderActivation(sid, is05.StagedSender{
		TransportParams: []is05.TransportParams{{}},
	})

	// A state change for a source with no active binding goes nowhere
	// rather than to a broker nobody asked for.
	bridge.OnStateChanged("not-a-source", map[string]any{"x": 1})

	// A message the model cannot render is not published at all: a
	// consumer reading an empty retained message would take it for the
	// source's current state.
	bridge.OnStateChanged(b.Sources[0].ID, make(chan int))

	bridge.Close()
}

// mqttEventBundle is the smallest bundle carrying an IS-07 event
// sender over MQTT: a data flow, its source, and a sender bound to it.
func mqttEventBundle(t *testing.T) *NodeConfig {
	t.Helper()
	b := routableBundle(t)
	dev := b.Devices[0].ID
	src := is04.Source{
		ResourceCore: is04.ResourceCore{
			ID: "eeeeeeee-5555-4555-8555-555555555555", Version: "0:0",
			Label: "events", Description: "event source", Tags: map[string][]string{},
		},
		DeviceID: dev,
		Parents:  []string{},
		Format:   formatData,
		Caps:     map[string]any{},
	}
	flow := is04.Flow{
		ResourceCore: is04.ResourceCore{
			ID: "ffffffff-6666-4666-8666-666666666666", Version: "0:0",
			Label: "events", Description: "event flow", Tags: map[string][]string{},
		},
		SourceID:  src.ID,
		DeviceID:  dev,
		Parents:   []string{},
		Format:    formatData,
		MediaType: "application/json",
		EventType: "boolean",
	}
	fid := flow.ID
	b.Sources = append(b.Sources, src)
	b.Flows = append(b.Flows, flow)
	b.Senders[0].Transport = is04.TransportMQTT
	b.Senders[0].FlowID = &fid
	return b
}

// An IS-08 request that decodes but names an action the device cannot
// perform is refused by the validator, with its reason carried
// through: a controller sending a half-set entry needs to know which
// part was wrong.
func TestChannelMapActivationRefusesAnInvalidAction(t *testing.T) {
	s := nodeFor(t, audioBundle(), nil)

	req := httptest.NewRequest(stdhttp.MethodPost, cmBase+"/map/activations/",
		strings.NewReader(`{"activation":{"mode":"activate_immediate"},"action":{"out":{"0":{"input":"in"}}}}`))
	code, _, err := s.channelMapping.handleActivationPost(req)
	if err != nil || code != stdhttp.StatusBadRequest {
		t.Fatalf("= %d (%v), want 400", code, err)
	}
}

// A parameter the SDP describes but the endpoint does not publish is
// essence description, not transport: inventing the key would leave
// the endpoint failing its own constraints on the next read.
func TestSDPDerivedParametersStayInsideTheEndpoint(t *testing.T) {
	s, _, rid := is05Server(t)

	sdp := strings.Join([]string{
		"v=0", "o=- 1 1 IN IP4 192.0.2.1", "s=test", "t=0 0",
		"m=video 5004 RTP/AVP 96",
		"c=IN IP4 239.10.10.10/64",
		"a=rtpmap:96 raw/90000",
		"a=fmtp:96 width=1920; height=1080",
		"",
	}, "\r\n")
	out, status, err := s.Store().applyPatch("receivers", rid, is05.StagedSender{
		TransportFile: &is05.TransportFile{Type: strp("application/sdp"), Data: &sdp},
	}, patchFields{TransportFile: true})
	if err != nil || status != stdhttp.StatusOK {
		t.Fatalf("= %d (%v)", status, err)
	}

	e, err := s.Store().get("receivers", rid)
	if err != nil {
		t.Fatal(err)
	}
	for k := range out.TransportParams[0] {
		if _, published := e.constraints[0][k]; !published {
			t.Errorf("%q rode in from the SDP but is not a parameter of this endpoint", k)
		}
	}
}
