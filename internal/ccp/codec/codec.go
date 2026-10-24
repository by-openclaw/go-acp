// Package codec is the wire layer of the Axon Cortex Control Panel
// Protocol (CCPP) 1.18, August 2019 — the protocol between the Cortex
// application (or any controller, Cerebrum included) and Axon hardware
// panels such as the CCP-1601.
//
// Transport (§1.0.0, §1.1.0): TCP, port 2071; the panel is the server and
// accepts at least two clients, sending unsolicited frames (button
// actions, GPI changes) to every open connection and replies only to the
// connection that asked.
//
// Framing (§1.1.0): one command byte followed by command data. There is
// NO length field: the command byte fixes the data length, except for the
// few commands that carry their own length or a terminated string. The
// decoder therefore knows the length of every panel command.
//
// Stdlib only; imports nothing from dhs (ADR-0006).
package codec

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// DefaultPort is the IANA port shared with the Axon Control Protocol (§1.1.0).
const DefaultPort = 2071

// Client commands (§1.1.0 table, controller → panel).
const (
	CmdPoll            = 0x01
	CmdReset           = 0x02
	CmdSoundAlarm      = 0x03
	CmdClear           = 0x04
	CmdSetColour       = 0x05
	CmdWriteBitmap     = 0x06
	CmdShowBitmap      = 0x08
	CmdWriteText       = 0x09
	CmdSetGPO          = 0x0A
	CmdReadGPI         = 0x0B
	CmdSetDebounce     = 0x0C
	CmdReadGPO         = 0x0D
	CmdReadDebounce    = 0x0E
	CmdReadTBar        = 0x0F
	CmdSetTextColour   = 0x10
	CmdReadSubnet      = 0x24
	CmdReadGateway     = 0x25
	CmdReadDHCP        = 0x26
	CmdReadSerial      = 0x28
	CmdSetBrightness   = 0x29
	CmdSetRGBColour    = 0x35
	CmdMultiple        = 0x40
	CmdReportFirmware  = 0x71
	CmdReadMAC         = 0x76
)

// Panel commands and responses (§1.1.0 table, panel → controller).
const (
	RspPoll         = 0x81
	RspButton       = 0x82
	RspError        = 0x83
	RspAck          = 0x84
	RspGPI          = 0x85
	RspGPO          = 0x86
	RspSubnet       = 0x87
	RspGateway      = 0x88
	RspDHCP         = 0x89
	RspDebounce     = 0x8A
	RspMAC          = 0x8B
	RspTBar         = 0x8C
	RspSerialParams = 0x8D
	RspSerialData   = 0x8E
	RspSerialBreak  = 0x8F
	RspFirmware     = 0xF1
)

// Button actions (§1.3.2).
const (
	ActionDown      = 0x01
	ActionUp        = 0x02
	ActionIncrement = 0x03
	ActionDecrement = 0x04
	ActionTBar      = 0x05
)

// Error codes (§1.3.3).
const (
	ErrPSU            = 0x01
	ErrInvalidCommand = 0x02
	ErrInvalidData    = 0x03
	ErrNotExecuted    = 0x04
)

// DeviceTypes is the poll-reply device type table (§1.3.1).
var DeviceTypes = map[byte]string{
	0x01: "CCP-1601", 0x02: "CCP-3201", 0x03: "CCP-4801", 0x04: "CCP-1601B",
	0x05: "CCP-3201B", 0x06: "CCP-4801B", 0x07: "CCP-4200B", 0x08: "CCP-4800B",
	0x09: "CCP-8002B", 0x0A: "CCP-6002B", 0x10: "CGP-4848", 0x20: "CCP-4800",
	0x21: "CCP-2410", 0x30: "LPP-1000",
}

