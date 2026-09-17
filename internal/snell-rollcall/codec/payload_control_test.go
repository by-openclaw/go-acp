package codec

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestGetFStat_RoundTrip(t *testing.T) {
	for _, want := range []GetFStat{{}, {Command: 1}, {Command: 0xFFFF}} {
		b := want.AppendTo(nil)
		if len(b) != GetFStatSize {
			t.Errorf("encoded %d bytes, want %d", len(b), GetFStatSize)
		}
		got, err := DecodeGetFStat(b)
		if err != nil {
			t.Fatalf("DecodeGetFStat: %v", err)
		}
		if got != want {
			t.Errorf("round trip %+v -> %+v", want, got)
		}
	}

	if b := (GetFStat{Command: 0x0102}).AppendTo(nil); !bytes.Equal(b, []byte{0x01, 0x02}) {
		t.Errorf("encoded %x, want big-endian 0102", b)
	}
	if _, err := DecodeGetFStat([]byte{0}); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
	if got := (GetFStat{Command: 7}).String(); got != "cmd=7" {
		t.Errorf("String() = %q", got)
	}
}

// TestFuncStatus_Numeric is the plain case of spec 11.5.2: an 8-byte structure
// with no tail. Value is signed 32-bit, which matters because devices publish
// checksums and offsets that overflow into the sign bit.
func TestFuncStatus_Numeric(t *testing.T) {
	tests := []struct {
		name string
		in   FuncStatus
		want string
	}{
		{
			"zero",
			FuncStatus{Command: 1, Mode: ModeValue},
			"0001 0001 00000000",
		},
		{
			"positive",
			FuncStatus{Command: 0x0102, Mode: ModeValue, Value: 1000},
			"0102 0001 000003e8",
		},
		{
			"negative",
			FuncStatus{Command: 3, Mode: ModeValue, Value: -1},
			"0003 0001 ffffffff",
		},
		{
			"largest negative",
			FuncStatus{Command: 4, Mode: ModeValue, Value: -2147483648},
			"0004 0001 80000000",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.in.AppendTo(nil)
			if err != nil {
				t.Fatalf("AppendTo: %v", err)
			}
			if want := mustHex(t, tc.want); !bytes.Equal(got, want) {
				t.Errorf("encoded %x, want %x", got, want)
			}
			back, err := DecodeFuncStatus(got)
			if err != nil {
				t.Fatalf("DecodeFuncStatus: %v", err)
			}
			if back.Command != tc.in.Command || back.Mode != tc.in.Mode || back.Value != tc.in.Value {
				t.Errorf("round trip\n got %+v\nwant %+v", back, tc.in)
			}
		})
	}
}

// TestFuncStatus_ValueAndString covers the combination the spec allows and a
// live device uses: both bits set, where the number alone would mislead and the
// string is what a user should see. A decoder that treats the two as mutually
// exclusive loses one of them.
func TestFuncStatus_ValueAndString(t *testing.T) {
	in := FuncStatus{
		Command: 0x0113,
		Mode:    ModeValue | ModeString,
		Value:   -1686180113, // as measured: a checksum, printed as hex
		Text:    "0x9B7EEEEF",
	}
	got, err := in.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	want := mustHex(t, "0113 0003 9b7eeeef"+"3078394237454545454600")
	if !bytes.Equal(got, want) {
		t.Errorf("encoded\n got %x\nwant %x", got, want)
	}

	back, err := DecodeFuncStatus(got)
	if err != nil {
		t.Fatalf("DecodeFuncStatus: %v", err)
	}
	if back.Value != in.Value || back.Text != in.Text {
		t.Errorf("= value %d text %q, want %d %q", back.Value, back.Text, in.Value, in.Text)
	}
}

// TestFuncStatus_Data covers the raw-bytes form, where the numeric field is a
// length rather than a value. Encoding must derive that length from the slice
// so the two can never disagree.
func TestFuncStatus_Data(t *testing.T) {
	in := FuncStatus{Command: 9, Mode: ModeData, Value: 999, Data: []byte{0xDE, 0xAD, 0xBE, 0xEF}}
	got, err := in.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	if want := mustHex(t, "0009 0004 00000004 deadbeef"); !bytes.Equal(got, want) {
		t.Errorf("encoded\n got %x\nwant %x", got, want)
	}

	back, err := DecodeFuncStatus(got)
	if err != nil {
		t.Fatalf("DecodeFuncStatus: %v", err)
	}
	if back.Value != 4 {
		t.Errorf("Value = %d, want the data length 4", back.Value)
	}
	if !bytes.Equal(back.Data, in.Data) {
		t.Errorf("Data = %x, want %x", back.Data, in.Data)
	}

	// Empty data is legal.
	empty, err := (FuncStatus{Mode: ModeData}).AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	if len(empty) != FuncStatusSize {
		t.Errorf("empty data encoded %d bytes, want %d", len(empty), FuncStatusSize)
	}
}

