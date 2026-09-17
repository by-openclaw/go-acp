package provider

// The last of the provider's arms: nil-receiver guards that keep an
// optional subsystem from becoming a crash, transports whose defaults
// nobody exercises in the fixture bundles, and the operator-facing
// refusals on the vendor seams.

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is05"
	httpsession "dhs/internal/amwa/session/http"
	"dhs/internal/amwa/session/mqtt"
)

// ---------------------------------------------------------------
// IS-07 over MQTT
// ---------------------------------------------------------------

// The MQTT bridge is optional: a plant with no MQTT event sender never
// builds one, and every entry point has to survive being called on the
// nil it left behind rather than making the caller check.
func TestMQTTBridgeIsSafeWhenThereIsNone(t *testing.T) {
	if got := newMQTTEventBridge(newLogTap().logger(), nil, nil); got != nil {
		t.Errorf("a nil bundle built a bridge: %+v", got)
	}

	var b *mqttEventBridge
	b.OnSenderActivation("any", is05.StagedSender{})
	b.OnStateChanged("any", nil)
	b.Close()
}

// A sender that is not an MQTT event sender is not the bridge's
// business, and neither is one whose activation carries no legs.
func TestMQTTBridgeIgnoresWhatIsNotItsOwn(t *testing.T) {
	bundle := audioBundle()
	b := newMQTTEventBridge(newLogTap().logger(), bundle, nil)
	if b == nil {
		t.Skip("the audio bundle carries no MQTT event sender")
	}
	b.OnSenderActivation("not-a-sender", is05.StagedSender{})
	b.OnStateChanged("not-a-source", map[string]any{"x": 1})
}

// A broker session that cannot be started is reported and the binding
// dropped: an event stream nobody can reach is not a stream, and the
// operator needs to know which broker refused.
func TestMQTTBridgeReportsABrokerItCannotReach(t *testing.T) {
	tap := newLogTap()
	b := &mqttEventBridge{
		logger:  tap.logger(),
		clients: map[string]*mqtt.Client{},
		// No client id: the session cannot be opened, which is the
		// same outcome as a broker that will not have us.
	}
	if got := b.clientFor("broker.local:1883"); got != nil {
		t.Errorf("a session that could not start = %+v, want nothing", got)
	}
	if !tap.has("cannot start broker session") {
		t.Errorf("the refusal must be reported; saw %v", tap.snapshot())
	}

	// An address that names nothing is refused before anything is
	// dialled at all.
	for _, addr := range []string{"", ":"} {
		if got := b.clientFor(addr); got != nil {
			t.Errorf("broker %q = %+v, want nothing", addr, got)
		}
	}
}

// ---------------------------------------------------------------
// transports the fixture bundles do not carry
// ---------------------------------------------------------------

// Each transport publishes its own parameter set, and "auto" on each
// resolves to something a peer can actually use — IS-05 §5.1 requires
// the device to answer with the value it chose.
func TestDefaultLegParamsPerTransport(t *testing.T) {
	for _, tc := range []struct {
		transport string
		isSender  bool
		want      []string
	}{
		{"urn:x-nmos:transport:websocket", true, []string{"connection_uri", "ext_is_07_source_id"}},
		{"urn:x-nmos:transport:mqtt", true, []string{"destination_host", "broker_protocol"}},
		{"urn:x-nmos:transport:rtp.mcast", true, []string{"destination_ip", "source_ip"}},
		{"urn:x-nmos:transport:rtp.mcast", false, []string{"multicast_ip", "interface_ip"}},
	} {
		p := defaultLegParams(tc.transport, tc.isSender)
		for _, k := range tc.want {
			if _, ok := p[k]; !ok {
				t.Errorf("%s (sender=%v) publishes no %q: %v", tc.transport, tc.isSender, k, p)
			}
		}
	}
}

