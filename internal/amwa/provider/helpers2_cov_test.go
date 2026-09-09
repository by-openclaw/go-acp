package provider

import (
	"net"
	"strconv"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is05"
)

// The address an IS-05 transport parameter or an sr-ctrl href names
// must be one a peer can correlate: a routable literal outranks
// loopback, which outranks a hostname — the endpoint list carries
// every name the Node answers to, so first-wins would be a coin toss.
func TestFirstNodeIPPrefersARoutableLiteral(t *testing.T) {
	mk := func(hosts ...string) *NodeConfig {
		eps := make([]is04.NodeEndpoint, 0, len(hosts))
		for _, h := range hosts {
			eps = append(eps, is04.NodeEndpoint{Host: h, Port: 8080, Protocol: "http"})
		}
		return &NodeConfig{Node: is04.Node{API: is04.NodeAPI{Endpoints: eps}}}
	}

	for name, tc := range map[string]struct {
		hosts []string
		want  string
	}{
		"a routable literal beats a hostname listed first": {
			[]string{"node.local", "10.6.239.113"}, "10.6.239.113",
		},
		"a routable literal beats loopback": {
			[]string{"127.0.0.1", "10.6.239.113"}, "10.6.239.113",
		},
		"loopback beats a hostname when it is the only literal": {
			[]string{"node.local", "127.0.0.1"}, "127.0.0.1",
		},
		"a hostname is used when there is no literal at all": {
			[]string{"", "node.local"}, "node.local",
		},
		"no endpoints names nothing": {nil, ""},
		"an endpoint with no host names nothing": {
			[]string{""}, "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := firstNodeIP(mk(tc.hosts...)); got != tc.want {
				t.Errorf("firstNodeIP(%v) = %q, want %q", tc.hosts, got, tc.want)
			}
		})
	}
}

// The SDP the IS-04 side serves comes from the Connection API, so both
// faces publish one generator's output. With no Connection API — or
// no such sender — there is nothing to serve.
func TestSenderSDPWithoutAConnectionAPI(t *testing.T) {
	s := &IS04NodeServer{}
	if got := s.senderSDP("any"); got != "" {
		t.Errorf("senderSDP with no Connection API = %q, want empty", got)
	}
	if got := s.ConnectionVersions(); got != nil {
		t.Errorf("ConnectionVersions with no Connection API = %v, want nil", got)
	}
}

// An SDP field the sender left blank falls back rather than emitting
// an empty line, and a value carrying line breaks is flattened — an
// SDP line is one line by definition.
func TestSDPTextAndParamHelpers(t *testing.T) {
	if got := sdpText("  ", "fallback"); got != "fallback" {
		t.Errorf("sdpText(blank) = %q", got)
	}
	if got := sdpText("a\r\nb", "fallback"); got != "a  b" {
		t.Errorf("sdpText with line breaks = %q", got)
	}

	params := is05.TransportParams{
		"destination_ip":   "239.0.0.1",
		"auto_ip":          "auto",
		"empty":            "",
		"not_a_string":     42,
		"destination_port": 5004,
		"port_i64":         int64(5006),
		"port_f64":         float64(5008),
		"port_string":      "5010",
	}
	for name, tc := range map[string]struct {
		key, fallback, want string
	}{
		"a value the sender set":          {"destination_ip", "fb", "239.0.0.1"},
		"the auto keyword is not a value": {"auto_ip", "fb", "fb"},
		"an empty value is not a value":   {"empty", "fb", "fb"},
		"a value of the wrong type":       {"not_a_string", "fb", "fb"},
		"a key the sender did not set":    {"absent", "fb", "fb"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := paramString(params, tc.key, tc.fallback); got != tc.want {
				t.Errorf("paramString(%s) = %q, want %q", tc.key, got, tc.want)
			}
		})
	}

	// Ports arrive as whichever numeric width the decoder produced.
	for key, want := range map[string]int{
		"destination_port": 5004,
		"port_i64":         5006,
		"port_f64":         5008,
		"port_string":      -1, // not numeric: the fallback
		"absent":           -1,
	} {
		if got := paramInt(params, key, -1); got != want {
			t.Errorf("paramInt(%s) = %d, want %d", key, got, want)
		}
	}
}

