package consumer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is05"
)

func legsOf(t *testing.T, patch map[string]any) []map[string]any {
	t.Helper()
	raw, err := json.Marshal(patch["transport_params"])
	if err != nil {
		t.Fatalf("marshal transport_params: %v", err)
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("transport_params is not an array of objects: %v", err)
	}
	return out
}

// TestPatchIsAlwaysFullLength is the one that matters for ST 2022-7.
//
// IS-05 matches transport_params POSITIONALLY. Sending a one-element
// array to a two-leg sender does not mean "leave leg 1 alone" — it
// describes leg 0, and on a device where the caller meant leg 1 that
// silently re-addresses the wrong network.
func TestPatchIsAlwaysFullLength(t *testing.T) {
	patch, err := buildSenderPatch(2, is05.ActivationModeImmediate, SetSenderRequest{
		SenderID:       "s",
		DestinationIPs: []string{"", "239.101.40.51"},
	})
	if err != nil {
		t.Fatalf("buildSenderPatch: %v", err)
	}
	legs := legsOf(t, patch)
	if len(legs) != 2 {
		t.Fatalf("transport_params has %d entries, want 2 — IS-05 matches legs by position", len(legs))
	}
	if len(legs[0]) != 0 {
		t.Errorf("leg 0 was not asked to change; it must merge to a no-op, got %v", legs[0])
	}
	if legs[1]["destination_ip"] != "239.101.40.51" {
		t.Errorf("leg 1 destination_ip = %v", legs[1]["destination_ip"])
	}
}

func TestPatchBothLegs(t *testing.T) {
	patch, err := buildSenderPatch(2, is05.ActivationModeImmediate, SetSenderRequest{
		SenderID:         "s",
		DestinationIPs:   []string{"239.100.40.51", "239.101.40.51"},
		DestinationPorts: []int{12700, 12700},
	})
	if err != nil {
		t.Fatalf("buildSenderPatch: %v", err)
	}
	legs := legsOf(t, patch)
	for i, want := range []string{"239.100.40.51", "239.101.40.51"} {
		if legs[i]["destination_ip"] != want {
			t.Errorf("leg %d destination_ip = %v, want %s", i, legs[i]["destination_ip"], want)
		}
		if legs[i]["destination_port"] != float64(12700) {
			t.Errorf("leg %d destination_port = %v", i, legs[i]["destination_port"])
		}
	}
}

// TestPatchOmitsUnsetKeys: IS-05 PATCH is a MERGE. A key we did not set
// must not appear, or the device takes our zero value as an instruction.
func TestPatchOmitsUnsetKeys(t *testing.T) {
	patch, err := buildSenderPatch(1, is05.ActivationModeImmediate, SetSenderRequest{
		SenderID:       "s",
		DestinationIPs: []string{"239.1.1.1"},
	})
	if err != nil {
		t.Fatalf("buildSenderPatch: %v", err)
	}
	if _, present := patch["master_enable"]; present {
		t.Error("master_enable was not requested; sending it would overwrite the device's own")
	}
	leg := legsOf(t, patch)[0]
	if _, present := leg["destination_port"]; present {
		t.Error("destination_port was not requested; sending it would overwrite the device's own")
	}
}

func TestPatchMasterEnableIsExplicit(t *testing.T) {
	for _, want := range []bool{true, false} {
		v := want
		patch, err := buildSenderPatch(1, is05.ActivationModeImmediate, SetSenderRequest{
			SenderID: "s", DestinationIPs: []string{"239.1.1.1"}, MasterEnable: &v,
		})
		if err != nil {
			t.Fatalf("buildSenderPatch: %v", err)
		}
		if patch["master_enable"] != want {
			t.Errorf("master_enable = %v, want %v", patch["master_enable"], want)
		}
	}
}

func TestPatchLegCountMismatch(t *testing.T) {
	cases := []struct {
		name string
		req  SetSenderRequest
		want string
	}{
		{"one destination for two legs",
			SetSenderRequest{SenderID: "s", DestinationIPs: []string{"239.1.1.1"}},
			"2 transport leg(s), you gave 1"},
		{"three destinations for two legs",
			SetSenderRequest{SenderID: "s", DestinationIPs: []string{"a", "b", "c"}},
			"2 transport leg(s), you gave 3"},
		{"one port for two legs",
			SetSenderRequest{SenderID: "s", DestinationPorts: []int{5004}},
			"2 transport leg(s), you gave 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildSenderPatch(2, is05.ActivationModeImmediate, tc.req)
			if err == nil {
				t.Fatal("a leg-count mismatch must be refused, not silently truncated")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the error must say how many legs the device has and how many "+
					"values were given, got: %v", err)
			}
		})
	}
}

