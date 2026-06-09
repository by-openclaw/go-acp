package acp1

import (
	"context"
	"errors"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/export/canonical"
)

// TestBroadcastAnnounce_GatedOff covers the broadcastAnnounceSkip early-return
// when the served tree carries Broadcasts=Off (slot 0 / control / id 4).
func TestBroadcastAnnounce_GatedOff(t *testing.T) {
	s := newTestServer(t)
	s.tree.entries[objectKey{slot: 0, group: codec.GroupControl, id: 4}] = &entry{
		acpType: codec.TypeEnum,
		param: &canonical.Parameter{Value: int64(0),
			EnumMap: []canonical.EnumEntry{{Key: "Off", Value: 0}, {Key: "On", Value: 1}}},
	}
	// Should return early at the gate — no panic, no socket needed.
	s.broadcastAnnounce(&codec.Message{
		MTID: 0, MType: codec.MTypeAnnounce, ObjGroup: codec.GroupFrame, ObjID: 0, Value: []byte{2},
	})
}

// TestHandleDatagram2_Branches drives the decode-fail, non-request-drop, and
// reply-send-error arms of the UDP datagram handler directly.
func TestHandleDatagram2_Branches(t *testing.T) {
	s := newTestServer(t)

	// Decode failure → logged + dropped, send never called.
	called := false
	s.handleDatagram2([]byte{0x01}, "1.2.3.4:5", func([]byte) error { called = true; return nil })
	if called {
		t.Error("undecodable datagram should not produce a reply")
	}

	// Non-request (announce) → handleRequest returns nil → dropped.
	ann := &codec.Message{MTID: 0, PVER: 1, MType: codec.MTypeAnnounce, MAddr: 1}
	annBytes, err := ann.Encode()
	if err != nil {
		t.Fatalf("encode announce: %v", err)
	}
	called = false
	s.handleDatagram2(annBytes, "x", func([]byte) error { called = true; return nil })
	if called {
		t.Error("announce datagram should be dropped (no reply)")
	}

	// Valid request but send fails → exercises the reply-send error branch.
	req := &codec.Message{
		MTID: 1, PVER: 1, MType: codec.MTypeRequest, MAddr: 1,
		MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupIdentity, ObjID: 0,
	}
	reqBytes, err := req.Encode()
	if err != nil {
		t.Fatalf("encode req: %v", err)
	}
	s.handleDatagram2(reqBytes, "x", func([]byte) error { return errors.New("boom") })
}

func TestServer_SetValue_BadValueType(t *testing.T) {
	s := newTestServer(t)
	// Level (1.2.2.0) is an Integer; a non-numeric value fails coercion in
	// encodeIncomingFromAny.
	if _, err := s.SetValue(context.Background(), "1.2.2.0", "not-a-number"); err == nil {
		t.Error("non-numeric value into Integer should error")
	}
}

func TestMutateEnum_DefaultOutOfRange(t *testing.T) {
	s := discardServer()
	e := &entry{acpType: codec.TypeEnum, param: &canonical.Parameter{
		Value: int64(1), Enumeration: strp("Off,On"), Default: int64(99), // > maxIdx
	}}
	b, err := s.applyMutation(e, codec.MethodSetDefValue, nil)
	if err != nil {
		t.Fatalf("setDef: %v", err)
	}
	if b[0] != 0 {
		t.Errorf("out-of-range default should fall back to 0, got %d", b[0])
	}
}

func TestMutateIPAddr_ClampToMin(t *testing.T) {
	s := discardServer()
	e := &entry{acpType: codec.TypeIPAddr, param: &canonical.Parameter{
		Value: "10.0.0.50", Minimum: "10.0.0.10", Maximum: "10.0.0.100", Default: "10.0.0.10",
	}}
	// 0.0.0.5 is below min → clamps up to 10.0.0.10.
	b, err := s.applyMutation(e, codec.MethodSetValue, []byte{0, 0, 0, 5})
	if err != nil {
		t.Fatalf("setValue: %v", err)
	}
	if e.param.Value != "10.0.0.10" {
		t.Errorf("clamp-to-min = %v, want 10.0.0.10 (bytes %v)", e.param.Value, b)
	}
}
