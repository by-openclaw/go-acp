package codec

import "strings"

// Service flags (spec 12.1, rc3comm.h).
//
// Service is unsigned. The vendor enum is signed, so SvcLongStr appears as a
// negative number in tools that print it as INT16 - RollCall Control Panel
// shows service 0x8003 as "-32765". Never store a service mask in a signed
// 16-bit value.
type Service uint16

const (
	SvcMenus     Service = 0x0001
	SvcControl   Service = 0x0002
	SvcDisplay   Service = 0x0004
	SvcFile      Service = 0x0008
	SvcLogging   Service = 0x0010
	SvcStream    Service = 0x0020
	SvcMap       Service = 0x0040
	SvcPorts     Service = 0x0080
	SvcNet       Service = 0x0100
	SvcExec      Service = 0x0200
	SvcTime      Service = 0x0400
	SvcRes2      Service = 0x0800
	SvcThumbnail Service = 0x1000 // spec calls this SV_LOC1
	SvcFastMenu  Service = 0x2000 // spec calls this SV_LOC2; semantics undocumented
	SvcLoc3      Service = 0x4000
	// SvcLongStr is the 2014 extension's capability bit. A unit advertising
	// it accepts 32-bit command numbers and long strings; a client requests
	// them by including this bit in its Call.
	SvcLongStr Service = 0x8000
)

var serviceNames = []struct {
	bit  Service
	name string
}{
	{SvcMenus, "Menus"}, {SvcControl, "Control"}, {SvcDisplay, "Display"},
	{SvcFile, "File"}, {SvcLogging, "Logging"}, {SvcStream, "Stream"},
	{SvcMap, "Map"}, {SvcPorts, "Ports"}, {SvcNet, "Net"}, {SvcExec, "Exec"},
	{SvcTime, "Time"}, {SvcRes2, "Res2"}, {SvcThumbnail, "Thumbnail"},
	{SvcFastMenu, "FastMenu"}, {SvcLoc3, "Loc3"}, {SvcLongStr, "LongStr"},
}

// Has reports whether every bit in want is present.
func (s Service) Has(want Service) bool { return s&want == want }

// LongStrings reports whether the mask advertises or requests the 32-bit
// generation.
func (s Service) LongStrings() bool { return s&SvcLongStr != 0 }

// String lists the set bits, e.g. "Menus|Control|LongStr", or "-" when none.
func (s Service) String() string {
	if s == 0 {
		return "-"
	}
	var b strings.Builder
	for _, e := range serviceNames {
		if s&e.bit != 0 {
			if b.Len() > 0 {
				b.WriteByte('|')
			}
			b.WriteString(e.name)
		}
	}
	return b.String()
}

// UserLevel selects which class of information a session sees (spec 12.2).
//
// Gating is per line and per command via a usermask the server holds. The
// control service answers an out-of-level command with Nack; the menu service
// replaces the line with a hidden, disabled Data line named "Reserved" rather
// than removing it, so line counts never change. Compare flags, not counts.
type UserLevel uint16

const (
	LevelUser       UserLevel = 0
	LevelEngineer   UserLevel = 1
	LevelSupervisor UserLevel = 2
	LevelFactory    UserLevel = 3

	// LevelAll is a mask value, never a level to send in a Call. The vendor
	// engine rejects any level at or above MAX_USERLEVEL (rc_menu.c).
	LevelAll UserLevel = 4
)

// Valid reports whether the level may be sent in a Call.
func (u UserLevel) Valid() bool { return u <= LevelFactory }

func (u UserLevel) String() string {
	switch u {
	case LevelUser:
		return "user"
	case LevelEngineer:
		return "engineer"
	case LevelSupervisor:
		return "supervisor"
	case LevelFactory:
		return "factory"
	case LevelAll:
		return "all"
	default:
		return "unknown"
	}
}

// Status flags (spec 12.5, rc3comm.h StatusFlags).
type Status uint16

const (
	StatusOnline     Status = 0x0002
	StatusMultiLevel Status = 0x0004 // vendor-only; spec leaves bit 2 unassigned
	StatusPresent    Status = 0x0008
	StatusLocal      Status = 0x0020
)

// Has reports whether every bit in want is present.
func (s Status) Has(want Status) bool { return s&want == want }

func (s Status) String() string {
	if s == 0 {
		return "-"
	}
	var b strings.Builder
	for _, e := range []struct {
		bit  Status
		name string
	}{
		{StatusOnline, "Online"}, {StatusMultiLevel, "MultiLevel"},
		{StatusPresent, "Present"}, {StatusLocal, "Local"},
	} {
		if s&e.bit != 0 {
			if b.Len() > 0 {
				b.WriteByte('|')
			}
			b.WriteString(e.name)
		}
	}
	return b.String()
}

// Mode is the rMode bit field carried by FuncStatus and Value (spec 12.8).
type Mode uint16

const (
	// ModeValue means the numeric field is meaningful.
	ModeValue Mode = 0x01
	// ModeString means a string follows the numeric field.
	ModeString Mode = 0x02
	// ModeData means raw bytes follow, their length given by the numeric
	// field. Mutually exclusive with Value and String.
	ModeData Mode = 0x04
	// ModeWrapped marks a value that wrapped past a limit.
	ModeWrapped Mode = 0x08
	// ModePreset asks the server to restore the default. The numeric field
	// is ignored: measured against a live device, the Control Panel sends
	// value 0 with this bit and expects the default back.
	ModePreset Mode = 0x10
	// ModeMatchID means a unit type follows, and the write applies only if
	// it matches the receiver. Used with blind control.
	ModeMatchID Mode = 0x20
)

