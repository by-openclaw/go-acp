package codec

import (
	"encoding/binary"
	"fmt"
)

// Wire sizes of the remaining service structures.
const (
	LogPacketSize   = 38 // LOGPACKET_STR, before the message text
	StreamModeSize  = 4  // STREAMMODE_STR
	StreamHdrSize   = 8  // STREAMHDR_STR, before the data
	DisplayCapsSize = 12 // DISPLAYCAPS_STR
	DrawBitmapSize  = 2  // DRAWBITMAP_STR, before the bitmap
	DrawTextSize    = 2  // DRAWTEXT_STR, before the text
	SetGroupSize    = 2  // SETGROUP_STR
	GroupParamSize  = 2  // GROUPPARAM_STR
	DownloadSize    = 2  // DOWNLOAD_STR

	// RouteErrorSize is zero because ROUTEERROR_STR has no definition.
	//
	// Packet type 38 refers to it in rc3comm.h and no shipped header declares
	// it, so its layout is unknown. The frame is delivered with its payload
	// intact and a caller may inspect it; inventing a structure would be a
	// guess presented as fact. See docs/spec-coverage.md §4.
	RouteErrorSize = 0
)

// LogPacket is LOGPACKET_STR: one line of a unit's log, pushed to a subscriber.
//
// It names its source by both type id and user-given name, which matters
// because a log server collects from many units and the frame's source address
// only says which gateway forwarded it.
type LogPacket struct {
	Time   SysTime
	Format uint16
	ID     uint16
	Name   string // the source unit's user-given name, 19 usable bytes

	// Text is the log message itself, which follows the structure.
	Text string
}

func (l LogPacket) String() string {
	return fmt.Sprintf("%s [%s] %s", l.Time, l.Name, l.Text)
}

// AppendTo appends the wire form: the structure, then the message text.
func (l LogPacket) AppendTo(dst []byte) ([]byte, error) {
	dst = l.Time.AppendTo(dst)

	var head [4]byte
	binary.BigEndian.PutUint16(head[0:2], l.Format)
	binary.BigEndian.PutUint16(head[2:4], l.ID)
	dst = append(dst, head[:]...)

	dst, err := appendFixedString(dst, l.Name, MaxTextSize)
	if err != nil {
		return nil, err
	}
	if l.Text == "" {
		return dst, nil
	}
	return appendCString(dst, l.Text)
}

// DecodeLogPacket reads a LOGPACKET_STR and its trailing message.
func DecodeLogPacket(b []byte) (LogPacket, error) {
	if err := need(b, LogPacketSize, "LogPacket", ""); err != nil {
		return LogPacket{}, err
	}
	l := LogPacket{
		Time:   sysTimeAt(b[0:TimeSize]),
		Format: binary.BigEndian.Uint16(b[14:16]),
		ID:     binary.BigEndian.Uint16(b[16:18]),
		Name:   fixedString(b[18:38]),
	}
	if len(b) > LogPacketSize {
		l.Text, _ = CString(b[LogPacketSize:])
	}
	return l, nil
}

// Stream mode flags (RC3STRM.H StreamModeFlags).
const (
	// StreamBinary opens the stream without line-ending translation.
	StreamBinary uint8 = 1
)

// StreamMode is STREAMMODE_STR: opening or reconfiguring a logical stream.
type StreamMode struct {
	Stream  uint8
	Mode    uint8
	MaxSize uint16
}

func (s StreamMode) String() string {
	return fmt.Sprintf("stream=%d mode=%02X max=%d", s.Stream, s.Mode, s.MaxSize)
}

// Binary reports whether the stream carries untranslated bytes.
func (s StreamMode) Binary() bool { return s.Mode&StreamBinary != 0 }

// AppendTo appends the 4-byte wire form.
func (s StreamMode) AppendTo(dst []byte) []byte {
	var b [StreamModeSize]byte
	b[0] = s.Stream
	b[1] = s.Mode
	binary.BigEndian.PutUint16(b[2:4], s.MaxSize)
	return append(dst, b[:]...)
}

// DecodeStreamMode reads a STREAMMODE_STR from the front of b.
func DecodeStreamMode(b []byte) (StreamMode, error) {
	if err := need(b, StreamModeSize, "StreamMode", ""); err != nil {
		return StreamMode{}, err
	}
	return StreamMode{
		Stream:  b[0],
		Mode:    b[1],
		MaxSize: binary.BigEndian.Uint16(b[2:4]),
	}, nil
}

// StreamHeader is STREAMHDR_STR: one block of stream data.
//
// Like the file service it carries a handle from each end, so a reply can be
// matched without either side having to agree on numbering.
type StreamHeader struct {
	ClientHandle int16
	ServerHandle int16
	Command      uint16
	Data         []byte
}

