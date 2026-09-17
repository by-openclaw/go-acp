package provider

// IS-05 transport-parameter matrix — pins which parameters this Node
// supports on PATCH /staged, how they merge, what each may hold, and
// how they surface on ACTIVE and in the SDP after activation.
//
// Field context (2026-08-29, VLAN600 plant): mcast + port retune was
// proven live against Cerebrum; these tests pin the full parameter
// set so "is param X supported?" has a compilable answer. FEC/RTCP
// parameters are deliberately NOT published by this Node (see
// defaultLegParams) — a PATCH naming them must be refused at the
// door, not silently absorbed.

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is05"
)

// patchOne PATCHes a single-leg transport_params body carrying just
// the given keys, with immediate activation when activate is true.
func patchOne(t *testing.T, s *connectionStore, kind, id string, leg is05.TransportParams, activate bool) (is05.StagedSender, int, error) {
	t.Helper()
	p := is05.StagedSender{TransportParams: []is05.TransportParams{leg}}
	if activate {
		p.Activation = is05.Activation{Mode: is05.ActivationModeImmediate}
	}
	return s.applyPatch(kind, id, p, patchFields{TransportParams: true})
}

// TestSenderParamMatrixAccepted: every RTP sender parameter this Node
// publishes accepts a spec-legal value, merges without disturbing its
// neighbours, and lands on ACTIVE verbatim after activation.
func TestSenderParamMatrixAccepted(t *testing.T) {
	cases := []struct {
		key string
		val any
	}{
		{"destination_ip", "239.9.1.1"},
		{"destination_port", float64(5100)}, // JSON numbers arrive as float64
		{"source_port", float64(5200)},
		{"source_ip", "192.0.2.10"},
		{"rtp_enabled", true},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			s, sid, _ := testStore(t)
			// Baseline: stage a full known leg first.
			if _, code, err := patchOne(t, s, "senders", sid, is05.TransportParams{
				"destination_ip": "239.20.1.1", "destination_port": float64(5004),
				"rtp_enabled": true,
			}, false); err != nil || code != 200 {
				t.Fatalf("baseline stage: code=%d err=%v", code, err)
			}
			// The single-key PATCH under test.
			if _, code, err := patchOne(t, s, "senders", sid,
				is05.TransportParams{tc.key: tc.val}, true); err != nil || code != 200 {
				t.Fatalf("patch %s: code=%d err=%v", tc.key, code, err)
			}
			e, _ := s.get("senders", sid)
			if got := e.active.TransportParams[0][tc.key]; got != tc.val {
				t.Errorf("active[%s] = %v (%T), want %v", tc.key, got, got, tc.val)
			}
			// Merge isolation: the baseline destination_ip survives every
			// single-key PATCH except its own.
			if tc.key != "destination_ip" {
				if got := e.active.TransportParams[0]["destination_ip"]; got != "239.20.1.1" {
					t.Errorf("destination_ip disturbed by %s PATCH: %v", tc.key, got)
				}
			}
		})
	}
}

// TestReceiverParamMatrixAccepted mirrors the sender matrix for the
// receiver-side parameter set.
func TestReceiverParamMatrixAccepted(t *testing.T) {
	cases := []struct {
		key string
		val any
	}{
		{"interface_ip", "192.0.2.20"},
		{"multicast_ip", "239.9.1.1"},
		{"destination_port", float64(5100)},
		{"rtp_enabled", true},
		{"source_ip", nil}, // a receiver may say "unknown far end"
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			s, _, rid := testStore(t)
			if _, code, err := patchOne(t, s, "receivers", rid,
				is05.TransportParams{tc.key: tc.val}, true); err != nil || code != 200 {
				t.Fatalf("patch %s: code=%d err=%v", tc.key, code, err)
			}
			e, _ := s.get("receivers", rid)
			if got := e.active.TransportParams[0][tc.key]; got != tc.val {
				t.Errorf("active[%s] = %v, want %v", tc.key, got, tc.val)
			}
		})
	}
}

