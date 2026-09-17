package codec

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestLogPacket pins LOGPACKET_STR: a time structure, a format word, the
// source unit's type id, its name in a fixed field, and then the message.
//
// The source is named twice over because a log server collects from many units
// and the frame's own source address says only which gateway forwarded it.
func TestLogPacket(t *testing.T) {
	in := LogPacket{
		Time:   NewSysTime(time.Date(2026, 9, 7, 3, 7, 55, 0, time.UTC)),
		Format: 0x0102,
		ID:     636,
		Name:   "Router Matrix",
		Text:   "level 3 destination 40 protected",
	}

	got, err := in.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	if len(got) < LogPacketSize {
		t.Fatalf("encoded %d bytes, want at least %d", len(got), LogPacketSize)
	}
	// The fixed part is the time, the two words and the twenty-byte name.
	if want := TimeSize + 4 + MaxTextSize; want != LogPacketSize {
		t.Fatalf("the fixed part is %d bytes, not %d", want, LogPacketSize)
	}

	back, err := DecodeLogPacket(got)
	if err != nil {
		t.Fatalf("DecodeLogPacket: %v", err)
	}
	if back.Format != in.Format || back.ID != in.ID || back.Name != in.Name || back.Text != in.Text {
		t.Errorf("round trip\n got %+v\nwant %+v", back, in)
	}
	when, ok := back.Time.Time()
	if !ok || when.Hour() != 3 {
		t.Errorf("time = %s,%v", when, ok)
	}

	// The type id names the product, which is what makes a log line legible
	// without the operator knowing the number.
	if got := UnitTypeName(back.ID); got != "Router Matrix" {
		t.Errorf("id %d is %q", back.ID, got)
	}
}

// TestLogPacket_NoText covers a log entry that is just an event with no
// message, which is how a unit reports a state change.
func TestLogPacket_NoText(t *testing.T) {
	in := LogPacket{Name: "Vega"}

	got, err := in.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	if len(got) != LogPacketSize {
		t.Errorf("encoded %d bytes, want exactly the fixed part %d", len(got), LogPacketSize)
	}

	back, err := DecodeLogPacket(got)
	if err != nil {
		t.Fatalf("DecodeLogPacket: %v", err)
	}
	if back.Text != "" || back.Name != "Vega" {
		t.Errorf("= %+v", back)
	}
}

func TestLogPacket_Errors(t *testing.T) {
	if _, err := DecodeLogPacket(make([]byte, LogPacketSize-1)); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}

	long := LogPacket{Name: strings.Repeat("n", MaxTextSize)}
	if _, err := long.AppendTo(nil); !errors.Is(err, ErrStringTooLong) {
		t.Errorf("err = %v, want ErrStringTooLong", err)
	}

	overText := LogPacket{Text: strings.Repeat("t", MaxLongString)}
	if _, err := overText.AppendTo(nil); !errors.Is(err, ErrStringTooLong) {
		t.Errorf("err = %v, want ErrStringTooLong", err)
	}
}

func TestLogPacket_String(t *testing.T) {
	l := LogPacket{
		Time: NewSysTime(time.Date(2026, 9, 7, 3, 7, 55, 0, time.UTC)),
		Name: "Vega",
		Text: "input lost",
	}
	got := l.String()
	for _, want := range []string{"2026-09-07", "Vega", "input lost"} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q, want it to contain %q", got, want)
		}
	}
}

func TestStreamMode(t *testing.T) {
	in := StreamMode{Stream: 3, Mode: StreamBinary, MaxSize: 420}

	got := in.AppendTo(nil)
	if want := mustHex(t, "03 01 01a4"); !bytes.Equal(got, want) {
		t.Errorf("encoded %x, want %x", got, want)
	}
	if len(got) != StreamModeSize {
		t.Errorf("encoded %d bytes, want %d", len(got), StreamModeSize)
	}

	back, err := DecodeStreamMode(got)
	if err != nil {
		t.Fatalf("DecodeStreamMode: %v", err)
	}
	if back != in {
		t.Errorf("round trip %+v -> %+v", in, back)
	}
	if !back.Binary() {
		t.Error("the binary flag was lost")
	}
	if (StreamMode{}).Binary() {
		t.Error("a zero mode is not binary")
	}

	if _, err := DecodeStreamMode(make([]byte, StreamModeSize-1)); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
	if got := in.String(); !strings.Contains(got, "stream=3") {
		t.Errorf("String() = %q", got)
	}
}

