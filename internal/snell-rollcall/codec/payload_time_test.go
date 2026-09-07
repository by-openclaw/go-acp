package codec

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

// TestSysTime_Layout pins TIME_STR. Its fields are laid out under
// #pragma pack(2): a 32-bit elapsed time, then eight single bytes, then a
// 16-bit day of year which lands on an even offset without padding.
func TestSysTime_Layout(t *testing.T) {
	in := SysTime{
		Elapsed: 0x11223344,
		Mode:    TimeModeSystem | TimeModeReal,
		Sec:     59, Min: 58, Hour: 23,
		Mday: 31, Mon: 11, Year: 126,
		Wday: 3, Yday: 364,
	}

	got := in.AppendTo(nil)
	want := mustHex(t, "11223344"+"09"+"3b"+"3a"+"17"+"1f"+"0b"+"7e"+"03"+"016c")
	if !bytes.Equal(got, want) {
		t.Errorf("encoded\n got %x\nwant %x", got, want)
	}
	if len(got) != TimeSize {
		t.Fatalf("encoded %d bytes, want %d", len(got), TimeSize)
	}
	// The day of year sits at offset 12, immediately after the single bytes.
	if !bytes.Equal(got[12:14], mustHex(t, "016c")) {
		t.Errorf("day of year is not at offset 12: %x", got[12:14])
	}

	back, err := DecodeSysTime(got)
	if err != nil {
		t.Fatalf("DecodeSysTime: %v", err)
	}
	if back != in {
		t.Errorf("round trip\n got %+v\nwant %+v", back, in)
	}

	if _, err := DecodeSysTime(make([]byte, TimeSize-1)); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
}

// TestSysTime_ModeGatesTheFields pins the rule that stops a unit with no clock
// being read as the first of January 1900. Which parts of the structure are
// meaningful is stated by the mode flags, never implied by the values.
func TestSysTime_ModeGatesTheFields(t *testing.T) {
	// A unit with no real-time clock: uptime only, date fields at zero.
	noClock := SysTime{Elapsed: 3600, Mode: TimeModeUptime | TimeModeElapsed}

	if _, ok := noClock.Time(); ok {
		t.Error("a unit without a clock must not report a date")
	}
	up, ok := noClock.Uptime()
	if !ok || up != time.Hour {
		t.Errorf("uptime = %s,%v want 1h,true", up, ok)
	}

	// A unit with a clock.
	withClock := NewSysTime(time.Date(2026, 9, 7, 12, 30, 45, 0, time.UTC))
	when, ok := withClock.Time()
	if !ok {
		t.Fatal("a unit with a clock must report a date")
	}
	if !when.Equal(time.Date(2026, 9, 7, 12, 30, 45, 0, time.UTC)) {
		t.Errorf("= %s, want 2026-09-07 12:30:45", when)
	}

	// A unit reporting neither.
	silent := SysTime{}
	if _, ok := silent.Time(); ok {
		t.Error("an empty structure must not report a date")
	}
	if _, ok := silent.Uptime(); ok {
		t.Error("an empty structure must not report an uptime")
	}
}

// TestSysTime_CConventions pins the two off-by-one traps. Month is zero-based
// and the year counts from 1900, both following C, so a naive reader is a
// month and a century out.
func TestSysTime_CConventions(t *testing.T) {
	at := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	st := NewSysTime(at)

	if st.Mon != 0 {
		t.Errorf("January encoded as %d, want the zero-based 0", st.Mon)
	}
	if st.Year != 126 {
		t.Errorf("2026 encoded as %d, want 126 years since 1900", st.Year)
	}
	if st.Yday != 0 {
		t.Errorf("the first of January is day %d, want the zero-based 0", st.Yday)
	}
	if st.Wday != uint8(time.Thursday) {
		t.Errorf("weekday = %d, want %d", st.Wday, time.Thursday)
	}

	back, ok := st.Time()
	if !ok {
		t.Fatal("the time should be readable")
	}
	if !back.Equal(at) {
		t.Errorf("round trip %s -> %s", at, back)
	}

	// December is the other end of the same trap.
	dec := NewSysTime(time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC))
	if dec.Mon != 11 {
		t.Errorf("December encoded as %d, want 11", dec.Mon)
	}
	if got, _ := dec.Time(); got.Month() != time.December {
		t.Errorf("December decoded as %s", got.Month())
	}
}

