package acp2

import (
	"context"
	"errors"
	"testing"

	"dhs/internal/consumer"

	"dhs/internal/acp2/codec"
)

// getset_loopback_test.go drives Plugin.GetValue / SetValue against the
// loopback harness, covering: get with a pre-walked tree, get with the
// fetchObjectMeta fallback (no tree), set numeric/string, label
// addressing, error replies (every stat code), and not-connected
// branches.

func TestGetValue_NotConnected(t *testing.T) {
	p := &Plugin{logger: testLogger()}
	p.trees = newWalkedTreeCache(8, 0)
	if _, err := p.GetValue(context.Background(), consumer.ValueRequest{ID: 3}); err != consumer.ErrNotConnected {
		t.Errorf("err = %v, want ErrNotConnected", err)
	}
}

func TestSetValue_NotConnected(t *testing.T) {
	p := &Plugin{logger: testLogger()}
	p.trees = newWalkedTreeCache(8, 0)
	_, err := p.SetValue(context.Background(), consumer.ValueRequest{ID: 3}, consumer.Value{Kind: consumer.KindUint, Uint: 1})
	if err != consumer.ErrNotConnected {
		t.Errorf("err = %v, want ErrNotConnected", err)
	}
}

// TestGetValue_AfterWalk reads a value addressed by label, using the
// type info learned from a prior Walk.
func TestGetValue_AfterWalk(t *testing.T) {
	srv, host, port := walkTreeServer(t)
	srv.onACP2 = wrapWithGetProperty(srv.onACP2, func(req *codec.ACP2Message) *codec.ACP2Message {
		// Volume (obj 3) get_property(pid=8) → u32 99.
		if req.ObjID == 3 {
			return propertyReply(codec.ACP2FuncGetProperty, 3,
				codec.MakeValueProperty(codec.PIDValue, codec.NumTypeU32, []byte{0, 0, 0, 99}))
		}
		return errorReply(codec.ErrInvalidObjID)
	})
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	if _, err := p.Walk(context.Background(), 1); err != nil {
		t.Fatalf("Walk: %v", err)
	}

	val, err := p.GetValue(context.Background(), consumer.ValueRequest{Slot: 1, Label: "Volume"})
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if val.Kind != consumer.KindUint || val.Uint != 99 {
		t.Errorf("value = %+v, want uint 99", val)
	}
}

// TestGetValue_NoTreeFallback reads a value with no walked tree; the
// plugin must issue get_object to learn the type, then get_property.
func TestGetValue_NoTreeFallback(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		switch req.Func {
		case codec.ACP2FuncGetObject:
			if req.ObjID == 7 {
				return objectReply(7, []codec.Property{
					u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNumber)),
					codec.MakeStringProperty(codec.PIDLabel, "Gain"),
					u32Prop(codec.PIDNumberType, 0, uint32(codec.NumTypeS32)),
				})
			}
			return errorReply(codec.ErrInvalidObjID)
		case codec.ACP2FuncGetProperty:
			if req.ObjID == 7 {
				return propertyReply(codec.ACP2FuncGetProperty, 7,
					codec.MakeValueProperty(codec.PIDValue, codec.NumTypeS32, []byte{0xFF, 0xFF, 0xFF, 0xFB})) // -5
			}
			return errorReply(codec.ErrInvalidObjID)
		}
		return errorReply(codec.ErrProtocol)
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	val, err := p.GetValue(context.Background(), consumer.ValueRequest{Slot: 0, ID: 7})
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if val.Kind != consumer.KindInt || val.Int != -5 {
		t.Errorf("value = %+v, want int -5", val)
	}
}

// TestGetValue_DeviceError verifies that an ACP2 error reply surfaces as
// the typed ACP2Error.
func TestGetValue_DeviceError(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		if req.Func == codec.ACP2FuncGetObject {
			// Type unknown so the plugin issues get_object first; fail it.
			return errorReply(codec.ErrInvalidObjID)
		}
		return errorReply(codec.ErrInvalidObjID)
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	_, err := p.GetValue(context.Background(), consumer.ValueRequest{Slot: 0, ID: 99})
	if err == nil {
		t.Fatal("GetValue on bad obj: want error")
	}
	var ae *codec.ACP2Error
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v, want *codec.ACP2Error", err)
	}
	if ae.Status != codec.ErrInvalidObjID {
		t.Errorf("stat = %d, want ErrInvalidObjID", ae.Status)
	}
}

// TestGetProperty_ExplicitPID reads a non-value pid (e.g. label) which
// returns KindRaw.
func TestGetValue_ExplicitPID(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		switch req.Func {
		case codec.ACP2FuncGetObject:
			return objectReply(8, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNumber)),
				u32Prop(codec.PIDNumberType, 0, uint32(codec.NumTypeU32)),
			})
		case codec.ACP2FuncGetProperty:
			// Reply carries the requested pid (access = 3).
			return propertyReply(codec.ACP2FuncGetProperty, 8,
				u32Prop(codec.PIDAccess, 0, 3))
		}
		return errorReply(codec.ErrProtocol)
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	val, err := p.GetValue(context.Background(), consumer.ValueRequest{Slot: 0, ID: 8, PID: int(codec.PIDAccess)})
	if err != nil {
		t.Fatalf("GetValue(pid=access): %v", err)
	}
	if val.Kind != consumer.KindRaw {
		t.Errorf("kind = %v, want KindRaw for non-value pid", val.Kind)
	}
}

