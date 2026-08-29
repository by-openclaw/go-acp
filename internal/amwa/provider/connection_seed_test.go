package provider

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is05"
)

func seedTestBundle() *NodeConfig {
	flowID := "2c47bf5e-1b2c-4abc-9def-00000000f001"
	return &NodeConfig{
		Senders: []is04.Sender{{
			ResourceCore: is04.ResourceCore{ID: "2c47bf5e-1b2c-4abc-9def-00000000a001"},
			FlowID:       &flowID,
			Transport:    "urn:x-nmos:transport:rtp.mcast",
		}},
		Receivers: []is04.Receiver{{
			ResourceCore: is04.ResourceCore{ID: "2c47bf5e-1b2c-4abc-9def-00000000b001"},
			Transport:    "urn:x-nmos:transport:rtp",
		}},
	}
}

// A seeded, enabled two-leg sender must boot ACTIVE: both legs
// present, the seeded groups verbatim, every "auto" resolved, an SDP
// generated — and the constraints envelope grown to match the legs,
// because the tool cross-checks those counts.
func TestConnectionSeedBootsSenderActive(t *testing.T) {
	cfg := seedTestBundle()
	cfg.Connection = &ConnectionSeed{
		Senders: map[string]*EndpointSeed{
			cfg.Senders[0].ID: {
				MasterEnable: true,
				TransportParams: []is05.TransportParams{
					{"destination_ip": "239.20.1.1", "destination_port": 5004, "rtp_enabled": true},
					{"destination_ip": "239.22.1.1", "destination_port": 5004, "rtp_enabled": true},
				},
			},
		},
	}
	if err := validateConnectionSeed(cfg); err != nil {
		t.Fatalf("seed should validate: %v", err)
	}

	st := newConnectionStore()
	st.seedFromBundle(cfg)
	st.setNodeIP("10.0.0.7")
	var promoted []string
	st.onPromote = func(kind, id string, active is05.StagedSender) string {
		promoted = append(promoted, kind+"/"+id)
		return "v=0 (test sdp)"
	}
	st.reresolveActive()
	st.promoteBootEnabled()

	e, err := st.get("senders", cfg.Senders[0].ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !e.active.MasterEnable {
		t.Error("boot-enabled sender should be ACTIVE master_enable=true")
	}
	if len(e.active.TransportParams) != 2 || len(e.constraints) != 2 {
		t.Fatalf("want 2 legs + 2 constraint sets, got %d legs %d constraints",
			len(e.active.TransportParams), len(e.constraints))
	}
	for li, want := range []string{"239.20.1.1", "239.22.1.1"} {
		if got := e.active.TransportParams[li]["destination_ip"]; got != want {
			t.Errorf("leg %d destination_ip: got %v want %v", li, got, want)
		}
	}
	for li, p := range e.active.TransportParams {
		for k, v := range p {
			if v == "auto" {
				t.Errorf("ACTIVE leg %d %s is still \"auto\" after boot promotion", li, k)
			}
		}
	}
	if e.active.Activation.ActivationTime == nil {
		t.Error("boot promotion must stamp activation_time")
	}
	if e.transportFile == "" {
		t.Error("boot-promoted sender should hold the SDP onPromote returned")
	}
	if len(promoted) != 1 || promoted[0] != "senders/"+cfg.Senders[0].ID {
		t.Errorf("promotions: %v", promoted)
	}
}

// A sender the seed leaves disabled (or omits) keeps the
// factory-fresh contract: master_enable=false, nothing promoted.
func TestConnectionSeedDisabledStaysCold(t *testing.T) {
	cfg := seedTestBundle()
	cfg.Connection = &ConnectionSeed{
		Receivers: map[string]*EndpointSeed{
			cfg.Receivers[0].ID: {
				MasterEnable:    false,
				TransportParams: []is05.TransportParams{{"multicast_ip": "239.20.1.1"}},
			},
		},
	}
	st := newConnectionStore()
	st.seedFromBundle(cfg)
	st.setNodeIP("10.0.0.7")
	st.reresolveActive()
	st.promoteBootEnabled()

	r, _ := st.get("receivers", cfg.Receivers[0].ID)
	if r.active.MasterEnable {
		t.Error("disabled seed must not enable the receiver")
	}
	if r.active.Activation.ActivationTime != nil {
		t.Error("disabled seed must not promote")
	}
	// The seeded value still shows in staged — configured, not active.
	if got := r.staged.TransportParams[0]["multicast_ip"]; got != "239.20.1.1" {
		t.Errorf("staged multicast_ip: got %v", got)
	}
	s, _ := st.get("senders", cfg.Senders[0].ID)
	if s.active.MasterEnable || len(s.staged.TransportParams) != 1 {
		t.Error("unseeded sender must stay factory-fresh, one leg, disabled")
	}
}

// Seeds naming unknown endpoints, unknown parameters, or illegal leg
// counts are configuration typos — rejected at load, not discovered
// four suites later as a constraints mismatch.
func TestConnectionSeedValidation(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*NodeConfig)
		want string
	}{
		{"unknown sender id", func(c *NodeConfig) {
			c.Connection = &ConnectionSeed{Senders: map[string]*EndpointSeed{"2c47bf5e-1b2c-4abc-9def-00000000dead": {}}}
		}, "no such sender"},
		{"unknown param key", func(c *NodeConfig) {
			c.Connection = &ConnectionSeed{Senders: map[string]*EndpointSeed{c.Senders[0].ID: {
				TransportParams: []is05.TransportParams{{"multicast_ip": "239.1.1.1"}}, // receiver key on a sender
			}}}
		}, "not a parameter"},
		{"three legs", func(c *NodeConfig) {
			c.Connection = &ConnectionSeed{Senders: map[string]*EndpointSeed{c.Senders[0].ID: {
				TransportParams: []is05.TransportParams{{}, {}, {}},
			}}}
		}, "at most 2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := seedTestBundle()
			tc.mut(cfg)
			err := validateConnectionSeed(cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}
