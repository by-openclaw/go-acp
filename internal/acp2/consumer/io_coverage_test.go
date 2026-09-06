package acp2

import (
	"context"
	"dhs/internal/plugin"
	"math"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/transport"

	"dhs/internal/acp2/codec"
)

// io_coverage_test.go rounds out coverage of the socket-reachable
// paths: the keep-alive prober + watchdog (live), the warm-restart
// reconnect loop, the full set of value-type decode/encode branches via
// Walk / GetValue / SetValue, and the small public accessors.

// ---- value-type walk: every ObjType / NumberType decode branch ----

func TestWalk_AllValueTypes(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		if req.Func != codec.ACP2FuncGetObject {
			return errorReply(codec.ErrProtocol)
		}
		switch req.ObjID {
		case 1: // root with one child per type
			return objectReply(1, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
				codec.MakeStringProperty(codec.PIDLabel, "ROOT"),
				childrenProp(10, 11, 12, 13, 14, 15),
			})
		case 10: // float number with min/max/step/default constraints
			return objectReply(10, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNumber)),
				codec.MakeStringProperty(codec.PIDLabel, "Gain"),
				u32Prop(codec.PIDNumberType, 0, uint32(codec.NumTypeFloat)),
				codec.MakeValueProperty(codec.PIDValue, codec.NumTypeFloat, f32be(1.5)),
				codec.MakeValueProperty(codec.PIDMinValue, codec.NumTypeFloat, f32be(0)),
				codec.MakeValueProperty(codec.PIDMaxValue, codec.NumTypeFloat, f32be(10)),
				codec.MakeValueProperty(codec.PIDStepSize, codec.NumTypeFloat, f32be(0.5)),
				codec.MakeValueProperty(codec.PIDDefaultValue, codec.NumTypeFloat, f32be(2.0)),
				codec.MakeStringProperty(codec.PIDUnit, "dB"),
			})
		case 11: // s64 number
			return objectReply(11, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNumber)),
				codec.MakeStringProperty(codec.PIDLabel, "Offset"),
				u32Prop(codec.PIDNumberType, 0, uint32(codec.NumTypeS64)),
				codec.MakeValueProperty(codec.PIDValue, codec.NumTypeS64, s64be(-1234)),
				codec.MakeValueProperty(codec.PIDMinValue, codec.NumTypeS64, s64be(-9999)),
			})
		case 12: // enum with options
			return objectReply(12, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeEnum)),
				codec.MakeStringProperty(codec.PIDLabel, "Mode"),
				u32Prop(codec.PIDNumberType, 0, uint32(codec.NumTypePreset)),
				optionsProp(map[uint32]string{0: "Off", 1: "On", 2: "Auto"}),
				codec.MakeValueProperty(codec.PIDValue, codec.NumTypePreset, beU32Bytes(1)),
				codec.MakeValueProperty(codec.PIDDefaultValue, codec.NumTypePreset, beU32Bytes(2)),
			})
		case 13: // ipv4
			return objectReply(13, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeIPv4)),
				codec.MakeStringProperty(codec.PIDLabel, "IP"),
				u32Prop(codec.PIDNumberType, 0, uint32(codec.NumTypeIPv4)),
				codec.MakeValueProperty(codec.PIDValue, codec.NumTypeIPv4, []byte{10, 0, 0, 5}),
			})
		case 14: // string with max length + alarm props
			return objectReply(14, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeString)),
				codec.MakeStringProperty(codec.PIDLabel, "Name"),
				u32Prop(codec.PIDStringMaxLength, 0, 32),
				codec.MakeStringProperty(codec.PIDValue, "hello"),
				codec.Property{PID: codec.PIDEventTag, VType: 7, PLen: 4},
				u32Prop(codec.PIDEventPrio, 0, 2),
			})
		case 15: // preset child
			return objectReply(15, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypePreset)),
				codec.MakeStringProperty(codec.PIDLabel, "Preset"),
				codec.MakeValueProperty(codec.PIDValue, codec.NumTypePreset, beU32Bytes(0)),
				u32Prop(codec.PIDPresetParent, 0, 1),
			})
		default:
			return errorReply(codec.ErrInvalidObjID)
		}
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	objs, err := p.Walk(context.Background(), 0)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	byLabel := map[string]consumer.Object{}
	for _, o := range objs {
		byLabel[o.Label] = o
	}

	if g := byLabel["Gain"]; g.Kind != consumer.KindFloat || math.Abs(g.Value.Float-1.5) > 1e-6 {
		t.Errorf("Gain = %+v, want float 1.5", g.Value)
	}
	if g := byLabel["Gain"]; g.Unit != "dB" {
		t.Errorf("Gain unit = %q, want dB", g.Unit)
	}
	if o := byLabel["Offset"]; o.Kind != consumer.KindInt || o.Value.Int != -1234 {
		t.Errorf("Offset = %+v, want int -1234", o.Value)
	}
	if m := byLabel["Mode"]; m.Kind != consumer.KindEnum || m.Value.Str != "On" {
		t.Errorf("Mode = %+v, want enum On", m.Value)
	}
	if ip := byLabel["IP"]; ip.Kind != consumer.KindIPAddr || ip.Value.IPAddr != [4]byte{10, 0, 0, 5} {
		t.Errorf("IP = %+v, want 10.0.0.5", ip.Value)
	}
	if n := byLabel["Name"]; n.Kind != consumer.KindString || n.Value.Str != "hello" {
		t.Errorf("Name = %+v, want string hello", n.Value)
	}
	if _, ok := byLabel["Preset"]; !ok {
		t.Error("Preset not walked")
	}
}

