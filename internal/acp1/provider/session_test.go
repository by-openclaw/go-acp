package acp1

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"dhs/internal/export/canonical"
	"dhs/internal/acp1/codec"
)

// newTestServer builds a server with a hand-crafted tree containing two
// slots — slot 0 as the rack controller (Frame object) and slot 1 as a
// card with a small set of objects covering the common types.
func newTestServer(t *testing.T) *server {
	t.Helper()
	rw := canonical.AccessReadWrite
	r := canonical.AccessRead

	hint := func(s string) *string { return &s }

	slot0 := &canonical.Node{
		Header: canonical.Header{
			Number:     0,
			Identifier: "slot-0",
			OID:        "1.1",
			Access:     r,
			Children: []canonical.Element{
				&canonical.Node{
					Header: canonical.Header{
						Number:     6,
						Identifier: "frame",
						OID:        "1.1.6",
						Access:     r,
						Children: []canonical.Element{
							&canonical.Parameter{
								Header: canonical.Header{
									Number:     0,
									Identifier: "frameStatus",
									OID:        "1.1.6.0",
									Access:     r,
									Children:   canonical.EmptyChildren(),
								},
								Type:   canonical.ParamOctets,
								Format: hint("frame"),
								Value:  []any{int64(2), int64(2), int64(0), int64(0)},
							},
						},
					},
				},
			},
		},
	}

	slot1 := &canonical.Node{
		Header: canonical.Header{
			Number:     1,
			Identifier: "slot-1",
			OID:        "1.2",
			Access:     r,
			Children: []canonical.Element{
				// Identity group (read-only label).
				&canonical.Node{
					Header: canonical.Header{
						Number: 1, Identifier: "identity", OID: "1.2.1",
						Access: r,
						Children: []canonical.Element{
							&canonical.Parameter{
								Header: canonical.Header{
									Number: 0, Identifier: "Model", OID: "1.2.1.0",
									Access: r, Children: canonical.EmptyChildren(),
								},
								Type:  canonical.ParamString,
								Value: "GIO-12",
							},
						},
					},
				},
				// Control group (writable level).
				&canonical.Node{
					Header: canonical.Header{
						Number: 2, Identifier: "control", OID: "1.2.2",
						Access: r,
						Children: []canonical.Element{
							&canonical.Parameter{
								Header: canonical.Header{
									Number: 0, Identifier: "Level", OID: "1.2.2.0",
									Access: rw, Children: canonical.EmptyChildren(),
								},
								Type:  canonical.ParamInteger,
								Value: int64(-6),
								Minimum: int64(-60), Maximum: int64(12),
								Step: int64(1), Default: int64(0),
							},
						},
					},
				},
			},
		},
	}

	exp := &canonical.Export{Root: &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "device", OID: "1", Access: r,
			Children: []canonical.Element{slot0, slot1},
		},
	}}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return newServer(logger, exp)
}

func TestSession_GetValue_ReadOnlyString(t *testing.T) {
	s := newTestServer(t)
	req := &codec.Message{
		MTID: 42, MType: codec.MTypeRequest, MAddr: 1,
		MCode: byte(codec.MethodGetValue),
		ObjGroup: codec.GroupIdentity, ObjID: 0,
	}
	rep, _ := s.handleRequest(req)
	if rep == nil {
		t.Fatal("nil reply")
	}
	if rep.MType != codec.MTypeReply {
		t.Fatalf("codec.MType=%d want reply", rep.MType)
	}
	if rep.MTID != 42 {
		t.Fatalf("MTID mirror broken: got %d", rep.MTID)
	}
	// Value should be NUL-terminated "GIO-12"
	want := []byte("GIO-12\x00")
	if string(rep.Value) != string(want) {
		t.Fatalf("value=%q want %q", rep.Value, want)
	}
}

func TestSession_GetObject_Integer(t *testing.T) {
	s := newTestServer(t)
	req := &codec.Message{
		MTID: 7, MType: codec.MTypeRequest, MAddr: 1,
		MCode: byte(codec.MethodGetObject),
		ObjGroup: codec.GroupControl, ObjID: 0,
	}
	rep, _ := s.handleRequest(req)
	if rep == nil || rep.MType != codec.MTypeReply {
		t.Fatalf("bad reply: %+v", rep)
	}
	o, err := codec.DecodeObject(rep.Value)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if o.Type != codec.TypeInteger {
		t.Fatalf("type=%d", o.Type)
	}
	if o.IntVal != -6 || o.MinInt != -60 || o.MaxInt != 12 {
		t.Errorf("integer fields: val=%d min=%d max=%d", o.IntVal, o.MinInt, o.MaxInt)
	}
	if o.Label != "Level" {
		t.Errorf("label=%q", o.Label)
	}
}

