package codec

import (
	"encoding/binary"
	"fmt"
	"time"
)

// Wire sizes of the time structures.
const (
	TimeSize     = 14 // TIME_STR
	TimecodeSize = 6  // TIMECODE_STR
)

// Time mode flags (rc3comm.h TimeModeFlags). They say which parts of the
// structure a sender filled in, and a reader that ignores them will read
// zeroes as a real date.
const (
	// TimeModeSystem means the time is absolute wall-clock time.
	TimeModeSystem uint8 = 1
	// TimeModeUptime means it is time since the unit powered on.
	TimeModeUptime uint8 = 2
	// TimeModeElapsed means the Elapsed field is meaningful.
	TimeModeElapsed uint8 = 4
	// TimeModeReal means the broken-down fields are meaningful.
	TimeModeReal uint8 = 8
)

// Timecode flags (rc3comm.h TimeCodeFlags).
const (
	TimecodeEvenField uint8 = 1
	Timecode30Hz      uint8 = 2
)

// SysTime is TIME_STR: a unit's idea of the time, in two forms at once.
//
// Elapsed is a single number and the broken-down fields are the same instant
// spelled out. Which of them is meaningful is stated by Mode, not implied, so
// a reader must check the flags: a unit with no real-time clock sends uptime
// with the broken-down fields left at zero, and treating that as a date puts
// 1900 in a log.
//
// Month is zero-based and weekday is zero-based from Sunday, both following C,
// which is the classic off-by-one in this structure.
type SysTime struct {
	// Elapsed is the vendor's "MS-DOS format time", meaningful when
	// TimeModeElapsed is set. In practice devices send seconds here.
	Elapsed int32
	Mode    uint8

	Sec, Min, Hour uint8
	Mday, Mon      uint8 // Mon is 0..11, following C
	Year           uint8 // years since 1900, following C
	Wday           uint8 // 0..6 from Sunday, following C
	Yday           int16 // 0..365
}

// Has reports whether every flag in want is set.
func (t SysTime) Has(want uint8) bool { return t.Mode&want == want }

// AppendTo appends the 14-byte wire form.
func (t SysTime) AppendTo(dst []byte) []byte {
	var b [TimeSize]byte
	binary.BigEndian.PutUint32(b[0:4], uint32(t.Elapsed))
	b[4] = t.Mode
	b[5] = t.Sec
	b[6] = t.Min
	b[7] = t.Hour
	b[8] = t.Mday
	b[9] = t.Mon
	b[10] = t.Year
	b[11] = t.Wday
	binary.BigEndian.PutUint16(b[12:14], uint16(t.Yday))
	return append(dst, b[:]...)
}

// DecodeSysTime reads a TIME_STR from the front of b.
func DecodeSysTime(b []byte) (SysTime, error) {
	if err := need(b, TimeSize, "SysTime", ""); err != nil {
		return SysTime{}, err
	}
	return sysTimeAt(b), nil
}

// sysTimeAt reads a TIME_STR from a slice the caller has already sized.
func sysTimeAt(b []byte) SysTime {
	return SysTime{
		Elapsed: int32(binary.BigEndian.Uint32(b[0:4])),
		Mode:    b[4],
		Sec:     b[5],
		Min:     b[6],
		Hour:    b[7],
		Mday:    b[8],
		Mon:     b[9],
		Year:    b[10],
		Wday:    b[11],
		Yday:    int16(binary.BigEndian.Uint16(b[12:14])),
	}
}

// Time converts the broken-down fields to a time.Time in UTC, and reports
// whether they were meaningful.
//
// It returns false unless the real-time flag is set, because a unit without a
// clock leaves these fields at zero and the alternative is silently reporting
// the first of January 1900.
func (t SysTime) Time() (time.Time, bool) {
	if !t.Has(TimeModeReal) {
		return time.Time{}, false
	}
	return time.Date(
		1900+int(t.Year),
		time.Month(t.Mon)+1, // the wire is zero-based, time.Month is not
		int(t.Mday),
		int(t.Hour), int(t.Min), int(t.Sec), 0,
		time.UTC,
	), true
}

// Uptime returns how long the unit has been running, and whether it said.
func (t SysTime) Uptime() (time.Duration, bool) {
	if !t.Has(TimeModeUptime | TimeModeElapsed) {
		return 0, false
	}
	return time.Duration(t.Elapsed) * time.Second, true
}

// NewSysTime builds a TIME_STR for an absolute instant, filling in both forms
// and flagging both as meaningful.
func NewSysTime(at time.Time) SysTime {
	at = at.UTC()
	return SysTime{
		Elapsed: int32(at.Unix()),
		Mode:    TimeModeSystem | TimeModeReal | TimeModeElapsed,
		Sec:     uint8(at.Second()),
		Min:     uint8(at.Minute()),
		Hour:    uint8(at.Hour()),
		Mday:    uint8(at.Day()),
		Mon:     uint8(at.Month() - 1),
		Year:    uint8(at.Year() - 1900),
		Wday:    uint8(at.Weekday()),
		Yday:    int16(at.YearDay() - 1),
	}
}

func (t SysTime) String() string {
	if when, ok := t.Time(); ok {
		return when.Format("2006-01-02 15:04:05")
	}
	if up, ok := t.Uptime(); ok {
		return "up " + up.String()
	}
	return fmt.Sprintf("mode=%d elapsed=%d", t.Mode, t.Elapsed)
}

// Timecode is TIMECODE_STR: a decoded video timecode.
type Timecode struct {
	Flags   uint8
	Frames  uint8
	Seconds uint8
	Minutes uint8
	Hours   uint16
}

// EvenField reports whether the field is even.
func (t Timecode) EvenField() bool { return t.Flags&TimecodeEvenField != 0 }

// Is30Hz reports whether the frame rate is 30 Hz rather than 25.
func (t Timecode) Is30Hz() bool { return t.Flags&Timecode30Hz != 0 }

// FrameRate returns the frame rate the flags imply.
func (t Timecode) FrameRate() int {
	if t.Is30Hz() {
		return 30
	}
	return 25
}

// AppendTo appends the 6-byte wire form.
func (t Timecode) AppendTo(dst []byte) []byte {
	var b [TimecodeSize]byte
	b[0] = t.Flags
	b[1] = t.Frames
	b[2] = t.Seconds
	b[3] = t.Minutes
	binary.BigEndian.PutUint16(b[4:6], t.Hours)
	return append(dst, b[:]...)
}

// DecodeTimecode reads a TIMECODE_STR from the front of b.
func DecodeTimecode(b []byte) (Timecode, error) {
	if err := need(b, TimecodeSize, "Timecode", ""); err != nil {
		return Timecode{}, err
	}
	return Timecode{
		Flags:   b[0],
		Frames:  b[1],
		Seconds: b[2],
		Minutes: b[3],
		Hours:   binary.BigEndian.Uint16(b[4:6]),
	}, nil
}

func (t Timecode) String() string {
	return fmt.Sprintf("%02d:%02d:%02d:%02d@%d",
		t.Hours, t.Minutes, t.Seconds, t.Frames, t.FrameRate())
}
