package consumer

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is05"
)

func snd(id, label string) is04.Sender {
	return is04.Sender{ResourceCore: is04.ResourceCore{ID: id, Label: label}}
}

func TestMatchSenderLabel(t *testing.T) {
	senders := []is04.Sender{
		snd("id-1", "CAM 1"),
		snd("id-2", "CAM 2"),
		snd("id-3", "CAM 2"), // duplicate label — legal per spec
	}

	if id, err := matchSenderLabel(senders, "CAM 1"); err != nil || id != "id-1" {
		t.Errorf("unique label: id=%q err=%v", id, err)
	}

	if _, err := matchSenderLabel(senders, "CAM 2"); err == nil ||
		!strings.Contains(err.Error(), "id-2") || !strings.Contains(err.Error(), "id-3") {
		t.Errorf("ambiguous label must list every candidate id: %v", err)
	}

	if _, err := matchSenderLabel(senders, "CAM 9"); err == nil ||
		!strings.Contains(err.Error(), "CAM 1") {
		t.Errorf("unknown label must list the labels present: %v", err)
	}
}

// TestMatchSenderLabelTruncatesLongList: when more than a dozen labels
// exist, the not-found error truncates the list with an ellipsis rather
// than dumping the whole plant into one line.
func TestMatchSenderLabelTruncatesLongList(t *testing.T) {
	var senders []is04.Sender
	for i := 0; i < 14; i++ {
		senders = append(senders, snd(uuidN(i), "LBL "+padID(i)))
	}
	_, err := matchSenderLabel(senders, "absent")
	if err == nil {
		t.Fatal("an absent label must error")
	}
	if !strings.Contains(err.Error(), "…") {
		t.Errorf("a >12 label list must be truncated with an ellipsis: %v", err)
	}
}

// TestResolveSenderByLabel walks the catalogue and maps a unique label
// to its UUID — the operator-facing entry point over matchSenderLabel.
func TestResolveSenderByLabel(t *testing.T) {
	h := newHarness(t)
	h.cat.senders = []is04.Sender{
		senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportRTPMcast),
		senderOn(uuidN(2), "CAM 2", testUUID, is04.TransportRTPMcast),
	}
	id, err := h.ctrl.ResolveSenderByLabel(context.Background(), "CAM 2")
	if err != nil {
		t.Fatalf("ResolveSenderByLabel: %v", err)
	}
	if id != uuidN(2) {
		t.Errorf("id = %q, want %q", id, uuidN(2))
	}
}

// TestResolveSenderByLabelNilSnapshot: if the catalogue walk yields no
// snapshot at all, ResolveSenderByLabel must report that rather than
// dereference nil. The walk seam is overridden to return a nil snapshot;
// Walk itself always returns a non-nil one, so the guard is otherwise
// unreachable.
func TestResolveSenderByLabelNilSnapshot(t *testing.T) {
	orig := walkCatalogue
	walkCatalogue = func(*Controller, context.Context) (*CatalogueSnapshot, []error) { return nil, nil }
	defer func() { walkCatalogue = orig }()

	h := newHarness(t)
	_, err := h.ctrl.ResolveSenderByLabel(context.Background(), "CAM 1")
	if err == nil || !strings.Contains(err.Error(), "returned nothing") {
		t.Fatalf("err = %v, want the nil-snapshot guard", err)
	}
}

// activeSenderBody renders an active sender through the IS-05 encoder so
// the served bytes match what the decoder accepts.
func activeSenderBody(t *testing.T, master bool, params []is05.TransportParams) []byte {
	t.Helper()
	b, err := is05.EncodeStagedSender(is05.StagedSender{
		MasterEnableField: is05.MasterEnableField{MasterEnable: master},
		Activation:        is05.Activation{Mode: is05.ActivationModeImmediate},
		TransportParams:   params,
	})
	if err != nil {
		t.Fatalf("encode active sender: %v", err)
	}
	return b
}