// A transport parameter that must be a whole number is one: a
// fractional JSON number is not an int, however it was written.
func TestToInt(t *testing.T) {
	for name, tc := range map[string]struct {
		in   any
		want int
		ok   bool
	}{
		"a JSON number":            {float64(5004), 5004, true},
		"a fractional JSON number": {float64(5004.5), 0, false},
		"an int":                   {int(5004), 5004, true},
		"an int64":                 {int64(5004), 5004, true},
		"a string":                 {"5004", 0, false},
		"nothing at all":           {nil, 0, false},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := toInt(tc.in)
			if got != tc.want || ok != tc.ok {
				t.Errorf("toInt(%v) = %d, %v; want %d, %v", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TAI times are two integers with a colon between them; anything else
// is not a time, and a nanosecond field outside its range is not one
// either.
func TestSplitTAITime(t *testing.T) {
	for in, want := range map[string]struct {
		sec, nsec int64
		ok        bool
	}{
		"1600000000:500":        {1600000000, 500, true},
		"0:0":                   {0, 0, true},
		"-5:0":                  {-5, 0, true}, // a relative activation may look back
		"1600000000":            {0, 0, false},
		"noon:0":                {0, 0, false},
		"1600000000:noon":       {0, 0, false},
		"1600000000:-1":         {0, 0, false},
		"1600000000:1000000000": {0, 0, false},
		"":                      {0, 0, false},
	} {
		sec, nsec, ok := splitTAITime(in)
		if sec != want.sec || nsec != want.nsec || ok != want.ok {
			t.Errorf("splitTAITime(%q) = %d, %d, %v; want %d, %d, %v",
				in, sec, nsec, ok, want.sec, want.nsec, want.ok)
		}
	}
}

// A scheduled activation names when the CONTROLLER wants the switch;
// relative counts forward from now, absolute is a TAI instant, and
// neither can be scheduled without a time.
func TestScheduledTimeLocked(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s := &connectionStore{now: func() time.Time { return now }}

	rel := "10:0"
	got, err := s.scheduledTimeLocked(is05.Activation{
		Mode: is05.ActivationModeScheduledRelative, RequestedTime: &rel,
	})
	if err != nil {
		t.Fatalf("a relative activation = %v", err)
	}
	if !got.Equal(now.Add(10 * time.Second)) {
		t.Errorf("relative = %v, want ten seconds from now", got)
	}

	abs := "1600000000:0"
	got, err = s.scheduledTimeLocked(is05.Activation{
		Mode: is05.ActivationModeScheduledAbsolute, RequestedTime: &abs,
	})
	if err != nil {
		t.Fatalf("an absolute activation = %v", err)
	}
	if got.IsZero() {
		t.Error("an absolute activation must resolve to an instant")
	}

	empty := ""
	for name, a := range map[string]is05.Activation{
		"no requested_time": {Mode: is05.ActivationModeScheduledRelative},
		"an empty requested_time": {
			Mode: is05.ActivationModeScheduledRelative, RequestedTime: &empty,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := s.scheduledTimeLocked(a); err == nil {
				t.Error("was accepted")
			}
		})
	}

	bad := "noon"
	if _, err := s.scheduledTimeLocked(is05.Activation{
		Mode: is05.ActivationModeScheduledAbsolute, RequestedTime: &bad,
	}); err == nil {
		t.Error("a requested_time that is not TAI must be refused")
	}
}

// A control href names the address the Node answers on: an
// --advertise-host that is an IP literal is authoritative, because a
// stale endpoint riding in from a bundle file would otherwise point
// every href at a dead address.
func TestControlHost(t *testing.T) {
	prev := osHostname
	osHostname = func() (string, error) { return "node-host", nil }
	t.Cleanup(func() { osHostname = prev })

	for name, tc := range map[string]struct {
		cfg  IS04NodeConfig
		want string
	}{
		"an advertise host with a port": {
			IS04NodeConfig{AdvertiseHost: "10.6.239.113:8080"}, "10.6.239.113:8080",
		},
		"an advertise host that is a bare literal": {
			IS04NodeConfig{AdvertiseHost: "10.6.239.113", Bind: ":0"}, "10.6.239.113",
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := &IS04NodeServer{cfg: tc.cfg}
			if got := s.controlHost(); got != tc.want {
				t.Errorf("controlHost = %q, want %q", got, tc.want)
			}
		})
	}

	// With no advertise host at all the Node still names something a
	// controller can dial.
	s := &IS04NodeServer{cfg: IS04NodeConfig{Bind: "127.0.0.1:8080"}}
	if got := s.controlHost(); got == "" {
		t.Error("controlHost must always name a host")
	}
}

// A device's control list is keyed by type: re-advertising one
// replaces it rather than growing a second entry a controller would
// have to choose between.
func TestUpsertControl(t *testing.T) {
	controls := []is04.DeviceControl{
		{Type: "urn:x-nmos:control:sr-ctrl/v1.1", Href: "http://old/"},
		{Type: "urn:x-nmos:control:cm-ctrl/v1.0", Href: "http://cm/"},
	}
	upsertControl(&controls, is04.DeviceControl{
		Type: "urn:x-nmos:control:sr-ctrl/v1.1", Href: "http://new/",
	})
	if len(controls) != 2 {
		t.Fatalf("controls = %+v, want the existing entry replaced", controls)
	}
	if controls[0].Href != "http://new/" {
		t.Errorf("href = %q, want the new one", controls[0].Href)
	}

	upsertControl(&controls, is04.DeviceControl{
		Type: "urn:x-nmos:control:sr-ctrl/v1.0", Href: "http://v10/",
	})
	if len(controls) != 3 {
		t.Errorf("a new control type must be appended: %+v", controls)
	}
}

// splitHostPort's port half is what a control href carries; a bind
// with no port names none.
func TestControlHostPortComposition(t *testing.T) {
	host, port := splitHostPort("10.6.239.113", "127.0.0.1:8080")
	if host != "10.6.239.113" || port != 8080 {
		t.Errorf("= %q, %d", host, port)
	}
	if got := net.JoinHostPort(host, strconv.Itoa(port)); got != "10.6.239.113:8080" {
		t.Errorf("joined = %q", got)
	}
}
