package provider

// Staging and activation: the leg count an endpoint fixes, the
// parameter values IS-05 gives a shape to, the three activation modes,
// and what a boot pass does with an endpoint that is already live.

import (
	stdhttp "net/http"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is05"
)

// stage applies one PATCH to the store and returns what it answered.
func stage(t *testing.T, s *IS05ConnectionServer, kind, id string,
	patch is05.StagedSender, present patchFields) (is05.StagedSender, int, error) {
	t.Helper()
	return s.Store().applyPatch(kind, id, patch, present)
}

// An activation the spec does not define is refused before anything is
// staged: a controller that asked for a mode this device cannot
// perform must not be told the request was accepted.
func TestActivationModeRefusals(t *testing.T) {
	s, sid, _ := is05Server(t)

	_, status, err := stage(t, s, "senders", sid, is05.StagedSender{
		Activation: is05.Activation{Mode: "activate_whenever"},
	}, patchFields{})
	if err == nil || status != 400 {
		t.Fatalf("= %d (%v), want 400", status, err)
	}
}

// The three modes IS-05 defines, and what each answers. A scheduled
// activation answers 202 with the absolute time the switch will
// happen — for a relative request only the server can say what "+5s"
// resolved to, and that is what makes a coordinated multi-device
// switch verifiable.
func TestActivationModes(t *testing.T) {
	s, sid, _ := is05Server(t)

	out, status, err := stage(t, s, "senders", sid, is05.StagedSender{}, patchFields{})
	if err != nil || status != 200 {
		t.Fatalf("staging alone = %d (%v)", status, err)
	}
	if out.Activation.Mode != "" {
		t.Errorf("a stage-only PATCH performed an activation: %+v", out.Activation)
	}

	out, status, err = stage(t, s, "senders", sid, is05.StagedSender{
		MasterEnableField: is05.MasterEnableField{MasterEnable: true},
		Activation:        is05.Activation{Mode: is05.ActivationModeImmediate},
	}, patchFields{MasterEnable: true})
	if err != nil || status != 200 {
		t.Fatalf("an immediate activation = %d (%v)", status, err)
	}
	if out.Activation.Mode != is05.ActivationModeImmediate {
		t.Errorf("the answer must report the activation performed: %+v", out.Activation)
	}

	when := "0:0"
	out, status, err = stage(t, s, "senders", sid, is05.StagedSender{
		Activation: is05.Activation{
			Mode: is05.ActivationModeScheduledRelative, RequestedTime: &when,
		},
	}, patchFields{})
	if err != nil || status != 202 {
		t.Fatalf("a scheduled activation = %d (%v)", status, err)
	}
	if out.Activation.ActivationTime == nil {
		t.Error("a scheduled activation must answer with the time it will happen")
	}

	bad := "not a TAI timestamp"
	if _, status, err := stage(t, s, "senders", sid, is05.StagedSender{
		Activation: is05.Activation{
			Mode: is05.ActivationModeScheduledAbsolute, RequestedTime: &bad,
		},
	}, patchFields{}); err == nil || status != 400 {
		t.Errorf("a time that will not parse = %d (%v), want 400", status, err)
	}
}

// The leg count is fixed by the endpoint, and a parameter outside the
// published set is a typo or a controller aimed at different hardware
// — IS-05 constraints carry additionalProperties:false, so the
// published set is the whole set.
func TestTransportParamRefusals(t *testing.T) {
	s, sid, _ := is05Server(t)

	if _, status, err := stage(t, s, "senders", sid, is05.StagedSender{
		TransportParams: []is05.TransportParams{{}, {}},
	}, patchFields{TransportParams: true}); err == nil ||
		!strings.Contains(err.Error(), "leg count") {
		t.Errorf("the wrong leg count = %d (%v)", status, err)
	}

	if _, status, err := stage(t, s, "senders", sid, is05.StagedSender{
		TransportParams: []is05.TransportParams{{"not_a_parameter": 1}},
	}, patchFields{TransportParams: true}); err == nil ||
		!strings.Contains(err.Error(), "not a parameter") {
		t.Errorf("an unpublished parameter = %d (%v)", status, err)
	}
}

