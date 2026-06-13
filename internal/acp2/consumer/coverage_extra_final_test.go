package acp2

import (
	"context"
	"errors"
	"testing"
	"time"

	"dhs/internal/acp2/codec"
	"dhs/internal/consumer"
)

// coverage_extra_final_test.go closes the remaining reachable branches:
// the connected ValidateValue pipeline, walker inline VType-fallback +
// event-property arms, decodeConstraint vtype fallback, plugin SetValue /
// GetValue / Subscribe / fetchObjectMeta error + fallback arms, and the
// announce KindUnknown raw fallback. Pure-decode helpers are driven
// directly; I/O paths use the loopback fakeServer.

// ----------------------------------------------------------------------
// walker.go — inline VType-fallback + event property arms

// TestParseObjectProperties_VTypeFallbacks builds an object whose metadata
// pids carry their value in the VType byte (no 4-byte Data), plus event-tag
// (2-byte data), event-prio, and event-messages properties — exercising the
// VType else-arms and the alarm-property arms.
func TestParseObjectProperties_VTypeFallbacks(t *testing.T) {
	w := &Walker{logger: unitLogger()}
	props := []codec.Property{
		// object_type via VType (Data empty).
		{PID: codec.PIDObjectType, VType: uint8(codec.ObjTypeString), PLen: 4},
		// access via VType.
		{PID: codec.PIDAccess, VType: 3, PLen: 4},
		// number_type via VType.
		{PID: codec.PIDNumberType, VType: uint8(codec.NumTypeString), PLen: 4},
		// string max length: PropertyU16 fails (1-byte data) but len>=4 path
		// needs 4 bytes — supply 4 so the else-if (p.Data[3]) arm runs after
		// PropertyU16 succeeds on the first two bytes... use 4 bytes where
		// the u16 decode succeeds (covers the happy arm); a separate 3-byte
		// case covers the else-if.
		{PID: codec.PIDStringMaxLength, VType: 0, PLen: 7, Data: []byte{0, 0, 64}},
		{PID: codec.PIDLabel, VType: 0, PLen: 4 + 4, Data: []byte("Lbl\x00")},
		// event tag: 2-byte data → AlarmTag = data[1].
		{PID: codec.PIDEventTag, VType: 0, PLen: 6, Data: []byte{0x00, 0x09}},
		// event prio: 4-byte data.
		{PID: codec.PIDEventPrio, VType: 0, PLen: 8, Data: []byte{0, 0, 0, 4}},
		// event messages: two NUL-separated strings.
		{PID: codec.PIDEventMessages, VType: 0, PLen: 4 + 8, Data: []byte("on\x00off\x00")},
	}
	obj, ot, nt, _, _ := w.parseObjectProperties(props, 1, 5, nil)
	if ot != codec.ObjTypeString {
		t.Errorf("objType via VType = %d, want string", ot)
	}
	if nt != codec.NumTypeString {
		t.Errorf("numType via VType = %d, want string", nt)
	}
	if obj.Access != 3 {
		t.Errorf("access via VType = %d, want 3", obj.Access)
	}
	if obj.AlarmTag != 0x09 {
		t.Errorf("alarm tag = %d, want 9", obj.AlarmTag)
	}
	if obj.AlarmPriority != 4 {
		t.Errorf("alarm prio = %d, want 4", obj.AlarmPriority)
	}
}

// TestDecodeConstraint_VTypeFallback covers decodeConstraint's nt==0 →
// numType fallback (a constraint property with VType 0).
func TestDecodeConstraint_VTypeFallback(t *testing.T) {
	w := &Walker{logger: unitLogger()}
	var obj consumer.Object
	p := &codec.Property{PID: codec.PIDMinValue, VType: 0, Data: []byte{0, 0, 0, 3}}
	w.decodeConstraint(p, codec.ObjTypeNumber, codec.NumTypeS32, &obj, "min")
	if got, _ := obj.Min.(int64); got != 3 {
		t.Errorf("min via vtype-fallback = %v, want 3", obj.Min)
	}
}