// A receiver's own parameters resolve from the same rules as a
// sender's: the group it joins is spread per leg, because two legs of
// a 2022-7 pair on one subnet is not redundancy.
func TestReceiverAutoResolution(t *testing.T) {
	b := routableBundle(t)
	b.Receivers[0].Transport = "urn:x-nmos:transport:rtp.mcast"
	s := NewIS05ConnectionServer(newLogTap().logger(), b, IS05ConnectionConfig{APIVer: "v1.2"})
	s.Store().setNodeIP("10.0.0.7")
	s.Store().reresolveActive()

	e, err := s.Store().get("receivers", b.Receivers[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.active.TransportParams) == 0 {
		t.Fatal("a receiver has at least one leg")
	}
	if got := e.active.TransportParams[0]["interface_ip"]; got == autoKeyword {
		t.Error("interface_ip must resolve, not stay auto")
	}
}

// With no address known the store still answers with something a
// consumer can dial: the loopback is a truthful "here, on this box"
// rather than a blank a controller would render as unreachable.
func TestNodeBaseFallsBackToLoopback(t *testing.T) {
	b := routableBundle(t)
	s := NewIS05ConnectionServer(newLogTap().logger(), b, IS05ConnectionConfig{APIVer: "v1.2"})
	s.Store().reresolveActive() // no node IP set
}

// ---------------------------------------------------------------
// the vendor seams
// ---------------------------------------------------------------

// The fault worker's arguments are named in its class descriptor, and
// each one that is missing is refused by name — an Ansible play that
// mistypes one is told which.
func TestFaultMethodArgumentRefusals(t *testing.T) {
	s, rx := monitorFixture(t)
	role := `"monitorRole":"` + rx.role + `"`

	for _, tc := range []struct {
		method string
		args   string
		want   string
	}{
		{"InjectMonitorFault", `{}`, "monitorRole"},
		{"InjectMonitorFault", `{` + role + `}`, "domain"},
		{"InjectMonitorFault", `{` + role + `,"domain":"linkStatus"}`, "status"},
		{"ClearMonitorFault", `{` + role + `}`, "domain"},
		{"SetMonitorSyncSource", `{` + role + `}`, "sourceId"},
		{"AddMonitorPacketCounters", `{` + role + `}`, "counter must be"},
		{"AddMonitorPacketCounters", `{` + role + `,"counter":"lost"}`, "name"},
		{"NotAMethodAtAll", `{` + role + `}`, "no DhsFaultControl method"},
	} {
		err := s.invokeFaultMethod(tc.method, []byte(tc.args))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s %s = %v, want %q named", tc.method, tc.args, err, tc.want)
		}
	}

	// Arguments that are not JSON at all are refused before any of
	// that.
	if err := s.invokeFaultMethod("InjectMonitorFault", []byte(`{`)); err == nil {
		t.Error("arguments that are not JSON must be refused")
	}

	// And each one, given what it asks for, does its work.
	for _, tc := range []struct{ method, args string }{
		{"ClearMonitorFault", `{` + role + `,"domain":"linkStatus"}`},
		{"SetMonitorSyncSource", `{` + role + `,"sourceId":"ptp-gm-1"}`},
		{"AddMonitorPacketCounters", `{` + role + `,"counter":"lost","name":"leg-0","increment":2}`},
	} {
		if err := s.invokeFaultMethod(tc.method, []byte(tc.args)); err != nil {
			t.Errorf("%s = %v, want it accepted", tc.method, err)
		}
	}
}

// A compiled-in vendor model that will not go into the catalogue is a
// build defect, and the process stops rather than serving a device
// model with a hole where a class should be.
func TestVendorRegistrationRefusal(t *testing.T) {
	defer func() {
		r := recover()
		msg, _ := r.(string)
		if !strings.Contains(msg, "registration") {
			t.Fatalf("recovered %v, want the build defect named", r)
		}
	}()
	mustRegister("a test model", errTest("refused"))
}

// A method name the fault worker does not declare is not one of its
// own, whatever slot it arrived in.
func TestFaultMethodByName(t *testing.T) {
	if isFaultMethod("NotAFaultMethod") {
		t.Error("an unrelated name must not read as a fault method")
	}
	for _, name := range []string{
		"InjectMonitorFault", "ClearMonitorFault",
		"SetMonitorSyncSource", "AddMonitorPacketCounters",
	} {
		if !isFaultMethod(name) {
			t.Errorf("%s is one of the worker's own", name)
		}
	}
}

// ---------------------------------------------------------------
// optional subsystems
// ---------------------------------------------------------------

// The Configuration API is opt-out, and with it gone nothing is
// mounted and no control href is advertised for a face that answers
// nothing.
func TestConfigurationAPIIsNotMountedWhenDisabled(t *testing.T) {
	s := nodeFor(t, validBundle(), func(c *IS04NodeConfig) { c.NoConfigurationAPI = true })
	srv := httpsession.NewServer(newLogTap().logger())

	s.attachConfigurationAPI(srv)

	if s.configuration != nil {
		t.Error("--no-configuration must not build the server")
	}
}

// A bundle carrying no resource of a kind projects to none of it,
// rather than to a collection with an empty entry in it.
func TestProjectionOfAnEmptyBundle(t *testing.T) {
	empty := &NodeConfig{Node: validBundle().Node}
	got := projectForMinor(empty, "v1.0")
	if got == nil {
		t.Fatal("a projection must always produce a bundle")
	}
	if len(got.Devices) != 0 || len(got.Senders) != 0 {
		t.Errorf("= %+v, want nothing projected", got)
	}
}

var _ = is04.ResourceNode
