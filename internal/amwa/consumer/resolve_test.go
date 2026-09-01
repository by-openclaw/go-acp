package consumer

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04"
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
