package codec

import (
	"encoding/binary"
	"fmt"
)

// Wire sizes of the 32-bit menu structures (2014 extension).
//
// Every structure in the protocol is compiled under #pragma pack(2)
// (RC3TYPES.H), so a 32-bit field following a 16-bit one is not aligned up and
// there is no padding anywhere in these layouts.
const (
	MenuReqSize  = 4  // MENUREQ_STR
	MenuSizeSize = 8  // MENUSIZE_STR
	MenuItemSize = 22 // MENUITEM_STR, before its two strings
)

// MenuReq asks about a menu, and means two different things depending on which
// message carries it.
//
// In GetMenuCount the index is the base of a partial menu, and zero means the
// whole menu set. In GetMenuItem it is the absolute index of one line.
//
// That absolute index is the substantive change from the 16-bit generation.
// GetNextPkt asks for an offset from the base of a preceding GetFunc and
// relies on the server remembering that base, so items must be walked in order
// on one session. GetMenuItem is stateless: any index, any order, and several
// requests may be in flight on different sessions.
type MenuReq struct {
	MenuIndex uint32
}

func (m MenuReq) String() string { return fmt.Sprintf("idx=%d", m.MenuIndex) }

// AppendTo appends the 4-byte wire form.
func (m MenuReq) AppendTo(dst []byte) []byte {
	var b [MenuReqSize]byte
	binary.BigEndian.PutUint32(b[:], m.MenuIndex)
	return append(dst, b[:]...)
}

// DecodeMenuReq reads a MENUREQ_STR from the front of b.
func DecodeMenuReq(b []byte) (MenuReq, error) {
	if err := need(b, MenuReqSize, "MenuReq", ""); err != nil {
		return MenuReq{}, err
	}
	return MenuReq{MenuIndex: binary.BigEndian.Uint32(b)}, nil
}

// MenuSize answers GetMenuCount: how many lines hang below a base index.
//
// The base is echoed rather than implied, so a client with several outstanding
// requests can match the answer to the question without holding sequence
// state.
type MenuSize struct {
	MenuIndex uint32
	MenuCount uint32
}

func (m MenuSize) String() string {
	return fmt.Sprintf("idx=%d count=%d", m.MenuIndex, m.MenuCount)
}

// AppendTo appends the 8-byte wire form.
func (m MenuSize) AppendTo(dst []byte) []byte {
	var b [MenuSizeSize]byte
	binary.BigEndian.PutUint32(b[0:4], m.MenuIndex)
	binary.BigEndian.PutUint32(b[4:8], m.MenuCount)
	return append(dst, b[:]...)
}

// DecodeMenuSize reads a MENUSIZE_STR from the front of b.
func DecodeMenuSize(b []byte) (MenuSize, error) {
	if err := need(b, MenuSizeSize, "MenuSize", ""); err != nil {
		return MenuSize{}, err
	}
	return MenuSize{
		MenuIndex: binary.BigEndian.Uint32(b[0:4]),
		MenuCount: binary.BigEndian.Uint32(b[4:8]),
	}, nil
}

// MenuItem is one menu line in the 32-bit generation: the 16-bit Func with
// wider indices and its two strings moved out of their fixed fields.
//
// The numeric fields carry exactly the meanings they do in Func, including the
// rule that Step on a container line is the span of the whole subtree and that
// a DivScale of zero means one. Style is unchanged and still 16 bits: the 2014
// paper adds no style flags.
//
// There is no 32-bit FuncStyle. A back-channel menu update in this generation
// is always a complete MenuItem, so a client never has to merge a style-only
// change into a line it already holds.
type MenuItem struct {
	MenuIndex uint32
	Style     Style
	Command   uint32
	MinRange  int32
	MaxRange  int32
	Step      uint32
	DivScale  uint16
	Text      string
	Param     string
}

// Scale returns DivScale with the zero-means-one rule applied (spec 7.2.2.6).
func (m MenuItem) Scale() uint16 {
	if m.DivScale == 0 {
		return 1
	}
	return m.DivScale
}

// AccessGated reports whether the server substituted this line because the
// session's user level may not see it.
func (m MenuItem) AccessGated() bool {
	return AccessGated(m.Style, m.Command, m.Text)
}

func (m MenuItem) String() string {
	return fmt.Sprintf("idx=%d %s cmd=%d %d..%d step=%d div=%d %q %q",
		m.MenuIndex, m.Style, m.Command, m.MinRange, m.MaxRange, m.Step, m.DivScale, m.Text, m.Param)
}

