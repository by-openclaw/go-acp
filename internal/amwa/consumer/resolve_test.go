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