// TestSenderActiveLegs reads a sender's ACTIVE addressing — the verify
// half of a retune — flattening the device's own transport_params.
func TestSenderActiveLegs(t *testing.T) {
	h := newHarness(t)
	h.cat.devices = []is04.Device{deviceWith(testUUID, h.controlHref)}
	h.cat.senders = []is04.Sender{senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportRTPMcast)}
	h.is05 = func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/senders/"+uuidN(1)+"/active") {
			_, _ = w.Write(activeSenderBody(t, true, []is05.TransportParams{
				{"source_ip": "10.6.0.9", "destination_ip": "239.1.1.1", "destination_port": float64(5004)},
			}))
			return
		}
		http.Error(w, "unexpected", http.StatusNotFound)
	}
	legs, master, err := h.ctrl.SenderActiveLegs(context.Background(), uuidN(1))
	if err != nil {
		t.Fatalf("SenderActiveLegs: %v", err)
	}
	if !master {
		t.Error("master_enable must be reported from the device's active state")
	}
	if len(legs) != 1 || legs[0].DestinationIP != "239.1.1.1" || legs[0].DestinationPort != 5004 {
		t.Errorf("legs = %+v, want the flattened active addressing", legs)
	}
}

// TestSenderActiveLegsErrors covers the three failure arms: a sender
// absent from the catalogue, a device whose control href will not build
// a client, and an active-state read that the device refuses.
func TestSenderActiveLegsErrors(t *testing.T) {
	t.Run("unknown sender", func(t *testing.T) {
		h := newHarness(t)
		if _, _, err := h.ctrl.SenderActiveLegs(context.Background(), uuidN(9)); err == nil ||
			!strings.Contains(err.Error(), "no sender") {
			t.Fatalf("err = %v, want a no-such-sender error", err)
		}
	})

	t.Run("bad control href", func(t *testing.T) {
		h := newHarness(t)
		h.cat.devices = []is04.Device{deviceWith(testUUID, "http://h/x-nmos/connection/nope")}
		h.cat.senders = []is04.Sender{senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportRTPMcast)}
		if _, _, err := h.ctrl.SenderActiveLegs(context.Background(), uuidN(1)); err == nil {
			t.Fatal("a control href with no api_ver must fail NewClient")
		}
	})

	t.Run("active read refused", func(t *testing.T) {
		h := newHarness(t)
		h.cat.devices = []is04.Device{deviceWith(testUUID, h.controlHref)}
		h.cat.senders = []is04.Sender{senderOn(uuidN(1), "CAM 1", testUUID, is04.TransportRTPMcast)}
		h.is05 = func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}
		if _, _, err := h.ctrl.SenderActiveLegs(context.Background(), uuidN(1)); err == nil {
			t.Fatal("a refused active read must surface as an error")
		}
	})
}

// TestBuildSenderPatchTrimsEmptyOverflow: `--leg red` always renders
// two slots; on a single-leg sender the trailing EMPTY slot trims
// away, while a non-empty overflow stays a hard error.
func TestBuildSenderPatchTrimsEmptyOverflow(t *testing.T) {
	req := SetSenderRequest{
		SenderID:         "s",
		DestinationIPs:   []string{"239.60.1.1", ""},
		DestinationPorts: []int{5010, 0},
	}
	patch, err := buildSenderPatch(1, "activate_immediate", req)
	if err != nil {
		t.Fatalf("empty overflow must trim: %v", err)
	}
	params := patch["transport_params"].([]map[string]any)
	if len(params) != 1 || params[0]["destination_ip"] != "239.60.1.1" || params[0]["destination_port"] != 5010 {
		t.Errorf("patch legs = %+v", params)
	}

	bad := SetSenderRequest{SenderID: "s", DestinationIPs: []string{"239.60.1.1", "239.62.1.1"}}
	if _, err := buildSenderPatch(1, "activate_immediate", bad); err == nil {
		t.Error("non-empty overflow must stay an error")
	}
}
