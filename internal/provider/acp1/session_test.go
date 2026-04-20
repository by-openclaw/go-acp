package acp1

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"acp/internal/export/canonical"
	iacp1 "acp/internal/protocol/acp1"
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
	req := &iacp1.Message{
		MTID: 42, MType: iacp1.MTypeRequest, MAddr: 1,
		MCode: byte(iacp1.MethodGetValue),
		ObjGroup: iacp1.GroupIdentity, ObjID: 0,
	}
	rep := s.handleRequest(req)
	if rep == nil {
		t.Fatal("nil reply")
	}
	if rep.MType != iacp1.MTypeReply {
		t.Fatalf("MType=%d want reply", rep.MType)
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
	req := &iacp1.Message{
		MTID: 7, MType: iacp1.MTypeRequest, MAddr: 1,
		MCode: byte(iacp1.MethodGetObject),
		ObjGroup: iacp1.GroupControl, ObjID: 0,
	}
	rep := s.handleRequest(req)
	if rep == nil || rep.MType != iacp1.MTypeReply {
		t.Fatalf("bad reply: %+v", rep)
	}
	o, err := iacp1.DecodeObject(rep.Value)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if o.Type != iacp1.TypeInteger {
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
	req := &iacp1.Message{
		MTID: 1, MType: iacp1.MTypeRequest, MAddr: 1,
		MCode: byte(iacp1.MethodGetValue),
		ObjGroup: iacp1.GroupRoot, ObjID: 0,
	}
	rep := s.handleRequest(req)
	if rep == nil || rep.MType != iacp1.MTypeReply {
		t.Fatalf("bad reply: %+v", rep)
	}
	if len(rep.Value) != 1 || rep.Value[0] != 0 {
		t.Fatalf("boot_mode bytes=%v want [0]", rep.Value)
	}

	// getObject on Root returns 9 properties: type(0), numProps(9),
	// access(1=read), boot_mode(0), numIdentity(1), numControl(1),
	// numStatus(0), numAlarm(0), numFile(0).
	req.MCode = byte(iacp1.MethodGetObject)
	rep = s.handleRequest(req)
	if rep == nil || rep.MType != iacp1.MTypeReply {
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
	req := &iacp1.Message{
		MTID: 1, MType: iacp1.MTypeRequest, MAddr: 1,
		MCode: byte(iacp1.MethodGetValue),
		ObjGroup: iacp1.GroupControl, ObjID: 99,
	}
	rep := s.handleRequest(req)
	if rep.MType != iacp1.MTypeError {
		t.Fatalf("want error, got MType=%d", rep.MType)
	}
	if rep.MCode != byte(iacp1.OErrInstanceNoExist) {
		t.Errorf("mcode=%d want %d", rep.MCode, iacp1.OErrInstanceNoExist)
	}
}

func TestSession_UnknownGroup_GroupError(t *testing.T) {
	s := newTestServer(t)
	// Alarm group has no entries on slot 1.
	req := &iacp1.Message{
		MTID: 1, MType: iacp1.MTypeRequest, MAddr: 1,
		MCode: byte(iacp1.MethodGetValue),
		ObjGroup: iacp1.GroupAlarm, ObjID: 0,
	}
	rep := s.handleRequest(req)
	if rep.MType != iacp1.MTypeError {
		t.Fatalf("want error")
	}
	if rep.MCode != byte(iacp1.OErrGroupNoExist) {
		t.Errorf("mcode=%d want %d", rep.MCode, iacp1.OErrGroupNoExist)
	}
}

func TestSession_SetValue_RoundTrip(t *testing.T) {
	s := newTestServer(t)
	// Control slot-1 id=0 is Level (int16, min=-60 max=12, currently -6).
	// Write 5, expect echo 5.
	req := &iacp1.Message{
		MTID: 1, MType: iacp1.MTypeRequest, MAddr: 1,
		MCode:    byte(iacp1.MethodSetValue),
		ObjGroup: iacp1.GroupControl, ObjID: 0,
		Value: []byte{0x00, 0x05},
	}
	rep := s.handleRequest(req)
	if rep.MType != iacp1.MTypeReply {
		t.Fatalf("bad reply: %+v", rep)
	}
	if string(rep.Value) != string([]byte{0x00, 0x05}) {
		t.Fatalf("echo bytes=%x want 0005", rep.Value)
	}
	// Re-read via getValue to confirm persistence.
	req.MCode = byte(iacp1.MethodGetValue)
	req.Value = nil
	rep = s.handleRequest(req)
	if string(rep.Value) != string([]byte{0x00, 0x05}) {
		t.Fatalf("getValue after set=%x want 0005", rep.Value)
	}
}

func TestSession_SetValue_ClampsToMax(t *testing.T) {
	s := newTestServer(t)
	// Level max=12; request 100 -> clamped to 12.
	req := &iacp1.Message{
		MTID: 1, MType: iacp1.MTypeRequest, MAddr: 1,
		MCode:    byte(iacp1.MethodSetValue),
		ObjGroup: iacp1.GroupControl, ObjID: 0,
		Value: []byte{0x00, 0x64}, // 100
	}
	rep := s.handleRequest(req)
	if rep.MType != iacp1.MTypeReply {
		t.Fatalf("bad reply: %+v", rep)
	}
	if string(rep.Value) != string([]byte{0x00, 0x0C}) {
		t.Fatalf("clamp bytes=%x want 000C (12)", rep.Value)
	}
}

func TestSession_SetIncDec_RespectsStepAndLimits(t *testing.T) {
	s := newTestServer(t)
	// Level: start at -6, step=1, min=-60, max=12.
	req := &iacp1.Message{
		MTID: 1, MType: iacp1.MTypeRequest, MAddr: 1,
		MCode:    byte(iacp1.MethodSetIncValue),
		ObjGroup: iacp1.GroupControl, ObjID: 0,
	}
	// Inc from -6 -> -5.
	rep := s.handleRequest(req)
	if rep.MType != iacp1.MTypeReply || string(rep.Value) != string([]byte{0xFF, 0xFB}) {
		t.Fatalf("setInc bytes=%x want FFFB (-5)", rep.Value)
	}
	// Dec from -5 -> -6.
	req.MCode = byte(iacp1.MethodSetDecValue)
	rep = s.handleRequest(req)
	if string(rep.Value) != string([]byte{0xFF, 0xFA}) {
		t.Fatalf("setDec bytes=%x want FFFA (-6)", rep.Value)
	}
}

func TestSession_SetDefValue_ResetsToDefault(t *testing.T) {
	s := newTestServer(t)
	// Level default=0.
	// First set to 5 so we can observe the reset.
	setReq := &iacp1.Message{
		MTID: 1, MType: iacp1.MTypeRequest, MAddr: 1,
		MCode:    byte(iacp1.MethodSetValue),
		ObjGroup: iacp1.GroupControl, ObjID: 0,
		Value: []byte{0x00, 0x05},
	}
	_ = s.handleRequest(setReq)

	defReq := &iacp1.Message{
		MTID: 2, MType: iacp1.MTypeRequest, MAddr: 1,
		MCode:    byte(iacp1.MethodSetDefValue),
		ObjGroup: iacp1.GroupControl, ObjID: 0,
	}
	rep := s.handleRequest(defReq)
	if rep.MType != iacp1.MTypeReply || string(rep.Value) != string([]byte{0x00, 0x00}) {
		t.Fatalf("setDef bytes=%x want 0000", rep.Value)
	}
}

func TestSession_SetValue_DeniedOnReadOnly(t *testing.T) {
	s := newTestServer(t)
	// Identity slot-1 group=1 id=0 is Model (read-only string).
	req := &iacp1.Message{
		MTID: 1, MType: iacp1.MTypeRequest, MAddr: 1,
		MCode:    byte(iacp1.MethodSetValue),
		ObjGroup: iacp1.GroupIdentity, ObjID: 0,
		Value: []byte("X\x00"),
	}
	rep := s.handleRequest(req)
	if rep.MType != iacp1.MTypeError {
		t.Fatalf("want error, got %+v", rep)
	}
	if rep.MCode != byte(iacp1.OErrNoWriteAccess) {
		t.Errorf("mcode=%d want %d (NoWriteAccess)", rep.MCode, iacp1.OErrNoWriteAccess)
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
	if methodSupported(iacp1.TypeAlarm, iacp1.MethodSetValue) {
		t.Error("Alarm should not support setValue")
	}
	if methodSupported(iacp1.TypeEnum, iacp1.MethodSetIncValue) {
		t.Error("Enum should not support setIncValue")
	}
	if !methodSupported(iacp1.TypeInteger, iacp1.MethodSetDefValue) {
		t.Error("Integer should support setDefValue")
	}
	if !methodSupported(iacp1.TypeFrame, iacp1.MethodGetValue) {
		t.Error("Frame should support getValue")
	}
}

func TestSession_AccessCheck(t *testing.T) {
	if checkAccess(iacp1.AccessRead, iacp1.MethodSetValue) != iacp1.OErrNoWriteAccess {
		t.Error("read-only should deny write")
	}
	if checkAccess(iacp1.AccessWrite, iacp1.MethodGetValue) != iacp1.OErrNoReadAccess {
		t.Error("write-only should deny read")
	}
	if checkAccess(iacp1.AccessRead|iacp1.AccessWrite, iacp1.MethodSetDefValue) != iacp1.OErrNoSetDefAccess {
		t.Error("no-setDef access should deny setDefValue")
	}
	if checkAccess(iacp1.AccessRead|iacp1.AccessWrite|iacp1.AccessSetDef, iacp1.MethodSetDefValue) != 0 {
		t.Error("all-access should permit setDefValue")
	}
}

func TestSession_NonRequestIgnored(t *testing.T) {
	s := newTestServer(t)
	for _, mt := range []iacp1.MType{iacp1.MTypeAnnounce, iacp1.MTypeReply, iacp1.MTypeError} {
		req := &iacp1.Message{MTID: 1, MType: mt, MAddr: 1}
		if rep := s.handleRequest(req); rep != nil {
			t.Errorf("MType=%d should be dropped, got %+v", mt, rep)
		}
	}
}
