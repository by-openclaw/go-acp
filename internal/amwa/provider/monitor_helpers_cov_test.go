package provider

import (
	"testing"
	"time"

	"dhs/internal/amwa/codec/is05"
	"dhs/internal/amwa/codec/ms05"
	"dhs/internal/amwa/session/mqtt"
)

// A monitor's status value arrives as whichever numeric shape put it
// there — seeded as an int, decoded from JSON as a float, or written
// by a typed setter — and every one of them reads back as the same
// status.
func TestAsInt(t *testing.T) {
	for name, tc := range map[string]struct {
		in   any
		want int
	}{
		"an int":             {int(3), 3},
		"an int64":           {int64(3), 3},
		"a uint32":           {uint32(3), 3},
		"a uint64":           {uint64(3), 3},
		"a JSON number":      {float64(3), 3},
		"a string":           {"3", 0},
		"nothing at all":     {nil, 0},
		"a value of no kind": {struct{}{}, 0},
	} {
		t.Run(name, func(t *testing.T) {
			if got := asInt(tc.in); got != tc.want {
				t.Errorf("asInt(%v) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// The reporting delay is the device's own statusReportingDelay when it
// declares a usable one, and the BCP-008 default otherwise — a zero or
// missing value must not collapse the delay to nothing.
func TestMonitorDelayLocked(t *testing.T) {
	withProp := func(v any) *configObject {
		id := ms05.NcPropertyId{Level: 3, Index: 3}
		return &configObject{props: []*configProperty{{
			value: v, desc: ms05.NcPropertyDescriptor{ID: id},
		}}}
	}

	if got := monitorDelayLocked(withProp(10)); got != 10*time.Second {
		t.Errorf("a declared delay = %v, want 10s", got)
	}
	if got := monitorDelayLocked(withProp(float64(5))); got != 5*time.Second {
		t.Errorf("a JSON-decoded delay = %v, want 5s", got)
	}
	if got := monitorDelayLocked(withProp(0)); got != 3*time.Second {
		t.Errorf("a zero delay = %v, want the BCP-008 default", got)
	}
	if got := monitorDelayLocked(&configObject{}); got != 3*time.Second {
		t.Errorf("no such property = %v, want the BCP-008 default", got)
	}
}

// A monitor's packet counters are read by oid; an oid the model does
// not carry, and a monitor with no health record, both report nothing
// rather than a partial list.
func TestMonitorPacketCountersLookup(t *testing.T) {
	s := configFixture(t)
	if got := s.MonitorPacketCounters(ms05.NcOid(9999), "lost"); got != nil {
		t.Errorf("an oid nobody has = %v, want nothing", got)
	}
	// The root block is a real object with no monitor health.
	if got := s.MonitorPacketCounters(ms05.NcOid(1), "lost"); got != nil {
		t.Errorf("an object that is not a monitor = %v, want nothing", got)
	}
}

// An IS-07 MQTT sender names its broker as host:port; a bare host
// takes the MQTT default port, and a malformed value is passed through
// with that default rather than dropped.
func TestSplitBroker(t *testing.T) {
	for addr, want := range map[string]struct {
		host string
		port int
	}{
		"broker.local:1884": {"broker.local", 1884},
		"broker.local":      {"broker.local", 1883},
		"broker.local:":     {"broker.local", 1883},
		"broker.local:0":    {"broker.local", 1883},
		"broker.local:port": {"broker.local", 1883},
		"":                  {"", 0},
	} {
		host, port := splitBroker(addr)
		if host != want.host || port != want.port {
			t.Errorf("splitBroker(%q) = %q, %d; want %q, %d", addr, host, port, want.host, want.port)
		}
	}
}

// The MQTT transport parameters are read defensively: a value of the
// wrong type, or one the sender never set, reads as absent rather than
// being coerced into a broker address.
func TestMQTTParamHelpers(t *testing.T) {
	params := is05.TransportParams{
		"destination_host": "broker.local",
		"not_a_string":     42,
		"destination_port": float64(1884),
		"port_int":         1885,
		"port_string":      "1886",
	}
	if got := mqttParamString(params, "destination_host"); got != "broker.local" {
		t.Errorf("mqttParamString = %q", got)
	}
	for _, key := range []string{"not_a_string", "absent"} {
		if got := mqttParamString(params, key); got != "" {
			t.Errorf("mqttParamString(%s) = %q, want empty", key, got)
		}
	}

	for key, want := range map[string]string{
		"destination_port": "1884",
		"port_int":         "1885",
		"port_string":      "",
		"absent":           "",
	} {
		if got := paramPort(params, key); got != want {
			t.Errorf("paramPort(%s) = %q, want %q", key, got, want)
		}
	}
}

// A broker address that names nothing gets no session — the bridge
// must not open a client to ":".
func TestMQTTClientForRefusesAnEmptyBroker(t *testing.T) {
	b := &mqttEventBridge{clients: map[string]*mqtt.Client{}}
	for _, addr := range []string{"", ":"} {
		if c := b.clientFor(addr); c != nil {
			t.Errorf("clientFor(%q) opened a session", addr)
		}
	}
}

// A sender the bundle does not carry has no SDP to render, and a
// Connection API with no bundle at all has none for anyone.
func TestSenderByID(t *testing.T) {
	s := &IS05ConnectionServer{}
	if got := s.senderByID("any"); got != nil {
		t.Error("a Connection API with no bundle names no sender")
	}

	// The audio bundle carries one MQTT-less RTP sender.
	bundle := audioBundle()
	if len(bundle.Senders) == 0 {
		t.Fatal("the audio bundle must carry a sender")
	}
	s = &IS05ConnectionServer{bundle: bundle}
	if got := s.senderByID(bundle.Senders[0].ID); got == nil {
		t.Error("a sender the bundle carries must be found")
	}
	if got := s.senderByID("11111111-1111-4111-8111-999999999999"); got != nil {
		t.Error("a sender the bundle does not carry must not be")
	}
}