func (s StreamHeader) String() string {
	return fmt.Sprintf("client=%d server=%d cmd=%d %dB",
		s.ClientHandle, s.ServerHandle, s.Command, len(s.Data))
}

// AppendTo appends the header and its data. The length is taken from the
// slice, so the two can never disagree.
func (s StreamHeader) AppendTo(dst []byte) ([]byte, error) {
	if len(s.Data) > MaxPayload {
		return nil, fmt.Errorf("%w: %d bytes of stream data", ErrPayloadTooLong, len(s.Data))
	}
	var b [StreamHdrSize]byte
	binary.BigEndian.PutUint16(b[0:2], uint16(s.ClientHandle))
	binary.BigEndian.PutUint16(b[2:4], uint16(s.ServerHandle))
	binary.BigEndian.PutUint16(b[4:6], s.Command)
	binary.BigEndian.PutUint16(b[6:8], uint16(int16(len(s.Data))))
	return append(append(dst, b[:]...), s.Data...), nil
}

// DecodeStreamHeader reads a STREAMHDR_STR and its data.
func DecodeStreamHeader(b []byte) (StreamHeader, error) {
	if err := need(b, StreamHdrSize, "StreamHeader", ""); err != nil {
		return StreamHeader{}, err
	}
	s := StreamHeader{
		ClientHandle: int16(binary.BigEndian.Uint16(b[0:2])),
		ServerHandle: int16(binary.BigEndian.Uint16(b[2:4])),
		Command:      binary.BigEndian.Uint16(b[4:6]),
	}
	n := int(int16(binary.BigEndian.Uint16(b[6:8])))
	if n < 0 {
		return StreamHeader{}, decodeErr("StreamHeader", "Bytes", 6,
			fmt.Errorf("negative data length %d", n))
	}
	if err := need(b[StreamHdrSize:], n, "StreamHeader", "Data"); err != nil {
		return StreamHeader{}, err
	}
	s.Data = b[StreamHdrSize : StreamHdrSize+n]
	return s, nil
}

// Display colour formats (rc3comm.h CF_ constants): bits per pixel.
const (
	ColourFormat2Bit  int16 = 1
	ColourFormat4Bit  int16 = 4
	ColourFormat8Bit  int16 = 8
	ColourFormat15Bit int16 = 15
	ColourFormat16Bit int16 = 16
	ColourFormat24Bit int16 = 24
)

// DisplayCaps is DISPLAYCAPS_STR: what a unit's display can show.
//
// A unit answers with one of these per display it has, which is what tells a
// client whether it is driving a four-line character panel or a bitmap screen.
type DisplayCaps struct {
	Display uint8
	Type    uint8
	Chars   int16 // characters per line
	Lines   int16
	XPixels int16
	YPixels int16
	Format  int16 // bits per pixel
}

// IsCharacter reports whether the display is character-based, which is true
// when it has no pixel resolution to speak of.
func (d DisplayCaps) IsCharacter() bool { return d.XPixels == 0 && d.YPixels == 0 }

func (d DisplayCaps) String() string {
	if d.IsCharacter() {
		return fmt.Sprintf("display=%d %dx%d chars", d.Display, d.Chars, d.Lines)
	}
	return fmt.Sprintf("display=%d %dx%d px %d-bit", d.Display, d.XPixels, d.YPixels, d.Format)
}

// AppendTo appends the 12-byte wire form.
func (d DisplayCaps) AppendTo(dst []byte) []byte {
	var b [DisplayCapsSize]byte
	b[0] = d.Display
	b[1] = d.Type
	binary.BigEndian.PutUint16(b[2:4], uint16(d.Chars))
	binary.BigEndian.PutUint16(b[4:6], uint16(d.Lines))
	binary.BigEndian.PutUint16(b[6:8], uint16(d.XPixels))
	binary.BigEndian.PutUint16(b[8:10], uint16(d.YPixels))
	binary.BigEndian.PutUint16(b[10:12], uint16(d.Format))
	return append(dst, b[:]...)
}

// DecodeDisplayCaps reads a DISPLAYCAPS_STR from the front of b.
func DecodeDisplayCaps(b []byte) (DisplayCaps, error) {
	if err := need(b, DisplayCapsSize, "DisplayCaps", ""); err != nil {
		return DisplayCaps{}, err
	}
	return DisplayCaps{
		Display: b[0],
		Type:    b[1],
		Chars:   int16(binary.BigEndian.Uint16(b[2:4])),
		Lines:   int16(binary.BigEndian.Uint16(b[4:6])),
		XPixels: int16(binary.BigEndian.Uint16(b[6:8])),
		YPixels: int16(binary.BigEndian.Uint16(b[8:10])),
		Format:  int16(binary.BigEndian.Uint16(b[10:12])),
	}, nil
}