// TestSetValue_Numeric sets a numeric value with no tree (fetchObjectMeta
// fallback) and asserts the confirmed reply value.
func TestSetValue_Numeric(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		switch req.Func {
		case codec.ACP2FuncGetObject:
			return objectReply(5, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNumber)),
				u32Prop(codec.PIDNumberType, 0, uint32(codec.NumTypeU32)),
			})
		case codec.ACP2FuncSetProperty:
			// Echo back the confirmed value (the property the client sent).
			var prop codec.Property
			if len(req.Properties) > 0 {
				prop = req.Properties[0]
			}
			return propertyReply(codec.ACP2FuncSetProperty, 5, prop)
		}
		return errorReply(codec.ErrProtocol)
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	val, err := p.SetValue(context.Background(),
		consumer.ValueRequest{Slot: 0, ID: 5},
		consumer.Value{Kind: consumer.KindUint, Uint: 7})
	if err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	if val.Kind != consumer.KindUint || val.Uint != 7 {
		t.Errorf("confirmed = %+v, want uint 7", val)
	}
}

// TestSetValue_String sets a string value via a pre-walked tree.
func TestSetValue_String(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		switch req.Func {
		case codec.ACP2FuncGetObject:
			if req.ObjID == 1 {
				return objectReply(1, []codec.Property{
					u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
					codec.MakeStringProperty(codec.PIDLabel, "ROOT"),
					childrenProp(2),
				})
			}
			return objectReply(2, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeString)),
				codec.MakeStringProperty(codec.PIDLabel, "User Label"),
				codec.MakeStringProperty(codec.PIDValue, "old"),
			})
		case codec.ACP2FuncSetProperty:
			var prop codec.Property
			if len(req.Properties) > 0 {
				prop = req.Properties[0]
			}
			return propertyReply(codec.ACP2FuncSetProperty, 2, prop)
		}
		return errorReply(codec.ErrProtocol)
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	if _, err := p.Walk(context.Background(), 0); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	val, err := p.SetValue(context.Background(),
		consumer.ValueRequest{Slot: 0, Label: "User Label"},
		consumer.Value{Kind: consumer.KindString, Str: "new"})
	if err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	if val.Kind != consumer.KindString || val.Str != "new" {
		t.Errorf("confirmed = %+v, want string new", val)
	}
}

// TestSetValue_NoAccessError exercises the stat=4 (no-access) error path.
func TestSetValue_NoAccessError(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		switch req.Func {
		case codec.ACP2FuncGetObject:
			return objectReply(6, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNumber)),
				u32Prop(codec.PIDNumberType, 0, uint32(codec.NumTypeU32)),
			})
		case codec.ACP2FuncSetProperty:
			return errorReply(codec.ErrNoAccess)
		}
		return errorReply(codec.ErrProtocol)
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	_, err := p.SetValue(context.Background(),
		consumer.ValueRequest{Slot: 0, ID: 6},
		consumer.Value{Kind: consumer.KindUint, Uint: 1})
	if err == nil {
		t.Fatal("SetValue on read-only obj: want error")
	}
	var ae *codec.ACP2Error
	if !errors.As(err, &ae) || ae.Status != codec.ErrNoAccess {
		t.Errorf("err = %v, want ACP2Error stat=ErrNoAccess", err)
	}
}

// TestGetValue_UnknownLabel exercises resolveRequest's label-not-found
// branch (tree present, label absent).
func TestGetValue_UnknownLabel(t *testing.T) {
	srv, host, port := walkTreeServer(t)
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	if _, err := p.Walk(context.Background(), 1); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	_, err := p.GetValue(context.Background(), consumer.ValueRequest{Slot: 1, Label: "Nonexistent"})
	if err == nil {
		t.Fatal("GetValue with bad label: want error")
	}
	if !errors.Is(err, consumer.ErrUnknownLabel) {
		t.Errorf("err = %v, want ErrUnknownLabel", err)
	}
}

// wrapWithGetProperty composes an existing get_object handler with a
// dedicated get_property handler so a walk-tree server can also answer
// value reads.
func wrapWithGetProperty(base func(uint8, *codec.ACP2Message) *codec.ACP2Message,
	getProp func(*codec.ACP2Message) *codec.ACP2Message) func(uint8, *codec.ACP2Message) *codec.ACP2Message {
	return func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		if req.Func == codec.ACP2FuncGetProperty {
			return getProp(req)
		}
		return base(slot, req)
	}
}
