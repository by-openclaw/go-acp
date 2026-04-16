package acp1

import (
	"context"
	"testing"
	"time"

	"acp/internal/protocol"
)

// TestWalker_HappyPath runs a full slot walk against a canned sequence of
// device replies. Proves the walker:
//   - issues getObject in the spec order (root → identity → control → status → alarm)
//   - builds the label → index map correctly
//   - populates protocol.Object fields from DecodedObject
//
// We control the client's MTID counter so we can pre-build replies with
// matching MTIDs. Each queued reply corresponds to one transaction.
func TestWalker_HappyPath(t *testing.T) {
	ft := &fakeTransport{}
	c := NewClient(ft, nil, ClientConfig{
		MaxRetries:     2,
		ReceiveTimeout: 50 * time.Millisecond,
	})
	defer c.Close()

	// Pre-seed MTID so allocMTID returns a predictable sequence starting
	// at 1001. Walker will issue: root + 1 identity + 1 control + 1 status
	// + 0 alarm = 4 transactions with MTIDs 1001..1004.
	c.nextMTID = 1000

	// Reply 1: root object with counts 1/1/1/0.
	rootValue := []byte{
		0x00, // type = root
		0x09, // num_props
		0x01, // access
		0x00, // boot_mode
		0x01, // num_identity = 1
		0x01, // num_control  = 1
		0x01, // num_status   = 1
		0x00, // num_alarm    = 0
		0x00, // num_file     = 0
	}
	// Reply 2: identity[0] — String object "Card Label"
	idValue := []byte{
		0x05, // type = string
		0x06, // num_props = 6
		0x01, // access = read
		'D', 'E', 'M', 'O', 0x00, // value
		0x10,                     // max_len
		'C', 'a', 'r', 'd', 0x00, // label
	}
	// Reply 3: control[0] — Float gain -6.0 dB
	ctrlValue := []byte{
		0x03,                   // float
		0x0A,                   // num_props
		0x03,                   // access rw
		0xC0, 0xC0, 0x00, 0x00, // value = -6.0
		0x00, 0x00, 0x00, 0x00, // default
		0x3F, 0x80, 0x00, 0x00, // step = 1.0
		0xC2, 0xA0, 0x00, 0x00, // min = -80
		0x41, 0xA0, 0x00, 0x00, // max = 20
		'G', 'a', 'i', 'n', 0x00,
		'd', 'B', 0x00,
	}
	// Reply 4: status[0] — Byte percentage
	statValue := []byte{
		0x0A, // byte
		0x0A, // num_props
		0x01, // access read
		0x50, // value = 80
		0x00, // default
		0x01, // step
		0x00, // min
		0x64, // max = 100
		'P', 'c', 't', 0x00,
		'%', 0x00,
	}

	ft.recv = [][]byte{
		buildReply(t, 1001, MTypeReply, byte(MethodGetObject), GroupRoot, 0, rootValue),
		buildReply(t, 1002, MTypeReply, byte(MethodGetObject), GroupIdentity, 0, idValue),
		buildReply(t, 1003, MTypeReply, byte(MethodGetObject), GroupControl, 0, ctrlValue),
		buildReply(t, 1004, MTypeReply, byte(MethodGetObject), GroupStatus, 0, statValue),
	}

	w := NewWalker(c)
	tree, err := w.Walk(context.Background(), 2)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if tree.Slot != 2 {
		t.Errorf("slot: got %d, want 2", tree.Slot)
	}
	if tree.BootMode != 0 {
		t.Errorf("boot mode: got %d", tree.BootMode)
	}
	if len(tree.Objects) != 3 {
		t.Fatalf("objects count: got %d, want 3", len(tree.Objects))
	}

	// Identity[0]: string "Card"
	id := tree.Objects[0]
	if id.Group != "identity" || id.ID != 0 || id.Label != "Card" {
		t.Errorf("identity: %+v", id)
	}

	// Control[0]: float "Gain" dB
	ctrl := tree.Objects[1]
	if ctrl.Group != "control" || ctrl.Label != "Gain" || ctrl.Unit != "dB" {
		t.Errorf("control: %+v", ctrl)
	}
	if ctrl.Kind != protocol.KindFloat {
		t.Errorf("control kind: got %d, want float", ctrl.Kind)
	}
	minf, okMin := ctrl.Min.(float64)
	maxf, okMax := ctrl.Max.(float64)
	if !okMin || !okMax || minf != -80 || maxf != 20 {
		t.Errorf("control range: %v..%v", ctrl.Min, ctrl.Max)
	}

	// Status[0]: byte "Pct" %
	stat := tree.Objects[2]
	if stat.Group != "status" || stat.Label != "Pct" || stat.Unit != "%" {
		t.Errorf("status: %+v", stat)
	}
	if stat.Kind != protocol.KindUint {
		t.Errorf("status kind: got %d, want uint", stat.Kind)
	}

	// Label map lookups
	if idx := tree.Lookup("control", "Gain"); idx != 1 {
		t.Errorf("Lookup control/Gain: got %d, want 1", idx)
	}
	if idx := tree.Lookup("status", "Pct"); idx != 2 {
		t.Errorf("Lookup status/Pct: got %d, want 2", idx)
	}
	if idx := tree.Lookup("control", "missing"); idx != -1 {
		t.Errorf("Lookup missing: got %d, want -1", idx)
	}
	if idx := tree.Lookup("nonexistent", "anything"); idx != -1 {
		t.Errorf("Lookup nonexistent group: got %d, want -1", idx)
	}
}

