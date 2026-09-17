package provider

import (
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is05"
)

func testStore(t *testing.T) (*connectionStore, string, string) {
	t.Helper()
	cfg := &NodeConfig{
		Senders: []is04.Sender{{
			ResourceCore: is04.ResourceCore{ID: "11111111-1111-4111-8111-111111111111"},
			Transport:    is04.TransportRTP,
		}},
		Receivers: []is04.Receiver{{
			ResourceCore: is04.ResourceCore{ID: "22222222-2222-4222-8222-222222222222"},
			Transport:    is04.TransportRTP,
		}},
	}
	s := newConnectionStore()
	s.seedFromBundle(cfg)
	return s, cfg.Senders[0].ID, cfg.Receivers[0].ID
}

// TestPatchWithoutActivationStagesOnly is the distinction the whole
// spec turns on. A device that promotes on every PATCH passes casual
// testing and then breaks the first time a controller stages a route
// it means to take later — which is how every multi-device switch is
// built.
func TestPatchWithoutActivationStagesOnly(t *testing.T) {
	s, sid, _ := testStore(t)

	patch := is05.StagedSender{
		MasterEnableField: is05.MasterEnableField{MasterEnable: true},
		TransportParams: []is05.TransportParams{
			{"destination_ip": "239.1.1.1", "destination_port": 5004},
		},
	}
	staged, code, err := s.applyPatch("senders", sid, patch,
		patchFields{MasterEnable: true, TransportParams: true})
	if err != nil || code != 200 {
		t.Fatalf("stage: code=%d err=%v", code, err)
	}
	if !staged.MasterEnable {
		t.Error("staged should carry the master_enable we sent")
	}

	e, _ := s.get("senders", sid)
	if e.active.MasterEnable {
		t.Error("ACTIVE must not change on a PATCH with no activation")
	}
	if got := e.active.TransportParams[0]["destination_ip"]; got == "239.1.1.1" {
		t.Error("ACTIVE must not see staged transport params before activation")
	}
}

// TestActivateImmediatePromotes: mode activate_immediate moves staged
// to active in the same request, and clears the staged activation
// block so a controller reading staged afterwards sees a clean slate
// rather than the request it just sent.
func TestActivateImmediatePromotes(t *testing.T) {
	s, sid, _ := testStore(t)
	s.now = func() time.Time { return time.Unix(1700000000, 0) }

	patch := is05.StagedSender{
		MasterEnableField: is05.MasterEnableField{MasterEnable: true},
		TransportParams:   []is05.TransportParams{{"destination_ip": "239.1.1.1"}},
		Activation:        is05.Activation{Mode: is05.ActivationModeImmediate},
	}
	_, code, err := s.applyPatch("senders", sid, patch,
		patchFields{MasterEnable: true, TransportParams: true})
	if err != nil || code != 200 {
		t.Fatalf("activate: code=%d err=%v", code, err)
	}

	e, _ := s.get("senders", sid)
	if !e.active.MasterEnable {
		t.Error("ACTIVE should be enabled after an immediate activation")
	}
	if got := e.active.TransportParams[0]["destination_ip"]; got != "239.1.1.1" {
		t.Errorf("ACTIVE destination_ip = %v, want the staged value", got)
	}
	if e.staged.Activation.Mode != "" {
		t.Errorf("staged activation should be cleared, got %q", e.staged.Activation.Mode)
	}
	if e.active.Activation.ActivationTime == nil {
		t.Error("ACTIVE must record WHEN it was activated")
	}
}

// TestScheduledActivationIs202AndDeferred: a scheduled activation
// answers 202, not 200 — the response describes something that has
// not happened. Reporting 200 tells the controller the switch is done
// when it is still pending.
func TestScheduledActivationIs202AndDeferred(t *testing.T) {
	s, sid, _ := testStore(t)
	now := time.Unix(1700000000, 0)
	s.now = func() time.Time { return now }

	// The CONTROLLER sends requested_time; activation_time is the
	// server's answer. Sending activation_time here is the inversion
	// the codec rejects.
	at := "10:0" // 10 s from now, relative
	patch := is05.StagedSender{
		MasterEnableField: is05.MasterEnableField{MasterEnable: true},
		Activation: is05.Activation{
			Mode:          is05.ActivationModeScheduledRelative,
			RequestedTime: &at,
		},
	}
	_, code, err := s.applyPatch("senders", sid, patch, patchFields{MasterEnable: true})
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if code != 202 {
		t.Errorf("scheduled activation returned %d, want 202 — it has not happened yet", code)
	}

	e, _ := s.get("senders", sid)
	if e.active.MasterEnable {
		t.Error("ACTIVE must not change until the scheduled time")
	}

	// Before the deadline: still nothing.
	if n := s.runScheduled(); n != 0 {
		t.Errorf("promoted %d endpoint(s) before the scheduled time", n)
	}
	// After: it fires.
	now = now.Add(11 * time.Second)
	if n := s.runScheduled(); n != 1 {
		t.Errorf("promoted %d endpoint(s) after the scheduled time, want 1", n)
	}
	e, _ = s.get("senders", sid)
	if !e.active.MasterEnable {
		t.Error("ACTIVE should be enabled once the scheduled time passes")
	}
	// A fired schedule does not fire twice.
	if n := s.runScheduled(); n != 0 {
		t.Errorf("a completed activation fired again (%d)", n)
	}
}

