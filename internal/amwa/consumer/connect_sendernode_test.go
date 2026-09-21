package consumer

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is04/v13"
)

// A plant with no Registry and two devices (a Neuron sender node, a
// FusioN receiver node): the Receiver's node has never heard of the
// Sender, so its IS-05 answers "{}" for the transport file. The SDP
// exists in exactly one place — the Sender's own IS-05 — and that is
// where Connect must fetch it from (#1114).

// senderNode serves one Node's IS-04 (v1.3) with one device + one
// sender, and that device's IS-05 transport file.
func senderNode(t *testing.T, senderID, sdp string, listSender bool) *httptest.Server {
	t.Helper()
	codec := v13.New()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		ctl := srv.URL + "/x-nmos/connection/v1.0"
		switch {
		case strings.HasSuffix(p, "/x-nmos/node/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`["v1.3/"]`))
		case strings.HasSuffix(p, "/transportfile"):
			_, _ = io.WriteString(w, sdp)
		case strings.HasSuffix(p, "/nodes"):
			writeNodes(t, w, codec, nil)
		case strings.HasSuffix(p, "/devices"):
			writeDevices(t, w, codec, []is04.Device{deviceWith(uuidN(9), ctl)})
		case strings.HasSuffix(p, "/sources"):
			writeSources(t, w, codec, nil)
		case strings.HasSuffix(p, "/flows"):
			writeFlows(t, w, codec, nil)
		case strings.HasSuffix(p, "/senders"):
			var xs []is04.Sender
			if listSender {
				xs = []is04.Sender{senderOn(senderID, "VTX-01", uuidN(9), is04.TransportRTPMcast)}
			}
			writeSenders(t, w, codec, xs)
		case strings.HasSuffix(p, "/receivers"):
			writeReceivers(t, w, codec, nil)
		default:
			http.Error(w, "unexpected "+p, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestConnectFetchesSDPFromTheSenderNode(t *testing.T) {
	const senderSDP = "v=0\r\no=- 1 1 IN IP4 10.6.40.50\r\ns=VTX-01\r\n"
	h := newHarness(t)
	h.cat.devices = []is04.Device{deviceWith(testUUID, h.controlHref)}
	h.cat.receivers = []is04.Receiver{receiverOn(uuidN(2), testUUID, is04.TransportRTPMcast)}
	h.is05 = connectIS05(t, "{}") // the receiver's node does not own the sender
	sn := senderNode(t, uuidN(1), senderSDP, true)

	res, err := h.ctrl.Connect(context.Background(), ConnectRequest{
		SenderID: uuidN(1), ReceiverID: uuidN(2), SenderNode: sn.URL, DryRun: true,
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	tf, _ := res.Patch["transport_file"].(map[string]any)
	if tf == nil || tf["data"] != senderSDP {
		t.Fatalf("staged transport_file = %v, want the sender node's SDP", res.Patch["transport_file"])
	}
	if res.SDPBytes != len(senderSDP) {
		t.Errorf("SDPBytes = %d", res.SDPBytes)
	}
}

func TestConnectSenderNodeErrors(t *testing.T) {
	h := newHarness(t)
	h.cat.devices = []is04.Device{deviceWith(testUUID, h.controlHref)}
	h.cat.receivers = []is04.Receiver{receiverOn(uuidN(2), testUUID, is04.TransportRTPMcast)}
	h.is05 = connectIS05(t, "{}")

	_, err := h.ctrl.Connect(context.Background(), ConnectRequest{SenderID: uuidN(1), ReceiverID: uuidN(2), SenderNode: "http://127.0.0.1:1"})
	if err == nil || !strings.Contains(err.Error(), "sender node http://127.0.0.1:1") {
		t.Errorf("unreachable sender node err = %v", err)
	}
	sn := senderNode(t, uuidN(1), "v=0", false)
	_, err = h.ctrl.Connect(context.Background(), ConnectRequest{SenderID: uuidN(1), ReceiverID: uuidN(2), SenderNode: sn.URL})
	if err == nil || !strings.Contains(err.Error(), "on neither this catalogue nor") {
		t.Errorf("sender absent everywhere err = %v", err)
	}
}

func TestConnectNeverStagesBracesAsSDP(t *testing.T) {
	// Without --sender-node the old behaviour would have staged "{}";
	// now it is the empty-transport-file warning and no transport_file.
	h := newHarness(t)
	h.cat.devices = []is04.Device{deviceWith(testUUID, h.controlHref)}
	h.cat.senders = []is04.Sender{senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportRTPMcast)}
	h.cat.receivers = []is04.Receiver{receiverOn(uuidN(2), testUUID, is04.TransportRTPMcast)}
	h.is05 = connectIS05(t, "{}")
	res, err := h.ctrl.Connect(context.Background(), ConnectRequest{SenderID: uuidN(1), ReceiverID: uuidN(2), DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, staged := res.Patch["transport_file"]; staged {
		t.Errorf("{} must not be staged as an SDP: %v", res.Patch)
	}
	found := false
	for _, ev := range h.rep.Snapshot() {
		if ev.Code == "nmos_is05_empty_transport_file" {
			found = true
		}
	}
	if !found {
		t.Error("an empty transport file must fire nmos_is05_empty_transport_file")
	}
}

func TestConnectFallsBackToReceiverIS05WhenSenderHrefIsBad(t *testing.T) {
	// The sender's device advertises a control href that is not a URL:
	// the transport file is then asked from the receiver's IS-05 (the
	// pre-#1114 path), which still serves it here.
	const sdp = "v=0\r\nfrom-receiver-side\r\n"
	h := newHarness(t)
	h.cat.devices = []is04.Device{deviceWith(testUUID, h.controlHref), deviceWith(uuidN(8), "http://h/x-nmos/connection/nope")}
	h.cat.senders = []is04.Sender{senderOn(uuidN(1), "CAM 1", uuidN(8), is04.TransportRTPMcast)}
	h.cat.receivers = []is04.Receiver{receiverOn(uuidN(2), testUUID, is04.TransportRTPMcast)}
	h.is05 = connectIS05(t, sdp)
	res, err := h.ctrl.Connect(context.Background(), ConnectRequest{SenderID: uuidN(1), ReceiverID: uuidN(2)})
	if err != nil {
		t.Fatal(err)
	}
	if res.SDPBytes != len(sdp) {
		t.Errorf("SDPBytes = %d, want the receiver-side SDP", res.SDPBytes)
	}
}
