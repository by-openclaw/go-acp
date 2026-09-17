package rollcall

import (
	"context"
	"strings"
	"testing"

	"dhs/internal/snell-rollcall/codec"
)

// A value read and a value written have to mean the same thing in both
// generations, because one device serves both at once and the two clients are
// looking at the same object.

func TestValueRoundTripsInBothGenerations(t *testing.T) {
	s := newServed(t, testTree())
	slot, gain := commandOf(t, s.p, "frame.card1.video.gain")

	old := s.open(slot, codec.SvcControl)
	modern := s.open(slot, codec.SvcControl|codec.SvcLongStr)

	// Written through the older generation.
	payload, err := codec.FuncStatus{
		Command: uint16(gain), Mode: codec.ModeValue, Value: -120,
	}.AppendTo(nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	fs, err := codec.DecodeFuncStatus(do(t, old, codec.MsgSetParam, payload).Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if fs.Value != -120 {
		t.Errorf("write returned %d, want -120", fs.Value)
	}

	// Read back through the newer one: the same object, the same number.
	v, err := codec.DecodeValue(do(t, modern, codec.MsgGetValue,
		codec.GetValue{Command: gain}.AppendTo(nil)).Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v.Val != -120 || v.Command != gain {
		t.Errorf("read back command %d = %d, want command %d = -120", v.Command, v.Val, gain)
	}
}

func TestAWriteOutOfRangeIsClampedAndTheReplySaysSo(t *testing.T) {
	s := newServed(t, testTree())
	slot, gain := commandOf(t, s.p, "frame.card1.video.gain")

	sess := s.open(slot, codec.SvcControl|codec.SvcLongStr)

	// The gain runs -60 to +6 dB, carried scaled by ten. Asking for more is
	// not an error: the device clamps, and the reply is what says what was
	// stored, so a client learns without reading back.
	payload, err := codec.Value{Command: gain, Mode: codec.ModeValue, Val: 9999}.AppendTo(nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	v, err := codec.DecodeValue(do(t, sess, codec.MsgSetValue, payload).Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v.Val != 60 {
		t.Errorf("clamped to %d, want 60", v.Val)
	}

	payload, _ = codec.Value{Command: gain, Mode: codec.ModeValue, Val: -9999}.AppendTo(nil)
	v, err = codec.DecodeValue(do(t, sess, codec.MsgSetValue, payload).Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v.Val != -600 {
		t.Errorf("clamped to %d, want -600", v.Val)
	}
}

func TestACheckboxIsNotClampedAgainstItsOnValue(t *testing.T) {
	s := newServed(t, testTree())
	slot, enable := commandOf(t, s.p, "frame.card1.video.enable")

	sess := s.open(slot, codec.SvcControl|codec.SvcLongStr)

	// On a checkbox the minimum field carries the "on" value rather than a
	// bound, so clamping against it would make the box impossible to clear.
	payload, _ := codec.Value{Command: enable, Mode: codec.ModeValue, Val: 0}.AppendTo(nil)
	v, err := codec.DecodeValue(do(t, sess, codec.MsgSetValue, payload).Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v.Val != 0 {
		t.Errorf("clearing a checkbox stored %d, want 0", v.Val)
	}
}

func TestAStringValueRoundTrips(t *testing.T) {
	s := newServed(t, testTree())
	slot, name := commandOf(t, s.p, "frame.card1.video.name")

	sess := s.open(slot, codec.SvcControl|codec.SvcLongStr)
	payload, err := codec.Value{
		Command: name, Mode: codec.ModeString, Text: "Camera 4",
	}.AppendTo(nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	v, err := codec.DecodeValue(do(t, sess, codec.MsgSetValue, payload).Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v.Text != "Camera 4" {
		t.Errorf("stored %q", v.Text)
	}

	// The older generation sees the same value cut to its fixed field.
	old := s.open(slot, codec.SvcControl)
	fs, err := codec.DecodeFuncStatus(do(t, old, codec.MsgGetFStat,
		codec.GetFStat{Command: uint16(name)}.AppendTo(nil)).Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if fs.Text != "Camera 4" {
		t.Errorf("16-bit read gave %q", fs.Text)
	}
}

func TestALongStringIsCutForTheOlderGenerationOnly(t *testing.T) {
	s := newServed(t, testTree())
	slot, name := commandOf(t, s.p, "frame.card1.video.name")

	long := "a name of more than nineteen bytes"
	if _, err := s.p.SetValue(context.Background(), "frame.card1.video.name", long); err != nil {
		t.Fatalf("SetValue: %v", err)
	}

	old := s.open(slot, codec.SvcControl)
	fs, err := codec.DecodeFuncStatus(do(t, old, codec.MsgGetFStat,
		codec.GetFStat{Command: uint16(name)}.AppendTo(nil)).Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(fs.Text) >= len(long) {
		t.Errorf("the 16-bit reply carried %q, which does not fit its field", fs.Text)
	}
	if !strings.HasPrefix(long, fs.Text) {
		t.Errorf("the truncation changed the text: %q", fs.Text)
	}
}

func TestAPresetWriteRestoresTheDefault(t *testing.T) {
	s := newServed(t, testTree())
	slot, gain := commandOf(t, s.p, "frame.card1.video.gain")

	sess := s.open(slot, codec.SvcControl|codec.SvcLongStr)

	// The preset flag asks the device for its own default, and the numeric
	// field is ignored: a client sends zero and gets the default back.
	payload, _ := codec.Value{Command: gain, Mode: codec.ModePreset, Val: 0}.AppendTo(nil)
	v, err := codec.DecodeValue(do(t, sess, codec.MsgSetValue, payload).Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v.Val != -600 {
		t.Errorf("default came back as %d, want -600", v.Val)
	}
}

func TestAWriteThatNamesAnotherUnitTypeIsIgnored(t *testing.T) {
	s := newServed(t, testTree())
	slot, gain := commandOf(t, s.p, "frame.card1.video.gain")

	sess := s.open(slot, codec.SvcControl|codec.SvcLongStr)
	before, err := codec.DecodeValue(do(t, sess, codec.MsgGetValue,
		codec.GetValue{Command: gain}.AppendTo(nil)).Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	// A controller writes one command to a whole frame; the cards it does not
	// mean answer with what they already had rather than applying it.
	payload, _ := codec.Value{
		Command: gain, MatchID: 0xBEEF,
		Mode: codec.ModeValue | codec.ModeMatchID, Val: 55,
	}.AppendTo(nil)
	v, err := codec.DecodeValue(do(t, sess, codec.MsgSetValue, payload).Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v.Val != before.Val {
		t.Errorf("a write for another unit type changed the value to %d", v.Val)
	}

	// A match ID of zero means everybody, so this one applies.
	payload, _ = codec.Value{
		Command: gain, MatchID: 0,
		Mode: codec.ModeValue | codec.ModeMatchID, Val: 55,
	}.AppendTo(nil)
	v, err = codec.DecodeValue(do(t, sess, codec.MsgSetValue, payload).Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v.Val != 55 {
		t.Errorf("a write for every unit type stored %d, want 55", v.Val)
	}
}

func TestAMatchedWriteForAnUnknownCommandIsRefused(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcControl|codec.SvcLongStr)

	payload, _ := codec.Value{
		Command: 4242, MatchID: 0xBEEF,
		Mode: codec.ModeValue | codec.ModeMatchID,
	}.AppendTo(nil)
	if got := refused(t, sess, codec.MsgSetValue, payload); got.Type != codec.MsgNack {
		t.Errorf("answered %s, want Nack", got.Type)
	}
}

func TestAWriteToAReadOnlyLineIsRefused(t *testing.T) {
	s := newServed(t, testTree())
	slot, status := commandOf(t, s.p, "frame.card1.status")

	sess := s.open(slot, codec.SvcControl|codec.SvcLongStr)
	payload, _ := codec.Value{Command: status, Mode: codec.ModeValue, Val: 1}.AppendTo(nil)
	if got := refused(t, sess, codec.MsgSetValue, payload); got.Type != codec.MsgNack {
		t.Errorf("answered %s, want Nack", got.Type)
	}
}

func TestControlRefusals(t *testing.T) {
	s := newServed(t, testTree())

	empty := s.open(0x40, codec.SvcControl|codec.SvcLongStr)
	if got := refused(t, empty, codec.MsgGetValue, codec.GetValue{}.AppendTo(nil)); got.Type != codec.MsgNack {
		t.Errorf("an empty slot answered %s to a read, want Nack", got.Type)
	}
	if got := refused(t, empty, codec.MsgSetValue, make([]byte, codec.ValueSize)); got.Type != codec.MsgNack {
		t.Errorf("an empty slot answered %s to a write, want Nack", got.Type)
	}

	noControl := s.open(1, codec.SvcMenus)
	if got := refused(t, noControl, codec.MsgGetValue, codec.GetValue{}.AppendTo(nil)); got.Type != codec.MsgNack {
		t.Errorf("a session without the control service read anyway: %s", got.Type)
	}
	if got := refused(t, noControl, codec.MsgSetValue, make([]byte, codec.ValueSize)); got.Type != codec.MsgNack {
		t.Errorf("a session without the control service wrote anyway: %s", got.Type)
	}

	sess := s.open(1, codec.SvcControl|codec.SvcLongStr)
	for _, tc := range []struct {
		name    string
		typ     codec.PacketType
		payload []byte
	}{
		{"a short value request", codec.MsgGetValue, []byte{0x01}},
		{"a short status request", codec.MsgGetFStat, []byte{0x01}},
		{"a short value write", codec.MsgSetValue, []byte{0x01}},
		{"a short parameter write", codec.MsgSetParam, []byte{0x01}},
		{"a read of no such command", codec.MsgGetValue,
			codec.GetValue{Command: 4242}.AppendTo(nil)},
		{"a 16-bit read of no such command", codec.MsgGetFStat,
			codec.GetFStat{Command: 4242}.AppendTo(nil)},
	} {
		if got := refused(t, sess, tc.typ, tc.payload); got.Type != codec.MsgNack {
			t.Errorf("%s answered %s, want Nack", tc.name, got.Type)
		}
	}

	if !hasEvent(s.p, EventUnknownCommand) {
		t.Error("a command that is not in the menu should be recorded")
	}
}

func TestAValueTooLongToEncodeIsRefusedRatherThanTruncated(t *testing.T) {
	s := newServed(t, testTree())
	slot, name := commandOf(t, s.p, "frame.card1.video.name")

	// Set through the Go API, which does not go through the wire and so is not
	// bounded by it. Reading it back in the 32-bit generation cannot encode:
	// the reply says so rather than silently sending a shortened string.
	huge := strings.Repeat("x", codec.MaxLongString+10)
	if _, err := s.p.SetValue(context.Background(), "frame.card1.video.name", huge); err != nil {
		t.Fatalf("SetValue: %v", err)
	}

	sess := s.open(slot, codec.SvcControl|codec.SvcLongStr)
	if got := refused(t, sess, codec.MsgGetValue,
		codec.GetValue{Command: name}.AppendTo(nil)); got.Type != codec.MsgNack {
		t.Errorf("answered %s, want Nack", got.Type)
	}
}

func TestSetValueThroughTheAPI(t *testing.T) {
	s := newServed(t, testTree())

	got, err := s.p.SetValue(context.Background(), "frame.card2.level", 7)
	if err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	if got != int64(7) {
		t.Errorf("stored %v, want 7", got)
	}

	if got, err = s.p.SetValue(context.Background(), "frame.card1.video.enable", false); err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	if got != int64(0) {
		t.Errorf("stored %v, want 0", got)
	}

	if got, err = s.p.SetValue(context.Background(), "frame.card1.video.name", "Studio"); err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	if got != "Studio" {
		t.Errorf("stored %v, want Studio", got)
	}

	// A real is given in the units a caller thinks in and stored scaled.
	if _, err = s.p.SetValue(context.Background(), "frame.card1.video.gain", -6.0); err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	slot, gain := commandOf(t, s.p, "frame.card1.video.gain")
	v, _ := s.p.model.port(slot).value(gain)
	if v.Val != -60 {
		t.Errorf("-6.0 dB stored as %d, want -60", v.Val)
	}
}

func TestSetValueRejectsWhatItCannotAddress(t *testing.T) {
	s := newServed(t, testTree())

	if _, err := s.p.SetValue(context.Background(), "frame.nothing.here", 1); err == nil {
		t.Error("writing to a path that does not exist should fail")
	}
	// A container has no command behind it, so it cannot be written either.
	if _, err := s.p.SetValue(context.Background(), "frame.card1.video", 1); err == nil {
		t.Error("writing to a container should fail")
	}
	if _, err := s.p.SetValue(context.Background(), "frame.card1.status", 1); err == nil {
		t.Error("writing to a read-only parameter should fail")
	}
}
