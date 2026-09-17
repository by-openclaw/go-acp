package provider

// What "auto" resolves to. IS-05 §5.1 requires the device to answer
// ACTIVE with the value it chose, so every one of these is an address
// or a port a peer will actually be pointed at.

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is05"
)

// resolved runs the auto-resolution over one leg and hands back what
// it chose.
func resolved(params is05.TransportParams, nodeIP string, isSender bool,
	index int, broker string) is05.TransportParams {
	p := is05.TransportParams{}
	for k, v := range params {
		p[k] = v
	}
	resolveAuto(p, nodeIP, isSender, index, broker)
	return p
}

// Every parameter with a defined resolution gets one, and the two legs
// of a 2022-7 pair land in different /24s: putting both on one subnet
// defeats the redundancy, which is the fault a plant audit found on 48
// senders.
func TestAutoResolutionPerParameter(t *testing.T) {
	auto := func(keys ...string) is05.TransportParams {
		p := is05.TransportParams{}
		for _, k := range keys {
			p[k] = autoKeyword
		}
		return p
	}

	leg0 := resolved(auto("source_ip", "interface_ip", "destination_ip", "multicast_ip",
		"source_port", "destination_port", "destination_host", "broker_protocol",
		"broker_authorization", "connection_authorization", "connection_uri",
		"mxl_domain_id", "mxl_flow_id", "no_defined_resolution"), "10.0.0.7", true, 0, "")
	leg1 := resolved(auto("destination_ip", "multicast_ip", "source_port"), "10.0.0.7", true, 1, "")

	for k, want := range map[string]any{
		"source_ip":                "10.0.0.7",
		"interface_ip":             "10.0.0.7",
		"destination_ip":           "239.4.1.1",
		"multicast_ip":             "239.4.1.1",
		"broker_protocol":          "mqtt",
		"broker_authorization":     false,
		"connection_authorization": false,
		"destination_host":         "10.0.0.7",
	} {
		if got := leg0[k]; got != want {
			t.Errorf("%s = %v, want %v", k, got, want)
		}
	}
	if got, ok := leg0["connection_uri"].(string); !ok || !strings.HasPrefix(got, "ws://10.0.0.7/") {
		t.Errorf("connection_uri = %v, want the events socket", leg0["connection_uri"])
	}
	if got, ok := leg0["mxl_domain_id"].(string); !ok || !strings.HasSuffix(got, "d") {
		t.Errorf("mxl_domain_id = %v", leg0["mxl_domain_id"])
	}
	if got, ok := leg0["mxl_flow_id"].(string); !ok || !strings.HasSuffix(got, "f") {
		t.Errorf("mxl_flow_id = %v", leg0["mxl_flow_id"])
	}

	// A parameter with no defined resolution keeps its "auto": inventing
	// a value would be worse than reporting the device never resolved
	// one.
	if got := leg0["no_defined_resolution"]; got != autoKeyword {
		t.Errorf("an unresolvable parameter = %v, want it left alone", got)
	}

	// The second leg is a different group and a different port.
	if leg1["destination_ip"] == leg0["destination_ip"] {
		t.Error("both legs landed in the same group; that is not redundancy")
	}
	if leg1["source_port"] == leg0["source_port"] {
		t.Error("both legs took the same port")
	}
}

// An MQTT leg resolves its destination from the broker the Node was
// told about, rather than pointing a consumer at the Node itself.
func TestAutoResolutionForAnMQTTLeg(t *testing.T) {
	p := resolved(is05.TransportParams{
		"destination_host": autoKeyword,
		"destination_port": autoKeyword,
		"broker_protocol":  autoKeyword,
	}, "10.0.0.7", true, 0, "broker.local:1883")

	if got := p["destination_host"]; got != "broker.local" {
		t.Errorf("destination_host = %v, want the broker", got)
	}
	if got := p["destination_port"]; got != 1883 {
		t.Errorf("destination_port = %v, want the broker's port", got)
	}

	// With no broker configured the leg falls back to the Node, which
	// is at least an address rather than a blank.
	fallback := resolved(is05.TransportParams{
		"destination_host": autoKeyword,
		"destination_port": autoKeyword,
	}, "10.0.0.7", true, 0, "")
	if got := fallback["destination_host"]; got != "10.0.0.7" {
		t.Errorf("destination_host = %v, want the Node's address", got)
	}
}

// IS-07 §5: the WebSocket URI and the REST URL are the only way a
// consumer finds an event stream and its current value. The socket
// exists from the moment the Node serves, so "not yet known" is never
// the truthful answer — the store fills both in as soon as it knows
// where the Node answers.
func TestEventExtensionParametersAreFilledIn(t *testing.T) {
	b := audioBundle()
	if len(b.Senders) == 0 {
		t.Skip("the audio bundle carries no sender")
	}
	b.Senders[0].Transport = "urn:x-nmos:transport:websocket"

	s := NewIS05ConnectionServer(newLogTap().logger(), b, IS05ConnectionConfig{APIVer: "v1.2"})
	s.Store().setNodeIP("10.0.0.7")
	s.Store().setNodeBase("10.0.0.7:8080")
	s.Store().reresolveActive()

	e, err := s.Store().get("senders", b.Senders[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.staged.TransportParams) == 0 {
		t.Fatal("a websocket sender has a leg")
	}
	p := e.staged.TransportParams[0]
	if v, ok := p["connection_uri"]; ok && v == nil {
		t.Error("a websocket sender with a null connection_uri publishes a stream nothing can reach")
	}
	if v, ok := p["ext_is_07_rest_api_url"]; ok && v == "" {
		t.Error("the REST base must be filled in once the Node knows where it answers")
	}
}
