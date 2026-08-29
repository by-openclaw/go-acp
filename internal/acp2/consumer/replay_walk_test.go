package acp2

// Unit tests for ReplayWalkFromTrace — the offline walk that
// reconstructs WalkedTree state from a recorded raw.an2.jsonl trace
// (ADR-0021). Wire bytes are synthesized with the same codec the
// decoder uses, per the spec shapes in internal/acp2/CLAUDE.md.

import (
	"encoding/binary"
	"encoding/hex"
	"io"
	"log/slog"
	"testing"

	"dhs/internal/acp2/codec"
	"dhs/internal/wiretrace"
)

// buildGetObjectReplyTrame assembles one Rx Trame carrying an AN2 data
// frame (proto=2) whose payload is an ACP2 get_object REPLY:
// header{type=1, mtid, func=1, pid=0} + obj-id(u32) + idx(u32) +
// encoded properties.
func buildGetObjectReplyTrame(t *testing.T, slot uint8, objID uint32, props []codec.Property) wiretrace.Trame {
	t.Helper()
	propBytes, err := codec.EncodeProperties(props)
	if err != nil {
		t.Fatalf("encode properties: %v", err)
	}
	payload := make([]byte, 12+len(propBytes))
	payload[0] = byte(codec.ACP2TypeReply)
	payload[1] = 7 // mtid — arbitrary req/reply id
	payload[2] = byte(codec.ACP2FuncGetObject)
	payload[3] = 0
	binary.BigEndian.PutUint32(payload[4:8], objID)
	binary.BigEndian.PutUint32(payload[8:12], 0)
	copy(payload[12:], propBytes)

	raw, err := codec.EncodeAN2Frame(&codec.AN2Frame{
		Proto:   codec.AN2ProtoACP2,
		Slot:    slot,
		MTID:    0,
		Type:    codec.AN2TypeData,
		Payload: payload,
	})
	if err != nil {
		t.Fatalf("encode AN2 frame: %v", err)
	}
	return wiretrace.Trame{Direction: wiretrace.DirectionRx, Hex: hex.EncodeToString(raw)}
}

// nodeProps builds the property set for a node-container object:
// object_type (u32, low byte = type), label (0-terminated), children (u32[]).
func nodeProps(label string, children []uint32) []codec.Property {
	childData := make([]byte, 4*len(children))
	for i, c := range children {
		binary.BigEndian.PutUint32(childData[i*4:], c)
	}
	return []codec.Property{
		{PID: codec.PIDObjectType, Data: []byte{0, 0, 0, 0}}, // 0 = node
		{PID: codec.PIDLabel, Data: append([]byte(label), 0)},
		{PID: codec.PIDChildren, Data: childData},
	}
}

// leafProps builds a string-typed leaf: object_type=5 (string) + label.
func leafProps(label string) []codec.Property {
	return []codec.Property{
		{PID: codec.PIDObjectType, Data: []byte{0, 0, 0, 5}},
		{PID: codec.PIDLabel, Data: append([]byte(label), 0)},
	}
}

