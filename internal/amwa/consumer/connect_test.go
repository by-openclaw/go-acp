package consumer

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is05"
)

// --- IS-05 connection stub -------------------------------------------

// stagedReceiverBody renders a staged/active receiver reply through the
// IS-05 encoder, so the bytes the stub serves are exactly what the
// decoder accepts. senderID nil renders receiver_id/sender_id null.
func stagedReceiverBody(t *testing.T, senderID *string, master bool, requestedTime *string) []byte {
	t.Helper()
	b, err := is05.EncodeStagedReceiver(is05.StagedReceiver{
		MasterEnableField: is05.MasterEnableField{MasterEnable: master},
		SenderID:          senderID,
		Activation:        is05.Activation{Mode: is05.ActivationModeImmediate, RequestedTime: requestedTime},
		TransportParams:   []is05.TransportParams{{}},
	})
	if err != nil {
		t.Fatalf("encode staged receiver: %v", err)
	}
	return b
}

// connectIS05 returns an IS-05 handler for Connect's happy paths: it
// echoes the PATCH back as the staged receiver (so master_enable and
// activation reflect what was sent) and serves an SDP transport file.
func connectIS05(t *testing.T, sdp string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/transportfile"):
			_, _ = io.WriteString(w, sdp)
		case strings.HasSuffix(p, "/staged"):
			// Echo: mirror master_enable from the PATCH so the result
			// reflects the request faithfully.
			body, _ := io.ReadAll(r.Body)
			master := strings.Contains(string(body), `"master_enable":true`)
			_, _ = w.Write(stagedReceiverBody(t, nil, master, nil))
		case strings.HasSuffix(p, "/active"):
			_, _ = w.Write(stagedReceiverBody(t, nil, false, nil))
		default:
			http.Error(w, "unexpected is05 "+p, http.StatusNotFound)
		}
	}
}

// --- Connect: input validation (no server needed) ---------------------

// TestConnectRejectsBadRequest covers the four pre-flight guards that
// fail before any walk: a missing receiver, an unknown activation mode,
// and a scheduled mode with no --when.
func TestConnectRejectsBadRequest(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		name string
		req  ConnectRequest
		want string
	}{
		{"no receiver", ConnectRequest{}, "receiver id is required"},
		{"bad mode", ConnectRequest{ReceiverID: "rx", Mode: "activate_someday"}, "not an IS-05 activation mode"},
		{"scheduled without when",
			ConnectRequest{ReceiverID: "rx", Mode: is05.ActivationModeScheduledAbsolute},
			"needs --when"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := h.ctrl.Connect(context.Background(), tc.req); err == nil ||
				!strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want contains %q", err, tc.want)
			}
		})
	}
}

// TestConnectUnknownReceiver: a receiver absent from the catalogue is a
// hard error naming it — never a guessed endpoint.
func TestConnectUnknownReceiver(t *testing.T) {
	h := newHarness(t)
	if _, err := h.ctrl.Connect(context.Background(),
		ConnectRequest{ReceiverID: uuidN(99)}); err == nil ||
		!strings.Contains(err.Error(), "no receiver") {
		t.Fatalf("err = %v, want a no-such-receiver error", err)
	}
}

// TestConnectBadControlHref: a Device whose sr-ctrl href is a valid URI
// but does not end in an api_ver segment fails at connection.NewClient
// (which parses the version back out of the href), before any route is
// attempted.
func TestConnectBadControlHref(t *testing.T) {
	h := newHarness(t)
	h.cat.devices = []is04.Device{deviceWith(testUUID, "http://h/x-nmos/connection/nope")}
	h.cat.receivers = []is04.Receiver{receiverOn(uuidN(1), testUUID, is04.TransportRTPMcast)}
	if _, err := h.ctrl.Connect(context.Background(),
		ConnectRequest{ReceiverID: uuidN(1)}); err == nil {
		t.Fatal("an unparseable control href must fail NewClient")
	}
}

// --- Connect: routing over the stub -----------------------------------