// DrawBitmap is DRAWBITMAP_STR and the bitmap that follows it.
//
// The header is two bytes: which display, and a pad the header documents as
// "must be zero". Everything about the bitmap's own layout comes from the
// display's capabilities rather than from this structure.
type DrawBitmap struct {
	Display uint8
	Bitmap  []byte
}

func (d DrawBitmap) String() string {
	return fmt.Sprintf("display=%d bitmap=%dB", d.Display, len(d.Bitmap))
}

// AppendTo appends the header and the bitmap.
func (d DrawBitmap) AppendTo(dst []byte) []byte {
	return append(append(dst, d.Display, 0), d.Bitmap...)
}

// DecodeDrawBitmap reads a DRAWBITMAP_STR and its bitmap.
func DecodeDrawBitmap(b []byte) (DrawBitmap, error) {
	if err := need(b, DrawBitmapSize, "DrawBitmap", ""); err != nil {
		return DrawBitmap{}, err
	}
	return DrawBitmap{Display: b[0], Bitmap: b[DrawBitmapSize:]}, nil
}

// DrawText is DRAWTEXT_STR and the text that follows it.
type DrawText struct {
	Display uint8
	Mode    uint8
	Text    string
}

func (d DrawText) String() string {
	return fmt.Sprintf("display=%d mode=%d %q", d.Display, d.Mode, d.Text)
}

// AppendTo appends the header and the text.
func (d DrawText) AppendTo(dst []byte) ([]byte, error) {
	dst = append(dst, d.Display, d.Mode)
	return appendCString(dst, d.Text)
}

// DecodeDrawText reads a DRAWTEXT_STR and its text.
func DecodeDrawText(b []byte) (DrawText, error) {
	if err := need(b, DrawTextSize, "DrawText", ""); err != nil {
		return DrawText{}, err
	}
	d := DrawText{Display: b[0], Mode: b[1]}
	if len(b) > DrawTextSize {
		d.Text, _ = CString(b[DrawTextSize:])
	}
	return d, nil
}

// SetGroup is SETGROUP_STR: putting a unit into a control group.
//
// A group lets one write reach several units at once. The master flag marks
// the unit whose values the others follow, which is how a group is kept
// consistent without the client writing to each in turn.
type SetGroup struct {
	Group  uint8
	Master bool
}

func (s SetGroup) String() string {
	if s.Master {
		return fmt.Sprintf("group=%d master", s.Group)
	}
	return fmt.Sprintf("group=%d", s.Group)
}

// AppendTo appends the 2-byte wire form.
func (s SetGroup) AppendTo(dst []byte) []byte {
	var m byte
	if s.Master {
		m = 1
	}
	return append(dst, s.Group, m)
}

// DecodeSetGroup reads a SETGROUP_STR from the front of b.
func DecodeSetGroup(b []byte) (SetGroup, error) {
	if err := need(b, SetGroupSize, "SetGroup", ""); err != nil {
		return SetGroup{}, err
	}
	return SetGroup{Group: b[0], Master: b[1] != 0}, nil
}

// GroupParam is GROUPPARAM_STR: which group a group write applies to.
//
// It precedes a FuncStatus in a group set, so the payload is this structure
// followed by the value.
type GroupParam struct {
	Group uint8
}

func (g GroupParam) String() string { return fmt.Sprintf("group=%d", g.Group) }

// AppendTo appends the 2-byte wire form, pad included and zeroed.
func (g GroupParam) AppendTo(dst []byte) []byte {
	return append(dst, g.Group, 0)
}

// DecodeGroupParam reads a GROUPPARAM_STR from the front of b.
func DecodeGroupParam(b []byte) (GroupParam, error) {
	if err := need(b, GroupParamSize, "GroupParam", ""); err != nil {
		return GroupParam{}, err
	}
	return GroupParam{Group: b[0]}, nil
}

// Download is DOWNLOAD_STR: a configuration download request.
//
// The vendor header labels its only field "No idea - DRAGONS" and nothing else
// in the sources explains it, so it is carried through unchanged rather than
// interpreted.
type Download struct {
	List uint16
}

func (d Download) String() string { return fmt.Sprintf("list=%d", d.List) }

// AppendTo appends the 2-byte wire form.
func (d Download) AppendTo(dst []byte) []byte {
	var b [DownloadSize]byte
	binary.BigEndian.PutUint16(b[:], d.List)
	return append(dst, b[:]...)
}

// DecodeDownload reads a DOWNLOAD_STR from the front of b.
func DecodeDownload(b []byte) (Download, error) {
	if err := need(b, DownloadSize, "Download", ""); err != nil {
		return Download{}, err
	}
	return Download{List: binary.BigEndian.Uint16(b)}, nil
}