func TestPatchNoLegs(t *testing.T) {
	if _, err := buildSenderPatch(0, is05.ActivationModeImmediate,
		SetSenderRequest{SenderID: "s", DestinationIPs: []string{"239.1.1.1"}}); err == nil {
		t.Fatal("a sender reporting no transport legs must be an error, not an empty patch")
	}
}

// TestPatchScheduledCarriesRequestedTime: requested_time belongs only
// to the scheduled modes; sending it with activate_immediate is a
// schema violation.
func TestPatchScheduledCarriesRequestedTime(t *testing.T) {
	patch, err := buildSenderPatch(1, is05.ActivationModeScheduledAbsolute, SetSenderRequest{
		SenderID: "s", DestinationIPs: []string{"239.1.1.1"}, When: "1800000037:0",
	})
	if err != nil {
		t.Fatalf("buildSenderPatch: %v", err)
	}
	act := patch["activation"].(map[string]any)
	if act["requested_time"] != "1800000037:0" {
		t.Errorf("requested_time = %v", act["requested_time"])
	}

	patch, err = buildSenderPatch(1, is05.ActivationModeImmediate, SetSenderRequest{
		SenderID: "s", DestinationIPs: []string{"239.1.1.1"}, When: "1800000037:0",
	})
	if err != nil {
		t.Fatalf("buildSenderPatch: %v", err)
	}
	act = patch["activation"].(map[string]any)
	if _, present := act["requested_time"]; present {
		t.Error("activate_immediate must not carry requested_time")
	}
}

// TestFlattenLegsReadsUntypedParams: transport_params are untyped JSON,
// so numbers arrive as float64 and a missing key must read as empty
// rather than panicking.
func TestFlattenLegsReadsUntypedParams(t *testing.T) {
	got := flattenLegs([]is05.TransportParams{
		{"source_ip": "10.6.40.51", "destination_ip": "0.0.0.0",
			"destination_port": float64(12700), "rtp_enabled": true},
		{}, // a leg the device described with nothing at all
	})
	if len(got) != 2 {
		t.Fatalf("got %d legs", len(got))
	}
	if got[0].SourceIP != "10.6.40.51" || got[0].DestinationIP != "0.0.0.0" {
		t.Errorf("leg 0 = %+v", got[0])
	}
	if got[0].DestinationPort != 12700 || !got[0].RTPEnabled {
		t.Errorf("leg 0 = %+v", got[0])
	}
	if got[1] != (LegState{}) {
		t.Errorf("an empty leg must flatten to zero values, got %+v", got[1])
	}
}

// TestFlattenLegsHandlesIntPort: transport_params usually arrive as
// untyped JSON (float64), but an in-process caller may hand a native
// int; intParam must read both without falling through to zero.
func TestFlattenLegsHandlesIntPort(t *testing.T) {
	got := flattenLegs([]is05.TransportParams{
		{"destination_port": 12700}, // native int, not float64
	})
	if len(got) != 1 || got[0].DestinationPort != 12700 {
		t.Errorf("int destination_port must read as 12700, got %+v", got)
	}
}

// --- SetSender over the connection stub -------------------------------

// setSenderIS05 serves a one-leg active sender and echoes a PATCH back
// with the requested destination applied to that leg. When forceZero is
// set, the echoed leg keeps 0.0.0.0 to model a device that accepted the
// stage but is still emitting nowhere.
func setSenderIS05(t *testing.T, senderID string, forceZero bool) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/senders/"+senderID+"/active"):
			_, _ = w.Write(activeSenderBody(t, true, []is05.TransportParams{
				{"source_ip": "10.6.0.9", "destination_ip": "0.0.0.0", "destination_port": float64(5004)},
			}))
		case strings.HasSuffix(p, "/senders/"+senderID+"/staged"):
			body, _ := io.ReadAll(r.Body)
			dst := "239.5.5.5"
			if forceZero || !strings.Contains(string(body), "239.5.5.5") {
				dst = "0.0.0.0"
			}
			_, _ = w.Write(activeSenderBody(t, true, []is05.TransportParams{
				{"source_ip": "10.6.0.9", "destination_ip": dst, "destination_port": float64(5004)},
			}))
		default:
			http.Error(w, "unexpected "+p, http.StatusNotFound)
		}
	}
}

