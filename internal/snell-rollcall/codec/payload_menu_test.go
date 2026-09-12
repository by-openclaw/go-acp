package codec

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// specFunc is the 58-byte FUNC_STR of spec 11.4.1: an enabled numeric line
// running 0..100 in steps of 1, scaled by 10 and formatted as one decimal.
const specFunc = "0007" + // rMenuIndex 7
	"0050" + // rStyle Number
	"0113" + // rCommand 275
	"00000000" + // rMinRange 0
	"00000064" + // rMaxRange 100
	"0001" + // rStep 1
	"000a" + // rDivScale 10
	"4761696e00000000000000000000000000000000" + // rText "Gain"
	"25302e3166000000000000000000000000000000" // rParam "%0.1f"

func wantFunc() Func {
	return Func{
		MenuIndex: 7,
		Style:     StyleNumber,
		Command:   0x0113,
		MinRange:  0,
		MaxRange:  100,
		Step:      1,
		DivScale:  10,
		Text:      "Gain",
		Param:     "%0.1f",
	}
}

func TestFunc_Decode(t *testing.T) {
	got, err := DecodeFunc(mustHex(t, specFunc))
	if err != nil {
		t.Fatalf("DecodeFunc: %v", err)
	}
	if want := wantFunc(); got != want {
		t.Errorf("decoded\n got %+v\nwant %+v", got, want)
	}
}

func TestFunc_Encode(t *testing.T) {
	got, err := wantFunc().AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	if want := mustHex(t, specFunc); !bytes.Equal(got, want) {
		t.Errorf("encoded\n got %x\nwant %x", got, want)
	}
	if len(got) != FuncSize {
		t.Errorf("encoded %d bytes, want %d", len(got), FuncSize)
	}
}

// TestFunc_NegativeRanges pins that the range fields are signed 32-bit. Audio
// levels and offsets are routinely negative, and reading them as unsigned turns
// a -60 dB floor into four billion.
func TestFunc_NegativeRanges(t *testing.T) {
	in := wantFunc()
	in.MinRange = -600
	in.MaxRange = -1
	b, err := in.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	if want := mustHex(t, "fffffda8"); !bytes.Equal(b[6:10], want) {
		t.Errorf("MinRange encoded %x, want %x", b[6:10], want)
	}
	got, err := DecodeFunc(b)
	if err != nil {
		t.Fatalf("DecodeFunc: %v", err)
	}
	if got.MinRange != -600 || got.MaxRange != -1 {
		t.Errorf("ranges = %d..%d, want -600..-1", got.MinRange, got.MaxRange)
	}
}

// TestFunc_ScaleZeroMeansOne pins spec 7.2.2.6. Devices publish a zero divisor
// routinely, and dividing by it directly faults.
func TestFunc_ScaleZeroMeansOne(t *testing.T) {
	tests := []struct {
		div  uint16
		want uint16
	}{{0, 1}, {1, 1}, {10, 10}, {1000, 1000}}
	for _, tc := range tests {
		f := Func{DivScale: tc.div}
		if got := f.Scale(); got != tc.want {
			t.Errorf("DivScale %d Scale = %d, want %d", tc.div, got, tc.want)
		}
	}
}

// TestFunc_StepIsTheWholeSubtree pins the menu tree model. On a container line
// Step is the span of every following line in the subtree, not the count of
// immediate children, so rebuilding the tree is a pre-order walk that consumes
// Step entries per container. Treating it as a child count nests the tree
// wrongly on any menu deeper than two levels.
func TestFunc_StepIsTheWholeSubtree(t *testing.T) {
	// index 0  List      step 4   the whole subtree below it
	// index 1    List    step 2   a nested container
	// index 2      Number
	// index 3      Number
	// index 4    Number
	menu := []Func{
		{MenuIndex: 0, Style: StyleList, Step: 4, Text: "Root"},
		{MenuIndex: 1, Style: StyleList, Step: 2, Text: "Video"},
		{MenuIndex: 2, Style: StyleNumber, Text: "Gain"},
		{MenuIndex: 3, Style: StyleNumber, Text: "Offset"},
		{MenuIndex: 4, Style: StyleNumber, Text: "Mode"},
	}

	if !menu[0].Style.Container() || !menu[1].Style.Container() {
		t.Fatal("list lines must report as containers")
	}
	if menu[2].Style.Container() {
		t.Fatal("a number line is not a container")
	}

	// The root spans lines 1..4: the nested container, its two children, and
	// the sibling after it. A child count would have said 2.
	if got := int(menu[0].Step); got != len(menu)-1 {
		t.Errorf("root Step = %d, want %d", got, len(menu)-1)
	}
	// The nested container spans only its own two children.
	if menu[1].Step != 2 {
		t.Errorf("nested Step = %d, want 2", menu[1].Step)
	}

	// A pre-order walk using Step as a span reaches every line exactly once.
	seen := make([]bool, len(menu))
	var walk func(start, end int)
	walk = func(start, end int) {
		for i := start; i < end; {
			if seen[i] {
				t.Fatalf("line %d visited twice", i)
			}
			seen[i] = true
			span := 0
			if menu[i].Style.Container() {
				span = int(menu[i].Step)
			}
			if span > 0 {
				walk(i+1, i+1+span)
			}
			i += 1 + span
		}
	}
	walk(0, len(menu))
	for i, ok := range seen {
		if !ok {
			t.Errorf("line %d was never visited", i)
		}
	}
}

