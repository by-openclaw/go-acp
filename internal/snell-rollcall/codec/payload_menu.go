package codec

import (
	"encoding/binary"
	"fmt"
)

// Wire sizes of the 16-bit menu and multi-packet structures.
const (
	FuncSize        = 58 // spec 11.4.1 FUNC_STR
	FuncStyleSize   = 6  // spec 11.4.2 FUNCSTYLE_STR
	BlockHeaderSize = 8  // spec 11.2.5 BLOCKHEADER_STR
	GetNextSize     = 4  // spec 11.2.6 GETNEXT_STR: 3 declared bytes plus a pad
)

// Func is one menu line in the 16-bit generation (spec 11.4.1).
//
// The meaning of the numeric fields depends on Style:
//
//   - container lines (tiled, list) put the size of their subtree in Step.
//     That is the whole span of following lines, not the count of immediate
//     children, so reconstructing the tree is a pre-order walk that consumes
//     Step entries per container.
//   - Partial puts the base index of the linked partial in Command.
//   - Button puts the value it writes in MinRange; a run of buttons sharing a
//     Command is a radio group.
//   - Checkbox puts the "on" value in MinRange, conventionally 1.
//   - numeric lines use MinRange, MaxRange, Step and DivScale, and Param is a
//     printf format applied to Value/DivScale.
//
// DivScale of zero means one. Devices publish it routinely and dividing
// blindly would fault (spec 7.2.2.6).
type Func struct {
	MenuIndex uint16
	Style     Style
	Command   uint16
	MinRange  int32
	MaxRange  int32
	Step      uint16
	DivScale  uint16
	Text      string // 19 usable bytes
	Param     string // printf format, 19 usable bytes
}

// Scale returns DivScale with the documented zero-means-one rule applied.
func (f Func) Scale() uint16 {
	if f.DivScale == 0 {
		return 1
	}
	return f.DivScale
}

// AccessGated reports whether the server substituted this line because the
// session's user level may not see it.
func (f Func) AccessGated() bool {
	return AccessGated(f.Style, uint32(f.Command), f.Text)
}

func (f Func) String() string {
	return fmt.Sprintf("idx=%d %s cmd=%d %d..%d step=%d div=%d %q %q",
		f.MenuIndex, f.Style, f.Command, f.MinRange, f.MaxRange, f.Step, f.DivScale, f.Text, f.Param)
}

// AppendTo appends the 58-byte wire form.
func (f Func) AppendTo(dst []byte) ([]byte, error) {
	var head [18]byte
	binary.BigEndian.PutUint16(head[0:2], f.MenuIndex)
	binary.BigEndian.PutUint16(head[2:4], uint16(f.Style))
	binary.BigEndian.PutUint16(head[4:6], f.Command)
	binary.BigEndian.PutUint32(head[6:10], uint32(f.MinRange))
	binary.BigEndian.PutUint32(head[10:14], uint32(f.MaxRange))
	binary.BigEndian.PutUint16(head[14:16], f.Step)
	binary.BigEndian.PutUint16(head[16:18], f.DivScale)
	dst = append(dst, head[:]...)

	dst, err := appendFixedString(dst, f.Text, MaxTextSize)
	if err != nil {
		return nil, err
	}
	return appendFixedString(dst, f.Param, MaxTextSize)
}

// DecodeFunc reads a FUNC_STR from the front of b.
func DecodeFunc(b []byte) (Func, error) {
	if err := need(b, FuncSize, "Func", ""); err != nil {
		return Func{}, err
	}
	return Func{
		MenuIndex: binary.BigEndian.Uint16(b[0:2]),
		Style:     Style(binary.BigEndian.Uint16(b[2:4])),
		Command:   binary.BigEndian.Uint16(b[4:6]),
		MinRange:  int32(binary.BigEndian.Uint32(b[6:10])),
		MaxRange:  int32(binary.BigEndian.Uint32(b[10:14])),
		Step:      binary.BigEndian.Uint16(b[14:16]),
		DivScale:  binary.BigEndian.Uint16(b[16:18]),
		Text:      fixedString(b[18:38]),
		Param:     fixedString(b[38:58]),
	}, nil
}

// FuncStyle updates only the style of an existing menu line (spec 11.4.2).
// It is a back-channel message and may only change the hidden and disabled
// bits; everything else in the line stays as it was (spec 9.30).
type FuncStyle struct {
	MenuIndex uint16
	Style     Style
	Command   uint16
}

