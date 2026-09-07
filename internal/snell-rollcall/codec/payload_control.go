package codec

import (
	"encoding/binary"
	"fmt"
)

// Wire sizes of the 16-bit control structures.
const (
	GetFStatSize   = 2  // spec 11.5.1 GETFSTAT_STR
	FuncStatusSize = 8  // spec 11.5.2 FUNCSTATUS_STR, before the flag-driven tail
	SetMultiSize   = 4  // spec 11.5.3 SETMULTI_STR, repeated
	DispSize       = 22 // spec 11.5.4 DISP_STR
)

// GetFStat asks for the current state of one command (spec 11.5.1).
type GetFStat struct {
	Command uint16
}

func (g GetFStat) String() string { return fmt.Sprintf("cmd=%d", g.Command) }

// AppendTo appends the 2-byte wire form.
func (g GetFStat) AppendTo(dst []byte) []byte {
	var b [GetFStatSize]byte
	binary.BigEndian.PutUint16(b[:], g.Command)
	return append(dst, b[:]...)
}

// DecodeGetFStat reads a GETFSTAT_STR from the front of b.
func DecodeGetFStat(b []byte) (GetFStat, error) {
	if err := need(b, GetFStatSize, "GetFStat", ""); err != nil {
		return GetFStat{}, err
	}
	return GetFStat{Command: binary.BigEndian.Uint16(b)}, nil
}

// FuncStatus carries a command's value in the 16-bit generation. It is the
// payload of SetParam and RetFStat (spec 11.5.2).
//
// What follows the fixed 8 bytes depends on Mode, and the order is fixed:
//
//	ModeString  a string. Normally NUL-terminated within 20 bytes, but when
//	            ModeMatchID is also set the field is a full 20 bytes so that
//	            the trailing ID sits at a known offset (spec 11.5.2).
//	ModeData    Value bytes of raw data. Excludes ModeValue and ModeString.
//	ModeMatchID a uint16 unit type. The write applies only if it matches the
//	            receiver, or if it is zero.
//
// When both ModeValue and ModeString are set, both are meaningful and the
// string is what should be displayed: it exists precisely because the number
// alone would mislead. A live device returns value -1686180113 with the string
// "0x9B7EEEEF" for a checksum.
type FuncStatus struct {
	Command uint16
	Mode    Mode
	Value   int32
	Text    string
	Data    []byte
	MatchID uint16
}

func (f FuncStatus) String() string {
	s := fmt.Sprintf("cmd=%d mode=%s value=%d", f.Command, f.Mode, f.Value)
	if f.Mode.Has(ModeString) {
		s += fmt.Sprintf(" %q", f.Text)
	}
	if f.Mode.Has(ModeData) {
		s += fmt.Sprintf(" data=%dB", len(f.Data))
	}
	if f.Mode.Has(ModeMatchID) {
		s += fmt.Sprintf(" match=%d", f.MatchID)
	}
	return s
}

// AppendTo appends the wire form including whichever tail Mode selects.
func (f FuncStatus) AppendTo(dst []byte) ([]byte, error) {
	var head [FuncStatusSize]byte
	binary.BigEndian.PutUint16(head[0:2], f.Command)
	binary.BigEndian.PutUint16(head[2:4], uint16(f.Mode))
	value := f.Value
	if f.Mode.Has(ModeData) {
		value = int32(len(f.Data))
	}
	binary.BigEndian.PutUint32(head[4:8], uint32(value))
	dst = append(dst, head[:]...)

	var err error
	if f.Mode.Has(ModeString) {
		if f.Mode.Has(ModeMatchID) {
			// Fixed width so the ID that follows is at a known offset.
			dst, err = appendFixedString(dst, f.Text, MaxTextSize)
		} else {
			if len(f.Text) > MaxTextSize-1 {
				return nil, fmt.Errorf("%w: %d bytes, 16-bit field holds %d",
					ErrStringTooLong, len(f.Text), MaxTextSize-1)
			}
			dst = append(dst, f.Text...)
			dst = append(dst, 0)
		}
		if err != nil {
			return nil, err
		}
	}
	if f.Mode.Has(ModeData) {
		dst = append(dst, f.Data...)
	}
	if f.Mode.Has(ModeMatchID) {
		var id [2]byte
		binary.BigEndian.PutUint16(id[:], f.MatchID)
		dst = append(dst, id[:]...)
	}
	return dst, nil
}