// TestConnectRoutesSenderToReceiver is the whole happy sequence: walk,
// discover the IS-05 endpoint, fetch the sender's SDP, stage it with
// sender_id + master_enable=true, and report the Device's own answer.
func TestConnectRoutesSenderToReceiver(t *testing.T) {
	const sdp = "v=0\r\no=- 0 0 IN IP4 10.6.0.9\r\n"
	h := newHarness(t)
	h.cat.devices = []is04.Device{deviceWith(testUUID, h.controlHref)}
	h.cat.senders = []is04.Sender{senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportRTPMcast)}
	h.cat.receivers = []is04.Receiver{receiverOn(uuidN(2), testUUID, is04.TransportRTPMcast)}
	h.is05 = connectIS05(t, sdp)

	res, err := h.ctrl.Connect(context.Background(), ConnectRequest{
		SenderID: uuidN(1), ReceiverID: uuidN(2),
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !res.MasterEnable {
		t.Error("a route with a sender must stage master_enable=true")
	}
	if res.SDPBytes != len(sdp) {
		t.Errorf("SDPBytes = %d, want the served SDP length %d", res.SDPBytes, len(sdp))
	}
	if res.Endpoint != h.controlHref {
		t.Errorf("Endpoint = %q, want the discovered IS-05 base", res.Endpoint)
	}
}

// TestConnectDisconnect: an empty SenderID is IS-05's disconnect — the
// stage carries sender_id=null and master_enable=false, and no SDP is
// fetched.
func TestConnectDisconnect(t *testing.T) {
	h := newHarness(t)
	h.cat.devices = []is04.Device{deviceWith(testUUID, h.controlHref)}
	h.cat.receivers = []is04.Receiver{receiverOn(uuidN(2), testUUID, is04.TransportRTPMcast)}
	var gotSDPFetch bool
	h.is05 = func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/transportfile") {
			gotSDPFetch = true
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"sender_id":null`) {
			t.Errorf("disconnect must send sender_id=null, body was %q", body)
		}
		_, _ = w.Write(stagedReceiverBody(t, nil, false, nil))
	}
	res, err := h.ctrl.Connect(context.Background(), ConnectRequest{ReceiverID: uuidN(2)})
	if err != nil {
		t.Fatalf("Connect disconnect: %v", err)
	}
	if res.MasterEnable {
		t.Error("a disconnect must not leave master_enable set")
	}
	if gotSDPFetch {
		t.Error("a disconnect must not fetch a transport file")
	}
}

// TestConnectMXLLegSkipsTransportFile: BCP-007-03 forbids a transport
// file on an MXL connection. Connect must stage sender_id WITHOUT
// fetching /transportfile.
func TestConnectMXLLegSkipsTransportFile(t *testing.T) {
	h := newHarness(t)
	h.cat.devices = []is04.Device{deviceWith(testUUID, h.controlHref)}
	h.cat.senders = []is04.Sender{senderOn(uuidN(1), "MXL 1", testUUID, is04.TransportMXL)}
	h.cat.receivers = []is04.Receiver{receiverOn(uuidN(2), testUUID, is04.TransportMXL)}
	var gotSDPFetch bool
	h.is05 = func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/transportfile") {
			gotSDPFetch = true
		}
		_, _ = w.Write(stagedReceiverBody(t, nil, true, nil))
	}
	res, err := h.ctrl.Connect(context.Background(), ConnectRequest{
		SenderID: uuidN(1), ReceiverID: uuidN(2),
	})
	if err != nil {
		t.Fatalf("Connect MXL: %v", err)
	}
	if gotSDPFetch {
		t.Error("an MXL leg must never request /transportfile (BCP-007-03-02 test_03)")
	}
	if res.SDPBytes != 0 {
		t.Errorf("SDPBytes = %d, want 0 for an MXL route", res.SDPBytes)
	}
}

// TestConnectNoTransportFileWarns: a sender that 404s its /transportfile
// is not fatal — the receiver can still be pointed at it by id, and a
// warning records the gap.
func TestConnectNoTransportFileWarns(t *testing.T) {
	h := newHarness(t)
	h.cat.devices = []is04.Device{deviceWith(testUUID, h.controlHref)}
	h.cat.senders = []is04.Sender{senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportRTPMcast)}
	h.cat.receivers = []is04.Receiver{receiverOn(uuidN(2), testUUID, is04.TransportRTPMcast)}
	h.is05 = func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/transportfile") {
			http.Error(w, "no such sender", http.StatusNotFound)
			return
		}
		_, _ = w.Write(stagedReceiverBody(t, nil, true, nil))
	}
	res, err := h.ctrl.Connect(context.Background(), ConnectRequest{SenderID: uuidN(1), ReceiverID: uuidN(2)})
	if err != nil {
		t.Fatalf("a missing transport file must not fail the route: %v", err)
	}
	if res.SDPBytes != 0 {
		t.Errorf("SDPBytes = %d, want 0 when no SDP was served", res.SDPBytes)
	}
	if !hasCode(h.rep, "nmos_is05_no_transport_file") {
		t.Error("a missing transport file must fire nmos_is05_no_transport_file")
	}
}

// TestConnectEmptyTransportFileWarns: a sender that serves an
// all-whitespace SDP is warned about and no transport_file is staged.
func TestConnectEmptyTransportFileWarns(t *testing.T) {
	h := newHarness(t)
	h.cat.devices = []is04.Device{deviceWith(testUUID, h.controlHref)}
	h.cat.senders = []is04.Sender{senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportRTPMcast)}
	h.cat.receivers = []is04.Receiver{receiverOn(uuidN(2), testUUID, is04.TransportRTPMcast)}
	h.is05 = connectIS05(t, "   \n\t ")
	res, err := h.ctrl.Connect(context.Background(), ConnectRequest{SenderID: uuidN(1), ReceiverID: uuidN(2)})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if res.SDPBytes != 0 {
		t.Errorf("SDPBytes = %d, want 0 for a blank transport file", res.SDPBytes)
	}
	if !hasCode(h.rep, "nmos_is05_empty_transport_file") {
		t.Error("a blank transport file must fire nmos_is05_empty_transport_file")
	}
}

// TestConnectMasterEnableIgnored: a device that accepts the stage but
// reports master_enable=false while a sender was requested is emitting
// nothing — Connect must fire an error event, not report success.
func TestConnectMasterEnableIgnored(t *testing.T) {
	h := newHarness(t)
	h.cat.devices = []is04.Device{deviceWith(testUUID, h.controlHref)}
	h.cat.senders = []is04.Sender{senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportRTPMcast)}
	h.cat.receivers = []is04.Receiver{receiverOn(uuidN(2), testUUID, is04.TransportRTPMcast)}
	h.is05 = func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/transportfile") {
			_, _ = io.WriteString(w, "v=0\r\n")
			return
		}
		// Silently drops master_enable — the classic silent failure.
		_, _ = w.Write(stagedReceiverBody(t, nil, false, nil))
	}
	if _, err := h.ctrl.Connect(context.Background(),
		ConnectRequest{SenderID: uuidN(1), ReceiverID: uuidN(2)}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !hasCode(h.rep, "nmos_is05_master_enable_ignored") {
		t.Error("a dropped master_enable must fire nmos_is05_master_enable_ignored")
	}
}

// TestConnectReportsActivationTime: when the device echoes a
// requested_time in the staged activation, Connect surfaces it as
// ActivationAt.
func TestConnectReportsActivationTime(t *testing.T) {
	when := "1800000037:0"
	h := newHarness(t)
	h.cat.devices = []is04.Device{deviceWith(testUUID, h.controlHref)}
	h.cat.senders = []is04.Sender{senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportMXL)}
	h.cat.receivers = []is04.Receiver{receiverOn(uuidN(2), testUUID, is04.TransportMXL)}
	h.is05 = func(w http.ResponseWriter, _ *http.Request) {
		b, err := is05.EncodeStagedReceiver(is05.StagedReceiver{
			MasterEnableField: is05.MasterEnableField{MasterEnable: true},
			Activation:        is05.Activation{Mode: is05.ActivationModeScheduledAbsolute, RequestedTime: &when},
			TransportParams:   []is05.TransportParams{{}},
		})
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		_, _ = w.Write(b)
	}
	res, err := h.ctrl.Connect(context.Background(), ConnectRequest{
		SenderID: uuidN(1), ReceiverID: uuidN(2),
		Mode: is05.ActivationModeScheduledAbsolute, When: when,
	})
	if err != nil {
		t.Fatalf("Connect scheduled: %v", err)
	}
	if res.ActivationAt != when {
		t.Errorf("ActivationAt = %q, want the echoed requested_time %q", res.ActivationAt, when)
	}
}

// TestConnectPatchError: a device that refuses the stage (non-2xx)
// surfaces as a route failure.
func TestConnectPatchError(t *testing.T) {
	h := newHarness(t)
	h.cat.devices = []is04.Device{deviceWith(testUUID, h.controlHref)}
	h.cat.senders = []is04.Sender{senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportMXL)}
	h.cat.receivers = []is04.Receiver{receiverOn(uuidN(2), testUUID, is04.TransportMXL)}
	h.is05 = func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "transport_params[0].destination_ip is not routable", http.StatusBadRequest)
	}
	if _, err := h.ctrl.Connect(context.Background(),
		ConnectRequest{SenderID: uuidN(1), ReceiverID: uuidN(2)}); err == nil ||
		!strings.Contains(err.Error(), "not routable") {
		t.Fatalf("err = %v, want the device's refusal surfaced", err)
	}
}

// --- Connect: dry run -------------------------------------------------

// TestConnectDryRunReadsActiveState: a dry run stages nothing but
// reports the body it WOULD send and the receiver's current active
// source — what the operator is about to overwrite.
func TestConnectDryRunReadsActiveState(t *testing.T) {
	cur := uuidN(7)
	h := newHarness(t)
	h.cat.devices = []is04.Device{deviceWith(testUUID, h.controlHref)}
	h.cat.senders = []is04.Sender{senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportMXL)}
	h.cat.receivers = []is04.Receiver{receiverOn(uuidN(2), testUUID, is04.TransportMXL)}
	patched := false
	h.is05 = func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patched = true
		}
		if strings.HasSuffix(r.URL.Path, "/active") {
			_, _ = w.Write(stagedReceiverBody(t, &cur, true, nil))
			return
		}
		http.Error(w, "unexpected", http.StatusNotFound)
	}
	res, err := h.ctrl.Connect(context.Background(), ConnectRequest{
		SenderID: uuidN(1), ReceiverID: uuidN(2), DryRun: true,
	})
	if err != nil {
		t.Fatalf("Connect dry run: %v", err)
	}
	if patched {
		t.Fatal("a dry run must never PATCH")
	}
	if !res.DryRun || res.Patch == nil {
		t.Error("a dry run must return the would-be patch")
	}
	if res.CurrentSenderID == nil || *res.CurrentSenderID != cur || !res.CurrentMasterEnable {
		t.Errorf("dry run must report the current active source, got %+v", res)
	}
}

// TestConnectDryRunActiveUnreadable: when the receiver's active state
// cannot be read, a dry run still returns the would-be patch and fires
// a warning rather than failing.
func TestConnectDryRunActiveUnreadable(t *testing.T) {
	h := newHarness(t)
	h.cat.devices = []is04.Device{deviceWith(testUUID, h.controlHref)}
	h.cat.senders = []is04.Sender{senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportMXL)}
	h.cat.receivers = []is04.Receiver{receiverOn(uuidN(2), testUUID, is04.TransportMXL)}
	h.is05 = func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "active unavailable", http.StatusServiceUnavailable)
	}
	res, err := h.ctrl.Connect(context.Background(), ConnectRequest{
		SenderID: uuidN(1), ReceiverID: uuidN(2), DryRun: true,
	})
	if err != nil {
		t.Fatalf("dry run must not fail on an unreadable active state: %v", err)
	}
	if !res.DryRun || res.Patch == nil {
		t.Error("the would-be patch must still be returned")
	}
	if !hasCode(h.rep, "nmos_is05_active_unreadable") {
		t.Error("an unreadable active state must fire nmos_is05_active_unreadable")
	}
}

// --- href resolution helpers (pure, snapshot-driven) ------------------

// TestHrefForDevice covers the three arms: a device advertising a
// control (href returned), a device with no sr-ctrl control (error),
// and a resource naming a device absent from the catalogue (error).
func TestHrefForDevice(t *testing.T) {
	var c Controller
	snap := &CatalogueSnapshot{
		Devices: []is04.Device{
			{ResourceCore: is04.ResourceCore{ID: "dev-ok"}, Controls: []is04.DeviceControl{
				{Href: "http://h/x-nmos/connection/v1.1", Type: "urn:x-nmos:control:sr-ctrl/v1.1"},
			}},
			{ResourceCore: is04.ResourceCore{ID: "dev-nocontrol"}},
		},
	}
	if href, err := c.hrefForDevice(snap, "dev-ok", "res", "sender"); err != nil ||
		href != "http://h/x-nmos/connection/v1.1" {
		t.Errorf("device with control: href=%q err=%v", href, err)
	}
	if _, err := c.hrefForDevice(snap, "dev-nocontrol", "res", "sender"); err == nil ||
		!strings.Contains(err.Error(), "sr-ctrl") {
		t.Errorf("a device with no sr-ctrl control must error: %v", err)
	}
	if _, err := c.hrefForDevice(snap, "dev-missing", "res-x", "receiver"); err == nil ||
		!strings.Contains(err.Error(), "not in this catalogue") {
		t.Errorf("a device absent from the catalogue must error: %v", err)
	}
}

// TestConnectionHrefResolvesByReceiver: connectionHref finds the owning
// device via the receiver, and reports a missing receiver distinctly.
func TestConnectionHrefResolvesByReceiver(t *testing.T) {
	var c Controller
	snap := &CatalogueSnapshot{
		Devices: []is04.Device{{ResourceCore: is04.ResourceCore{ID: "d1"}, Controls: []is04.DeviceControl{
			{Href: "http://h/x-nmos/connection/v1.1", Type: "urn:x-nmos:control:sr-ctrl/v1.1"},
		}}},
		Receivers: []is04.Receiver{{ResourceCore: is04.ResourceCore{ID: "rx1"}, DeviceID: "d1"}},
	}
	if href, err := c.connectionHref(snap, "rx1"); err != nil || href == "" {
		t.Errorf("known receiver: href=%q err=%v", href, err)
	}
	if _, err := c.connectionHref(snap, "rx-missing"); err == nil ||
		!strings.Contains(err.Error(), "no receiver") {
		t.Errorf("a missing receiver must error: %v", err)
	}
}

// TestSenderConnectionHrefResolvesBySender is the sender-side twin.
func TestSenderConnectionHrefResolvesBySender(t *testing.T) {
	var c Controller
	snap := &CatalogueSnapshot{
		Devices: []is04.Device{{ResourceCore: is04.ResourceCore{ID: "d1"}, Controls: []is04.DeviceControl{
			{Href: "http://h/x-nmos/connection/v1.1", Type: "urn:x-nmos:control:sr-ctrl/v1.1"},
		}}},
		Senders: []is04.Sender{{ResourceCore: is04.ResourceCore{ID: "tx1"}, DeviceID: "d1"}},
	}
	if href, err := c.senderConnectionHref(snap, "tx1"); err != nil || href == "" {
		t.Errorf("known sender: href=%q err=%v", href, err)
	}
	if _, err := c.senderConnectionHref(snap, "tx-missing"); err == nil ||
		!strings.Contains(err.Error(), "no sender") {
		t.Errorf("a missing sender must error: %v", err)
	}
}

// TestPickConnectionControl: controls are unordered and a device lists
// several sr-ctrl minors; the highest version string wins, and a device
// with no sr-ctrl control yields "".
func TestPickConnectionControl(t *testing.T) {
	got := pickConnectionControl([]is04.DeviceControl{
		{Href: "http://h/other", Type: "urn:x-nmos:control:events/v1.0"},
		{Href: "http://h/v1.0", Type: "urn:x-nmos:control:sr-ctrl/v1.0"},
		{Href: "http://h/v1.1", Type: "urn:x-nmos:control:sr-ctrl/v1.1"},
	})
	if got != "http://h/v1.1" {
		t.Errorf("pickConnectionControl = %q, want the highest sr-ctrl minor", got)
	}
	if got := pickConnectionControl([]is04.DeviceControl{
		{Href: "http://h/x", Type: "urn:x-nmos:control:events/v1.0"},
	}); got != "" {
		t.Errorf("no sr-ctrl control must yield empty, got %q", got)
	}
}

// TestActivationBody: only the scheduled modes carry requested_time;
// activate_immediate must not (a schema violation otherwise).
func TestActivationBody(t *testing.T) {
	imm := activationBody(is05.ActivationModeImmediate, "1800000037:0")
	if _, present := imm["requested_time"]; present {
		t.Error("activate_immediate must not carry requested_time")
	}
	sched := activationBody(is05.ActivationModeScheduledRelative, "5:0")
	if sched["requested_time"] != "5:0" {
		t.Errorf("scheduled mode must carry requested_time, got %v", sched["requested_time"])
	}
}