// TestPatchIsAMergeNotAReplace: fields the controller did not send
// keep their staged value. A replace would make every PATCH a full
// re-specification of the endpoint, which is not what controllers
// send — and would silently clear parameters nobody mentioned.
func TestPatchIsAMergeNotAReplace(t *testing.T) {
	s, sid, _ := testStore(t)

	first := is05.StagedSender{
		TransportParams: []is05.TransportParams{
			{"destination_ip": "239.1.1.1", "destination_port": 5004},
		},
	}
	if _, _, err := s.applyPatch("senders", sid, first, patchFields{TransportParams: true}); err != nil {
		t.Fatalf("first patch: %v", err)
	}
	// Second PATCH touches only the port.
	second := is05.StagedSender{
		TransportParams: []is05.TransportParams{{"destination_port": 5006}},
	}
	if _, _, err := s.applyPatch("senders", sid, second, patchFields{TransportParams: true}); err != nil {
		t.Fatalf("second patch: %v", err)
	}

	e, _ := s.get("senders", sid)
	if got := e.staged.TransportParams[0]["destination_ip"]; got != "239.1.1.1" {
		t.Errorf("destination_ip = %v — a PATCH that did not mention it must not clear it", got)
	}
	if got := e.staged.TransportParams[0]["destination_port"]; got != 5006 {
		t.Errorf("destination_port = %v, want the newly patched 5006", got)
	}
}

// TestAbsentMasterEnableIsNotFalse: master_enable is a plain bool on
// the canonical struct, so only the presence set distinguishes "not
// sent" from "sent false". Getting this wrong silently disables an
// endpoint the controller never mentioned.
func TestAbsentMasterEnableIsNotFalse(t *testing.T) {
	s, sid, _ := testStore(t)

	on := is05.StagedSender{MasterEnableField: is05.MasterEnableField{MasterEnable: true}}
	if _, _, err := s.applyPatch("senders", sid, on, patchFields{MasterEnable: true}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	// A later PATCH that says nothing about master_enable.
	quiet := is05.StagedSender{TransportParams: []is05.TransportParams{{"destination_port": 5010}}}
	if _, _, err := s.applyPatch("senders", sid, quiet, patchFields{TransportParams: true}); err != nil {
		t.Fatalf("quiet patch: %v", err)
	}
	e, _ := s.get("senders", sid)
	if !e.staged.MasterEnable {
		t.Error("a PATCH that omitted master_enable disabled the endpoint")
	}
}

// TestLegCountIsFixedByTheEndpoint: a 2022-7 sender has two legs and a
// single-leg PATCH against it is a controller error, not a
// reconfiguration. Accepting it would silently drop the second leg —
// exactly the unprotected-stream fault the plant audit found.
func TestLegCountIsFixedByTheEndpoint(t *testing.T) {
	s, sid, _ := testStore(t)
	two := is05.StagedSender{
		TransportParams: []is05.TransportParams{
			{"destination_ip": "239.1.1.1"},
			{"destination_ip": "239.2.1.1"},
		},
	}
	_, code, err := s.applyPatch("senders", sid, two, patchFields{TransportParams: true})
	if err == nil {
		t.Error("a 2-leg PATCH against a 1-leg endpoint must be refused")
	}
	if code != 400 {
		t.Errorf("leg-count mismatch returned %d, want 400", code)
	}
}

// TestUnknownEndpointIs404: an id that is not in the Node API must not
// be addressable in the Connection API. IS-05 §4.1 requires the id
// sets to match.
func TestUnknownEndpointIs404(t *testing.T) {
	s, _, _ := testStore(t)
	_, code, err := s.applyPatch("senders", "99999999-9999-4999-8999-999999999999",
		is05.StagedSender{}, patchFields{})
	if err == nil || code != 404 {
		t.Errorf("unknown sender returned code=%d err=%v, want 404", code, err)
	}
}

// TestReceiverViewNamesSenderID: a receiver's staged body carries
// sender_id, a sender's carries receiver_id. Serving one shape for the
// other is a schema failure a controller rejects outright.
func TestReceiverViewNamesSenderID(t *testing.T) {
	s, _, rid := testStore(t)
	e, err := s.get("receivers", rid)
	if err != nil {
		t.Fatalf("get receiver: %v", err)
	}
	v := viewOf("receivers", e.staged)
	if _, ok := v.(is05.StagedReceiver); !ok {
		t.Fatalf("receiver view rendered as %T, want is05.StagedReceiver", v)
	}
	if _, ok := viewOf("senders", e.staged).(is05.StagedSender); !ok {
		t.Error("sender view should render as is05.StagedSender")
	}
}
