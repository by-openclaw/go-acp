package codec

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestMenuReq_RoundTrip(t *testing.T) {
	for _, want := range []MenuReq{{}, {MenuIndex: 1}, {MenuIndex: 0x00010203}, {MenuIndex: 0xFFFFFFFF}} {
		b := want.AppendTo(nil)
		if len(b) != MenuReqSize {
			t.Errorf("encoded %d bytes, want %d", len(b), MenuReqSize)
		}
		got, err := DecodeMenuReq(b)
		if err != nil {
			t.Fatalf("DecodeMenuReq: %v", err)
		}
		if got != want {
			t.Errorf("round trip %+v -> %+v", want, got)
		}
	}

	if b := (MenuReq{MenuIndex: 0x01020304}).AppendTo(nil); !bytes.Equal(b, []byte{1, 2, 3, 4}) {
		t.Errorf("encoded %x, want big-endian 01020304", b)
	}
	if _, err := DecodeMenuReq([]byte{0, 0, 0}); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
	if got := (MenuReq{MenuIndex: 7}).String(); got != "idx=7" {
		t.Errorf("String() = %q", got)
	}
}

// TestMenuSize_EchoesTheBase pins why the answer repeats the question. A client
// may have several menu requests outstanding at once, and matching the reply to
// the request by its echoed base is what makes that safe without sequence
// state.
func TestMenuSize_EchoesTheBase(t *testing.T) {
	in := MenuSize{MenuIndex: 0x0100, MenuCount: 473}
	got := in.AppendTo(nil)
	if want := mustHex(t, "00000100 000001d9"); !bytes.Equal(got, want) {
		t.Errorf("encoded %x, want %x", got, want)
	}
	if len(got) != MenuSizeSize {
		t.Errorf("encoded %d bytes, want %d", len(got), MenuSizeSize)
	}

	back, err := DecodeMenuSize(got)
	if err != nil {
		t.Fatalf("DecodeMenuSize: %v", err)
	}
	if back != in {
		t.Errorf("round trip %+v -> %+v", in, back)
	}
	if back.MenuIndex != in.MenuIndex {
		t.Error("the reply must echo the requested base")
	}

	if _, err := DecodeMenuSize(make([]byte, MenuSizeSize-1)); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
	if got := in.String(); got != "idx=256 count=473" {
		t.Errorf("String() = %q", got)
	}
}

// specMenuItem is the MENUITEM_STR of the 2014 paper, with field widths taken
// from rc3comm.h under #pragma pack(2): 22 fixed bytes and then two
// NUL-terminated strings.
const specMenuItem = "00010203" + // rMenuIndex, 32-bit
	"0050" + // rStyle Number, still 16-bit
	"00040506" + // rCommand, 32-bit
	"ffffff9c" + // rMinRange -100, signed
	"00000064" + // rMaxRange 100
	"0001" + // rStep
	"000a" + // rDivScale
	"4c6576656c00" + // "Level"
	"25302e316620644200" // "%0.1f dB"

func wantMenuItem() MenuItem {
	return MenuItem{
		MenuIndex: 0x00010203,
		Style:     StyleNumber,
		Command:   0x00040506,
		MinRange:  -100,
		MaxRange:  100,
		Step:      1,
		DivScale:  10,
		Text:      "Level",
		Param:     "%0.1f dB",
	}
}

func TestMenuItem_Decode(t *testing.T) {
	got, err := DecodeMenuItem(mustHex(t, specMenuItem))
	if err != nil {
		t.Fatalf("DecodeMenuItem: %v", err)
	}
	if want := wantMenuItem(); got != want {
		t.Errorf("decoded\n got %+v\nwant %+v", got, want)
	}
}

func TestMenuItem_Encode(t *testing.T) {
	got, err := wantMenuItem().AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	if want := mustHex(t, specMenuItem); !bytes.Equal(got, want) {
		t.Errorf("encoded\n got %x\nwant %x", got, want)
	}
}

// TestMenuItem_NoPaddingBetweenFields pins the packing rule. The structures are
// compiled under #pragma pack(2), so the 32-bit command that follows the
// 16-bit style starts immediately at offset 6. Natural alignment would insert
// two bytes there and shift every field after it.
func TestMenuItem_NoPaddingBetweenFields(t *testing.T) {
	b, err := wantMenuItem().AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	if got := len(b) - len("Level\x00%0.1f dB\x00"); got != MenuItemSize {
		t.Fatalf("fixed part is %d bytes, want %d", got, MenuItemSize)
	}
	// The command sits at offset 6, immediately after the 16-bit style.
	if want := mustHex(t, "00040506"); !bytes.Equal(b[6:10], want) {
		t.Errorf("bytes at offset 6 are %x, want the command %x", b[6:10], want)
	}
}

// TestMenuItem_ShortStrings covers a device that stops after the text, or after
// the fixed part entirely. Dropping the line for a missing format string would
// lose a menu entry over a field that is optional in practice.
func TestMenuItem_ShortStrings(t *testing.T) {
	fixed := mustHex(t, "00000001 0050 00000002 00000000 00000064 0001 000a")

	tests := []struct {
		name  string
		in    []byte
		text  string
		param string
	}{
		{"both", append(append([]byte{}, fixed...), "a\x00b\x00"...), "a", "b"},
		{"text only", append(append([]byte{}, fixed...), "a\x00"...), "a", ""},
		{"unterminated text", append(append([]byte{}, fixed...), "abc"...), "abc", ""},
		{"neither", fixed, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DecodeMenuItem(tc.in)
			if err != nil {
				t.Fatalf("DecodeMenuItem: %v", err)
			}
			if got.Text != tc.text || got.Param != tc.param {
				t.Errorf("= %q / %q, want %q / %q", got.Text, got.Param, tc.text, tc.param)
			}
			if got.MenuIndex != 1 || got.Command != 2 {
				t.Errorf("fixed fields decoded wrongly: %+v", got)
			}
		})
	}
}