func TestSysTime_RoundTripsThroughWire(t *testing.T) {
	for _, at := range []time.Time{
		time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2000, 2, 29, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 7, 3, 7, 55, 0, time.UTC),
		time.Date(2155, 12, 31, 23, 59, 59, 0, time.UTC),
	} {
		st := NewSysTime(at)
		back, err := DecodeSysTime(st.AppendTo(nil))
		if err != nil {
			t.Fatalf("%s: DecodeSysTime: %v", at, err)
		}
		got, ok := back.Time()
		if !ok {
			t.Fatalf("%s: the decoded time was not marked real", at)
		}
		if !got.Equal(at) {
			t.Errorf("%s round trips to %s", at, got)
		}
	}
}

func TestSysTime_String(t *testing.T) {
	withClock := NewSysTime(time.Date(2026, 9, 7, 12, 30, 45, 0, time.UTC))
	if got := withClock.String(); got != "2026-09-07 12:30:45" {
		t.Errorf("String() = %q", got)
	}

	uptime := SysTime{Elapsed: 90, Mode: TimeModeUptime | TimeModeElapsed}
	if got := uptime.String(); got != "up 1m30s" {
		t.Errorf("String() = %q", got)
	}

	bare := SysTime{Elapsed: 5}
	if got := bare.String(); got != "mode=0 elapsed=5" {
		t.Errorf("String() = %q", got)
	}
}

func TestSysTime_Has(t *testing.T) {
	st := SysTime{Mode: TimeModeSystem | TimeModeReal}

	if !st.Has(TimeModeSystem) || !st.Has(TimeModeSystem|TimeModeReal) {
		t.Error("Has must accept a subset")
	}
	if st.Has(TimeModeUptime) {
		t.Error("Has must not find a clear flag")
	}
	if st.Has(TimeModeSystem | TimeModeUptime) {
		t.Error("Has must require every requested flag, not any")
	}

	if TimeModeSystem != 1 || TimeModeUptime != 2 || TimeModeElapsed != 4 || TimeModeReal != 8 {
		t.Error("the time mode flags do not match the header")
	}
}

// TestTimecode pins TIMECODE_STR, whose hour field is the only 16-bit one
// because a timecode may run past twenty-four hours.
func TestTimecode(t *testing.T) {
	in := Timecode{Flags: Timecode30Hz, Frames: 29, Seconds: 59, Minutes: 59, Hours: 100}

	got := in.AppendTo(nil)
	if want := mustHex(t, "02 1d 3b 3b 0064"); !bytes.Equal(got, want) {
		t.Errorf("encoded %x, want %x", got, want)
	}
	if len(got) != TimecodeSize {
		t.Errorf("encoded %d bytes, want %d", len(got), TimecodeSize)
	}

	back, err := DecodeTimecode(got)
	if err != nil {
		t.Fatalf("DecodeTimecode: %v", err)
	}
	if back != in {
		t.Errorf("round trip %+v -> %+v", in, back)
	}

	if _, err := DecodeTimecode(make([]byte, TimecodeSize-1)); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
}

// TestTimecode_Flags pins the frame rate, which is a flag rather than a field:
// a timecode read at the wrong rate drifts against the video it names.
func TestTimecode_Flags(t *testing.T) {
	pal := Timecode{Frames: 24, Seconds: 1}
	if pal.Is30Hz() || pal.FrameRate() != 25 {
		t.Errorf("without the flag the rate is %d, want 25", pal.FrameRate())
	}
	if pal.EvenField() {
		t.Error("the even-field flag was not set")
	}

	ntsc := Timecode{Flags: Timecode30Hz | TimecodeEvenField, Frames: 29}
	if !ntsc.Is30Hz() || ntsc.FrameRate() != 30 {
		t.Errorf("with the flag the rate is %d, want 30", ntsc.FrameRate())
	}
	if !ntsc.EvenField() {
		t.Error("the even-field flag was lost")
	}

	if got := ntsc.String(); got != "00:00:00:29@30" {
		t.Errorf("String() = %q", got)
	}
	if got := pal.String(); got != "00:00:01:24@25" {
		t.Errorf("String() = %q", got)
	}
}