// AppendTo appends the wire form: 22 fixed bytes then the two strings, each
// NUL-terminated.
func (m MenuItem) AppendTo(dst []byte) ([]byte, error) {
	if m.Step > 0xFFFF {
		return nil, fmt.Errorf("%w: step %d exceeds the 16-bit field", ErrFieldRange, m.Step)
	}

	var head [MenuItemSize]byte
	binary.BigEndian.PutUint32(head[0:4], m.MenuIndex)
	binary.BigEndian.PutUint16(head[4:6], uint16(m.Style))
	binary.BigEndian.PutUint32(head[6:10], m.Command)
	binary.BigEndian.PutUint32(head[10:14], uint32(m.MinRange))
	binary.BigEndian.PutUint32(head[14:18], uint32(m.MaxRange))
	binary.BigEndian.PutUint16(head[18:20], uint16(m.Step))
	binary.BigEndian.PutUint16(head[20:22], m.DivScale)
	dst = append(dst, head[:]...)

	dst, err := appendCString(dst, m.Text)
	if err != nil {
		return nil, err
	}
	return appendCString(dst, m.Param)
}

// DecodeMenuItem reads a MENUITEM_STR and its two trailing strings.
//
// Either string may be absent when the payload ends early: a device that has
// no format string for a line stops after the text rather than sending a lone
// terminator, and dropping the whole line for that would lose a menu entry.
func DecodeMenuItem(b []byte) (MenuItem, error) {
	if err := need(b, MenuItemSize, "MenuItem", ""); err != nil {
		return MenuItem{}, err
	}
	m := MenuItem{
		MenuIndex: binary.BigEndian.Uint32(b[0:4]),
		Style:     Style(binary.BigEndian.Uint16(b[4:6])),
		Command:   binary.BigEndian.Uint32(b[6:10]),
		MinRange:  int32(binary.BigEndian.Uint32(b[10:14])),
		MaxRange:  int32(binary.BigEndian.Uint32(b[14:18])),
		Step:      uint32(binary.BigEndian.Uint16(b[18:20])),
		DivScale:  binary.BigEndian.Uint16(b[20:22]),
	}

	rest := b[MenuItemSize:]
	if len(rest) > 0 {
		s, n := CString(rest)
		m.Text = s
		rest = rest[n:]
	}
	if len(rest) > 0 {
		m.Param, _ = CString(rest)
	}
	return m, nil
}

// ToFunc projects a 32-bit menu line onto the 16-bit structure, for a provider
// answering a client that did not negotiate long strings.
//
// Text is truncated rather than refused: the 16-bit generation has no way to
// carry a longer label, and a client that cannot see a line at all is worse
// off than one that sees a shortened name. Indices and commands that do not
// fit are an error, because a truncated command number addresses a different
// command.
func (m MenuItem) ToFunc() (Func, error) {
	if m.MenuIndex > 0xFFFF {
		return Func{}, fmt.Errorf("%w: menu index %d exceeds the 16-bit generation",
			ErrFieldRange, m.MenuIndex)
	}
	if m.Command > 0xFFFF {
		return Func{}, fmt.Errorf("%w: command %d exceeds the 16-bit generation",
			ErrFieldRange, m.Command)
	}
	if m.Step > 0xFFFF {
		return Func{}, fmt.Errorf("%w: step %d exceeds the 16-bit generation",
			ErrFieldRange, m.Step)
	}
	return Func{
		MenuIndex: uint16(m.MenuIndex),
		Style:     m.Style,
		Command:   uint16(m.Command),
		MinRange:  m.MinRange,
		MaxRange:  m.MaxRange,
		Step:      uint16(m.Step),
		DivScale:  m.DivScale,
		Text:      TruncateFixed(m.Text, MaxTextSize),
		Param:     TruncateFixed(m.Param, MaxTextSize),
	}, nil
}

// ToMenuItem widens a 16-bit menu line, for a consumer that holds one tree for
// both generations. It cannot fail: every 16-bit field fits.
func (f Func) ToMenuItem() MenuItem {
	return MenuItem{
		MenuIndex: uint32(f.MenuIndex),
		Style:     f.Style,
		Command:   uint32(f.Command),
		MinRange:  f.MinRange,
		MaxRange:  f.MaxRange,
		Step:      uint32(f.Step),
		DivScale:  f.DivScale,
		Text:      f.Text,
		Param:     f.Param,
	}
}