// IS-05 gives each transport parameter a shape, and a plausible name
// with an impossible value has to be refused at the door: accepting it
// stages an endpoint that fails its own constraints on the next read.
func TestTransportParamValueShapes(t *testing.T) {
	for _, tc := range []struct {
		key      string
		value    any
		isSender bool
		ok       bool
	}{
		{"source_ip", "192.0.2.1", true, true},
		{"source_ip", autoKeyword, true, true},
		{"source_ip", nil, true, false}, // a sender must know where it sends from
		{"source_ip", nil, false, true}, // a receiver may not have chosen yet
		{"source_ip", 7, true, false},
		{"source_ip", "not an address", true, false},
		{"destination_port", 5004, true, true},
		{"destination_port", autoKeyword, true, true},
		{"destination_port", "5004", true, false},
		{"rtp_enabled", true, true, true},
		{"rtp_enabled", "yes", true, false},
		{"connection_authorization", true, false, true},
		{"connection_authorization", autoKeyword, false, true},
		{"connection_authorization", "sometimes", false, false},
		{"connection_authorization", 1, false, false},
		{"connection_uri", "mqtt://broker.local:1883", false, true},
		{"connection_uri", nil, false, true},
		{"connection_uri", 7, false, false},
		{"broker_topic", "x-nmos/events", false, true},
	} {
		err := validateParamValue(tc.key, tc.value, tc.isSender)
		if tc.ok && err != nil {
			t.Errorf("%s=%v (%T) = %v, want it accepted", tc.key, tc.value, tc.value, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("%s=%v (%T) was accepted, want it refused", tc.key, tc.value, tc.value)
		}
	}
}

// A receiver's activation writes its IS-04 subscription, because a
// controller reads subscription.active from the Node API and an active
// receiver that looks idle there is invisible everywhere except the
// Connection API.
func TestReceiverActivationWritesItsIS04Subscription(t *testing.T) {
	b := routableBundle(t)
	s := NewIS05ConnectionServer(newLogTap().logger(), b, IS05ConnectionConfig{APIVer: "v1.2"})
	rid := b.Receivers[0].ID
	sid := b.Senders[0].ID

	_, status, err := s.Store().applyPatch("receivers", rid, is05.StagedSender{
		MasterEnableField: is05.MasterEnableField{MasterEnable: true},
		ReceiverID:        &sid,
		Activation:        is05.Activation{Mode: is05.ActivationModeImmediate},
	}, patchFields{MasterEnable: true, SenderID: true})
	if err != nil || status != stdhttp.StatusOK {
		t.Fatalf("= %d (%v)", status, err)
	}

	if got := b.Receivers[0].Subscription; !got.Active ||
		got.SenderID == nil || *got.SenderID != sid {
		t.Errorf("subscription = %+v, want it naming the sender", got)
	}
}

// The boot pass promotes what the bundle seeded as enabled — once. A
// restart shares this path with a cold boot, so an endpoint that is
// already live must not be re-promoted over its own state.
func TestBootPromotionSkipsWhatIsAlreadyLive(t *testing.T) {
	s, sid, _ := is05Server(t)
	st := s.Store()

	_, _, err := st.applyPatch("senders", sid, is05.StagedSender{
		MasterEnableField: is05.MasterEnableField{MasterEnable: true},
		Activation:        is05.Activation{Mode: is05.ActivationModeImmediate},
	}, patchFields{MasterEnable: true})
	if err != nil {
		t.Fatal(err)
	}
	e, err := st.get("senders", sid)
	if err != nil {
		t.Fatal(err)
	}
	before := e.active.Activation.ActivationTime
	if before == nil {
		t.Fatal("an immediate activation stamps a time")
	}

	st.promoteBootEnabled()

	if got := e.active.Activation.ActivationTime; got != before {
		t.Errorf("the boot pass re-promoted a live endpoint: %v -> %v", before, got)
	}
}

// A staged body whose fields are the right names but the wrong types
// is refused: the probe decodes, the typed decode does not, and that
// is still a bad request rather than a partial stage.
func TestStagedBodyWithTheWrongTypes(t *testing.T) {
	if _, _, err := decodePatch([]byte(`{"master_enable":"yes"}`)); err == nil {
		t.Fatal("a string where a boolean belongs must be refused")
	}
}
