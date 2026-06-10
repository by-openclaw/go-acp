package acp1

import (
	"context"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
	"dhs/internal/export/canonical"
)

func TestKindToCanonicalType(t *testing.T) {
	cases := map[consumer.ValueKind]string{
		consumer.KindBool:   canonical.ParamBoolean,
		consumer.KindInt:    canonical.ParamInteger,
		consumer.KindUint:   canonical.ParamInteger,
		consumer.KindFloat:  canonical.ParamReal,
		consumer.KindEnum:   canonical.ParamEnum,
		consumer.KindString: canonical.ParamString,
		consumer.KindIPAddr: canonical.ParamString,
		consumer.KindAlarm:  canonical.ParamBoolean,
		consumer.KindRaw:    canonical.ParamOctets,
		consumer.KindFrame:  canonical.ParamOctets,
	}
	for k, want := range cases {
		if got := kindToCanonicalType(k, codec.TypeInteger); got != want {
			t.Errorf("kindToCanonicalType(%v) = %q, want %q", k, got, want)
		}
	}
	// File fallback via the default arm.
	if got := kindToCanonicalType(consumer.KindUnknown, codec.TypeFile); got != canonical.ParamString {
		t.Errorf("File fallback = %q", got)
	}
}

func TestAccessString_Canonical(t *testing.T) {
	cases := map[uint8]string{
		0x01: canonical.AccessRead,
		0x02: canonical.AccessWrite,
		0x03: canonical.AccessReadWrite,
		0x00: canonical.AccessNone,
	}
	for a, want := range cases {
		if got := accessString(a); got != want {
			t.Errorf("accessString(%#x) = %q, want %q", a, got, want)
		}
	}
}

func TestValueToAny(t *testing.T) {
	if valueToAny(consumer.Value{Kind: consumer.KindBool, Bool: true}) != true {
		t.Error("bool")
	}
	if valueToAny(consumer.Value{Kind: consumer.KindInt, Int: 5}) != int64(5) {
		t.Error("int")
	}
	if valueToAny(consumer.Value{Kind: consumer.KindUint, Uint: 6}) != uint64(6) {
		t.Error("uint")
	}
	if valueToAny(consumer.Value{Kind: consumer.KindFloat, Float: 1.5}) != 1.5 {
		t.Error("float")
	}
	if valueToAny(consumer.Value{Kind: consumer.KindEnum, Enum: 2}) != int64(2) {
		t.Error("enum")
	}
	if valueToAny(consumer.Value{Kind: consumer.KindString, Str: "x"}) != "x" {
		t.Error("string")
	}
	if valueToAny(consumer.Value{Kind: consumer.KindIPAddr, IPAddr: [4]byte{10, 0, 0, 1}}) != "10.0.0.1" {
		t.Error("ipaddr")
	}
	if valueToAny(consumer.Value{Kind: consumer.KindAlarm, Bool: true}) != true {
		t.Error("alarm")
	}
	if valueToAny(consumer.Value{Kind: consumer.KindFrame}) != nil {
		t.Error("frame should be nil")
	}
}

func TestKindToACPType(t *testing.T) {
	cases := map[consumer.ValueKind]codec.ObjectType{
		consumer.KindBool:   codec.TypeEnum,
		consumer.KindEnum:   codec.TypeEnum,
		consumer.KindInt:    codec.TypeInteger,
		consumer.KindUint:   codec.TypeByte,
		consumer.KindFloat:  codec.TypeFloat,
		consumer.KindString: codec.TypeString,
		consumer.KindIPAddr: codec.TypeIPAddr,
		consumer.KindAlarm:  codec.TypeAlarm,
		consumer.KindFrame:  codec.TypeFrame,
	}
	for k, want := range cases {
		if got := kindToACPType(k, nil); got != want {
			t.Errorf("kindToACPType(%v) = %d, want %d", k, got, want)
		}
	}
	// Meta overrides in each numeric form.
	for _, meta := range []map[string]any{
		{"acp1_type": float64(codec.TypeLong)},
		{"acp1_type": int(codec.TypeLong)},
		{"acp1_type": int64(codec.TypeLong)},
		{"acp1_type": codec.TypeLong},
	} {
		if got := kindToACPType(consumer.KindInt, meta); got != codec.TypeLong {
			t.Errorf("meta override = %d, want Long", got)
		}
	}
}