// ----------------------------------------------------------------------
// value_validate.go — connected ValidateValue pipeline

// TestValidateValue_Connected drives ValidateValue end to end against the
// loopback peer: no tree → fetchObjectMeta builds a mini-tree with an
// options map, reverseEnum is built, and the type check passes for a valid
// enum label.
func TestValidateValue_Connected(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		if req.Func == codec.ACP2FuncGetObject && req.ObjID == 4 {
			return objectReply(4, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeEnum)),
				codec.MakeStringProperty(codec.PIDLabel, "Mode"),
				u32Prop(codec.PIDNumberType, 0, uint32(codec.NumTypePreset)),
				optionsProp(map[uint32]string{0: "Off", 1: "On"}),
			})
		}
		return errorReply(codec.ErrInvalidObjID)
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	// Valid enum label "On" → ValidateValue succeeds (covers fetchObjectMeta
	// + reverseEnum build + validateValueAgainstType happy path).
	if err := p.ValidateValue(context.Background(),
		consumer.ValueRequest{Slot: 0, ID: 4},
		consumer.Value{Kind: consumer.KindString, Str: "On"}); err != nil {
		t.Fatalf("ValidateValue(valid enum): %v", err)
	}
}

// TestValidateValue_ObjectNotFound covers ValidateValue's stat=1 →
// ErrObjectNotFound arm (device rejects the get_object with invalid-obj-id).
func TestValidateValue_ObjectNotFound(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		return errorReply(codec.ErrInvalidObjID)
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	err := p.ValidateValue(context.Background(),
		consumer.ValueRequest{Slot: 0, ID: 99},
		consumer.Value{Kind: consumer.KindInt, Int: 1})
	if !errors.Is(err, consumer.ErrObjectNotFound) {
		t.Fatalf("err = %v, want ErrObjectNotFound", err)
	}
}

// TestValidateValue_MetaFetchTransportError covers ValidateValue's
// non-ACP2-error meta-fetch failure arm (a dropped reply → ctx/conn error,
// not an ACP2Error stat=1).
func TestValidateValue_MetaFetchTransportError(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		return nil // drop → DoACP2 hits ctx deadline → non-ACP2Error
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	err := p.ValidateValue(ctx,
		consumer.ValueRequest{Slot: 0, ID: 7},
		consumer.Value{Kind: consumer.KindInt, Int: 1})
	if !errors.Is(err, consumer.ErrValidationFailed) {
		t.Fatalf("err = %v, want ErrValidationFailed", err)
	}
}

// ----------------------------------------------------------------------
// plugin.go — GetValue/SetValue error + fallback arms, Subscribe init,
// fetchObjectMeta VType fallbacks, IdentityProbe missing card name,
// announce raw fallback.

// TestGetValue_DoACP2Error covers GetValue's get_property DoACP2 error arm:
// the type is known from a walked tree (so no fetchObjectMeta), but the
// get_property is rejected.
func TestGetValue_DoACP2Error(t *testing.T) {
	srv, host, port := newFakeServer(t)
	base := func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		if req.Func == codec.ACP2FuncGetObject {
			switch req.ObjID {
			case 1:
				return objectReply(1, []codec.Property{
					u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
					codec.MakeStringProperty(codec.PIDLabel, "ROOT"),
					childrenProp(2),
				})
			case 2:
				return objectReply(2, []codec.Property{
					u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNumber)),
					codec.MakeStringProperty(codec.PIDLabel, "Gain"),
					u32Prop(codec.PIDNumberType, 0, uint32(codec.NumTypeU32)),
				})
			}
		}
		return errorReply(codec.ErrInvalidObjID)
	}
	srv.onACP2 = wrapWithGetProperty(base, func(req *codec.ACP2Message) *codec.ACP2Message {
		return errorReply(codec.ErrInvalidPID) // get_property fails
	})
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	if _, err := p.Walk(context.Background(), 1); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if _, err := p.GetValue(context.Background(),
		consumer.ValueRequest{Slot: 1, Label: "Gain"}); err == nil {
		t.Fatal("GetValue with failing get_property should error")
	}
}