// TestGetValue_EnumWithLabel reads an enum and resolves the option label
// from the walked tree's options map.
func TestGetValue_EnumWithLabel(t *testing.T) {
	srv, host, port := newFakeServer(t)
	base := func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		switch req.ObjID {
		case 1:
			return objectReply(1, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
				codec.MakeStringProperty(codec.PIDLabel, "ROOT"),
				childrenProp(2),
			})
		case 2:
			return objectReply(2, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeEnum)),
				codec.MakeStringProperty(codec.PIDLabel, "Mode"),
				u32Prop(codec.PIDNumberType, 0, uint32(codec.NumTypePreset)),
				optionsProp(map[uint32]string{0: "Off", 1: "On"}),
				codec.MakeValueProperty(codec.PIDValue, codec.NumTypePreset, beU32Bytes(0)),
			})
		}
		return errorReply(codec.ErrInvalidObjID)
	}
	srv.onACP2 = wrapWithGetProperty(base, func(req *codec.ACP2Message) *codec.ACP2Message {
		return propertyReply(codec.ACP2FuncGetProperty, 2,
			codec.MakeValueProperty(codec.PIDValue, codec.NumTypePreset, beU32Bytes(1)))
	})
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	if _, err := p.Walk(context.Background(), 1); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	val, err := p.GetValue(context.Background(), consumer.ValueRequest{Slot: 1, Label: "Mode"})
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if val.Kind != consumer.KindEnum || val.Str != "On" {
		t.Errorf("value = %+v, want enum On", val)
	}
}

// TestSetValue_Enum sets an enum by label, exercising the reverse-enum
// resolution + encodeSetProperty enum branch.
func TestSetValue_Enum(t *testing.T) {
	srv, host, port := newFakeServer(t)
	base := func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		switch req.ObjID {
		case 1:
			return objectReply(1, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
				codec.MakeStringProperty(codec.PIDLabel, "ROOT"),
				childrenProp(2),
			})
		case 2:
			return objectReply(2, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeEnum)),
				codec.MakeStringProperty(codec.PIDLabel, "Mode"),
				u32Prop(codec.PIDNumberType, 0, uint32(codec.NumTypePreset)),
				optionsProp(map[uint32]string{0: "Off", 1: "On"}),
				codec.MakeValueProperty(codec.PIDValue, codec.NumTypePreset, beU32Bytes(0)),
			})
		}
		return errorReply(codec.ErrInvalidObjID)
	}
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		if req.Func == codec.ACP2FuncSetProperty {
			var prop codec.Property
			if len(req.Properties) > 0 {
				prop = req.Properties[0]
			}
			return propertyReply(codec.ACP2FuncSetProperty, 2, prop)
		}
		return base(slot, req)
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	if _, err := p.Walk(context.Background(), 1); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	val, err := p.SetValue(context.Background(),
		consumer.ValueRequest{Slot: 1, Label: "Mode"},
		consumer.Value{Kind: consumer.KindString, Str: "On"})
	if err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	// Confirmed value should resolve back to label "On" (wire idx 1).
	if val.Str != "On" {
		t.Errorf("confirmed = %+v, want On", val)
	}
}