func TestMenuItem_Errors(t *testing.T) {
	if _, err := DecodeMenuItem(make([]byte, MenuItemSize-1)); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}

	m := wantMenuItem()
	m.Text = strings.Repeat("x", MaxLongString)
	if _, err := m.AppendTo(nil); !errors.Is(err, ErrStringTooLong) {
		t.Errorf("long text err = %v, want ErrStringTooLong", err)
	}
	m = wantMenuItem()
	m.Param = strings.Repeat("x", MaxLongString)
	if _, err := m.AppendTo(nil); !errors.Is(err, ErrStringTooLong) {
		t.Errorf("long param err = %v, want ErrStringTooLong", err)
	}

	// Step is 32-bit in our struct but 16-bit on the wire in both
	// generations, so an oversized span is refused rather than truncated.
	m = wantMenuItem()
	m.Step = 0x10000
	if _, err := m.AppendTo(nil); !errors.Is(err, ErrFieldRange) {
		t.Errorf("oversized step err = %v, want ErrFieldRange", err)
	}
}

func TestMenuItem_ScaleAndGating(t *testing.T) {
	if got := (MenuItem{DivScale: 0}).Scale(); got != 1 {
		t.Errorf("Scale = %d, want 1 for a zero divisor", got)
	}
	if got := (MenuItem{DivScale: 100}).Scale(); got != 100 {
		t.Errorf("Scale = %d, want 100", got)
	}

	gated := MenuItem{Style: StyleData | StyleHidden | StyleDisabled, Text: "Reserved"}
	if !gated.AccessGated() {
		t.Error("the level-gated substitution must be recognised in this generation too")
	}
	if wantMenuItem().AccessGated() {
		t.Error("an ordinary line must not read as gated")
	}
}

func TestMenuItem_String(t *testing.T) {
	got := wantMenuItem().String()
	for _, want := range []string{"idx=66051", "Number", "-100..100", `"Level"`} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q, want it to contain %q", got, want)
		}
	}
}

// TestMenuItem_Projection covers a provider holding one tree and answering both
// generations. Widening always works; narrowing truncates text, because a
// shortened label still names the line, but refuses a number that would name a
// different line.
func TestMenuItem_Projection(t *testing.T) {
	// A line whose numbers all fit the older generation. The sample item used
	// elsewhere in this file deliberately does not, which is the next test.
	wide := wantMenuItem()
	wide.MenuIndex = 0x0203
	wide.Command = 0x0506

	narrow, err := wide.ToFunc()
	if err != nil {
		t.Fatalf("ToFunc: %v", err)
	}
	if narrow.MenuIndex != 0x0203 || narrow.Command != 0x0506 {
		t.Errorf("narrowed to idx=%d cmd=%d", narrow.MenuIndex, narrow.Command)
	}
	if narrow.MinRange != -100 || narrow.MaxRange != 100 {
		t.Error("ranges are 32-bit in both generations and must survive")
	}
	if narrow.Text != "Level" {
		t.Errorf("Text = %q", narrow.Text)
	}

	// A label too long for the fixed field is cut, not refused.
	wide.Text = strings.Repeat("n", 40)
	narrow, err = wide.ToFunc()
	if err != nil {
		t.Fatalf("ToFunc: %v", err)
	}
	if len(narrow.Text) != MaxTextSize-1 {
		t.Errorf("Text is %d bytes, want %d", len(narrow.Text), MaxTextSize-1)
	}
	if _, err := narrow.AppendTo(nil); err != nil {
		t.Errorf("the truncated line must still encode: %v", err)
	}

	// Widening is lossless.
	back := narrow.ToMenuItem()
	if back.MenuIndex != uint32(narrow.MenuIndex) || back.Command != uint32(narrow.Command) ||
		back.Text != narrow.Text || back.Step != uint32(narrow.Step) {
		t.Errorf("widening changed the line: %+v", back)
	}
}

// TestMenuItem_ProjectionRefusesUnrepresentable pins the case that must never
// be papered over. A command above 0xFFFF has no 16-bit spelling, and
// truncating it would address a different command on the device.
func TestMenuItem_ProjectionRefusesUnrepresentable(t *testing.T) {
	tests := []struct {
		name string
		in   MenuItem
	}{
		{"menu index", MenuItem{MenuIndex: 0x10000}},
		{"command", MenuItem{Command: 0x10000}},
		{"step", MenuItem{Step: 0x10000}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.in.ToFunc(); !errors.Is(err, ErrFieldRange) {
				t.Errorf("err = %v, want ErrFieldRange", err)
			}
		})
	}

	// The largest representable values are accepted.
	ok := MenuItem{MenuIndex: 0xFFFF, Command: 0xFFFF, Step: 0xFFFF}
	if _, err := ok.ToFunc(); err != nil {
		t.Errorf("the 16-bit maximum must be accepted: %v", err)
	}
}