// Has reports whether every bit in want is present.
func (m Mode) Has(want Mode) bool { return m&want == want }

func (m Mode) String() string {
	if m == 0 {
		return "-"
	}
	var b strings.Builder
	for _, e := range []struct {
		bit  Mode
		name string
	}{
		{ModeValue, "VALUE"}, {ModeString, "STRING"}, {ModeData, "DATA"},
		{ModeWrapped, "WRAPPED"}, {ModePreset, "PRESET"}, {ModeMatchID, "MATCH_ID"},
	} {
		if m&e.bit != 0 {
			if b.Len() > 0 {
				b.WriteByte('|')
			}
			b.WriteString(e.name)
		}
	}
	return b.String()
}

// Style is the rStyle field of a menu line (spec 12.6). The high nibble
// selects the line kind, the low nibble carries flags.
type Style uint16

// Line kinds, in the high nibble.
const (
	StyleTiled      Style = 0x00
	StyleList       Style = 0x10
	StyleDisplay    Style = 0x20
	StyleButton     Style = 0x30
	StyleCheckbox   Style = 0x40
	StyleNumber     Style = 0x50
	StyleVGraph     Style = 0x60
	StyleHGraph     Style = 0x70
	StyleEditString Style = 0x80
	StyleVLevel     Style = 0x90
	StyleHLevel     Style = 0xA0
	StylePartial    Style = 0xB0
	StyleData       Style = 0xC0
	StyleLink       Style = 0xD0

	StyleKindMask Style = 0x00F0
	StyleFlagMask Style = 0x000F
)

// Line flags, in the low nibble, plus one private bit.
const (
	StyleCacheable Style = 0x0001
	StyleWraps     Style = 0x0002
	StyleDisabled  Style = 0x0004
	StyleHidden    Style = 0x0008

	// StyleDeferred is set by the vendor's Rope engine on back-channel menu
	// updates to say "more are coming, hold the redraw". It is not in the
	// specification's style table (ropedef.h).
	StyleDeferred Style = 0x8000
)

// Kind returns the line kind with the flags masked off.
func (s Style) Kind() Style { return s & StyleKindMask }

// Container reports whether the line owns the following Step lines as its
// subtree. Only tiled and list lines do.
func (s Style) Container() bool {
	k := s.Kind()
	return k == StyleTiled || k == StyleList
}

// Cacheable reports whether a client may cache the line across connections.
func (s Style) Cacheable() bool { return s&StyleCacheable != 0 }

// Hidden reports whether the line should not be drawn.
func (s Style) Hidden() bool { return s&StyleHidden != 0 }

// Disabled reports whether the line is inert. A disabled container disables
// its whole subtree.
func (s Style) Disabled() bool { return s&StyleDisabled != 0 }

// Deferred reports the vendor's hold-the-redraw hint.
func (s Style) Deferred() bool { return s&StyleDeferred != 0 }

// AccessGated reports the exact substitution the vendor menu engine performs
// for a line the session's user level may not see: a Data line, hidden and
// disabled, with command 0 and the text "Reserved" (rc_menu.c).
//
// It means "this line exists but not at your level", which is different from a
// broken or empty line, and it is the only way to detect level gating: the
// line count never changes.
func AccessGated(s Style, command uint32, text string) bool {
	return s.Kind() == StyleData && s.Hidden() && s.Disabled() &&
		command == 0 && text == "Reserved"
}

func (s Style) String() string {
	var name string
	switch s.Kind() {
	case StyleTiled:
		name = "Tiled"
	case StyleList:
		name = "List"
	case StyleDisplay:
		name = "Display"
	case StyleButton:
		name = "Button"
	case StyleCheckbox:
		name = "Checkbox"
	case StyleNumber:
		name = "Number"
	case StyleVGraph:
		name = "VGraph"
	case StyleHGraph:
		name = "HGraph"
	case StyleEditString:
		name = "EditString"
	case StyleVLevel:
		name = "VLevel"
	case StyleHLevel:
		name = "HLevel"
	case StylePartial:
		name = "Partial"
	case StyleData:
		name = "Data"
	case StyleLink:
		name = "Link"
	default:
		name = "Style?"
	}
	var b strings.Builder
	b.WriteString(name)
	for _, e := range []struct {
		bit  Style
		name string
	}{
		{StyleCacheable, "Cacheable"}, {StyleWraps, "Wraps"},
		{StyleDisabled, "Disabled"}, {StyleHidden, "Hidden"},
		{StyleDeferred, "Deferred"},
	} {
		if s&e.bit != 0 {
			b.WriteByte('+')
			b.WriteString(e.name)
		}
	}
	return b.String()
}

// TermCode explains why a session ended (spec 12.4).
type TermCode uint16

const (
	TermUser     TermCode = 0
	TermTimeout  TermCode = 1
	TermNetError TermCode = 2
	TermRemote   TermCode = 3
)

func (t TermCode) String() string {
	switch t {
	case TermUser:
		return "user"
	case TermTimeout:
		return "timeout"
	case TermNetError:
		return "net-error"
	case TermRemote:
		return "remote"
	default:
		return "unknown(" + itoa(uint16(t)) + ")"
	}
}

// Display line numbers below zero carry priority rather than position
// (spec 11.5.4).
const (
	DisplayLineError   int16 = -1
	DisplayLineWarning int16 = -2
)

func itoa(v uint16) string {
	if v == 0 {
		return "0"
	}
	var buf [5]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
