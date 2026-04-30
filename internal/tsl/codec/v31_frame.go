package codec

import (
	"errors"
	"fmt"
)

// V31 wire constants (§3.0 of the TSL UMD spec).
const (
	V31FrameSize   = 18
	V31HeaderIdx   = 0
	V31CtrlIdx     = 1
	V31DataIdx     = 2
	V31DataSize    = 16
	V31AddressMax  = 126  // address range 0..126
	V31HeaderMSB   = 0x80 // bit 7 fixed
	V31ASCIIMin    = 0x20
	V31ASCIIMax    = 0x7E
	V31PadSpace    = 0x20
	V31CtrlBitRsvd = 1 << 6
	V31CtrlBit7    = 1 << 7
)

// Errors returned by the v3.1 codec.
var (
	ErrV31FrameSize   = errors.New("tsl v3.1: frame size != 18 bytes")
	ErrV31HeaderMSB   = errors.New("tsl v3.1: header bit 7 is not set (0x80)")
	ErrV31AddressRng  = errors.New("tsl v3.1: address out of range 0..126")
	ErrV31NonPrintTx  = errors.New("tsl v3.1: non-printable ASCII in text on encode")
)

// V31Frame is the decoded v3.1 display message.
//
//	HEADER(1) | CTRL(1) | DATA(16)
//
//	HEADER = (Address & 0x7F) | 0x80
//	CTRL:   bit0 Tally1 .. bit3 Tally4, bit4-5 Brightness, bit6 reserved, bit7 = 0
//	DATA:   16 ASCII 0x20..0x7E, space-padded if Text < 16 chars
type V31Frame struct {
	Address    uint8
	Tally1     bool
	Tally2     bool
	Tally3     bool
	Tally4     bool
	Brightness Brightness
	// Text is a space-padded 16-byte string post-Encode; free-form on
	// input to Encode (truncated or padded to 16).
	Text string
	// Notes carries spec-deviation notes observed during Decode. Empty
	// on new frames constructed for Encode.
	Notes []ComplianceNote
}

// Encode returns the 18-byte wire frame. Errors on address overflow or
// non-printable ASCII in Text (0x20..0x7E only, per §3.0).
func (f V31Frame) Encode() ([]byte, error) {
	if f.Address > V31AddressMax {
		return nil, fmt.Errorf("%w: %d", ErrV31AddressRng, f.Address)
	}
	out := make([]byte, V31FrameSize)
	out[V31HeaderIdx] = (f.Address & 0x7F) | V31HeaderMSB

	var ctrl uint8
	if f.Tally1 {
		ctrl |= 1 << 0
	}
	if f.Tally2 {
		ctrl |= 1 << 1
	}
	if f.Tally3 {
		ctrl |= 1 << 2
	}
	if f.Tally4 {
		ctrl |= 1 << 3
	}
	ctrl |= (uint8(f.Brightness) & 0x3) << 4
	// bits 6 and 7 stay 0 per spec.
	out[V31CtrlIdx] = ctrl

	// DATA: 16 ASCII, space-pad if Text shorter.
	n := len(f.Text)
	for i := 0; i < V31DataSize; i++ {
		var c byte
		if i < n {
			c = f.Text[i]
			if c < V31ASCIIMin || c > V31ASCIIMax {
				return nil, fmt.Errorf("%w: byte 0x%02x at index %d", ErrV31NonPrintTx, c, i)
			}
		} else {
			c = V31PadSpace
		}
		out[V31DataIdx+i] = c
	}
	return out, nil
}

// DecodeV31 parses an 18-byte v3.1 frame. Spec deviations are surfaced
// via Frame.Notes rather than errors unless the frame is structurally
// unparseable (wrong size, bit 7 of HEADER clear).
func DecodeV31(data []byte) (V31Frame, error) {
	if len(data) != V31FrameSize {
		return V31Frame{}, fmt.Errorf("%w: got %d", ErrV31FrameSize, len(data))
	}
	header := data[V31HeaderIdx]
	if header&V31HeaderMSB == 0 {
		return V31Frame{}, fmt.Errorf("%w: header=0x%02x", ErrV31HeaderMSB, header)
	}

	f := V31Frame{
		Address: header & 0x7F,
	}
	ctrl := data[V31CtrlIdx]
	f.Tally1 = ctrl&(1<<0) != 0
	f.Tally2 = ctrl&(1<<1) != 0
	f.Tally3 = ctrl&(1<<2) != 0
	f.Tally4 = ctrl&(1<<3) != 0
	f.Brightness = Brightness((ctrl >> 4) & 0x3)

	// Reserved-bit check (§3.0: CTRL bit 6 = reserved, bit 7 = cleared).
	if ctrl&V31CtrlBitRsvd != 0 {
		f.Notes = append(f.Notes, ComplianceNote{
			Kind:   "tsl_reserved_bit_set",
			Detail: fmt.Sprintf("v3.1 CTRL bit 6 set (CTRL=0x%02x)", ctrl),
		})
	}
	if ctrl&V31CtrlBit7 != 0 {
		f.Notes = append(f.Notes, ComplianceNote{
			Kind:   "tsl_reserved_bit_set",
			Detail: fmt.Sprintf("v3.1 CTRL bit 7 set (CTRL=0x%02x)", ctrl),
		})
	}

	// DATA: tolerate 0x00 pad (Decimator/TallyArbiter deviation) and any
	// non-printable chars — fire a note but keep the raw bytes.
	nullPadSeen := false
	for i := 0; i < V31DataSize; i++ {
		b := data[V31DataIdx+i]
		if b == 0x00 {
			nullPadSeen = true
		} else if b < V31ASCIIMin || b > V31ASCIIMax {
			f.Notes = append(f.Notes, ComplianceNote{
				Kind:   "tsl_label_charset",
				Detail: fmt.Sprintf("v3.1 DATA[%d]=0x%02x outside ASCII 0x20..0x7E", i, b),
			})
		}
	}
	if nullPadSeen {
		f.Notes = append(f.Notes, ComplianceNote{
			Kind:   "tsl_v31_null_pad",
			Detail: "v3.1 DATA contains 0x00 pad (spec requires space 0x20)",
		})
	}
	f.Text = string(data[V31DataIdx : V31DataIdx+V31DataSize])
	return f, nil
}