// DecodeFuncStatus reads a FUNCSTATUS_STR and its flag-driven tail.
//
// Offsets are computed from Mode rather than scanned for, because the tail is
// not self-describing: with ModeMatchID the string field is fixed-width, so
// searching for a terminator would find the wrong boundary.
func DecodeFuncStatus(b []byte) (FuncStatus, error) {
	if err := need(b, FuncStatusSize, "FuncStatus", ""); err != nil {
		return FuncStatus{}, err
	}
	f := FuncStatus{
		Command: binary.BigEndian.Uint16(b[0:2]),
		Mode:    Mode(binary.BigEndian.Uint16(b[2:4])),
		Value:   int32(binary.BigEndian.Uint32(b[4:8])),
	}

	off := FuncStatusSize
	if f.Mode.Has(ModeString) {
		if f.Mode.Has(ModeMatchID) {
			// Fixed width, so the trailing ID sits at a known offset.
			if err := need(b[off:], MaxTextSize, "FuncStatus", "Text"); err != nil {
				return FuncStatus{}, err
			}
			f.Text = fixedString(b[off : off+MaxTextSize])
			off += MaxTextSize
		} else {
			s, n := cString(b[off:])
			f.Text = s
			off += n
		}
	}
	if f.Mode.Has(ModeData) {
		n := int(f.Value)
		if n < 0 {
			return FuncStatus{}, decodeErr("FuncStatus", "Data", off,
				fmt.Errorf("negative data length %d", n))
		}
		if err := need(b[off:], n, "FuncStatus", "Data"); err != nil {
			return FuncStatus{}, err
		}
		f.Data = b[off : off+n]
		off += n
	}
	if f.Mode.Has(ModeMatchID) {
		if err := need(b[off:], 2, "FuncStatus", "MatchID"); err != nil {
			return FuncStatus{}, err
		}
		f.MatchID = binary.BigEndian.Uint16(b[off : off+2])
	}
	return f, nil
}

// SetMulti sets one numeric command as part of a batch (spec 11.5.3).
//
// Values are 16-bit to save bandwidth on slow links and are promoted to signed
// 32-bit before use. A value outside int16 must be sent as two consecutive
// entries with the same command, high word first (spec 9.49).
//
// A server sends no acknowledgement for a batch it accepts, only a Nack if it
// rejects one, so this cannot be used inside the one-in-flight active queue.
type SetMulti struct {
	Command int16
	Value   int16
}

// AppendMultiValues appends a batch of SetMulti entries.
func AppendMultiValues(dst []byte, vals []SetMulti) []byte {
	for _, v := range vals {
		var b [SetMultiSize]byte
		binary.BigEndian.PutUint16(b[0:2], uint16(v.Command))
		binary.BigEndian.PutUint16(b[2:4], uint16(v.Value))
		dst = append(dst, b[:]...)
	}
	return dst
}

// DecodeMultiValues reads a whole SetMulti payload. The count is implied by
// the payload length (spec 9.49).
func DecodeMultiValues(b []byte) ([]SetMulti, error) {
	if len(b)%SetMultiSize != 0 {
		return nil, decodeErr("SetMulti", "", len(b),
			fmt.Errorf("payload %d is not a multiple of %d", len(b), SetMultiSize))
	}
	out := make([]SetMulti, 0, len(b)/SetMultiSize)
	for off := 0; off < len(b); off += SetMultiSize {
		out = append(out, SetMulti{
			Command: int16(binary.BigEndian.Uint16(b[off : off+2])),
			Value:   int16(binary.BigEndian.Uint16(b[off+2 : off+4])),
		})
	}
	return out, nil
}

// Disp is one line of the status display a control server maintains
// (spec 11.5.4). Lines 0..3 are the common four; negative lines carry priority
// rather than position.
type Disp struct {
	Line int16
	Text string
}

func (d Disp) String() string {
	switch d.Line {
	case DisplayLineError:
		return fmt.Sprintf("error %q", d.Text)
	case DisplayLineWarning:
		return fmt.Sprintf("warning %q", d.Text)
	default:
		return fmt.Sprintf("line=%d %q", d.Line, d.Text)
	}
}

// AppendTo appends the 22-byte wire form.
func (d Disp) AppendTo(dst []byte) ([]byte, error) {
	var line [2]byte
	binary.BigEndian.PutUint16(line[:], uint16(d.Line))
	return appendFixedString(append(dst, line[:]...), d.Text, MaxTextSize)
}

// DecodeDisp reads a DISP_STR from the front of b.
func DecodeDisp(b []byte) (Disp, error) {
	if err := need(b, 2, "Disp", "Line"); err != nil {
		return Disp{}, err
	}
	d := Disp{Line: int16(binary.BigEndian.Uint16(b[0:2]))}
	if len(b) > 2 {
		d.Text = fixedString(b[2:minInt(2+MaxTextSize, len(b))])
	}
	return d, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