// TestSetValue_ResolveAndMetaErrors covers SetValue's fetchObjectMeta error
// arm (no tree, device rejects get_object).
func TestSetValue_MetaFetchError(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		return errorReply(codec.ErrInvalidObjID)
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	_, err := p.SetValue(context.Background(),
		consumer.ValueRequest{Slot: 0, ID: 99},
		consumer.Value{Kind: consumer.KindUint, Uint: 1})
	if err == nil {
		t.Fatal("SetValue with failing get_object meta fetch should error")
	}
}

// TestSetValue_EncodeError covers SetValue's encodeSetProperty error arm:
// the object resolves (via fetchObjectMeta) to a Node type, which
// encodeSetProperty cannot encode for a non-raw value → error.
func TestSetValue_EncodeError(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		if req.Func == codec.ACP2FuncGetObject {
			// object_type = Node → no encodeSetProperty case → error.
			return objectReply(8, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
			})
		}
		return errorReply(codec.ErrProtocol)
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	_, err := p.SetValue(context.Background(),
		consumer.ValueRequest{Slot: 0, ID: 8},
		consumer.Value{Kind: consumer.KindInt, Int: 1})
	if err == nil {
		t.Fatal("SetValue on a Node-typed object should fail at encodeSetProperty")
	}
}

// TestSetValue_RawFallback covers SetValue's trailing raw-body fallback:
// the set reply carries no pid=8 property, so SetValue returns KindRaw of
// the body.
func TestSetValue_RawFallback(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		switch req.Func {
		case codec.ACP2FuncGetObject:
			return objectReply(5, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNumber)),
				u32Prop(codec.PIDNumberType, 0, uint32(codec.NumTypeU32)),
			})
		case codec.ACP2FuncSetProperty:
			// Reply with NO pid=8 property → SetValue raw fallback.
			return &codec.ACP2Message{
				Type: codec.ACP2TypeReply, Func: codec.ACP2FuncSetProperty,
				Body: objectBody(5, nil),
			}
		}
		return errorReply(codec.ErrProtocol)
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	val, err := p.SetValue(context.Background(),
		consumer.ValueRequest{Slot: 0, ID: 5},
		consumer.Value{Kind: consumer.KindUint, Uint: 3})
	if err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	if val.Kind != consumer.KindRaw {
		t.Errorf("value = %+v, want raw fallback", val)
	}
}

// TestGetValue_RawFallback covers GetValue's trailing raw-body fallback when
// the reply carries no matching pid.
func TestGetValue_RawFallback(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		switch req.Func {
		case codec.ACP2FuncGetObject:
			return objectReply(5, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNumber)),
				u32Prop(codec.PIDNumberType, 0, uint32(codec.NumTypeU32)),
			})
		case codec.ACP2FuncGetProperty:
			// Reply carries a DIFFERENT pid than requested → no match → raw.
			return &codec.ACP2Message{
				Type: codec.ACP2TypeReply, Func: codec.ACP2FuncGetProperty,
				Body: objectBody(5, nil),
			}
		}
		return errorReply(codec.ErrProtocol)
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	val, err := p.GetValue(context.Background(), consumer.ValueRequest{Slot: 0, ID: 5})
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if val.Kind != consumer.KindRaw {
		t.Errorf("value = %+v, want raw fallback", val)
	}
}