// CommandNames names every command byte of the §1.1.0 table.
var CommandNames = map[byte]string{
	CmdPoll: "poll", CmdReset: "reset", CmdSoundAlarm: "sound alarm", CmdClear: "clear",
	CmdSetColour: "set colour", CmdWriteBitmap: "write bitmap", 0x07: "write bitmap to storage",
	CmdShowBitmap: "show bitmap", CmdWriteText: "write text", CmdSetGPO: "set GPO",
	CmdReadGPI: "read GPI", CmdSetDebounce: "set debounce", CmdReadGPO: "read GPO",
	CmdReadDebounce: "read debounce", CmdReadTBar: "read T-bar", CmdSetTextColour: "set text colour",
	0x20: "set IP", 0x21: "set subnet", 0x22: "set gateway", 0x23: "set DHCP",
	CmdReadSubnet: "read subnet", CmdReadGateway: "read gateway", CmdReadDHCP: "read DHCP",
	0x27: "set serial", CmdReadSerial: "read serial", CmdSetBrightness: "set brightness",
	CmdSetRGBColour: "set RGB colour", CmdMultiple: "multiple", 0x41: "send serial packet",
	0x42: "send serial break", CmdReportFirmware: "report firmware", 0x73: "enter bootload",
	0x75: "set MAC", CmdReadMAC: "read MAC",
	RspPoll: "poll reply", RspButton: "button action", RspError: "error", RspAck: "acknowledge",
	RspGPI: "GPI state", RspGPO: "GPO state", RspSubnet: "subnet", RspGateway: "gateway",
	RspDHCP: "DHCP mode", RspDebounce: "debounce", RspMAC: "MAC", RspTBar: "T-bar position",
	RspSerialParams: "serial parameters", RspSerialData: "serial data", RspSerialBreak: "serial break",
	RspFirmware: "firmware version",
}

// Builders of client commands. Each returns the bytes to write.

func Poll() []byte           { return []byte{CmdPoll} }
func ReportFirmware() []byte { return []byte{CmdReportFirmware} }
func ReadMAC() []byte        { return []byte{CmdReadMAC} }
func ReadDHCP() []byte       { return []byte{CmdReadDHCP} }
func ReadSubnet() []byte     { return []byte{CmdReadSubnet} }
func ReadGateway() []byte    { return []byte{CmdReadGateway} }
func ReadGPI() []byte        { return []byte{CmdReadGPI} }
func ReadGPO() []byte        { return []byte{CmdReadGPO} }
func ReadDebounce() []byte   { return []byte{CmdReadDebounce} }
func ReadSerial() []byte     { return []byte{CmdReadSerial} }

// SoundAlarm sounds the panel for duration × 100 ms (0 = nominal) (§1.2.3).
func SoundAlarm(kind, duration byte) []byte { return []byte{CmdSoundAlarm, kind, duration} }

// Clear clears one button/display; 0 = all (§1.2.4).
func Clear(id byte) []byte { return []byte{CmdClear, id} }

// SetColour sets the colour index of one button; 0 = all (§1.2.5).
func SetColour(id, colour byte) []byte { return []byte{CmdSetColour, id, colour} }

// SetTextColour sets the text colour of one button (§1.2.16).
func SetTextColour(id, colour byte) []byte { return []byte{CmdSetTextColour, id, colour} }

// SetBrightness sets the brightness level 0..7 of one button (§1.2.41).
func SetBrightness(id, level byte) []byte { return []byte{CmdSetBrightness, id, level} }

// ShowBitmap shows a stored bitmap on a button (§1.2.8).
func ShowBitmap(id, storage byte) []byte { return []byte{CmdShowBitmap, id, storage} }

// ReadTBar reads the T-bar position of one control (§1.2.15).
func ReadTBar(id byte) []byte { return []byte{CmdReadTBar, id} }

// SetRGBColour sets an RGB565 colour, LSB first (§1.2.53).
func SetRGBColour(id byte, rgb565 uint16) []byte {
	return []byte{CmdSetRGBColour, id, byte(rgb565), byte(rgb565 >> 8)}
}

// ErrText is returned for text the panel cannot carry.
var ErrText = errors.New("ccp: text must be 7-bit ASCII without NUL")

// WriteText writes NUL-terminated ASCII text to a display (§1.2.9).
func WriteText(id, line, font byte, text string) ([]byte, error) {
	for _, r := range text {
		if r == 0 || r > 0x7F {
			return nil, ErrText
		}
	}
	b := []byte{CmdWriteText, id, line, font}
	b = append(b, text...)
	return append(b, 0), nil
}

