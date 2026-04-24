package codec

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unicode/utf16"
	"unicode/utf8"
)

// V5.0 packet envelope (§5.0).
//
//	PBC(2LE) | VER(1) | FLAGS(1) | SCREEN(2LE) | body
//
// PBC is the total byte count of body (excludes the 2 PBC bytes).
// VER is the minor version (0 for v5.0).
// FLAGS bit 0: 0 = ASCII, 1 = UTF-16LE text
// FLAGS bit 1: 0 = body is DMSG+, 1 = body is SCONTROL
// FLAGS bits 2-7: reserved
// SCREEN: 0-65534; 0xFFFF = broadcast; 0 if unused
const (
	V50PBCIdx     = 0
	V50VERIdx     = 2
	V50FLAGSIdx   = 3
	V50SCREENIdx  = 4
	V50HeaderSize = 6

	V50MinorVersion = 0
	V50BroadcastIdx = 0xFFFF // special SCREEN / INDEX value

	v50FlagsUTF16LE   = 1 << 0
	v50FlagsSCONTROL  = 1 << 1
	v50FlagsReserved  = 0xFC // bits 2-7 must be 0

	// DMSG offsets (relative to start of DMSG block)
	v50DMSGIndexIdx    = 0
	v50DMSGControlIdx  = 2
	v50DMSGLengthIdx   = 4 // present when CONTROL bit 15 == 0
	v50DMSGHeaderSize  = 4 // INDEX + CONTROL

	// CONTROL bit layout
	v50CtrlRHMask         = 0x3
	v50CtrlTextShift      = 2
	v50CtrlTextMask       = 0x3 << 2
	v50CtrlLHShift        = 4
	v50CtrlLHMask         = 0x3 << 4
	v50CtrlBrightnessShft = 6
	v50CtrlBrightnessMask = 0x3 << 6
	v50CtrlReservedMask   = 0x7F << 8 // bits 8-14
	v50CtrlControlDataBit = 1 << 15

	// Spec: "Maximum packet length is 2048 bytes."
	V50MaxPacketSize = 2048
)

// Errors returned by the v5.0 codec.
var (
	ErrV50PacketTooSmall  = errors.New("tsl v5.0: packet < header size (6 bytes)")
	ErrV50PBCMismatch     = errors.New("tsl v5.0: PBC byte count doesn't match available payload")
	ErrV50VersionUnknown  = errors.New("tsl v5.0: unknown VER (non-zero minor)")
	ErrV50FlagsReserved   = errors.New("tsl v5.0: reserved FLAGS bits set")
	ErrV50DMSGTruncated   = errors.New("tsl v5.0: DMSG truncated")
	ErrV50TextDecode      = errors.New("tsl v5.0: TEXT decode failure")
	ErrV50PacketTooLarge  = errors.New("tsl v5.0: packet exceeds 2048-byte maximum")
)

// DMSG is one display message inside a v5.0 packet.
//
// Tally-colour fields are named LH / TextTally / RH to avoid collision
// with the Text string field (the UMD label).
type DMSG struct {
	Index       uint16     // display address; 0xFFFF = broadcast
	RH          TallyColor // CONTROL bits 0-1
	TextTally   TallyColor // CONTROL bits 2-3
	LH          TallyColor // CONTROL bits 4-5
	Brightness  Brightness // CONTROL bits 6-7 (0-3)
	ControlData bool       // CONTROL bit 15
	// ReservedBits carries the raw CONTROL bits 8-14 for compliance checks.
	ReservedBits uint8

	// Text is UTF-8 after decode. On Encode, the caller provides UTF-8;
	// the codec transcodes to UTF-16LE if V50Packet.Flags has UTF16LE set.
	Text string

	// ControlDataBytes is populated when ControlData is true — the
	// remaining DMSG payload is undefined per spec and kept verbatim.
	ControlDataBytes []byte
}

// V50Packet is the decoded v5.0 envelope + body.
type V50Packet struct {
	Version    uint8 // VER byte (0 for v5.0)
	UTF16LE    bool  // FLAGS bit 0
	SControl   bool  // FLAGS bit 1
	Screen     uint16
	DMSGs      []DMSG
	SControlRaw []byte // when SControl == true, the body after the envelope

	Notes []ComplianceNote
}

// encodeControl packs a DMSG's CONTROL word per §5.0.
func (d DMSG) encodeControl() uint16 {
	var c uint16
	c |= uint16(d.RH) & 0x3
	c |= (uint16(d.TextTally) & 0x3) << 2
	c |= (uint16(d.LH) & 0x3) << 4
	c |= (uint16(d.Brightness) & 0x3) << 6
	c |= uint16(d.ReservedBits&0x7F) << 8
	if d.ControlData {
		c |= v50CtrlControlDataBit
	}
	return c
}