func TestIdentityValueAsString(t *testing.T) {
	if identityValueAsString(nil) != "" {
		t.Error("nil")
	}
	cases := []struct {
		d    *codec.DecodedObject
		want string
	}{
		{&codec.DecodedObject{Type: codec.TypeString, StrValue: "RRS18"}, "RRS18"},
		{&codec.DecodedObject{Type: codec.TypeInteger, IntVal: -5}, "-5"},
		{&codec.DecodedObject{Type: codec.TypeLong, IntVal: 70000}, "70000"},
		{&codec.DecodedObject{Type: codec.TypeByte, ByteVal: 7}, "7"},
		{&codec.DecodedObject{Type: codec.TypeEnum, ByteVal: 1}, "1"},
		{&codec.DecodedObject{Type: codec.TypeIPAddr, UintVal: 42}, "42"},
		{&codec.DecodedObject{Type: codec.TypeFloat, FloatVal: 1.5}, "1.5"},
		{&codec.DecodedObject{Type: codec.TypeFrame}, ""},
	}
	for _, c := range cases {
		if got := identityValueAsString(c.d); got != c.want {
			t.Errorf("identityValueAsString(%d) = %q, want %q", c.d.Type, got, c.want)
		}
	}
}

func TestCache_UpdateObjectValueAndDefaults(t *testing.T) {
	cfg := defaultCacheConfig()
	if cfg.MaxSize != 32 {
		t.Errorf("default MaxSize = %d, want 32", cfg.MaxSize)
	}
	cache := newSlotTreeCache(cfg.MaxSize, cfg.TTL)
	cache.Put(1, &SlotTree{
		Slot:    1,
		Objects: []consumer.Object{{Group: "control", ID: 5, Value: consumer.Value{Kind: consumer.KindInt, Int: 1}}},
	})
	cache.UpdateObjectValue(1, "control", 5, consumer.Value{Kind: consumer.KindInt, Int: 99})
	got, ok := cache.Get(1)
	if !ok || got.Objects[0].Value.Int != 99 {
		t.Errorf("UpdateObjectValue did not apply: %+v ok=%v", got, ok)
	}
	// No-ops: nil cache, unknown slot.
	var nilCache *slotTreeCache
	nilCache.UpdateObjectValue(1, "control", 5, consumer.Value{})
	cache.UpdateObjectValue(99, "control", 5, consumer.Value{}) // unknown slot
}