// TestSetSenderRejectsBadRequest covers the pre-flight guards that fail
// before any walk: no id, nothing to change, a bad mode, a scheduled
// mode with no --when, and a malformed IP.
func TestSetSenderRejectsBadRequest(t *testing.T) {
	h := newHarness(t)
	enable := true
	cases := []struct {
		name string
		req  SetSenderRequest
		want string
	}{
		{"no id", SetSenderRequest{}, "sender id is required"},
		{"nothing to change", SetSenderRequest{SenderID: "s"}, "nothing to change"},
		{"bad mode", SetSenderRequest{SenderID: "s", MasterEnable: &enable, Mode: "activate_someday"},
			"not an IS-05 activation mode"},
		{"scheduled without when",
			SetSenderRequest{SenderID: "s", MasterEnable: &enable, Mode: is05.ActivationModeScheduledRelative},
			"needs --when"},
		{"bad ip", SetSenderRequest{SenderID: "s", DestinationIPs: []string{"not-an-ip"}}, "not an IP address"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := h.ctrl.SetSender(context.Background(), tc.req); err == nil ||
				!strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want contains %q", err, tc.want)
			}
		})
	}
}

// TestSetSenderUnknownSender: a sender absent from the catalogue is a
// hard error, before any device is touched.
func TestSetSenderUnknownSender(t *testing.T) {
	h := newHarness(t)
	if _, err := h.ctrl.SetSender(context.Background(),
		SetSenderRequest{SenderID: uuidN(9), DestinationIPs: []string{"239.5.5.5"}}); err == nil ||
		!strings.Contains(err.Error(), "no sender") {
		t.Fatalf("err = %v, want a no-such-sender error", err)
	}
}

// TestSetSenderActiveReadError: SetSender reads the device's active
// state first (to learn the leg count); a refused read is fatal.
func TestSetSenderActiveReadError(t *testing.T) {
	h := newHarness(t)
	h.cat.devices = []is04.Device{deviceWith(testUUID, h.controlHref)}
	h.cat.senders = []is04.Sender{senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportRTPMcast)}
	h.is05 = func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}
	if _, err := h.ctrl.SetSender(context.Background(),
		SetSenderRequest{SenderID: uuidN(1), DestinationIPs: []string{"239.5.5.5"}}); err == nil ||
		!strings.Contains(err.Error(), "read sender") {
		t.Fatalf("err = %v, want a read-sender failure", err)
	}
}

// TestSetSenderNoLegs: a sender that reports zero transport legs cannot
// be addressed positionally — buildSenderPatch refuses it.
func TestSetSenderNoLegs(t *testing.T) {
	h := newHarness(t)
	h.cat.devices = []is04.Device{deviceWith(testUUID, h.controlHref)}
	h.cat.senders = []is04.Sender{senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportRTPMcast)}
	h.is05 = func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/active") {
			_, _ = w.Write(activeSenderBody(t, true, []is05.TransportParams{})) // no legs
			return
		}
		http.Error(w, "unexpected", http.StatusNotFound)
	}
	if _, err := h.ctrl.SetSender(context.Background(),
		SetSenderRequest{SenderID: uuidN(1), DestinationIPs: []string{"239.5.5.5"}}); err == nil ||
		!strings.Contains(err.Error(), "no transport legs") {
		t.Fatalf("err = %v, want a no-transport-legs error", err)
	}
}

// TestSetSenderBadControlHref: a device whose control href carries no
// api_ver fails NewClient before any read.
func TestSetSenderBadControlHref(t *testing.T) {
	h := newHarness(t)
	h.cat.devices = []is04.Device{deviceWith(testUUID, "http://h/x-nmos/connection/nope")}
	h.cat.senders = []is04.Sender{senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportRTPMcast)}
	if _, err := h.ctrl.SetSender(context.Background(),
		SetSenderRequest{SenderID: uuidN(1), DestinationIPs: []string{"239.5.5.5"}}); err == nil {
		t.Fatal("a control href with no api_ver must fail NewClient")
	}
}

// TestSetSenderDryRun: a dry run reads the active state and returns the
// would-be patch plus the current legs, without PATCHing.
func TestSetSenderDryRun(t *testing.T) {
	h := newHarness(t)
	h.cat.devices = []is04.Device{deviceWith(testUUID, h.controlHref)}
	h.cat.senders = []is04.Sender{senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportRTPMcast)}
	patched := false
	h.is05 = func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patched = true
		}
		if strings.HasSuffix(r.URL.Path, "/active") {
			_, _ = w.Write(activeSenderBody(t, true, []is05.TransportParams{
				{"source_ip": "10.6.0.9", "destination_ip": "0.0.0.0", "destination_port": float64(5004)},
			}))
			return
		}
		http.Error(w, "unexpected", http.StatusNotFound)
	}
	res, err := h.ctrl.SetSender(context.Background(), SetSenderRequest{
		SenderID: uuidN(1), DestinationIPs: []string{"239.5.5.5"}, DryRun: true,
	})
	if err != nil {
		t.Fatalf("SetSender dry run: %v", err)
	}
	if patched {
		t.Fatal("a dry run must never PATCH")
	}
	if !res.DryRun || res.Patch == nil || len(res.Current) != 1 {
		t.Errorf("dry run must return the would-be patch and current legs, got %+v", res)
	}
}