// TestFuncStatus_MatchIDFixesTheStringWidth covers the rule that makes this
// structure impossible to parse by scanning: with ModeMatchID set, the string
// field is a full 20 bytes so the trailing ID sits at a known offset. Searching
// for the terminator instead would read the ID out of the padding.
func TestFuncStatus_MatchIDFixesTheStringWidth(t *testing.T) {
	in := FuncStatus{
		Command: 0x0020,
		Mode:    ModeValue | ModeString | ModeMatchID,
		Value:   1,
		Text:    "on",
		MatchID: 483,
	}
	got, err := in.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}

	want := mustHex(t, "0020 0023 00000001"+
		"6f6e000000000000000000000000000000000000"+ // "on" in a 20-byte field
		"01e3")
	if !bytes.Equal(got, want) {
		t.Errorf("encoded\n got %x\nwant %x", got, want)
	}
	if len(got) != FuncStatusSize+MaxTextSize+2 {
		t.Fatalf("encoded %d bytes, want %d", len(got), FuncStatusSize+MaxTextSize+2)
	}

	back, err := DecodeFuncStatus(got)
	if err != nil {
		t.Fatalf("DecodeFuncStatus: %v", err)
	}
	if back.Text != "on" || back.MatchID != 483 {
		t.Errorf("= text %q id %d, want %q 483", back.Text, back.MatchID, "on")
	}

	// Without the ID bit the string is variable-width, so the same text
	// produces a shorter frame. This is the difference a scanning parser
	// would get wrong.
	short := in
	short.Mode &^= ModeMatchID
	sb, err := short.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	if len(sb) != FuncStatusSize+3 {
		t.Errorf("variable-width form is %d bytes, want %d", len(sb), FuncStatusSize+3)
	}
}

func TestFuncStatus_Errors(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want error
	}{
		{"short header", make([]byte, FuncStatusSize-1), ErrShortBuffer},
		{
			"data runs past the payload",
			mustHex(t, "0009 0004 000000ff deadbeef"),
			ErrShortBuffer,
		},
		{
			"fixed string truncated by the match-id form",
			mustHex(t, "0020 0023 00000001 6f6e00"),
			ErrShortBuffer,
		},
		{
			"match id missing",
			mustHex(t, "0020 0021 00000001"),
			ErrShortBuffer,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DecodeFuncStatus(tc.in); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}

	// A negative length in the data form is a malformed frame, not a panic.
	_, err := DecodeFuncStatus(mustHex(t, "0009 0004 ffffffff"))
	if err == nil {
		t.Error("a negative data length must be rejected")
	}

	// Text too long for the 16-bit field is refused rather than truncated.
	long := FuncStatus{Mode: ModeString, Text: strings.Repeat("x", MaxTextSize)}
	if _, err := long.AppendTo(nil); !errors.Is(err, ErrStringTooLong) {
		t.Errorf("err = %v, want ErrStringTooLong", err)
	}
	long.Mode |= ModeMatchID
	if _, err := long.AppendTo(nil); !errors.Is(err, ErrStringTooLong) {
		t.Errorf("fixed-width form err = %v, want ErrStringTooLong", err)
	}
}

func TestFuncStatus_String(t *testing.T) {
	f := FuncStatus{
		Command: 5,
		Mode:    ModeValue | ModeString | ModeData | ModeMatchID,
		Value:   7,
		Text:    "x",
		Data:    []byte{1, 2},
		MatchID: 483,
	}
	got := f.String()
	for _, want := range []string{"cmd=5", "value=7", `"x"`, "data=2B", "match=483"} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q, want it to contain %q", got, want)
		}
	}
	if got := (FuncStatus{Command: 1, Mode: ModeValue}).String(); strings.Contains(got, "data=") {
		t.Errorf("String() = %q, want no data clause", got)
	}
}

// TestSetMulti covers the batch write of spec 11.5.3. The count is implied by
// the payload length, so a payload that is not a whole number of entries is a
// framing error and must not be decoded as a shorter batch.
func TestSetMulti(t *testing.T) {
	vals := []SetMulti{
		{Command: 1, Value: 100},
		{Command: 2, Value: -1},
		{Command: 0x0102, Value: -32768},
	}
	got := AppendMultiValues(nil, vals)
	want := mustHex(t, "0001 0064 0002 ffff 0102 8000")
	if !bytes.Equal(got, want) {
		t.Errorf("encoded\n got %x\nwant %x", got, want)
	}

	back, err := DecodeMultiValues(got)
	if err != nil {
		t.Fatalf("DecodeMultiValues: %v", err)
	}
	if len(back) != len(vals) {
		t.Fatalf("decoded %d entries, want %d", len(back), len(vals))
	}
	for i := range vals {
		if back[i] != vals[i] {
			t.Errorf("entry %d = %+v, want %+v", i, back[i], vals[i])
		}
	}

	// An empty batch is legal and decodes to no entries.
	if b, err := DecodeMultiValues(nil); err != nil || len(b) != 0 {
		t.Errorf("empty batch = %v, %v", b, err)
	}

	for _, n := range []int{1, 2, 3, 5, 7} {
		if _, err := DecodeMultiValues(make([]byte, n)); err == nil {
			t.Errorf("a %d-byte payload is not a whole number of entries", n)
		}
	}
}