func TestWalker_SlotOutOfRange(t *testing.T) {
	w := NewWalker(nil)
	if _, err := w.Walk(context.Background(), 99); err == nil {
		t.Fatal("expected error for slot=99")
	}
	if _, err := w.Walk(context.Background(), -1); err == nil {
		t.Fatal("expected error for slot=-1")
	}
}

// TestResolve covers the Plugin's label / group+id addressing fallback.
func TestResolve(t *testing.T) {
	tree := &SlotTree{
		Slot: 1,
		Objects: []protocol.Object{
			{Slot: 1, Group: "control", ID: 7, Label: "Gain"},
			{Slot: 1, Group: "status", ID: 3, Label: "Temp"},
		},
		Labels: map[string]map[string]int{
			"control": {"Gain": 0},
			"status":  {"Temp": 1},
		},
	}

	// 1. Label lookup with explicit group
	g, id, err := resolve(protocol.ValueRequest{Slot: 1, Group: "control", Label: "Gain"}, tree)
	if err != nil || g != GroupControl || id != 7 {
		t.Errorf("label+group: got g=%d id=%d err=%v", g, id, err)
	}

	// 2. Label lookup without group (searches all)
	g, id, err = resolve(protocol.ValueRequest{Slot: 1, Label: "Temp"}, tree)
	if err != nil || g != GroupStatus || id != 3 {
		t.Errorf("label-only: got g=%d id=%d err=%v", g, id, err)
	}

	// 3. Label not found
	_, _, err = resolve(protocol.ValueRequest{Slot: 1, Group: "control", Label: "Nope"}, tree)
	if err == nil {
		t.Error("expected ErrUnknownLabel for missing label")
	}

	// 4. Label supplied but no tree
	_, _, err = resolve(protocol.ValueRequest{Slot: 1, Label: "Gain"}, nil)
	if err == nil {
		t.Error("expected error when tree is nil")
	}

	// 5. Group+ID fallback (no label)
	g, id, err = resolve(protocol.ValueRequest{Slot: 1, Group: "alarm", ID: 5}, nil)
	if err != nil || g != GroupAlarm || id != 5 {
		t.Errorf("group+id: got g=%d id=%d err=%v", g, id, err)
	}

	// 6. Invalid group name
	_, _, err = resolve(protocol.ValueRequest{Slot: 1, Group: "bogus", ID: 0}, nil)
	if err == nil {
		t.Error("expected error for invalid group")
	}

	// 7. ID out of range
	_, _, err = resolve(protocol.ValueRequest{Slot: 1, Group: "control", ID: 999}, nil)
	if err == nil {
		t.Error("expected error for ID > 255")
	}
}