// TestStreamHeader covers a block of stream data. Like the file service it
// carries a handle from each end, so neither has to agree with the other on
// numbering.
func TestStreamHeader(t *testing.T) {
	in := StreamHeader{ClientHandle: 1, ServerHandle: 2, Command: 3, Data: []byte{0xDE, 0xAD}}

	got, err := in.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	if want := mustHex(t, "0001 0002 0003 0002 dead"); !bytes.Equal(got, want) {
		t.Errorf("encoded\n got %x\nwant %x", got, want)
	}

	back, err := DecodeStreamHeader(got)
	if err != nil {
		t.Fatalf("DecodeStreamHeader: %v", err)
	}
	if back.ClientHandle != 1 || back.ServerHandle != 2 || back.Command != 3 {
		t.Errorf("= %+v", back)
	}
	if !bytes.Equal(back.Data, in.Data) {
		t.Errorf("data = %x, want %x", back.Data, in.Data)
	}

	// An empty block is legal: it is how a stream is closed.
	empty, err := (StreamHeader{ClientHandle: 1}).AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	if len(empty) != StreamHdrSize {
		t.Errorf("an empty block is %d bytes, want %d", len(empty), StreamHdrSize)
	}
}

func TestStreamHeader_Errors(t *testing.T) {
	if _, err := DecodeStreamHeader(make([]byte, StreamHdrSize-1)); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
	// A length longer than the block.
	if _, err := DecodeStreamHeader(mustHex(t, "0001 0002 0003 00ff dead")); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
	// The length is signed, so a negative one is malformed rather than huge.
	if _, err := DecodeStreamHeader(mustHex(t, "0001 0002 0003 ffff")); err == nil {
		t.Error("a negative length must be rejected")
	}
	// More data than a frame can carry.
	big := StreamHeader{Data: make([]byte, MaxPayload+1)}
	if _, err := big.AppendTo(nil); !errors.Is(err, ErrPayloadTooLong) {
		t.Errorf("err = %v, want ErrPayloadTooLong", err)
	}
	if got := (StreamHeader{Data: []byte{1}}).String(); !strings.Contains(got, "1B") {
		t.Errorf("String() = %q", got)
	}
}

// TestDisplayCaps covers what a unit says about its display, which is what
// tells a client whether it is driving a character panel or a bitmap screen.
func TestDisplayCaps(t *testing.T) {
	panel := DisplayCaps{Display: 0, Type: 1, Chars: 20, Lines: 4}

	got := panel.AppendTo(nil)
	if want := mustHex(t, "00 01 0014 0004 0000 0000 0000"); !bytes.Equal(got, want) {
		t.Errorf("encoded\n got %x\nwant %x", got, want)
	}
	if len(got) != DisplayCapsSize {
		t.Errorf("encoded %d bytes, want %d", len(got), DisplayCapsSize)
	}

	back, err := DecodeDisplayCaps(got)
	if err != nil {
		t.Fatalf("DecodeDisplayCaps: %v", err)
	}
	if back != panel {
		t.Errorf("round trip %+v -> %+v", panel, back)
	}
	if !back.IsCharacter() {
		t.Error("a display with no pixels is a character display")
	}
	if got := back.String(); !strings.Contains(got, "20x4 chars") {
		t.Errorf("String() = %q", got)
	}

	screen := DisplayCaps{Display: 1, XPixels: 320, YPixels: 240, Format: ColourFormat16Bit}
	if screen.IsCharacter() {
		t.Error("a display with pixels is not a character display")
	}
	if got := screen.String(); !strings.Contains(got, "320x240 px") || !strings.Contains(got, "16-bit") {
		t.Errorf("String() = %q", got)
	}

	if _, err := DecodeDisplayCaps(make([]byte, DisplayCapsSize-1)); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
}

// TestColourFormats pins the bits-per-pixel values, which are the numbers
// themselves rather than an enumeration, except the two-bit case.
func TestColourFormats(t *testing.T) {
	tests := map[int16]int16{
		ColourFormat2Bit: 1, ColourFormat4Bit: 4, ColourFormat8Bit: 8,
		ColourFormat15Bit: 15, ColourFormat16Bit: 16, ColourFormat24Bit: 24,
	}
	for got, want := range tests {
		if got != want {
			t.Errorf("colour format = %d, want %d", got, want)
		}
	}
}

func TestDrawBitmap(t *testing.T) {
	in := DrawBitmap{Display: 1, Bitmap: []byte{0xAA, 0xBB, 0xCC}}

	got := in.AppendTo(nil)
	if want := mustHex(t, "01 00 aabbcc"); !bytes.Equal(got, want) {
		t.Errorf("encoded %x, want %x", got, want)
	}
	// The second byte is the header's documented "must be zero" pad.
	if got[1] != 0 {
		t.Errorf("pad = %02X, want zero", got[1])
	}

	back, err := DecodeDrawBitmap(got)
	if err != nil {
		t.Fatalf("DecodeDrawBitmap: %v", err)
	}
	if back.Display != 1 || !bytes.Equal(back.Bitmap, in.Bitmap) {
		t.Errorf("= %+v", back)
	}

	// A header with no bitmap is legal: it clears the display.
	bare, err := DecodeDrawBitmap(mustHex(t, "0100"))
	if err != nil {
		t.Fatalf("DecodeDrawBitmap: %v", err)
	}
	if len(bare.Bitmap) != 0 {
		t.Errorf("bitmap = %x, want none", bare.Bitmap)
	}

	if _, err := DecodeDrawBitmap([]byte{1}); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
	if got := in.String(); !strings.Contains(got, "3B") {
		t.Errorf("String() = %q", got)
	}
}

