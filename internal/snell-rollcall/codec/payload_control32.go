package codec

import (
	"encoding/binary"
	"fmt"
)

// Wire sizes of the 32-bit control structures (2014 extension).
const (
	GetValueSize = 4  // GETVALUE_STR
	ValueSize    = 12 // VALUE_STR, before its flag-driven tail
)

// GetValue asks for the current state of one command (GETVALUE_STR).
type GetValue struct {
	Command uint32
}

func (g GetValue) String() string { return fmt.Sprintf("cmd=%d", g.Command) }

// AppendTo appends the 4-byte wire form.
func (g GetValue) AppendTo(dst []byte) []byte {
	var b [GetValueSize]byte
	binary.BigEndian.PutUint32(b[:], g.Command)
	return append(dst, b[:]...)
}

// DecodeGetValue reads a GETVALUE_STR from the front of b.
func DecodeGetValue(b []byte) (GetValue, error) {
	if err := need(b, GetValueSize, "GetValue", ""); err != nil {
		return GetValue{}, err
	}
	return GetValue{Command: binary.BigEndian.Uint32(b)}, nil
}

// Value carries a command's value in the 32-bit generation. It is the payload
// of SetValue and RetValue (VALUE_STR).
//
// The tail follows the same rules as FuncStatus: ModeString means a string
// follows, ModeData means Value bytes of data follow and excludes the other
// two, and both ModeValue and ModeString together is normal rather than
// exceptional.
//
// What differs is the match ID. Here it is a declared field at a fixed offset,
// always present on the wire whether or not ModeMatchID is set. In
// FUNCSTATUS_STR it trails the payload and only exists when the flag is set,
// which is why the string field there has to become fixed-width to keep the ID
// findable. This layout has no such problem, so the string is variable-length
// in every combination.
type Value struct {
	Command uint32
	MatchID uint16
	Mode    Mode
	Val     int32
	Text    string
	Data    []byte
}

func (v Value) String() string {
	s := fmt.Sprintf("cmd=%d mode=%s value=%d", v.Command, v.Mode, v.Val)
	if v.Mode.Has(ModeString) {
		s += fmt.Sprintf(" %q", v.Text)
	}
	if v.Mode.Has(ModeData) {
		s += fmt.Sprintf(" data=%dB", len(v.Data))
	}
	if v.Mode.Has(ModeMatchID) {
		s += fmt.Sprintf(" match=%d", v.MatchID)
	}
	return s
}

// AppendTo appends the wire form including whichever tail Mode selects.
func (v Value) AppendTo(dst []byte) ([]byte, error) {
	var head [ValueSize]byte
	binary.BigEndian.PutUint32(head[0:4], v.Command)
	binary.BigEndian.PutUint16(head[4:6], v.MatchID)
	binary.BigEndian.PutUint16(head[6:8], uint16(v.Mode))
	val := v.Val
	if v.Mode.Has(ModeData) {
		val = int32(len(v.Data))
	}
	binary.BigEndian.PutUint32(head[8:12], uint32(val))
	dst = append(dst, head[:]...)

	if v.Mode.Has(ModeString) {
		var err error
		if dst, err = appendCString(dst, v.Text); err != nil {
			return nil, err
		}
	}
	if v.Mode.Has(ModeData) {
		dst = append(dst, v.Data...)
	}
	return dst, nil
}

// DecodeValue reads a VALUE_STR and its flag-driven tail.
func DecodeValue(b []byte) (Value, error) {
	if err := need(b, ValueSize, "Value", ""); err != nil {
		return Value{}, err
	}
	v := Value{
		Command: binary.BigEndian.Uint32(b[0:4]),
		MatchID: binary.BigEndian.Uint16(b[4:6]),
		Mode:    Mode(binary.BigEndian.Uint16(b[6:8])),
		Val:     int32(binary.BigEndian.Uint32(b[8:12])),
	}

	off := ValueSize
	if v.Mode.Has(ModeString) {
		s, n := cString(b[off:])
		v.Text = s
		off += n
	}
	if v.Mode.Has(ModeData) {
		n := int(v.Val)
		if n < 0 {
			return Value{}, decodeErr("Value", "Data", off,
				fmt.Errorf("negative data length %d", n))
		}
		if err := need(b[off:], n, "Value", "Data"); err != nil {
			return Value{}, err
		}
		v.Data = b[off : off+n]
	}
	return v, nil
}

// ToFuncStatus projects a 32-bit value onto the 16-bit structure, for a
// provider answering a client that did not negotiate long strings.
//
// A command number above 0xFFFF cannot be represented and is an error: the
// 16-bit generation simply has no way to name it, which is exactly why the
// router command set requires long strings.
func (v Value) ToFuncStatus() (FuncStatus, error) {
	if v.Command > 0xFFFF {
		return FuncStatus{}, fmt.Errorf("%w: command %d exceeds the 16-bit generation",
			ErrFieldRange, v.Command)
	}
	return FuncStatus{
		Command: uint16(v.Command),
		Mode:    v.Mode,
		Value:   v.Val,
		Text:    truncateFixed(v.Text, MaxTextSize),
		Data:    v.Data,
		MatchID: v.MatchID,
	}, nil
}

// ToValue widens a 16-bit value, for a consumer that holds one cache for both
// generations. It cannot fail.
func (f FuncStatus) ToValue() Value {
	return Value{
		Command: uint32(f.Command),
		MatchID: f.MatchID,
		Mode:    f.Mode,
		Val:     f.Value,
		Text:    f.Text,
		Data:    f.Data,
	}
}
