package acp1

import (
	"context"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
)

// These tests drive the socket-reachable Plugin methods through the in-memory
// fakeTransport (see client_test.go / identity_test.go) with canned device
// replies — covering the GetDeviceInfo / GetSlotInfo / GetValue paths and their
// not-connected + error branches without a live device.

func TestGetDeviceInfo_HappyPath(t *testing.T) {
	p, ft, mtid := newPluginWithClient(t)
	// Frame-status value = [num_slots, status_0, status_1, status_2].
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeReply, byte(codec.MethodGetValue),
			codec.GroupFrame, 0, []byte{3, 2, 2, 0}),
	}
	di, err := p.GetDeviceInfo(context.Background())
	if err != nil {
		t.Fatalf("GetDeviceInfo: %v", err)
	}
	if di.NumSlots != 3 {
		t.Errorf("NumSlots = %d, want 3", di.NumSlots)
	}
	if di.ProtocolVersion != 1 {
		t.Errorf("ProtocolVersion = %d, want 1", di.ProtocolVersion)
	}
}

func TestGetDeviceInfo_NotConnected(t *testing.T) {
	p := &Plugin{} // client == nil
	if _, err := p.GetDeviceInfo(context.Background()); err != consumer.ErrNotConnected {
		t.Fatalf("err = %v, want ErrNotConnected", err)
	}
}

func TestGetDeviceInfo_ErrorReply(t *testing.T) {
	p, ft, mtid := newPluginWithClient(t)
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeError, 3 /* transaction-timeout */, codec.GroupFrame, 0, nil),
	}
	if _, err := p.GetDeviceInfo(context.Background()); err == nil {
		t.Fatal("GetDeviceInfo: want error from error reply, got nil")
	}
}

func TestGetSlotInfo_HappyPath(t *testing.T) {
	p, ft, mtid := newPluginWithClient(t)
	// value[slot+1] is the slot's status byte; slot 1 -> value[2] = 2 (present).
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeReply, byte(codec.MethodGetValue),
			codec.GroupFrame, 0, []byte{4, 0, 2, 0, 3}),
	}
	si, err := p.GetSlotInfo(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetSlotInfo: %v", err)
	}
	if si.Slot != 1 {
		t.Errorf("Slot = %d, want 1", si.Slot)
	}
	if si.Status != consumer.SlotStatus(2) {
		t.Errorf("Status = %d, want 2 (present)", si.Status)
	}
}

func TestGetSlotInfo_NotConnected(t *testing.T) {
	p := &Plugin{}
	if _, err := p.GetSlotInfo(context.Background(), 0); err != consumer.ErrNotConnected {
		t.Fatalf("err = %v, want ErrNotConnected", err)
	}
}

func TestGetValue_NotConnected(t *testing.T) {
	p := &Plugin{}
	if _, err := p.GetValue(context.Background(), consumer.ValueRequest{Slot: 0, Group: "control", ID: 5}); err != consumer.ErrNotConnected {
		t.Fatalf("err = %v, want ErrNotConnected", err)
	}
}

// GetValue with no cached tree: the getValue reply arrives, the cold-cache
// meta fetch (getObject) returns an error, so the value falls back to the
// group decoder (control -> raw bytes). Exercises the cold-cache branch.
func TestGetValue_RawFallbackOnColdCache(t *testing.T) {
	p, ft, mtid := newPluginWithClient(t)
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeReply, byte(codec.MethodGetValue),
			codec.GroupControl, 5, []byte{0x07}),
		buildReply(t, mtid+2, codec.MTypeError, 17 /* instance not exist */, codec.GroupControl, 5, nil),
	}
	v, err := p.GetValue(context.Background(), consumer.ValueRequest{Slot: 0, Group: "control", ID: 5})
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if v.Kind != consumer.KindRaw {
		t.Errorf("Kind = %v, want KindRaw (group fallback)", v.Kind)
	}
	if len(v.Raw) != 1 || v.Raw[0] != 0x07 {
		t.Errorf("Raw = %v, want [7]", v.Raw)
	}
}

func TestGetValue_ErrorReply(t *testing.T) {
	p, ft, mtid := newPluginWithClient(t)
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeError, 20 /* no-read-access */, codec.GroupControl, 5, nil),
	}
	if _, err := p.GetValue(context.Background(), consumer.ValueRequest{Slot: 0, Group: "control", ID: 5}); err == nil {
		t.Fatal("GetValue: want error from error reply, got nil")
	}
}

func TestSetValue_NotConnected(t *testing.T) {
	p := &Plugin{}
	_, err := p.SetValue(context.Background(), consumer.ValueRequest{Slot: 0, Group: "control", ID: 5}, consumer.Value{Raw: []byte{1}})
	if err != consumer.ErrNotConnected {
		t.Fatalf("err = %v, want ErrNotConnected", err)
	}
}

// Raw escape hatch: val.Raw with no typed fields bypasses the codec (and the
// cold-cache meta fetch) and ships bytes as-is; the confirmed reply with no
// cached type decodes to KindRaw.
func TestSetValue_RawEscapeHatch(t *testing.T) {
	p, ft, mtid := newPluginWithClient(t)
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeReply, byte(codec.MethodSetValue),
			codec.GroupControl, 5, []byte{0x09}),
	}
	v, err := p.SetValue(context.Background(),
		consumer.ValueRequest{Slot: 0, Group: "control", ID: 5},
		consumer.Value{Raw: []byte{0x09}})
	if err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	if v.Kind != consumer.KindRaw || len(v.Raw) != 1 || v.Raw[0] != 0x09 {
		t.Errorf("confirmed = %+v, want KindRaw [9]", v)
	}
	if len(ft.sent) != 1 {
		t.Errorf("sent %d frames, want 1 (no meta fetch on raw path)", len(ft.sent))
	}
}

func TestSetValue_ErrorReply(t *testing.T) {
	p, ft, mtid := newPluginWithClient(t)
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeError, 19 /* no-write-access */, codec.GroupControl, 5, nil),
	}
	_, err := p.SetValue(context.Background(),
		consumer.ValueRequest{Slot: 0, Group: "control", ID: 5},
		consumer.Value{Raw: []byte{0x09}})
	if err == nil {
		t.Fatal("SetValue: want error from error reply, got nil")
	}
}

// Typed cold-cache path: no tree, so SetValue does a getObject meta fetch to
// learn the type, encodes the typed value, sends setValue, and decodes the
// confirmed echo. Exercises fetchObjectMeta + EncodeValueBytes + DecodeValueBytes.
func TestSetValue_TypedColdCache(t *testing.T) {
	p, ft, mtid := newPluginWithClient(t)
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeReply, byte(codec.MethodGetObject),
			codec.GroupControl, 5, integerObject(0, "Level")), // meta fetch
		buildReply(t, mtid+2, codec.MTypeReply, byte(codec.MethodSetValue),
			codec.GroupControl, 5, []byte{0x00, 0x05}), // confirmed int16 = 5
	}
	v, err := p.SetValue(context.Background(),
		consumer.ValueRequest{Slot: 0, Group: "control", ID: 5},
		consumer.Value{Kind: consumer.KindInt, Int: 5})
	if err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	if v.Int != 5 {
		t.Errorf("confirmed Int = %d, want 5", v.Int)
	}
}
