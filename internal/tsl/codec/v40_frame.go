package codec

import (
	"errors"
	"fmt"
)

// V40 wire constants (§4.0 of the TSL UMD spec).
//
// v4.0 extends v3.1 with a checksum byte (CHKSUM), a version/byte-count
// byte (VBC), and an XDATA payload. The v3.1 section is unchanged — a
// compliant v3.1-only receiver ignores bytes past offset 17.
//
//	v3.1 part (18) | CHKSUM (1) | VBC (1) | XDATA (N = VBC.0-3)
//
// CHKSUM   = 2's-complement of sum(HEADER+CTRL+DATA) modulo 128
// VBC bit7 = 0
// VBC bit6-4 = minor version (v4.0 → 0)
// VBC bit3-0 = XDATA byte count (0-15)
//
// At minor version 0 (v4.0 baseline) XDATA is exactly 2 bytes — Xbyte L
// for the left display, Xbyte R for the right. Each Xbyte packs three
// 2-bit tally colours (LH / Text / RH) + reserved bits.
const (
	V40ChksumIdx     = V31FrameSize
	V40VBCIdx        = V31FrameSize + 1
	V40XDataStartIdx = V31FrameSize + 2

	V40MinorVersionV40 = 0 // v4.0 → VBC bits 6-4 == 0
	V40XDataCountV40   = 2 // at min-version 0, XDATA is 2 bytes

	V40FrameSize = V31FrameSize + 1 + 1 + V40XDataCountV40 // 22

	// XDATA byte bit layout (per spec §4.0 XDATA table)
	v40XDataBit7Mask   = 1 << 7
	v40XDataBit6Mask   = 1 << 6
	v40XDataLHMask     = 0x3 << 4
	v40XDataTextMask   = 0x3 << 2
	v40XDataRHMask     = 0x3 << 0
	v40XDataLHShift    = 4
	v40XDataTextShift  = 2
	v40XDataRHShift    = 0
	v40VBCMinorMask    = 0x7 << 4
	v40VBCMinorShift   = 4
	v40VBCXDataMask    = 0x0F
	v40VBCBit7Mask     = 1 << 7
)

// Errors returned by the v4.0 codec.
var (
	ErrV40FrameSize      = errors.New("tsl v4.0: frame too small (need >= 20 bytes)")
	ErrV40ChecksumFail   = errors.New("tsl v4.0: CHKSUM mismatch")
	ErrV40VBCBit7Set     = errors.New("tsl v4.0: VBC bit 7 not cleared")
	ErrV40XDataCountBad  = errors.New("tsl v4.0: VBC XDATA count does not match available payload")
)

// XByte is one Display-L or Display-R 6-bit tally triple plus reserved bits.
type XByte struct {
	LH       TallyColor
	Text     TallyColor
	RH       TallyColor
	Reserved bool // bit 6 — observed on rx (should be 0)
}

// Encode packs into a single byte: bit7=0, bit6=Reserved, bits 5-4 LH,
// bits 3-2 Text, bits 1-0 RH.
func (x XByte) Encode() byte {
	var b byte
	b |= (byte(x.LH) & 0x3) << v40XDataLHShift
	b |= (byte(x.Text) & 0x3) << v40XDataTextShift
	b |= (byte(x.RH) & 0x3) << v40XDataRHShift
	if x.Reserved {
		b |= v40XDataBit6Mask
	}
	// bit 7 stays 0 per spec.
	return b
}

// DecodeXByte parses one v4.0 XDATA byte.
func DecodeXByte(b byte) XByte {
	return XByte{
		LH:       TallyColor((b & v40XDataLHMask) >> v40XDataLHShift),
		Text:     TallyColor((b & v40XDataTextMask) >> v40XDataTextShift),
		RH:       TallyColor((b & v40XDataRHMask) >> v40XDataRHShift),
		Reserved: b&v40XDataBit6Mask != 0,
	}
}

// V40Frame is the decoded v4.0 display message. The v3.1 block is kept
// inline for field reuse — it's the exact same 18 bytes on the wire.
type V40Frame struct {
	V31          V31Frame
	DisplayLeft  XByte // Xbyte 1 — bits for Display L
	DisplayRight XByte // Xbyte 2 — bits for Display R
	MinorVersion uint8 // VBC bits 6-4
	XDataCount   uint8 // VBC bits 3-0
	Chksum       uint8 // transmitted CHKSUM byte

	// Notes captures compliance-event observations beyond those already
	// carried on V31.Notes.
	Notes []ComplianceNote
}