// TestFunc_AccessGated covers the level-gated line as it arrives in a menu
// walk. The server substitutes rather than removes, so the line count is
// identical at every user level and only the flags reveal the difference.
func TestFunc_AccessGated(t *testing.T) {
	gated := Func{
		MenuIndex: 12,
		Style:     StyleData | StyleHidden | StyleDisabled,
		Command:   0,
		Text:      "Reserved",
	}
	if !gated.AccessGated() {
		t.Error("the substituted line must be recognised as access-gated")
	}

	// The same line as a supervisor sees it.
	visible := Func{MenuIndex: 12, Style: StyleNumber, Command: 0x0201, Text: "Bias"}
	if visible.AccessGated() {
		t.Error("a normal line must not read as access-gated")
	}

	// The count is the same either way, which is why counting is useless.
	low := []Func{visible, gated}
	high := []Func{visible, visible}
	if len(low) != len(high) {
		t.Fatal("the two levels must produce the same number of lines")
	}
	var gatedCount int
	for _, f := range low {
		if f.AccessGated() {
			gatedCount++
		}
	}
	if gatedCount != 1 {
		t.Errorf("found %d gated lines, want 1", gatedCount)
	}
}

func TestFunc_Errors(t *testing.T) {
	good := mustHex(t, specFunc)
	for _, n := range []int{0, 17, 37, 57} {
		if _, err := DecodeFunc(good[:n]); !errors.Is(err, ErrShortBuffer) {
			t.Errorf("DecodeFunc(%d bytes) err = %v, want ErrShortBuffer", n, err)
		}
	}

	f := wantFunc()
	f.Text = strings.Repeat("x", MaxTextSize)
	if _, err := f.AppendTo(nil); !errors.Is(err, ErrStringTooLong) {
		t.Errorf("long text err = %v, want ErrStringTooLong", err)
	}
	f = wantFunc()
	f.Param = strings.Repeat("x", MaxTextSize)
	if _, err := f.AppendTo(nil); !errors.Is(err, ErrStringTooLong) {
		t.Errorf("long param err = %v, want ErrStringTooLong", err)
	}
}

func TestFunc_String(t *testing.T) {
	got := wantFunc().String()
	for _, want := range []string{"idx=7", "Number", "cmd=275", "0..100", `"Gain"`, `"%0.1f"`} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q, want it to contain %q", got, want)
		}
	}
}

// TestFuncStyle covers the back-channel style update of spec 11.4.2. It may
// only change the hidden and disabled bits; a client that rebuilt the whole
// line from it would drop the text, ranges and format it never carried.
func TestFuncStyle(t *testing.T) {
	in := FuncStyle{MenuIndex: 7, Style: StyleNumber | StyleDisabled, Command: 0x0113}
	got := in.AppendTo(nil)
	if want := mustHex(t, "0007 0054 0113"); !bytes.Equal(got, want) {
		t.Errorf("encoded %x, want %x", got, want)
	}
	if len(got) != FuncStyleSize {
		t.Errorf("encoded %d bytes, want %d", len(got), FuncStyleSize)
	}

	back, err := DecodeFuncStyle(got)
	if err != nil {
		t.Fatalf("DecodeFuncStyle: %v", err)
	}
	if back != in {
		t.Errorf("round trip %+v -> %+v", in, back)
	}
	if !back.Style.Disabled() || back.Style.Kind() != StyleNumber {
		t.Errorf("Style = %s, want a disabled number line", back.Style)
	}

	if _, err := DecodeFuncStyle(make([]byte, FuncStyleSize-1)); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
	if s := in.String(); !strings.Contains(s, "idx=7") || !strings.Contains(s, "cmd=275") {
		t.Errorf("String() = %q", s)
	}
}