func TestValidateValueAgainstType_Table(t *testing.T) {
	rw := consumer.Object{Access: 0x03}
	ro := consumer.Object{Access: 0x01}
	enumObj := consumer.Object{Access: 0x03, EnumItems: []string{"Off", "On"}}
	strObj := consumer.Object{Access: 0x03, MaxLen: 4}

	type tc struct {
		name    string
		t       codec.ObjectType
		obj     consumer.Object
		val     consumer.Value
		wantErr bool
	}
	cases := []tc{
		{"raw-escape", codec.TypeInteger, rw, consumer.Value{Raw: []byte{1, 2}}, false},
		{"read-only", codec.TypeInteger, ro, consumer.Value{Kind: consumer.KindInt, Int: 5}, true},
		{"int-ok", codec.TypeInteger, rw, consumer.Value{Kind: consumer.KindInt, Int: 5}, false},
		{"int-unknown", codec.TypeInteger, rw, consumer.Value{Kind: consumer.KindUnknown}, true},
		{"int-string", codec.TypeInteger, rw, consumer.Value{Kind: consumer.KindString, Str: "x"}, true},
		{"byte-uint", codec.TypeByte, rw, consumer.Value{Kind: consumer.KindUint, Uint: 9}, false},
		{"byte-neg", codec.TypeByte, rw, consumer.Value{Kind: consumer.KindInt, Int: -1}, true},
		{"byte-int-ok", codec.TypeByte, rw, consumer.Value{Kind: consumer.KindInt, Int: 7}, false},
		{"byte-toobig", codec.TypeByte, rw, consumer.Value{Kind: consumer.KindUint, Uint: 300}, true},
		{"byte-nonnum", codec.TypeByte, rw, consumer.Value{Kind: consumer.KindString, Str: "x"}, true},
		{"byte-unknown", codec.TypeByte, rw, consumer.Value{Kind: consumer.KindUnknown}, true},
		{"float-ok", codec.TypeFloat, rw, consumer.Value{Kind: consumer.KindFloat, Float: 1.5}, false},
		{"float-unknown", codec.TypeFloat, rw, consumer.Value{Kind: consumer.KindUnknown}, true},
		{"float-emptystr", codec.TypeFloat, rw, consumer.Value{Kind: consumer.KindString, Str: ""}, true},
		{"float-str", codec.TypeFloat, rw, consumer.Value{Kind: consumer.KindString, Str: "1.5"}, true},
		{"ip-parsed", codec.TypeIPAddr, rw, consumer.Value{IPAddr: [4]byte{10, 0, 0, 1}}, false},
		{"ip-empty", codec.TypeIPAddr, rw, consumer.Value{Kind: consumer.KindString, Str: ""}, true},
		{"ip-badparts", codec.TypeIPAddr, rw, consumer.Value{Str: "1.2.3"}, true},
		{"ip-badoctet", codec.TypeIPAddr, rw, consumer.Value{Str: "1.2.3.999"}, true},
		{"ip-valid", codec.TypeIPAddr, rw, consumer.Value{Str: "1.2.3.4"}, false},
		{"enum-ok", codec.TypeEnum, enumObj, consumer.Value{Str: "On"}, false},
		{"enum-bad", codec.TypeEnum, enumObj, consumer.Value{Str: "Nope"}, true},
		{"string-ok", codec.TypeString, strObj, consumer.Value{Str: "hi"}, false},
		{"string-toolong", codec.TypeString, strObj, consumer.Value{Str: "toolong"}, true},
		{"alarm-passthrough", codec.TypeAlarm, rw, consumer.Value{Kind: consumer.KindUint, Uint: 1}, false},
		{"unknown-type", codec.ObjectType(99), rw, consumer.Value{Kind: consumer.KindInt, Int: 1}, false},
	}
	for _, c := range cases {
		err := validateValueAgainstType(c.t, c.obj, c.val)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err=%v wantErr=%v", c.name, err, c.wantErr)
		}
	}
}

func TestSeedTreeFromCachedObjects_ThenWalkCacheHit(t *testing.T) {
	p, _, _ := newPluginWithClient(t)
	p.walker = NewWalker(p.client) // satisfy Walk's nil-check; cache hit avoids using it
	objs := []consumer.Object{
		{Group: "control", ID: 0, Label: "Level", Kind: consumer.KindInt},
		{Group: "identity", ID: 0, Label: "Card name", Kind: consumer.KindString},
		{Group: "", ID: 9}, // skipped (no group/label)
	}
	p.SeedTreeFromCachedObjects(1, objs)

	got, err := p.Walk(context.Background(), 1)
	if err != nil {
		t.Fatalf("Walk after seed: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Walk returned %d objects, want 3 (cache hit)", len(got))
	}
}

func TestIdentityProbe_HappyPath(t *testing.T) {
	p, ft, mtid := newPluginWithClient(t)
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeReply, byte(codec.MethodGetObject),
			codec.GroupIdentity, 0, stringObject("RRS18", "Card Label")),
		buildReply(t, mtid+2, codec.MTypeReply, byte(codec.MethodGetObject),
			codec.GroupIdentity, 3, stringObject("1601", "Sw Rev")),
		buildReply(t, mtid+3, codec.MTypeReply, byte(codec.MethodGetObject),
			codec.GroupIdentity, 4, stringObject("A2", "Hw Rev")),
	}
	id, err := p.IdentityProbe(context.Background(), 1)
	if err != nil {
		t.Fatalf("IdentityProbe: %v", err)
	}
	if id != "RRS18@1601" {
		t.Errorf("IdentityProbe = %q, want RRS18@1601", id)
	}
}

func TestIdentityProbe_EmptyModel(t *testing.T) {
	p, ft, mtid := newPluginWithClient(t)
	// Card-label probe returns an error → GetIdentity fails → probe errors.
	ft.recv = [][]byte{
		buildReply(t, mtid+1, codec.MTypeError, 17, codec.GroupIdentity, 0, nil),
	}
	if _, err := p.IdentityProbe(context.Background(), 1); err == nil {
		t.Fatal("IdentityProbe with NAK on card label: want error")
	}
}
