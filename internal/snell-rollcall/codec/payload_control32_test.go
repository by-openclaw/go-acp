package codec

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestGetValue_RoundTrip(t *testing.T) {
	for _, want := range []GetValue{{}, {Command: 100}, {Command: 0x01020304}, {Command: 0xFFFFFFFF}} {
		b := want.AppendTo(nil)
		if len(b) != GetValueSize {
			t.Errorf("encoded %d bytes, want %d", len(b), GetValueSize)
		}
		got, err := DecodeGetValue(b)
		if err != nil {
			t.Fatalf("DecodeGetValue: %v", err)
		}
		if got != want {
			t.Errorf("round trip %+v -> %+v", want, got)
		}
	}

	if b := (GetValue{Command: 0x01020304}).AppendTo(nil); !bytes.Equal(b, []byte{1, 2, 3, 4}) {
		t.Errorf("encoded %x, want big-endian 01020304", b)
	}
	if _, err := DecodeGetValue([]byte{0, 0, 0}); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
	if got := (GetValue{Command: 100}).String(); got != "cmd=100" {
		t.Errorf("String() = %q", got)
	}
}

// TestValue_Numeric covers the plain form: the 12-byte structure with no tail.
// Note the field order, which is the one place this differs visibly from the
// 16-bit structure: the match ID sits between the command and the mode.
func TestValue_Numeric(t *testing.T) {
	in := Value{Command: 0x00000065, Mode: ModeValue, Val: -60}
	got, err := in.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	want := mustHex(t, "00000065"+"0000"+"0001"+"ffffffc4")
	if !bytes.Equal(got, want) {
		t.Errorf("encoded\n got %x\nwant %x", got, want)
	}
	if len(got) != ValueSize {
		t.Errorf("encoded %d bytes, want %d", len(got), ValueSize)
	}

	back, err := DecodeValue(got)
	if err != nil {
		t.Fatalf("DecodeValue: %v", err)
	}
	if back.Command != in.Command || back.Val != in.Val || back.Mode != in.Mode {
		t.Errorf("round trip\n got %+v\nwant %+v", back, in)
	}
}

// TestValue_MatchIDIsAlwaysPresent is the structural difference that matters.
// In FUNCSTATUS_STR the match ID trails the payload and exists only when the
// flag is set, which forces the string before it to be fixed-width so the ID
// stays findable. Here the ID is a declared field at a fixed offset, so the
// string is variable-length in every combination and both forms are the same
// length on the wire.
func TestValue_MatchIDIsAlwaysPresent(t *testing.T) {
	withFlag := Value{Command: 1, MatchID: 483, Mode: ModeValue | ModeString | ModeMatchID, Val: 1, Text: "on"}
	without := withFlag
	without.Mode &^= ModeMatchID

	a, err := withFlag.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	b, err := without.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	if len(a) != len(b) {
		t.Errorf("setting the match-id flag changed the length: %d vs %d", len(a), len(b))
	}
	if want := ValueSize + len("on") + 1; len(a) != want {
		t.Errorf("encoded %d bytes, want %d (no fixed-width string here)", len(a), want)
	}

	// The ID is at offset 4 either way.
	if want := mustHex(t, "01e3"); !bytes.Equal(a[4:6], want) || !bytes.Equal(b[4:6], want) {
		t.Errorf("match id is not at offset 4: %x / %x", a[4:6], b[4:6])
	}

	back, err := DecodeValue(a)
	if err != nil {
		t.Fatalf("DecodeValue: %v", err)
	}
	if back.MatchID != 483 || back.Text != "on" || back.Val != 1 {
		t.Errorf("= %+v", back)
	}
}

// TestValue_LongString covers what the extension exists for: a label longer
// than the 20 bytes the older generation can carry.
func TestValue_LongString(t *testing.T) {
	const label = "Camera 1 — Studio A wide shot, main output"

	// A command number the older generation could also name, so that the
	// projection below fails on the label alone rather than on the number.
	in := Value{Command: 0x0203, Mode: ModeValue | ModeString, Val: 7, Text: label}
	b, err := in.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	back, err := DecodeValue(b)
	if err != nil {
		t.Fatalf("DecodeValue: %v", err)
	}
	if back.Text != label {
		t.Errorf("Text = %q, want %q", back.Text, label)
	}
	if len(label) <= MaxTextSize-1 {
		t.Fatal("the test label must exceed what the 16-bit generation can carry")
	}

	// The same value cannot be carried whole by the older generation.
	narrow, err := in.ToFuncStatus()
	if err != nil {
		t.Fatalf("ToFuncStatus: %v", err)
	}
	if narrow.Text == label {
		t.Error("the 16-bit projection must have truncated the label")
	}
	if _, err := narrow.AppendTo(nil); err != nil {
		t.Errorf("the truncated form must still encode: %v", err)
	}
}