// computeChksum returns the 2's-complement-mod-128 of the v3.1 body.
func computeV40Chksum(v31Body []byte) uint8 {
	if len(v31Body) < V31FrameSize {
		return 0
	}
	var sum int
	for i := 0; i < V31FrameSize; i++ {
		sum += int(v31Body[i])
	}
	return uint8((-sum) & 0x7F)
}

// Encode serialises the frame. Address/text validation is delegated to
// V31.Encode; XDATA count is fixed at 2 (minor version 0).
func (f V40Frame) Encode() ([]byte, error) {
	v31Bytes, err := f.V31.Encode()
	if err != nil {
		return nil, err
	}
	out := make([]byte, V40FrameSize)
	copy(out, v31Bytes)

	// CHKSUM — over the 18-byte v3.1 block.
	out[V40ChksumIdx] = computeV40Chksum(v31Bytes)

	// VBC — bit 7 = 0, minor version in bits 6-4, XDATA count in bits 3-0.
	var vbc uint8
	vbc |= (V40MinorVersionV40 & 0x7) << v40VBCMinorShift
	vbc |= V40XDataCountV40 & v40VBCXDataMask
	out[V40VBCIdx] = vbc

	// XDATA: Xbyte L, Xbyte R.
	out[V40XDataStartIdx+0] = f.DisplayLeft.Encode()
	out[V40XDataStartIdx+1] = f.DisplayRight.Encode()
	return out, nil
}

// DecodeV40 parses a v4.0 frame. Minimum size is 20 bytes (v3.1 +
// CHKSUM + VBC); maximum at min-version 0 is 22 (including 2-byte
// XDATA). Decoding tolerates a mismatching checksum and out-of-spec
// minor version — both surface via Notes and do not cause decode to
// fail structurally.
func DecodeV40(data []byte) (V40Frame, error) {
	const minSize = V31FrameSize + 2
	if len(data) < minSize {
		return V40Frame{}, fmt.Errorf("%w: got %d", ErrV40FrameSize, len(data))
	}

	v31, err := DecodeV31(data[:V31FrameSize])
	if err != nil {
		return V40Frame{}, fmt.Errorf("v3.1 section: %w", err)
	}

	f := V40Frame{V31: v31}

	f.Chksum = data[V40ChksumIdx]
	expected := computeV40Chksum(data[:V31FrameSize])
	if f.Chksum != expected {
		f.Notes = append(f.Notes, ComplianceNote{
			Kind:   "tsl_checksum_fail",
			Detail: fmt.Sprintf("v4.0 CHKSUM got 0x%02x, expected 0x%02x", f.Chksum, expected),
		})
	}

	vbc := data[V40VBCIdx]
	if vbc&v40VBCBit7Mask != 0 {
		f.Notes = append(f.Notes, ComplianceNote{
			Kind:   "tsl_reserved_bit_set",
			Detail: fmt.Sprintf("v4.0 VBC bit 7 set (VBC=0x%02x)", vbc),
		})
	}
	f.MinorVersion = (vbc & v40VBCMinorMask) >> v40VBCMinorShift
	f.XDataCount = vbc & v40VBCXDataMask

	if f.MinorVersion != V40MinorVersionV40 {
		f.Notes = append(f.Notes, ComplianceNote{
			Kind:   "tsl_version_mismatch",
			Detail: fmt.Sprintf("v4.0 VBC minor version %d, expected 0", f.MinorVersion),
		})
	}

	available := len(data) - V40XDataStartIdx
	if int(f.XDataCount) > available {
		f.Notes = append(f.Notes, ComplianceNote{
			Kind:   "tsl_v40_xdata_truncated",
			Detail: fmt.Sprintf("VBC says %d XDATA bytes but only %d available", f.XDataCount, available),
		})
	}

	// Decode the two XDATA bytes when present. If VBC reports a different
	// count, trust the bytes we actually have (up to 2) and rely on the
	// compliance note.
	if len(data) > V40XDataStartIdx {
		f.DisplayLeft = DecodeXByte(data[V40XDataStartIdx])
	}
	if len(data) > V40XDataStartIdx+1 {
		f.DisplayRight = DecodeXByte(data[V40XDataStartIdx+1])
	}

	// CTRL.6 in v4.0 marks command data — reserved in this version.
	ctrl := data[V31CtrlIdx]
	if ctrl&V31CtrlBitRsvd != 0 {
		f.Notes = append(f.Notes, ComplianceNote{
			Kind:   "tsl_control_data_undefined",
			Detail: "v4.0 CTRL bit 6 (command-data flag) set — reserved at this version",
		})
	}

	return f, nil
}