// TestFuncStyle_DeferredIsVendorOnly records the Rope engine's hold-the-redraw
// bit. It is not in the specification's style table, so a client must tolerate
// it rather than reject the line.
func TestFuncStyle_DeferredIsVendorOnly(t *testing.T) {
	in := FuncStyle{MenuIndex: 1, Style: StyleNumber | StyleDeferred}
	back, err := DecodeFuncStyle(in.AppendTo(nil))
	if err != nil {
		t.Fatalf("DecodeFuncStyle: %v", err)
	}
	if !back.Style.Deferred() {
		t.Error("the deferred bit must survive a round trip")
	}
	if back.Style.Kind() != StyleNumber {
		t.Errorf("Kind = %s, want the deferred bit not to disturb it", back.Style.Kind())
	}
}

// TestBlockHeader covers the multi-packet opener of spec 11.2.5. PktType names
// the request that produced the transfer, which is how a client knows what the
// items it is about to fetch will be.
func TestBlockHeader(t *testing.T) {
	in := BlockHeader{PktType: MsgGetFunc, Count: 473, MaxSize: 58, Function: 0}
	got := in.AppendTo(nil)
	if want := mustHex(t, "08 00 01d9 003a 0000"); !bytes.Equal(got, want) {
		t.Errorf("encoded %x, want %x", got, want)
	}
	if len(got) != BlockHeaderSize {
		t.Errorf("encoded %d bytes, want %d", len(got), BlockHeaderSize)
	}

	back, err := DecodeBlockHeader(got)
	if err != nil {
		t.Fatalf("DecodeBlockHeader: %v", err)
	}
	if back != in {
		t.Errorf("round trip %+v -> %+v", in, back)
	}
	if back.PktType != MsgGetFunc {
		t.Errorf("PktType = %s, want GETFUNC", back.PktType)
	}

	if _, err := DecodeBlockHeader(make([]byte, BlockHeaderSize-1)); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
	if s := in.String(); !strings.Contains(s, "GETFUNC") || !strings.Contains(s, "count=473") {
		t.Errorf("String() = %q", s)
	}
}

// TestBlockHeader_SpareMustBeZero separates the two padding cases. The spec
// declares this byte "Must be zero", so a non-zero value is a real deviation
// and is preserved rather than masked, unlike the undefined pad in GetNext.
func TestBlockHeader_SpareMustBeZero(t *testing.T) {
	if h, err := DecodeBlockHeader(mustHex(t, "08 00 0001 0002 0003")); err != nil || h.Spare != 0 {
		t.Errorf("= %+v, %v; want Spare 0", h, err)
	}
	h, err := DecodeBlockHeader(mustHex(t, "08 cd 0001 0002 0003"))
	if err != nil {
		t.Fatalf("a non-zero spare must still decode: %v", err)
	}
	if h.Spare != 0xCD {
		t.Errorf("Spare = %02X, want it preserved so the deviation is reportable", h.Spare)
	}
}

// TestGetNext covers the item request of spec 11.2.6, whose declared size and
// wire size differ. Encoding four bytes with a zeroed pad is what the vendor
// expects; accepting three is what a peer that trims it needs.
func TestGetNext(t *testing.T) {
	in := GetNext{Index: 42, PktType: MsgGetFunc}
	got := in.AppendTo(nil)
	if want := mustHex(t, "002a 08 00"); !bytes.Equal(got, want) {
		t.Errorf("encoded %x, want %x", got, want)
	}
	if len(got) != GetNextSize {
		t.Errorf("encoded %d bytes, want %d (three declared plus a pad)", len(got), GetNextSize)
	}

	back, err := DecodeGetNext(got)
	if err != nil {
		t.Fatalf("DecodeGetNext: %v", err)
	}
	if back != in {
		t.Errorf("round trip %+v -> %+v", in, back)
	}

	if s := in.String(); !strings.Contains(s, "idx=42") || !strings.Contains(s, "GETFUNC") {
		t.Errorf("String() = %q", s)
	}
}

// TestGetNext_PadIsUndefined pins that the fourth byte carries no meaning. The
// vendor packs to a two-byte boundary and senders use sizeof, so whatever was
// in the stack frame goes on the wire. Reporting it as a deviation would fire
// on every device.
func TestGetNext_PadIsUndefined(t *testing.T) {
	for _, pad := range []byte{0x00, 0xCD, 0xFF} {
		got, err := DecodeGetNext([]byte{0x00, 0x2A, 0x08, pad})
		if err != nil {
			t.Fatalf("pad %02X: %v", pad, err)
		}
		if got.Index != 42 || got.PktType != MsgGetFunc {
			t.Errorf("pad %02X changed the decode: %+v", pad, got)
		}
	}

	// Three bytes is the declared size and must be accepted.
	got, err := DecodeGetNext([]byte{0x00, 0x2A, 0x08})
	if err != nil {
		t.Fatalf("three-byte form: %v", err)
	}
	if got.Index != 42 || got.PktType != MsgGetFunc {
		t.Errorf("three-byte form = %+v", got)
	}

	if _, err := DecodeGetNext([]byte{0x00, 0x2A}); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
}