func newReplayPlugin() *Plugin {
	return &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestReplayWalkFromTrace_RootOne(t *testing.T) {
	p := newReplayPlugin()
	trames := []wiretrace.Trame{
		buildGetObjectReplyTrame(t, 0, 1, nodeProps("ROOT_NODE_V2", []uint32{2, 3})),
		buildGetObjectReplyTrame(t, 0, 2, leafProps("Card Name")),
		// child 3 deliberately absent — best-effort partial walk.
	}
	if err := p.ReplayWalkFromTrace(trames); err != nil {
		t.Fatalf("replay: %v", err)
	}
	tree, ok := p.trees.Get(0)
	if !ok {
		t.Fatal("no tree cached for slot 0")
	}
	if tree.Slot != 0 {
		t.Errorf("tree.Slot = %d, want 0", tree.Slot)
	}
	if len(tree.Objects) != 2 {
		t.Fatalf("objects = %d, want 2 (root + one child; missing child is non-fatal)", len(tree.Objects))
	}
	if got := tree.Objects[0].Label; got != "ROOT_NODE_V2" {
		t.Errorf("root label = %q", got)
	}
	if idx, ok := tree.Labels["Card Name"]; !ok || idx != 1 {
		t.Errorf("Labels[Card Name] = %d,%v want 1,true", idx, ok)
	}
	if len(tree.ObjTypes) != 2 || len(tree.NumTypes) != 2 || len(tree.OptionsMaps) != 2 {
		t.Errorf("parallel arrays out of step: %d/%d/%d",
			len(tree.ObjTypes), len(tree.NumTypes), len(tree.OptionsMaps))
	}
}

func TestReplayWalkFromTrace_RootZeroFallback(t *testing.T) {
	p := newReplayPlugin()
	trames := []wiretrace.Trame{
		buildGetObjectReplyTrame(t, 3, 0, nodeProps("SPEC_ROOT", nil)),
	}
	if err := p.ReplayWalkFromTrace(trames); err != nil {
		t.Fatalf("replay with root 0: %v", err)
	}
	tree, ok := p.trees.Get(3)
	if !ok || len(tree.Objects) != 1 || tree.Objects[0].Label != "SPEC_ROOT" {
		t.Fatalf("fallback tree wrong: ok=%v tree=%+v", ok, tree)
	}
}

func TestReplayWalkFromTrace_MultiSlot(t *testing.T) {
	p := newReplayPlugin()
	trames := []wiretrace.Trame{
		buildGetObjectReplyTrame(t, 0, 1, nodeProps("R0", nil)),
		buildGetObjectReplyTrame(t, 1, 1, nodeProps("R1", nil)),
	}
	if err := p.ReplayWalkFromTrace(trames); err != nil {
		t.Fatalf("replay: %v", err)
	}
	for slot, want := range map[int]string{0: "R0", 1: "R1"} {
		tree, ok := p.trees.Get(slot)
		if !ok || tree.Objects[0].Label != want {
			t.Errorf("slot %d: ok=%v", slot, ok)
		}
	}
	// Second replay reuses the existing cache (p.trees != nil branch).
	if err := p.ReplayWalkFromTrace(trames[:1]); err != nil {
		t.Fatalf("second replay: %v", err)
	}
}

func TestReplayWalkFromTrace_NoReplies(t *testing.T) {
	p := newReplayPlugin()
	err := p.ReplayWalkFromTrace(nil)
	if err == nil {
		t.Fatal("want error for empty trace")
	}
}

func TestReplayWalkFromTrace_NoRoot(t *testing.T) {
	p := newReplayPlugin()
	// Valid reply, but only for obj-id 9 — neither root 1 nor 0.
	trames := []wiretrace.Trame{
		buildGetObjectReplyTrame(t, 0, 9, leafProps("orphan")),
	}
	if err := p.ReplayWalkFromTrace(trames); err == nil {
		t.Fatal("want error when trace has no root object")
	}
}

func TestReplayWalkFromTrace_SkipsNoise(t *testing.T) {
	p := newReplayPlugin()
	good := buildGetObjectReplyTrame(t, 0, 1, nodeProps("ROOT", nil))

	// A tx-direction record (ignored), bad hex, a non-ACP2 AN2 frame,
	// a non-data AN2 frame, and an ACP2 request (not reply) — all must
	// be skipped without derailing the replay.
	tx := good
	tx.Direction = wiretrace.DirectionTx

	badHex := wiretrace.Trame{Direction: wiretrace.DirectionRx, Hex: "zz-not-hex"}

	an2Internal, err := codec.EncodeAN2Frame(&codec.AN2Frame{
		Proto: codec.AN2ProtoInternal, Slot: 0, Type: codec.AN2TypeData, Payload: []byte{0},
	})
	if err != nil {
		t.Fatal(err)
	}
	wrongProto := wiretrace.Trame{Direction: wiretrace.DirectionRx, Hex: hex.EncodeToString(an2Internal)}

	reqPayload := make([]byte, 12)
	reqPayload[0] = byte(codec.ACP2TypeRequest)
	reqPayload[2] = byte(codec.ACP2FuncGetObject)
	binary.BigEndian.PutUint32(reqPayload[4:8], 1)
	reqFrame, err := codec.EncodeAN2Frame(&codec.AN2Frame{
		Proto: codec.AN2ProtoACP2, Slot: 0, Type: codec.AN2TypeData, Payload: reqPayload,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := wiretrace.Trame{Direction: wiretrace.DirectionRx, Hex: hex.EncodeToString(reqFrame)}

	// Valid hex, but not a decodable AN2 frame (bad magic).
	badFrame := wiretrace.Trame{Direction: wiretrace.DirectionRx, Hex: "deadbeef00000000"}

	// AN2 frame that is ACP2 but not a data frame (type=reply).
	nonData, err := codec.EncodeAN2Frame(&codec.AN2Frame{
		Proto: codec.AN2ProtoACP2, Slot: 0, MTID: 1, Type: codec.AN2TypeReply, Payload: []byte{0},
	})
	if err != nil {
		t.Fatal(err)
	}
	notData := wiretrace.Trame{Direction: wiretrace.DirectionRx, Hex: hex.EncodeToString(nonData)}

	// ACP2 data frame whose payload is too short to decode as a message.
	shortMsg, err := codec.EncodeAN2Frame(&codec.AN2Frame{
		Proto: codec.AN2ProtoACP2, Slot: 0, Type: codec.AN2TypeData, Payload: []byte{1, 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	undecodable := wiretrace.Trame{Direction: wiretrace.DirectionRx, Hex: hex.EncodeToString(shortMsg)}

	trames := []wiretrace.Trame{tx, badHex, badFrame, wrongProto, notData, undecodable, request, good}
	if err := p.ReplayWalkFromTrace(trames); err != nil {
		t.Fatalf("replay with noise: %v", err)
	}
	if tree, ok := p.trees.Get(0); !ok || len(tree.Objects) != 1 {
		t.Fatalf("noise disturbed the walk: ok=%v", ok)
	}
}