// decodeControl unpacks a CONTROL word into tally/brightness fields.
func decodeControl(c uint16) (d DMSG) {
	d.RH = TallyColor(c & v50CtrlRHMask)
	d.TextTally = TallyColor((c & v50CtrlTextMask) >> v50CtrlTextShift)
	d.LH = TallyColor((c & v50CtrlLHMask) >> v50CtrlLHShift)
	d.Brightness = Brightness((c & v50CtrlBrightnessMask) >> v50CtrlBrightnessShft)
	d.ReservedBits = uint8((c >> 8) & 0x7F)
	d.ControlData = c&v50CtrlControlDataBit != 0
	return d
}

// Encode returns the v5.0 packet body bytes including the PBC header.
// ASCII mode packs the text bytes as-is; UTF-16LE mode transcodes via
// encoding/binary little-endian.
func (p V50Packet) Encode() ([]byte, error) {
	// Build the body first (everything after the 2-byte PBC), then
	// prepend PBC.
	body := make([]byte, 0, 64)
	body = append(body, p.Version)

	var flags uint8
	if p.UTF16LE {
		flags |= v50FlagsUTF16LE
	}
	if p.SControl {
		flags |= v50FlagsSCONTROL
	}
	body = append(body, flags)

	var screen [2]byte
	binary.LittleEndian.PutUint16(screen[:], p.Screen)
	body = append(body, screen[:]...)

	if p.SControl {
		body = append(body, p.SControlRaw...)
	} else {
		for _, d := range p.DMSGs {
			var ib [2]byte
			binary.LittleEndian.PutUint16(ib[:], d.Index)
			body = append(body, ib[:]...)

			var cb [2]byte
			binary.LittleEndian.PutUint16(cb[:], d.encodeControl())
			body = append(body, cb[:]...)

			if d.ControlData {
				body = append(body, d.ControlDataBytes...)
				continue
			}

			// Encode TEXT with length prefix.
			textBytes, err := encodeV50Text(d.Text, p.UTF16LE)
			if err != nil {
				return nil, err
			}
			if len(textBytes) > 0xFFFF {
				return nil, fmt.Errorf("tsl v5.0: DMSG TEXT too long (%d bytes)", len(textBytes))
			}
			var lb [2]byte
			binary.LittleEndian.PutUint16(lb[:], uint16(len(textBytes)))
			body = append(body, lb[:]...)
			body = append(body, textBytes...)
		}
	}

	// PBC = byte count of body (spec §5.0).
	if len(body) > V50MaxPacketSize-2 {
		return nil, fmt.Errorf("%w: body=%d", ErrV50PacketTooLarge, len(body))
	}
	var pbcBytes [2]byte
	binary.LittleEndian.PutUint16(pbcBytes[:], uint16(len(body)))

	out := make([]byte, 0, 2+len(body))
	out = append(out, pbcBytes[:]...)
	out = append(out, body...)
	return out, nil
}

// encodeV50Text returns the bytes for the TEXT field. In ASCII mode each
// rune must be in 0x20..0x7E; UTF-16LE mode transcodes to 2 bytes per
// code unit (surrogates supported via encoding/binary + unicode/utf16).
func encodeV50Text(s string, utf16le bool) ([]byte, error) {
	if !utf16le {
		for i := 0; i < len(s); i++ {
			c := s[i]
			if c < 0x20 || c > 0x7E {
				return nil, fmt.Errorf("tsl v5.0: non-printable ASCII 0x%02x at index %d", c, i)
			}
		}
		return []byte(s), nil
	}
	runes := []rune(s)
	u16 := utf16.Encode(runes)
	out := make([]byte, 2*len(u16))
	for i, r := range u16 {
		binary.LittleEndian.PutUint16(out[2*i:], r)
	}
	return out, nil
}