func TestSession_Root_Synthesised(t *testing.T) {
	s := newTestServer(t)
	// getValue on Root returns boot_mode byte (0 = normal).
	req := &codec.Message{
		MTID: 1, MType: codec.MTypeRequest, MAddr: 1,
		MCode: byte(codec.MethodGetValue),
		ObjGroup: codec.GroupRoot, ObjID: 0,
	}
	rep, _ := s.handleRequest(req)
	if rep == nil || rep.MType != codec.MTypeReply {
		t.Fatalf("bad reply: %+v", rep)
	}
	if len(rep.Value) != 1 || rep.Value[0] != 0 {
		t.Fatalf("boot_mode bytes=%v want [0]", rep.Value)
	}

	// getObject on Root returns 9 properties: type(0), numProps(9),
	// access(1=read), boot_mode(0), numIdentity(1), numControl(1),
	// numStatus(0), numAlarm(0), numFile(0).
	req.MCode = byte(codec.MethodGetObject)
	rep, _ = s.handleRequest(req)
	if rep == nil || rep.MType != codec.MTypeReply {
		t.Fatalf("bad reply: %+v", rep)
	}
	want := []byte{0, 9, 1, 0, 1, 1, 0, 0, 0}
	if string(rep.Value) != string(want) {
		t.Fatalf("root object bytes=%v want %v", rep.Value, want)
	}
}

func TestSession_UnknownObject_InstanceError(t *testing.T) {
	s := newTestServer(t)
	// control group exists, id=99 does not.
	req := &codec.Message{
		MTID: 1, MType: codec.MTypeRequest, MAddr: 1,
		MCode: byte(codec.MethodGetValue),
		ObjGroup: codec.GroupControl, ObjID: 99,
	}
	rep, _ := s.handleRequest(req)
	if rep.MType != codec.MTypeError {
		t.Fatalf("want error, got codec.MType=%d", rep.MType)
	}
	if rep.MCode != byte(codec.OErrInstanceNoExist) {
		t.Errorf("mcode=%d want %d", rep.MCode, codec.OErrInstanceNoExist)
	}
}

func TestSession_UnknownGroup_GroupError(t *testing.T) {
	s := newTestServer(t)
	// Alarm group has no entries on slot 1.
	req := &codec.Message{
		MTID: 1, MType: codec.MTypeRequest, MAddr: 1,
		MCode: byte(codec.MethodGetValue),
		ObjGroup: codec.GroupAlarm, ObjID: 0,
	}
	rep, _ := s.handleRequest(req)
	if rep.MType != codec.MTypeError {
		t.Fatalf("want error")
	}
	if rep.MCode != byte(codec.OErrGroupNoExist) {
		t.Errorf("mcode=%d want %d", rep.MCode, codec.OErrGroupNoExist)
	}
}

func TestSession_SetValue_RoundTrip(t *testing.T) {
	s := newTestServer(t)
	// Control slot-1 id=0 is Level (int16, min=-60 max=12, currently -6).
	// Write 5, expect echo 5.
	req := &codec.Message{
		MTID: 1, MType: codec.MTypeRequest, MAddr: 1,
		MCode:    byte(codec.MethodSetValue),
		ObjGroup: codec.GroupControl, ObjID: 0,
		Value: []byte{0x00, 0x05},
	}
	rep, _ := s.handleRequest(req)
	if rep.MType != codec.MTypeReply {
		t.Fatalf("bad reply: %+v", rep)
	}
	if string(rep.Value) != string([]byte{0x00, 0x05}) {
		t.Fatalf("echo bytes=%x want 0005", rep.Value)
	}
	// Re-read via getValue to confirm persistence.
	req.MCode = byte(codec.MethodGetValue)
	req.Value = nil
	rep, _ = s.handleRequest(req)
	if string(rep.Value) != string([]byte{0x00, 0x05}) {
		t.Fatalf("getValue after set=%x want 0005", rep.Value)
	}
}

func TestSession_SetValue_ClampsToMax(t *testing.T) {
	s := newTestServer(t)
	// Level max=12; request 100 -> clamped to 12.
	req := &codec.Message{
		MTID: 1, MType: codec.MTypeRequest, MAddr: 1,
		MCode:    byte(codec.MethodSetValue),
		ObjGroup: codec.GroupControl, ObjID: 0,
		Value: []byte{0x00, 0x64}, // 100
	}
	rep, _ := s.handleRequest(req)
	if rep.MType != codec.MTypeReply {
		t.Fatalf("bad reply: %+v", rep)
	}
	if string(rep.Value) != string([]byte{0x00, 0x0C}) {
		t.Fatalf("clamp bytes=%x want 000C (12)", rep.Value)
	}
}

func TestSession_SetIncDec_RespectsStepAndLimits(t *testing.T) {
	s := newTestServer(t)
	// Level: start at -6, step=1, min=-60, max=12.
	req := &codec.Message{
		MTID: 1, MType: codec.MTypeRequest, MAddr: 1,
		MCode:    byte(codec.MethodSetIncValue),
		ObjGroup: codec.GroupControl, ObjID: 0,
	}
	// Inc from -6 -> -5.
	rep, _ := s.handleRequest(req)
	if rep.MType != codec.MTypeReply || string(rep.Value) != string([]byte{0xFF, 0xFB}) {
		t.Fatalf("setInc bytes=%x want FFFB (-5)", rep.Value)
	}
	// Dec from -5 -> -6.
	req.MCode = byte(codec.MethodSetDecValue)
	rep, _ = s.handleRequest(req)
	if string(rep.Value) != string([]byte{0xFF, 0xFA}) {
		t.Fatalf("setDec bytes=%x want FFFA (-6)", rep.Value)
	}
}