// TestSetSenderRoutes: the happy path stages a destination and reports
// the device's echoed leg state.
func TestSetSenderRoutes(t *testing.T) {
	h := newHarness(t)
	h.cat.devices = []is04.Device{deviceWith(testUUID, h.controlHref)}
	h.cat.senders = []is04.Sender{senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportRTPMcast)}
	h.is05 = setSenderIS05(t, uuidN(1), false)
	res, err := h.ctrl.SetSender(context.Background(), SetSenderRequest{
		SenderID: uuidN(1), DestinationIPs: []string{"239.5.5.5"},
	})
	if err != nil {
		t.Fatalf("SetSender: %v", err)
	}
	if len(res.Legs) != 1 || res.Legs[0].DestinationIP != "239.5.5.5" {
		t.Errorf("Legs = %+v, want the staged destination echoed", res.Legs)
	}
}

// TestSetSenderSkipsEmptyIP: an empty slot in --destination is the
// "leave this leg alone" marker (as `--leg red` renders it), so IP
// validation must skip it rather than reject it. The trailing empty
// then trims away, leaving a one-leg patch.
func TestSetSenderSkipsEmptyIP(t *testing.T) {
	h := newHarness(t)
	h.cat.devices = []is04.Device{deviceWith(testUUID, h.controlHref)}
	h.cat.senders = []is04.Sender{senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportRTPMcast)}
	h.is05 = setSenderIS05(t, uuidN(1), false)
	res, err := h.ctrl.SetSender(context.Background(), SetSenderRequest{
		SenderID: uuidN(1), DestinationIPs: []string{"239.5.5.5", ""},
	})
	if err != nil {
		t.Fatalf("an empty destination slot must be skipped, not rejected: %v", err)
	}
	if len(res.Legs) != 1 || res.Legs[0].DestinationIP != "239.5.5.5" {
		t.Errorf("Legs = %+v, want the single addressed leg", res.Legs)
	}
}

// TestSetSenderDestinationIgnored: a device that accepts the stage but
// keeps 0.0.0.0 on a leg we asked to address is still emitting nowhere;
// SetSender must fire an error event, not report success.
func TestSetSenderDestinationIgnored(t *testing.T) {
	h := newHarness(t)
	h.cat.devices = []is04.Device{deviceWith(testUUID, h.controlHref)}
	h.cat.senders = []is04.Sender{senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportRTPMcast)}
	h.is05 = setSenderIS05(t, uuidN(1), true) // device keeps 0.0.0.0
	if _, err := h.ctrl.SetSender(context.Background(), SetSenderRequest{
		SenderID: uuidN(1), DestinationIPs: []string{"239.5.5.5"},
	}); err != nil {
		t.Fatalf("SetSender: %v", err)
	}
	if !hasCode(h.rep, "nmos_is05_destination_ignored") {
		t.Error("a leg the device kept at 0.0.0.0 must fire nmos_is05_destination_ignored")
	}
}

// TestSetSenderPatchError: a device that refuses the stage surfaces the
// error.
func TestSetSenderPatchError(t *testing.T) {
	h := newHarness(t)
	h.cat.devices = []is04.Device{deviceWith(testUUID, h.controlHref)}
	h.cat.senders = []is04.Sender{senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportRTPMcast)}
	h.is05 = func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/active") {
			_, _ = w.Write(activeSenderBody(t, true, []is05.TransportParams{
				{"source_ip": "10.6.0.9", "destination_ip": "0.0.0.0", "destination_port": float64(5004)},
			}))
			return
		}
		http.Error(w, "destination_ip is not routable", http.StatusBadRequest)
	}
	if _, err := h.ctrl.SetSender(context.Background(), SetSenderRequest{
		SenderID: uuidN(1), DestinationIPs: []string{"239.5.5.5"},
	}); err == nil || !strings.Contains(err.Error(), "not routable") {
		t.Fatalf("err = %v, want the device's refusal surfaced", err)
	}
}
