package acp1

import (
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/export/canonical"
)

// TestEncodeObject_NumericFieldErrors isolates each numeric coercion error
// branch (value / default / step / min / max) of encodeInteger / Long / Byte /
// Float / IPAddr by feeding one out-of-range field at a time while keeping the
// others valid, so execution reaches the targeted coercion.
func TestEncodeObject_NumericFieldErrors(t *testing.T) {
	types := []struct {
		name  string
		ty    codec.ObjectType
		ct    string
		fmt   string
		valid any
		bad   any
	}{
		{"integer", codec.TypeInteger, canonical.ParamInteger, "", int64(0), int64(99999)},
		{"long", codec.TypeLong, canonical.ParamInteger, "int32", int64(0), int64(1) << 40},
		{"byte", codec.TypeByte, canonical.ParamInteger, "uint8", int64(0), int64(999)},
		{"float", codec.TypeFloat, canonical.ParamReal, "", float64(0), "x"},
		{"ipaddr", codec.TypeIPAddr, canonical.ParamString, "ipv4", "1.2.3.4", "bad.addr"},
	}
	fields := []struct {
		name string
		opt  func(any) paramOpt
	}{
		{"value", func(v any) paramOpt { return withValue(v) }},
		{"default", func(v any) paramOpt { return withDefault(v) }},
		{"step", func(v any) paramOpt { return withStep(v) }},
		{"min", func(v any) paramOpt { return withMin(v) }},
		{"max", func(v any) paramOpt { return withMax(v) }},
	}
	for _, ty := range types {
		for _, f := range fields {
			t.Run(ty.name+"/"+f.name, func(t *testing.T) {
				opts := []paramOpt{withValue(ty.valid)}
				if ty.fmt != "" {
					opts = append(opts, withFormat(ty.fmt))
				}
				if f.name == "value" {
					opts = []paramOpt{f.opt(ty.bad)}
					if ty.fmt != "" {
						opts = append(opts, withFormat(ty.fmt))
					}
				} else {
					opts = append(opts, f.opt(ty.bad))
				}
				e := &entry{acpType: ty.ty, access: codec.AccessRead, param: param("x", ty.ct, opts...)}
				if _, err := encodeObject(e); err == nil {
					t.Errorf("%s/%s: expected coercion error", ty.name, f.name)
				}
			})
		}
	}
}

func TestEncodeEnum_Errors(t *testing.T) {
	// value not coercible to uint8.
	if _, err := encodeObject(&entry{acpType: codec.TypeEnum, access: codec.AccessRead,
		param: param("e", canonical.ParamEnum, withValue("nope"))}); err == nil {
		t.Error("enum bad value should error")
	}
	// no items at all.
	if _, err := encodeObject(&entry{acpType: codec.TypeEnum, access: codec.AccessRead,
		param: param("e", canonical.ParamEnum, withValue(int64(0)))}); err == nil {
		t.Error("enum with no items should error")
	}
}

func TestEncodeString_And_File_Errors(t *testing.T) {
	// string value not a string.
	if _, err := encodeObject(&entry{acpType: codec.TypeString, access: codec.AccessRead,
		param: param("s", canonical.ParamString, withValue(12345))}); err == nil {
		t.Error("string bad value should error")
	}
	// file: value (name) not a string.
	if _, err := encodeObject(&entry{acpType: codec.TypeFile, access: codec.AccessRead,
		param: param("f", canonical.ParamString, withFormat("file"), withValue(123))}); err == nil {
		t.Error("file bad name should error")
	}
	// file: num_fragments (Default) not an int.
	if _, err := encodeObject(&entry{acpType: codec.TypeFile, access: codec.AccessRead,
		param: param("f", canonical.ParamString, withFormat("file"), withValue("fw.bin"), withDefault("x"))}); err == nil {
		t.Error("file bad num_fragments should error")
	}
}

func TestEncodeFrame_BadValue(t *testing.T) {
	// Frame value must be []any; a string triggers frameSlotStatuses error.
	if _, err := encodeObject(&entry{acpType: codec.TypeFrame, access: codec.AccessRead,
		param: param("frame", canonical.ParamOctets, withFormat("frame"), withValue("not-a-frame"))}); err == nil {
		t.Error("frame bad value should error")
	}
}

// TestEncodeAlarm_LongMessages drives cstrWithLimit truncation via over-length
// on/off event messages, and exercises alarmMessages parsing from Description.
func TestEncodeAlarm_LongMessages(t *testing.T) {
	long := "this-alarm-message-is-considerably-longer-than-the-thirty-two-byte-wire-limit"
	desc := "on: " + long + " / off: " + long
	p := param("Alarm", canonical.ParamBoolean, withFormat("alarm"), withValue(true))
	p.Description = &desc
	b, err := encodeObject(&entry{acpType: codec.TypeAlarm, access: codec.AccessRead, param: p})
	if err != nil {
		t.Fatalf("encodeAlarm: %v", err)
	}
	if o, err := codec.DecodeObject(b); err != nil {
		t.Fatalf("decode: %v", err)
	} else if len(o.EventOnMsg) > codec.MaxAlarmMsg || len(o.EventOffMsg) > codec.MaxAlarmMsg {
		t.Errorf("alarm messages not truncated: on=%d off=%d", len(o.EventOnMsg), len(o.EventOffMsg))
	}
}