// TestSetValue_IPv4 sets an IPv4 value from a dotted string.
func TestSetValue_IPv4(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		switch req.Func {
		case codec.ACP2FuncGetObject:
			return objectReply(3, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeIPv4)),
				u32Prop(codec.PIDNumberType, 0, uint32(codec.NumTypeIPv4)),
			})
		case codec.ACP2FuncSetProperty:
			var prop codec.Property
			if len(req.Properties) > 0 {
				prop = req.Properties[0]
			}
			return propertyReply(codec.ACP2FuncSetProperty, 3, prop)
		}
		return errorReply(codec.ErrProtocol)
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	val, err := p.SetValue(context.Background(),
		consumer.ValueRequest{Slot: 0, ID: 3},
		consumer.Value{Kind: consumer.KindString, Str: "192.168.1.20"})
	if err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	if val.Kind != consumer.KindIPAddr || val.IPAddr != [4]byte{192, 168, 1, 20} {
		t.Errorf("confirmed = %+v, want 192.168.1.20", val)
	}
}

// ---- keep-alive prober + watchdog (live) ----

func TestKeepAlive_ProberAndWatchdog(t *testing.T) {
	srv, host, port := newFakeServer(t)
	defer srv.stop()

	p := &Plugin{logger: testLogger()}
	// Fast cadence so the prober + watchdog actually run within the test.
	p.SetKeepAlive(consumer.KeepAliveConfig{
		Interval: 20 * time.Millisecond,
		Timeout:  60 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.Connect(ctx, host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	// Wait for at least one prober cycle to land (lastRx advances).
	ok := waitFor(t, 2*time.Second, func() bool {
		return p.SessionLive()
	})
	if !ok {
		t.Fatal("session never reported live after keepalive probes")
	}
	// The prober's AN2 GetSlotInfo(0) reply updates slot 0 status.
	si, err := p.GetSlotInfo(context.Background(), 0)
	if err != nil {
		t.Fatalf("GetSlotInfo(0): %v", err)
	}
	if si.Status != consumer.SlotPresent {
		t.Errorf("slot 0 status = %v, want present", si.Status)
	}
}

// ---- reconnect: server drops the connection, plugin re-dials ----

func TestReconnect_WarmRestart(t *testing.T) {
	srv, host, port := newFakeServer(t)
	defer srv.stop()

	p := &Plugin{logger: testLogger()}
	p.SetKeepAlive(consumer.KeepAliveConfig{
		Interval: consumer.DisableInterval,
		Timeout:  consumer.DisableTimeout,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.Connect(ctx, host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	p.mu.Lock()
	first := p.session
	p.mu.Unlock()

	// Register a subscription so the reconnect replays it.
	if err := p.Subscribe(consumer.ValueRequest{Slot: -1, ID: -1}, func(consumer.Event) {}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Drop every server-side connection — the client readLoop sees EOF,
	// session.Done() fires, and the reconnect loop re-dials the still-
	// listening server.
	srv.mu.Lock()
	for _, c := range srv.conns {
		_ = c.Close()
	}
	srv.conns = nil
	srv.mu.Unlock()

	// Wait for a fresh session (different pointer) to come up.
	ok := waitFor(t, 5*time.Second, func() bool {
		p.mu.Lock()
		s := p.session
		p.mu.Unlock()
		return s != nil && s != first
	})
	if !ok {
		t.Fatal("session not re-established after drop")
	}

	// The reconnected session should still serve requests.
	di, err := p.GetDeviceInfo(context.Background())
	if err != nil {
		t.Fatalf("GetDeviceInfo after reconnect: %v", err)
	}
	if di.NumSlots != 2 {
		t.Errorf("NumSlots after reconnect = %d, want 2", di.NumSlots)
	}
}

// ---- small accessors + factory ----

func TestFactory_NewAndMeta(t *testing.T) {
	f := &Factory{}
	meta := f.Meta()
	if meta.Name != "acp2" {
		t.Errorf("meta.Name = %q, want acp2", meta.Name)
	}
	if meta.DefaultPort != codec.DefaultPort {
		t.Errorf("meta.DefaultPort = %d, want %d", meta.DefaultPort, codec.DefaultPort)
	}
	pl := f.New(plugin.Deps{Logger: testLogger()})
	if pl == nil {
		t.Fatal("New returned nil")
	}
}

func TestAccessors_RecorderProfileWalkProgress(t *testing.T) {
	srv, host, port := newFakeServer(t)
	defer srv.stop()

	p := &Plugin{logger: testLogger()}
	p.SetKeepAlive(consumer.KeepAliveConfig{
		Interval: consumer.DisableInterval,
		Timeout:  consumer.DisableTimeout,
	})
	// Before Connect: no profile.
	if p.ComplianceProfile() != nil {
		t.Error("ComplianceProfile non-nil before Connect")
	}
	rec, err := transport.NewRecorder(t.TempDir() + "/cap.jsonl")
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	defer func() { _ = rec.Close() }()
	p.SetRecorder(rec)
	var progressCount int
	p.SetWalkProgress(func(count int, obj *consumer.Object) { progressCount = count })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.Connect(ctx, host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	// After Connect: profile exists.
	if p.ComplianceProfile() == nil {
		t.Error("ComplianceProfile nil after Connect")
	}
	_ = progressCount
}

func TestSession_SlotStatusAndSetRecorder(t *testing.T) {
	s := NewSession(nil, testLogger())
	rec, err := transport.NewRecorder(t.TempDir() + "/cap.jsonl")
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	defer func() { _ = rec.Close() }()
	s.SetRecorder(rec)
	// Out-of-range slot returns SlotNoCard.
	if got := s.SlotStatus(99); got != consumer.SlotNoCard {
		t.Errorf("SlotStatus(99) = %v, want SlotNoCard", got)
	}
	if got := s.SlotStatus(-1); got != consumer.SlotNoCard {
		t.Errorf("SlotStatus(-1) = %v, want SlotNoCard", got)
	}
}

// ---- helpers ----

func f32be(v float32) []byte {
	bits := math.Float32bits(v)
	return []byte{byte(bits >> 24), byte(bits >> 16), byte(bits >> 8), byte(bits)}
}

func s64be(v int64) []byte {
	u := uint64(v)
	return []byte{
		byte(u >> 56), byte(u >> 48), byte(u >> 40), byte(u >> 32),
		byte(u >> 24), byte(u >> 16), byte(u >> 8), byte(u),
	}
}

func beU32Bytes(v uint32) []byte {
	return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
}

// optionsProp builds a pid=15 options property using the variable-length
// per-option wire convention PropertyOptionsMap decodes: u32 idx +
// NUL-terminated name + pad to 4-byte boundary.
func optionsProp(opts map[uint32]string) codec.Property {
	var data []byte
	// Emit in index order for determinism.
	for idx := uint32(0); int(idx) < len(opts); idx++ {
		name, ok := opts[idx]
		if !ok {
			continue
		}
		rec := beU32Bytes(idx)
		rec = append(rec, []byte(name)...)
		rec = append(rec, 0) // NUL
		for len(rec)%4 != 0 {
			rec = append(rec, 0)
		}
		data = append(data, rec...)
	}
	return codec.Property{PID: codec.PIDOptions, PLen: uint16(4 + len(data)), Data: data}
}