// TestSubscribe_InitsActiveSubs covers Subscribe's p.activeSubs==nil init
// arm: subscribing on a connected plugin whose activeSubs was cleared.
func TestSubscribe_InitsActiveSubs(t *testing.T) {
	srv, host, port := newFakeServer(t)
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	// Force activeSubs nil so Subscribe re-allocates it.
	p.mu.Lock()
	p.activeSubs = nil
	p.mu.Unlock()
	if err := p.Subscribe(consumer.ValueRequest{Slot: -1, ID: -1}, func(consumer.Event) {}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
}

// TestFetchObjectMeta_VTypeFallbacks covers fetchObjectMeta's PIDObjectType /
// PIDNumberType VType-else arms: the get_object reply carries object_type +
// number_type in the VType byte (no 4-byte Data).
func TestFetchObjectMeta_VTypeFallbacks(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		switch req.Func {
		case codec.ACP2FuncGetObject:
			return objectReply(6, []codec.Property{
				// object_type + number_type via VType only (PLen=4, no Data).
				{PID: codec.PIDObjectType, VType: uint8(codec.ObjTypeNumber), PLen: 4},
				{PID: codec.PIDNumberType, VType: uint8(codec.NumTypeU32), PLen: 4},
			})
		case codec.ACP2FuncGetProperty:
			return propertyReply(codec.ACP2FuncGetProperty, 6,
				codec.MakeValueProperty(codec.PIDValue, codec.NumTypeU32, beU32Bytes(11)))
		}
		return errorReply(codec.ErrProtocol)
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	val, err := p.GetValue(context.Background(), consumer.ValueRequest{Slot: 0, ID: 6})
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if val.Kind != consumer.KindUint || val.Uint != 11 {
		t.Errorf("value = %+v, want uint 11", val)
	}
}

// TestIdentityProbe_MissingCardName covers IdentityProbe's missing-Card-Name
// error arm: the walk finds IDENTITY but no "Card Name" label.
func TestIdentityProbe_MissingCardName(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		if req.Func != codec.ACP2FuncGetObject {
			return errorReply(codec.ErrProtocol)
		}
		switch req.ObjID {
		case 1:
			return objectReply(1, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
				codec.MakeStringProperty(codec.PIDLabel, "ROOT_NODE_V2"),
				childrenProp(2),
			})
		case 2:
			return objectReply(2, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeNode)),
				codec.MakeStringProperty(codec.PIDLabel, "BOARD"),
				childrenProp(10),
			})
		case 10:
			return objectReply(10, []codec.Property{
				u32Prop(codec.PIDObjectType, 0, uint32(codec.ObjTypeString)),
				codec.MakeStringProperty(codec.PIDLabel, "Hardware Version"),
				codec.MakeStringProperty(codec.PIDValue, "1.0"),
			})
		}
		return errorReply(codec.ErrInvalidObjID)
	}
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	if _, err := p.IdentityProbe(context.Background(), 1); err == nil {
		t.Fatal("IdentityProbe with no Card Name should error")
	}
}

// TestSubscribe_AnnounceRawFallback covers buildAnnounceClosure's
// KindUnknown→raw final fallback: an announce for an obj NOT in the tree
// carrying a value whose vtype maps to an objType the typed decoder can't
// resolve to a known kind (string with empty data → KindUnknown → raw).
func TestSubscribe_AnnounceRawFallback(t *testing.T) {
	srv, host, port := newFakeServer(t)
	defer srv.stop()

	p := connectPlugin(t, srv, host, port)
	got := make(chan consumer.Event, 4)
	if err := p.Subscribe(consumer.ValueRequest{Slot: -1, ID: -1}, func(ev consumer.Event) {
		got <- ev
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	p.mu.Lock()
	s := p.session
	p.mu.Unlock()

	// Announce for an obj NOT in any tree, with a value property whose typed
	// decode leaves the value unset (enum vtype with <4 data bytes), so the
	// closure's KindUnknown→raw final fallback runs. Driven deterministically
	// via feedAnnounce (same fan-out the readLoop uses).
	prop := codec.Property{PID: codec.PIDValue, VType: uint8(codec.NumTypePreset),
		PLen: 5, Data: []byte{0x01}}
	feedAnnounce(s, 1, &codec.ACP2Message{
		Type: codec.ACP2TypeAnnounce, MTID: 0, PID: codec.PIDValue, ObjID: 77777,
		Properties: []codec.Property{prop},
		Body:       objectBody(77777, []codec.Property{prop}),
	})

	select {
	case ev := <-got:
		if ev.Value.Kind != consumer.KindRaw {
			t.Errorf("announce value kind = %v, want raw fallback", ev.Value.Kind)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no announce delivered")
	}
}