// decodeV50Text converts a TEXT byte slice to a UTF-8 Go string.
func decodeV50Text(b []byte, utf16le bool) (string, error) {
	if !utf16le {
		return string(b), nil
	}
	if len(b)%2 != 0 {
		return "", fmt.Errorf("%w: UTF-16LE payload length %d is odd", ErrV50TextDecode, len(b))
	}
	u16 := make([]uint16, len(b)/2)
	for i := range u16 {
		u16[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	runes := utf16.Decode(u16)
	// Validate every rune encodes back to UTF-8.
	var out []byte
	for _, r := range runes {
		var buf [4]byte
		n := utf8.EncodeRune(buf[:], r)
		out = append(out, buf[:n]...)
	}
	return string(out), nil
}

// DecodeV50 parses a packet body (without the outer DLE/STX TCP wrapper).
// For UDP, pass the raw datagram. For TCP, de-stuff first via
// DecodeDLEStream.
func DecodeV50(pkt []byte) (V50Packet, error) {
	if len(pkt) < V50HeaderSize {
		return V50Packet{}, fmt.Errorf("%w: got %d", ErrV50PacketTooSmall, len(pkt))
	}
	if len(pkt) > V50MaxPacketSize {
		return V50Packet{}, fmt.Errorf("%w: got %d", ErrV50PacketTooLarge, len(pkt))
	}

	pbc := binary.LittleEndian.Uint16(pkt[V50PBCIdx:])
	if int(pbc)+2 != len(pkt) {
		// Some devices set PBC to the total including itself, or
		// mis-size it. Record as note; try to continue with available
		// bytes.
		return V50Packet{}, fmt.Errorf("%w: PBC=%d, packet=%d", ErrV50PBCMismatch, pbc, len(pkt))
	}

	p := V50Packet{}
	p.Version = pkt[V50VERIdx]
	if p.Version != V50MinorVersion {
		p.Notes = append(p.Notes, ComplianceNote{
			Kind:   "tsl_version_mismatch",
			Detail: fmt.Sprintf("v5.0 VER=%d, expected %d", p.Version, V50MinorVersion),
		})
	}

	flags := pkt[V50FLAGSIdx]
	p.UTF16LE = flags&v50FlagsUTF16LE != 0
	p.SControl = flags&v50FlagsSCONTROL != 0
	if flags&v50FlagsReserved != 0 {
		p.Notes = append(p.Notes, ComplianceNote{
			Kind:   "tsl_reserved_bit_set",
			Detail: fmt.Sprintf("v5.0 FLAGS reserved bits set (FLAGS=0x%02x)", flags),
		})
	}

	p.Screen = binary.LittleEndian.Uint16(pkt[V50SCREENIdx:])
	if p.Screen == V50BroadcastIdx {
		p.Notes = append(p.Notes, ComplianceNote{
			Kind:   "tsl_broadcast_received",
			Detail: "v5.0 SCREEN = 0xFFFF (broadcast)",
		})
	}

	body := pkt[V50HeaderSize:]
	if p.SControl {
		p.SControlRaw = append([]byte(nil), body...)
		p.Notes = append(p.Notes, ComplianceNote{
			Kind:   "tsl_control_data_undefined",
			Detail: "v5.0 SCONTROL body not defined in this version",
		})
		return p, nil
	}

	// Parse DMSGs sequentially.
	cursor := 0
	for cursor < len(body) {
		if cursor+v50DMSGHeaderSize > len(body) {
			return p, fmt.Errorf("%w: DMSG header at offset %d", ErrV50DMSGTruncated, cursor)
		}
		d := decodeControl(binary.LittleEndian.Uint16(body[cursor+v50DMSGControlIdx:]))
		d.Index = binary.LittleEndian.Uint16(body[cursor+v50DMSGIndexIdx:])
		cursor += v50DMSGHeaderSize

		if d.Index == V50BroadcastIdx {
			p.Notes = append(p.Notes, ComplianceNote{
				Kind:   "tsl_broadcast_received",
				Detail: "v5.0 DMSG INDEX = 0xFFFF (broadcast)",
			})
		}
		if d.ReservedBits != 0 {
			p.Notes = append(p.Notes, ComplianceNote{
				Kind:   "tsl_reserved_bit_set",
				Detail: fmt.Sprintf("v5.0 CONTROL reserved bits 8-14 set (0x%02x)", d.ReservedBits),
			})
		}

		if d.ControlData {
			// Remainder of body is undefined CONTROL_DATA per spec.
			d.ControlDataBytes = append([]byte(nil), body[cursor:]...)
			p.DMSGs = append(p.DMSGs, d)
			p.Notes = append(p.Notes, ComplianceNote{
				Kind:   "tsl_control_data_undefined",
				Detail: "v5.0 DMSG CONTROL bit 15 set — CONTROL_DATA undefined at this version",
			})
			break
		}

		if cursor+2 > len(body) {
			return p, fmt.Errorf("%w: LENGTH at offset %d", ErrV50DMSGTruncated, cursor)
		}
		textLen := binary.LittleEndian.Uint16(body[cursor:])
		cursor += 2
		if cursor+int(textLen) > len(body) {
			return p, fmt.Errorf("%w: TEXT(%d) at offset %d exceeds body", ErrV50DMSGTruncated, textLen, cursor)
		}
		text, err := decodeV50Text(body[cursor:cursor+int(textLen)], p.UTF16LE)
		if err != nil {
			return p, err
		}
		d.Text = text
		cursor += int(textLen)
		p.DMSGs = append(p.DMSGs, d)

		if p.UTF16LE {
			p.Notes = append(p.Notes, ComplianceNote{
				Kind:   "tsl_charset_transcode",
				Detail: "v5.0 TEXT was UTF-16LE; transcoded to UTF-8",
			})
		}
	}
	return p, nil
}