// TestSetMulti_WideValuesSplit records the encoding of spec 9.49 for a value
// outside int16: two consecutive entries with the same command, high word
// first. The codec carries the entries; composing them is the session layer's
// job, and this test is what that layer is written against.
func TestSetMulti_WideValuesSplit(t *testing.T) {
	wide := int32(0x0001E240) // 123456, outside the 16-bit field

	pair := []SetMulti{
		{Command: 0x0042, Value: int16(uint16(wide >> 16))},
		{Command: 0x0042, Value: int16(uint16(wide))},
	}
	b := AppendMultiValues(nil, pair)
	if want := mustHex(t, "0042 0001 0042 e240"); !bytes.Equal(b, want) {
		t.Errorf("encoded %x, want %x", b, want)
	}

	back, err := DecodeMultiValues(b)
	if err != nil {
		t.Fatalf("DecodeMultiValues: %v", err)
	}
	got := int32(uint32(uint16(back[0].Value))<<16 | uint32(uint16(back[1].Value)))
	if got != wide {
		t.Errorf("recomposed %d, want %d", got, wide)
	}
}

// TestDisp covers the status display of spec 11.5.4, where a negative line
// number is a priority rather than a position.
func TestDisp(t *testing.T) {
	tests := []struct {
		name string
		in   Disp
		hex  string
	}{
		{"line 0", Disp{Line: 0, Text: "OK"}, "0000" + "4f4b000000000000000000000000000000000000"},
		{"error", Disp{Line: DisplayLineError, Text: "No input"}, "ffff" + "4e6f20696e707574000000000000000000000000"},
		{"warning", Disp{Line: DisplayLineWarning, Text: ""}, "fffe" + "0000000000000000000000000000000000000000"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.in.AppendTo(nil)
			if err != nil {
				t.Fatalf("AppendTo: %v", err)
			}
			if want := mustHex(t, tc.hex); !bytes.Equal(got, want) {
				t.Errorf("encoded\n got %x\nwant %x", got, want)
			}
			if len(got) != DispSize {
				t.Errorf("encoded %d bytes, want %d", len(got), DispSize)
			}
			back, err := DecodeDisp(got)
			if err != nil {
				t.Fatalf("DecodeDisp: %v", err)
			}
			if back != tc.in {
				t.Errorf("round trip %+v -> %+v", tc.in, back)
			}
		})
	}
}

// TestDisp_ShortText covers a device that trims the fixed field. The line still
// carries a usable message and must not be dropped.
func TestDisp_ShortText(t *testing.T) {
	got, err := DecodeDisp(append([]byte{0x00, 0x02}, "hi"...))
	if err != nil {
		t.Fatalf("DecodeDisp: %v", err)
	}
	if got.Line != 2 || got.Text != "hi" {
		t.Errorf("= %+v, want line 2 %q", got, "hi")
	}

	// The line number alone is the minimum.
	if _, err := DecodeDisp([]byte{0x00, 0x00}); err != nil {
		t.Errorf("a bare line number must decode: %v", err)
	}
	if _, err := DecodeDisp([]byte{0x00}); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
}

func TestDisp_TextTooLong(t *testing.T) {
	d := Disp{Text: strings.Repeat("x", MaxTextSize)}
	if _, err := d.AppendTo(nil); !errors.Is(err, ErrStringTooLong) {
		t.Errorf("err = %v, want ErrStringTooLong", err)
	}
}

func TestDisp_String(t *testing.T) {
	tests := []struct {
		in   Disp
		want string
	}{
		{Disp{Line: DisplayLineError, Text: "boom"}, `error "boom"`},
		{Disp{Line: DisplayLineWarning, Text: "hot"}, `warning "hot"`},
		{Disp{Line: 3, Text: "ok"}, `line=3 "ok"`},
	}
	for _, tc := range tests {
		if got := tc.in.String(); got != tc.want {
			t.Errorf("String() = %q, want %q", got, tc.want)
		}
	}
}

func TestMinInt(t *testing.T) {
	if minInt(1, 2) != 1 || minInt(2, 1) != 1 || minInt(3, 3) != 3 {
		t.Error("minInt does not return the smaller value")
	}
}