// SetGPO sets every output from a bitmap, GPO-1 in the LS bit of the MS byte (§1.2.10).
func SetGPO(bitmap []byte) []byte {
	return append([]byte{CmdSetGPO, byte(len(bitmap))}, bitmap...)
}

// Multiple wraps several commands in one frame (§1.2.64): count, a 2-byte
// MSB-first byte count, then the commands.
func Multiple(cmds ...[]byte) ([]byte, error) {
	if len(cmds) < 2 {
		return nil, fmt.Errorf("ccp: multiple needs at least two commands")
	}
	if len(cmds) > 0xFF {
		return nil, fmt.Errorf("ccp: multiple carries at most 255 commands")
	}
	var body []byte
	for _, c := range cmds {
		if len(c) == 0 || c[0] == CmdMultiple {
			return nil, fmt.Errorf("ccp: multiple cannot nest 0x40 or carry an empty command")
		}
		body = append(body, c...)
	}
	if len(body) > 0x13FE {
		return nil, fmt.Errorf("ccp: multiple body %d bytes exceeds 0x13fe", len(body))
	}
	out := []byte{CmdMultiple, byte(len(cmds))}
	out = binary.BigEndian.AppendUint16(out, uint16(len(body)))
	return append(out, body...), nil
}

// Frame is one decoded panel command.
type Frame struct {
	Cmd  byte
	Data []byte // the bytes after the command byte
}

// Name returns the command's name.
func (f Frame) Name() string {
	if n, ok := CommandNames[f.Cmd]; ok {
		return n
	}
	return fmt.Sprintf("0x%02x", f.Cmd)
}

// ErrUnknownCommand is returned for a byte that starts no panel command:
// without a length field the stream cannot be re-synchronised past it.
type ErrUnknownCommand struct{ Byte byte }

func (e ErrUnknownCommand) Error() string {
	return fmt.Sprintf("ccp: unknown panel command 0x%02x", e.Byte)
}

// Next returns the first complete panel frame in buf and the number of
// bytes it took. n == 0 with a nil error means more bytes are needed.
func Next(buf []byte) (f Frame, n int, err error) {
	if len(buf) == 0 {
		return Frame{}, 0, nil
	}
	cmd := buf[0]
	need := func(dataLen int) (Frame, int, error) {
		if len(buf) < 1+dataLen {
			return Frame{}, 0, nil
		}
		return Frame{Cmd: cmd, Data: append([]byte(nil), buf[1:1+dataLen]...)}, 1 + dataLen, nil
	}
	switch cmd {
	case RspPoll:
		return need(5)
	case RspButton:
		if len(buf) < 2 {
			return Frame{}, 0, nil
		}
		if buf[1] == ActionTBar {
			return need(4) // action, id, 16-bit position
		}
		return need(3) // action, id, 1-byte value
	case RspError:
		return need(3)
	case RspAck, RspSerialBreak:
		return need(0)
	case RspGPI, RspGPO:
		if len(buf) < 2 {
			return Frame{}, 0, nil
		}
		return need(1 + int(buf[1]))
	case RspSubnet, RspGateway:
		return need(4)
	case RspDHCP, RspDebounce:
		return need(1)
	case RspMAC:
		return need(6)
	case RspTBar:
		return need(3)
	case RspSerialParams:
		return need(6)
	case RspSerialData:
		if len(buf) < 3 {
			return Frame{}, 0, nil
		}
		return need(2 + int(binary.BigEndian.Uint16(buf[1:3])))
	case RspFirmware:
		// §1.3.113: up to 9 ASCII bytes, NUL-terminated — except the
		// CCP-1601/3201/4801, which send 9 characters and no NUL.
		for i := 1; i < len(buf) && i <= 9; i++ {
			if buf[i] == 0 {
				return Frame{Cmd: cmd, Data: append([]byte(nil), buf[1:i]...)}, i + 1, nil
			}
		}
		if len(buf) >= 10 {
			return Frame{Cmd: cmd, Data: append([]byte(nil), buf[1:10]...)}, 10, nil
		}
		return Frame{}, 0, nil
	}
	return Frame{}, 0, ErrUnknownCommand{Byte: cmd}
}