func (f FuncStyle) String() string {
	return fmt.Sprintf("idx=%d %s cmd=%d", f.MenuIndex, f.Style, f.Command)
}

// AppendTo appends the 6-byte wire form.
func (f FuncStyle) AppendTo(dst []byte) []byte {
	var b [FuncStyleSize]byte
	binary.BigEndian.PutUint16(b[0:2], f.MenuIndex)
	binary.BigEndian.PutUint16(b[2:4], uint16(f.Style))
	binary.BigEndian.PutUint16(b[4:6], f.Command)
	return append(dst, b[:]...)
}

// DecodeFuncStyle reads a FUNCSTYLE_STR from the front of b.
func DecodeFuncStyle(b []byte) (FuncStyle, error) {
	if err := need(b, FuncStyleSize, "FuncStyle", ""); err != nil {
		return FuncStyle{}, err
	}
	return FuncStyle{
		MenuIndex: binary.BigEndian.Uint16(b[0:2]),
		Style:     Style(binary.BigEndian.Uint16(b[2:4])),
		Command:   binary.BigEndian.Uint16(b[4:6]),
	}, nil
}

// BlockHeader opens a multi-packet transfer, telling the client how many items
// follow (spec 11.2.5). The client then requests each with GetNextPkt.
//
// Spare is declared "Must be zero"; a non-zero value is a genuine deviation,
// unlike the undefined pad byte in GetNext.
type BlockHeader struct {
	PktType  PacketType // the request that produced this transfer
	Spare    uint8
	Count    uint16
	MaxSize  uint16
	Function uint16 // optional, used when the transfer relates to a command
}

func (h BlockHeader) String() string {
	return fmt.Sprintf("for=%s count=%d maxsize=%d", h.PktType, h.Count, h.MaxSize)
}

// AppendTo appends the 8-byte wire form.
func (h BlockHeader) AppendTo(dst []byte) []byte {
	var b [BlockHeaderSize]byte
	b[0] = byte(h.PktType)
	b[1] = h.Spare
	binary.BigEndian.PutUint16(b[2:4], h.Count)
	binary.BigEndian.PutUint16(b[4:6], h.MaxSize)
	binary.BigEndian.PutUint16(b[6:8], h.Function)
	return append(dst, b[:]...)
}

// DecodeBlockHeader reads a BLOCKHEADER_STR from the front of b.
func DecodeBlockHeader(b []byte) (BlockHeader, error) {
	if err := need(b, BlockHeaderSize, "BlockHeader", ""); err != nil {
		return BlockHeader{}, err
	}
	return BlockHeader{
		PktType:  PacketType(b[0]),
		Spare:    b[1],
		Count:    binary.BigEndian.Uint16(b[2:4]),
		MaxSize:  binary.BigEndian.Uint16(b[4:6]),
		Function: binary.BigEndian.Uint16(b[6:8]),
	}, nil
}

// GetNext requests one item of a multi-packet transfer (spec 11.2.6).
//
// The structure declares three bytes but occupies four: the vendor packs to a
// two-byte boundary and senders use sizeof, so a pad byte goes on the wire.
// Its content is undefined and must never be reported as a deviation.
type GetNext struct {
	Index   uint16
	PktType PacketType // the request that produced the transfer
}

func (g GetNext) String() string {
	return fmt.Sprintf("idx=%d for=%s", g.Index, g.PktType)
}

// AppendTo appends the 4-byte wire form, pad byte included and zeroed.
func (g GetNext) AppendTo(dst []byte) []byte {
	var b [GetNextSize]byte
	binary.BigEndian.PutUint16(b[0:2], g.Index)
	b[2] = byte(g.PktType)
	return append(dst, b[:]...)
}

// DecodeGetNext reads a GETNEXT_STR from the front of b.
//
// Three bytes are accepted as well as four: the declared size is three and a
// peer that trims the pad is not wrong.
func DecodeGetNext(b []byte) (GetNext, error) {
	if err := need(b, 3, "GetNext", ""); err != nil {
		return GetNext{}, err
	}
	return GetNext{
		Index:   binary.BigEndian.Uint16(b[0:2]),
		PktType: PacketType(b[2]),
	}, nil
}