func TestDrawText(t *testing.T) {
	in := DrawText{Display: 1, Mode: 2, Text: "OK"}

	got, err := in.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	if want := mustHex(t, "01 02 4f4b00"); !bytes.Equal(got, want) {
		t.Errorf("encoded %x, want %x", got, want)
	}

	back, err := DecodeDrawText(got)
	if err != nil {
		t.Fatalf("DecodeDrawText: %v", err)
	}
	if back != in {
		t.Errorf("round trip %+v -> %+v", in, back)
	}

	bare, err := DecodeDrawText(mustHex(t, "0102"))
	if err != nil {
		t.Fatalf("DecodeDrawText: %v", err)
	}
	if bare.Text != "" {
		t.Errorf("text = %q, want none", bare.Text)
	}

	if _, err := DecodeDrawText([]byte{1}); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
	long := DrawText{Text: strings.Repeat("x", MaxLongString)}
	if _, err := long.AppendTo(nil); !errors.Is(err, ErrStringTooLong) {
		t.Errorf("err = %v, want ErrStringTooLong", err)
	}
	if got := in.String(); !strings.Contains(got, `"OK"`) {
		t.Errorf("String() = %q", got)
	}
}

// TestSetGroup covers putting a unit into a control group, which is how one
// write reaches several units at once.
func TestSetGroup(t *testing.T) {
	master := SetGroup{Group: 3, Master: true}

	got := master.AppendTo(nil)
	if want := mustHex(t, "0301"); !bytes.Equal(got, want) {
		t.Errorf("encoded %x, want %x", got, want)
	}

	back, err := DecodeSetGroup(got)
	if err != nil {
		t.Fatalf("DecodeSetGroup: %v", err)
	}
	if back != master {
		t.Errorf("round trip %+v -> %+v", master, back)
	}

	follower := SetGroup{Group: 3}
	if got := follower.AppendTo(nil); !bytes.Equal(got, mustHex(t, "0300")) {
		t.Errorf("encoded %x", got)
	}
	// Any non-zero byte means master, not only one.
	odd, err := DecodeSetGroup(mustHex(t, "03ff"))
	if err != nil {
		t.Fatalf("DecodeSetGroup: %v", err)
	}
	if !odd.Master {
		t.Error("a non-zero master byte must read as master")
	}

	if _, err := DecodeSetGroup([]byte{1}); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
	if got := master.String(); got != "group=3 master" {
		t.Errorf("String() = %q", got)
	}
	if got := follower.String(); got != "group=3" {
		t.Errorf("String() = %q", got)
	}
}

func TestGroupParam(t *testing.T) {
	in := GroupParam{Group: 7}

	got := in.AppendTo(nil)
	if want := mustHex(t, "0700"); !bytes.Equal(got, want) {
		t.Errorf("encoded %x, want %x", got, want)
	}
	// The second byte is the header's "must be zero" alignment field.
	if got[1] != 0 {
		t.Errorf("pad = %02X, want zero", got[1])
	}

	back, err := DecodeGroupParam(got)
	if err != nil {
		t.Fatalf("DecodeGroupParam: %v", err)
	}
	if back != in {
		t.Errorf("round trip %+v -> %+v", in, back)
	}

	if _, err := DecodeGroupParam([]byte{1}); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
	if got := in.String(); got != "group=7" {
		t.Errorf("String() = %q", got)
	}
}

// TestDownload covers the one structure whose meaning the vendor header does
// not know either: its field is labelled "No idea - DRAGONS". It is carried
// through unchanged rather than interpreted.
func TestDownload(t *testing.T) {
	in := Download{List: 0x0102}

	got := in.AppendTo(nil)
	if want := mustHex(t, "0102"); !bytes.Equal(got, want) {
		t.Errorf("encoded %x, want %x", got, want)
	}

	back, err := DecodeDownload(got)
	if err != nil {
		t.Fatalf("DecodeDownload: %v", err)
	}
	if back != in {
		t.Errorf("round trip %+v -> %+v", in, back)
	}

	if _, err := DecodeDownload([]byte{1}); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
	if got := in.String(); got != "list=258" {
		t.Errorf("String() = %q", got)
	}
}