// PollReply is a decoded 0x81 (§1.3.1).
type PollReply struct {
	DeviceType      byte
	Model           string
	ProtocolVersion byte
	PSUA, PSUB      bool
	RefreshRequired bool
}

// ParsePoll decodes a poll reply.
func ParsePoll(f Frame) (PollReply, error) {
	if f.Cmd != RspPoll || len(f.Data) != 5 {
		return PollReply{}, fmt.Errorf("ccp: not a poll reply")
	}
	m, ok := DeviceTypes[f.Data[0]]
	if !ok {
		m = fmt.Sprintf("type 0x%02x", f.Data[0])
	}
	return PollReply{DeviceType: f.Data[0], Model: m, ProtocolVersion: f.Data[1],
		PSUA: f.Data[2] != 0, PSUB: f.Data[3] != 0, RefreshRequired: f.Data[4] != 0}, nil
}

// Button is a decoded 0x82 (§1.3.2).
type Button struct {
	Action byte
	ID     byte
	Value  uint16
}

// ActionName names a button action.
func (b Button) ActionName() string {
	switch b.Action {
	case ActionDown:
		return "down"
	case ActionUp:
		return "up"
	case ActionIncrement:
		return "increment"
	case ActionDecrement:
		return "decrement"
	case ActionTBar:
		return "t-bar"
	}
	return fmt.Sprintf("action 0x%02x", b.Action)
}

// ParseButton decodes a button action.
func ParseButton(f Frame) (Button, error) {
	if f.Cmd != RspButton || len(f.Data) < 3 {
		return Button{}, fmt.Errorf("ccp: not a button action")
	}
	b := Button{Action: f.Data[0], ID: f.Data[1], Value: uint16(f.Data[2])}
	if len(f.Data) == 4 {
		b.Value = binary.BigEndian.Uint16(f.Data[2:4])
	}
	return b, nil
}

// PanelError is a decoded 0x83 (§1.3.3).
type PanelError struct {
	Code         byte
	Data1, Data2 byte
}

func (e PanelError) Error() string {
	switch e.Code {
	case ErrPSU:
		return fmt.Sprintf("ccp: PSU error (A=%d B=%d)", e.Data1, e.Data2)
	case ErrInvalidCommand:
		return fmt.Sprintf("ccp: panel refused command 0x%02x (%s) as invalid", e.Data1, CommandNames[e.Data1])
	case ErrInvalidData:
		return fmt.Sprintf("ccp: panel refused data of command 0x%02x at byte %d", e.Data1, e.Data2)
	case ErrNotExecuted:
		return fmt.Sprintf("ccp: panel did not execute command 0x%02x", e.Data1)
	}
	return fmt.Sprintf("ccp: panel error code 0x%02x (0x%02x 0x%02x)", e.Code, e.Data1, e.Data2)
}

// ParseError decodes an error frame.
func ParseError(f Frame) (PanelError, error) {
	if f.Cmd != RspError || len(f.Data) != 3 {
		return PanelError{}, fmt.Errorf("ccp: not an error frame")
	}
	return PanelError{Code: f.Data[0], Data1: f.Data[1], Data2: f.Data[2]}, nil
}

// IPv4 renders a 4-byte address (subnet, gateway replies).
func IPv4(b []byte) string {
	if len(b) != 4 {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], b[3])
}

// Firmware returns the firmware text of a 0xF1 frame.
func Firmware(f Frame) string { return strings.TrimRight(string(f.Data), "\x00") }

// ExpectedReply is the reply a client command waits for: Ack for every
// command unless §1.2 names another (§1.2.0).
func ExpectedReply(cmd byte) byte {
	switch cmd {
	case CmdPoll:
		return RspPoll
	case CmdReportFirmware:
		return RspFirmware
	case CmdReadMAC:
		return RspMAC
	case CmdReadDHCP:
		return RspDHCP
	case CmdReadSubnet:
		return RspSubnet
	case CmdReadGateway:
		return RspGateway
	case CmdReadGPI:
		return RspGPI
	case CmdReadGPO:
		return RspGPO
	case CmdReadDebounce:
		return RspDebounce
	case CmdReadTBar:
		return RspTBar
	case CmdReadSerial:
		return RspSerialParams
	}
	return RspAck
}