// TestParamRejections: every refusal IS-05 §5.1 requires answers 400
// at the door, naming the offence — never a 200 that quietly ignores
// the parameter.
func TestParamRejections(t *testing.T) {
	cases := []struct {
		name string
		kind string
		leg  is05.TransportParams
		want string // substring of the error
	}{
		{"unknown param fec_enabled", "senders", is05.TransportParams{"fec_enabled": true}, "not a parameter"},
		{"unknown param rtcp_enabled", "senders", is05.TransportParams{"rtcp_enabled": true}, "not a parameter"},
		{"port out of range", "senders", is05.TransportParams{"destination_port": float64(70000)}, "port number"},
		{"port as word", "senders", is05.TransportParams{"destination_port": "hello"}, "port number"},
		{"fractional port", "senders", is05.TransportParams{"source_port": 50.5}, "port number"},
		{"address not an IP", "senders", is05.TransportParams{"destination_ip": "not-an-ip"}, "IP address"},
		{"sender source_ip null", "senders", is05.TransportParams{"source_ip": nil}, "may not be null"},
		{"rtp_enabled non-bool", "senders", is05.TransportParams{"rtp_enabled": "yes"}, "boolean"},
		{"receiver bad multicast", "receivers", is05.TransportParams{"multicast_ip": "279.1.1.1"}, "IP address"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, sid, rid := testStore(t)
			id := sid
			if tc.kind == "receivers" {
				id = rid
			}
			_, code, err := patchOne(t, s, tc.kind, id, tc.leg, false)
			if code != 400 || err == nil {
				t.Fatalf("code=%d err=%v — want 400 with an error", code, err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err %q does not name the offence (%q)", err, tc.want)
			}
		})
	}
}

// TestLegCountIsFixed: the endpoint owns its leg count. A 2022-7
// two-leg PATCH against a single-leg endpoint is a controller error.
func TestLegCountIsFixed(t *testing.T) {
	s, sid, _ := testStore(t)
	p := is05.StagedSender{TransportParams: []is05.TransportParams{
		{"destination_port": float64(5004)},
		{"destination_port": float64(5004)},
	}}
	_, code, err := s.applyPatch("senders", sid, p, patchFields{TransportParams: true})
	if code != 400 || err == nil || !strings.Contains(err.Error(), "leg") {
		t.Fatalf("two-leg PATCH on one-leg endpoint: code=%d err=%v", code, err)
	}
}

// TestAutoResolvesOnActive: "auto" is a staged-only value. After
// activation, ports and destination get the concrete values the
// device chose (IS-05 test_11/test_12 semantics).
func TestAutoResolvesOnActive(t *testing.T) {
	s, sid, _ := testStore(t)
	s.setNodeIP("192.0.2.1")
	if _, code, err := patchOne(t, s, "senders", sid, is05.TransportParams{
		"destination_ip": "auto", "destination_port": "auto", "source_port": "auto",
	}, true); err != nil || code != 200 {
		t.Fatalf("activate with autos: code=%d err=%v", code, err)
	}
	e, _ := s.get("senders", sid)
	leg := e.active.TransportParams[0]
	for k, v := range leg {
		if s, ok := v.(string); ok && s == "auto" {
			t.Errorf("active[%s] still \"auto\" after activation", k)
		}
	}
	if leg["destination_ip"] != "239.4.1.1" {
		t.Errorf("auto destination_ip resolved to %v, want 239.4.1.1 (leg 0)", leg["destination_ip"])
	}
	if leg["destination_port"] != 5004 {
		t.Errorf("auto destination_port resolved to %v, want 5004", leg["destination_port"])
	}
}

// TestSDPReflectsRetunedParams: the transport file is generated from
// ACTIVE, so a retune (new mcast + port) must appear in the SDP a
// controller fetches next — the live receipt from the plant, pinned
// as a unit test.
func TestSDPReflectsRetunedParams(t *testing.T) {
	flowID := "33333333-3333-4333-8333-333333333333"
	cfg := &NodeConfig{
		Senders: []is04.Sender{{
			ResourceCore: is04.ResourceCore{ID: "11111111-1111-4111-8111-111111111111",
				Label: "retune-test", Version: "100:0"},
			Transport: is04.TransportRTPMcast,
			FlowID:    &flowID,
		}},
		Flows: []is04.Flow{{
			ResourceCore: is04.ResourceCore{ID: flowID},
			Format:       "urn:x-nmos:format:audio",
			BitDepth:     24,
		}},
	}
	srv := NewIS05ConnectionServer(slog.Default(), cfg, IS05ConnectionConfig{APIVer: "v1.1"})
	st := srv.Store()
	st.setNodeIP("192.0.2.1")
	st.now = func() time.Time { return time.Unix(1700000000, 0) }

	sid := cfg.Senders[0].ID
	if _, code, err := patchOne(t, st, "senders", sid, is05.TransportParams{
		"destination_ip": "239.30.1.1", "destination_port": float64(5010),
		"rtp_enabled": true, "source_ip": "192.0.2.1",
	}, true); err != nil || code != 200 {
		t.Fatalf("retune: code=%d err=%v", code, err)
	}
	e, _ := st.get("senders", sid)
	sdp := srv.sdpForSender(sid, e.active)
	for _, want := range []string{"m=audio 5010", "c=IN IP4 239.30.1.1/64", "L24/48000"} {
		if !strings.Contains(sdp, want) {
			t.Errorf("SDP missing %q after retune:\n%s", want, sdp)
		}
	}
}