func TestSession_SetDefValue_ResetsToDefault(t *testing.T) {
	s := newTestServer(t)
	// Level default=0.
	// First set to 5 so we can observe the reset.
	setReq := &codec.Message{
		MTID: 1, MType: codec.MTypeRequest, MAddr: 1,
		MCode:    byte(codec.MethodSetValue),
		ObjGroup: codec.GroupControl, ObjID: 0,
		Value: []byte{0x00, 0x05},
	}
	if _, ann := s.handleRequest(setReq); ann == nil {
		t.Fatal("setValue should have produced an announcement")
	}

	defReq := &codec.Message{
		MTID: 2, MType: codec.MTypeRequest, MAddr: 1,
		MCode:    byte(codec.MethodSetDefValue),
		ObjGroup: codec.GroupControl, ObjID: 0,
	}
	rep, ann := s.handleRequest(defReq)
	if rep.MType != codec.MTypeReply || string(rep.Value) != string([]byte{0x00, 0x00}) {
		t.Fatalf("setDef bytes=%x want 0000", rep.Value)
	}
	if ann == nil || ann.MTID != 0 || ann.MType != codec.MTypeReply {
		t.Fatalf("announce missing or wrong shape: %+v", ann)
	}
	if string(ann.Value) != string([]byte{0x00, 0x00}) {
		t.Errorf("announce value=%x want 0000", ann.Value)
	}
}

func TestSession_SetValue_DeniedOnReadOnly(t *testing.T) {
	s := newTestServer(t)
	// Identity slot-1 group=1 id=0 is Model (read-only string).
	req := &codec.Message{
		MTID: 1, MType: codec.MTypeRequest, MAddr: 1,
		MCode:    byte(codec.MethodSetValue),
		ObjGroup: codec.GroupIdentity, ObjID: 0,
		Value: []byte("X\x00"),
	}
	rep, _ := s.handleRequest(req)
	if rep.MType != codec.MTypeError {
		t.Fatalf("want error, got %+v", rep)
	}
	if rep.MCode != byte(codec.OErrNoWriteAccess) {
		t.Errorf("mcode=%d want %d (NoWriteAccess)", rep.MCode, codec.OErrNoWriteAccess)
	}
}

func TestServer_SetValueAPIPath(t *testing.T) {
	s := newTestServer(t)
	// API-driven setValue uses the Provider interface.
	stored, err := s.SetValue(context.Background(), "1.2.2.0", int64(7))
	if err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	if stored.(int64) != 7 {
		t.Fatalf("stored=%v want 7", stored)
	}
}

func TestSession_MethodSupport_InvalidForAlarm(t *testing.T) {
	// Alarm objects do not support setValue per the spec matrix.
	// Verify methodSupported table directly.
	if methodSupported(codec.TypeAlarm, codec.MethodSetValue) {
		t.Error("Alarm should not support setValue")
	}
	if methodSupported(codec.TypeEnum, codec.MethodSetIncValue) {
		t.Error("Enum should not support setIncValue")
	}
	if !methodSupported(codec.TypeInteger, codec.MethodSetDefValue) {
		t.Error("Integer should support setDefValue")
	}
	if !methodSupported(codec.TypeFrame, codec.MethodGetValue) {
		t.Error("Frame should support getValue")
	}
}

func TestSession_AccessCheck(t *testing.T) {
	if checkAccess(codec.AccessRead, codec.MethodSetValue) != codec.OErrNoWriteAccess {
		t.Error("read-only should deny write")
	}
	if checkAccess(codec.AccessWrite, codec.MethodGetValue) != codec.OErrNoReadAccess {
		t.Error("write-only should deny read")
	}
	if checkAccess(codec.AccessRead|codec.AccessWrite, codec.MethodSetDefValue) != codec.OErrNoSetDefAccess {
		t.Error("no-setDef access should deny setDefValue")
	}
	if checkAccess(codec.AccessRead|codec.AccessWrite|codec.AccessSetDef, codec.MethodSetDefValue) != 0 {
		t.Error("all-access should permit setDefValue")
	}
}

func TestSession_NonRequestIgnored(t *testing.T) {
	s := newTestServer(t)
	for _, mt := range []codec.MType{codec.MTypeAnnounce, codec.MTypeReply, codec.MTypeError} {
		req := &codec.Message{MTID: 1, MType: mt, MAddr: 1}
		if rep, _ := s.handleRequest(req); rep != nil {
			t.Errorf("codec.MType=%d should be dropped, got %+v", mt, rep)
		}
	}
}