func TestValue_Data(t *testing.T) {
	in := Value{Command: 9, Mode: ModeData, Val: 999, Data: []byte{0xDE, 0xAD}}
	got, err := in.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	if want := mustHex(t, "00000009 0000 0004 00000002 dead"); !bytes.Equal(got, want) {
		t.Errorf("encoded\n got %x\nwant %x", got, want)
	}

	back, err := DecodeValue(got)
	if err != nil {
		t.Fatalf("DecodeValue: %v", err)
	}
	if back.Val != 2 {
		t.Errorf("Val = %d, want the data length 2", back.Val)
	}
	if !bytes.Equal(back.Data, in.Data) {
		t.Errorf("Data = %x, want %x", back.Data, in.Data)
	}
}

func TestValue_Errors(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want error
	}{
		{"short header", make([]byte, ValueSize-1), ErrShortBuffer},
		{"data runs past the payload", mustHexBytes("00000009 0000 0004 000000ff dead"), ErrShortBuffer},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DecodeValue(tc.in); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}

	if _, err := DecodeValue(mustHexBytes("00000009 0000 0004 ffffffff")); err == nil {
		t.Error("a negative data length must be rejected")
	}

	long := Value{Mode: ModeString, Text: strings.Repeat("x", MaxLongString)}
	if _, err := long.AppendTo(nil); !errors.Is(err, ErrStringTooLong) {
		t.Errorf("err = %v, want ErrStringTooLong", err)
	}
}

// TestValue_StringFlagWithoutText covers a peer that sets the string flag and
// ends the payload. The value is still meaningful and must not be dropped.
func TestValue_StringFlagWithoutText(t *testing.T) {
	got, err := DecodeValue(mustHexBytes("00000065 0000 0003 0000002a"))
	if err != nil {
		t.Fatalf("DecodeValue: %v", err)
	}
	if got.Val != 42 || got.Text != "" {
		t.Errorf("= %+v, want value 42 and no text", got)
	}
}

func TestValue_String(t *testing.T) {
	v := Value{
		Command: 5,
		MatchID: 483,
		Mode:    ModeValue | ModeString | ModeData | ModeMatchID,
		Val:     7,
		Text:    "x",
		Data:    []byte{1, 2},
	}
	got := v.String()
	for _, want := range []string{"cmd=5", "value=7", `"x"`, "data=2B", "match=483"} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q, want it to contain %q", got, want)
		}
	}
	if got := (Value{Command: 1, Mode: ModeValue}).String(); strings.Contains(got, "match=") {
		t.Errorf("String() = %q, want no match clause", got)
	}
}

// TestValue_Projection covers a consumer holding one value cache for both
// generations, and a provider answering a 16-bit client from a 32-bit tree.
func TestValue_Projection(t *testing.T) {
	wide := Value{Command: 0x0113, MatchID: 483, Mode: ModeValue | ModeString, Val: -60, Text: "-60 dB"}

	narrow, err := wide.ToFuncStatus()
	if err != nil {
		t.Fatalf("ToFuncStatus: %v", err)
	}
	if narrow.Command != 0x0113 || narrow.Value != -60 || narrow.Text != "-60 dB" || narrow.MatchID != 483 {
		t.Errorf("narrowed to %+v", narrow)
	}

	back := narrow.ToValue()
	if back.Command != wide.Command || back.Val != wide.Val ||
		back.Text != wide.Text || back.MatchID != wide.MatchID || back.Mode != wide.Mode {
		t.Errorf("widening changed the value:\n got %+v\nwant %+v", back, wide)
	}
}

// TestValue_ProjectionRefusesRouterCommands pins why the router command set
// requires this generation. Its commands are placed by arithmetic well above
// 0xFFFF, so there is no 16-bit spelling for them and narrowing must refuse
// rather than address a different command.
func TestValue_ProjectionRefusesRouterCommands(t *testing.T) {
	// A destination command in a large matrix, from the Full Control set.
	routerCmd := Value{Command: 0x0010_4EEF, Mode: ModeValue, Val: 3}
	if _, err := routerCmd.ToFuncStatus(); !errors.Is(err, ErrFieldRange) {
		t.Errorf("err = %v, want ErrFieldRange", err)
	}

	if _, err := (Value{Command: 0xFFFF}).ToFuncStatus(); err != nil {
		t.Errorf("the 16-bit maximum must be accepted: %v", err)
	}
	if _, err := (Value{Command: 0x10000}).ToFuncStatus(); !errors.Is(err, ErrFieldRange) {
		t.Errorf("one above the maximum must be refused, got %v", err)
	}
}
