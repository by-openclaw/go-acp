package codec

import (
	"fmt"
)

// Type tags recognised by the codec. See CLAUDE.md for the authoritative
// list. The 1.0 required set is `i f s b`; extended tags `h d t S c r m`
// are widely deployed and handled here. 1.1 additions `T F N I [ ]` are
// wired via tagstring.go (PhaseA vs PhaseB guard).
const (
	TagInt32     = 'i'
	TagFloat32   = 'f'
	TagString    = 's'
	TagBlob      = 'b'
	TagInt64     = 'h'
	TagFloat64   = 'd'
	TagTimetag   = 't'
	TagSymbol    = 'S'
	TagChar      = 'c'
	TagRGBA32    = 'r'
	TagMIDI      = 'm'

	// OSC 1.1 tags (payload-less)
	TagTrue       = 'T'
	TagFalse      = 'F'
	TagNil        = 'N'
	TagInfinitum  = 'I'
	TagArrayBegin = '['
	TagArrayEnd   = ']'
)

// Arg is one argument in an OSC Message. The Tag byte drives which of
// the Value fields the caller reads; zero-valued fields for other tags
// are irrelevant.
//
// Value is typed implicitly by Tag:
//
//	'i' → Int32       'f' → Float32
//	'h' → Int64       'd' → Float64
//	's' / 'S' → String
//	'b' → Blob
//	't' → Uint64      (OSC Timetag NTP format)
//	'c' → Int32       ('c' holds a single ASCII char in the low byte)
//	'r' → Blob (4 bytes RGBA)    'm' → Blob (4 bytes MIDI)
//	'T' / 'F' / 'N' / 'I' — no payload; Tag alone conveys the value
type Arg struct {
	Tag byte

	Int32   int32
	Float32 float32
	Int64   int64
	Float64 float64
	String  string
	Blob    []byte
	Uint64  uint64
}

// Convenience constructors.
func Int32(v int32) Arg     { return Arg{Tag: TagInt32, Int32: v} }
func Float32(v float32) Arg { return Arg{Tag: TagFloat32, Float32: v} }
func Int64(v int64) Arg     { return Arg{Tag: TagInt64, Int64: v} }
func Float64(v float64) Arg { return Arg{Tag: TagFloat64, Float64: v} }
func String(s string) Arg   { return Arg{Tag: TagString, String: s} }
func Blob(b []byte) Arg     { return Arg{Tag: TagBlob, Blob: b} }
func Symbol(s string) Arg   { return Arg{Tag: TagSymbol, String: s} }
func Char(r int32) Arg      { return Arg{Tag: TagChar, Int32: r} }
func RGBA(b []byte) Arg     { return Arg{Tag: TagRGBA32, Blob: b} }
func MIDI(b []byte) Arg     { return Arg{Tag: TagMIDI, Blob: b} }
func Timetag(ntp uint64) Arg { return Arg{Tag: TagTimetag, Uint64: ntp} }
func True() Arg              { return Arg{Tag: TagTrue} }
func False() Arg             { return Arg{Tag: TagFalse} }
func Nil() Arg               { return Arg{Tag: TagNil} }
func Infinitum() Arg         { return Arg{Tag: TagInfinitum} }

// Message is a decoded OSC Message. Address is the OSC address
// (typically begins with '/'), Args are the decoded arguments.
type Message struct {
	Address string
	Args    []Arg

	// Notes carries spec-deviation notes observed during Decode.
	Notes []ComplianceNote
}

// Encode serialises an OSC Message per spec §OSC Message. For 1.1 tags
// (T/F/N/I/[/]) the caller must opt in via an enabler — but Encode
// always produces their canonical form when supplied via Arg.Tag.
//
// No version check is performed here; Plugin.Connect / Server.SendX
// is where 1.0 vs 1.1 framing distinction happens.
func (m Message) Encode() ([]byte, error) {
	if m.Address == "" {
		return nil, fmt.Errorf("osc message: empty address")
	}
	out := make([]byte, 0, 64)
	out = encodeString(out, m.Address)

	// Build the type-tag string.
	tags := make([]byte, 0, len(m.Args)+1)
	tags = append(tags, ',')
	for _, a := range m.Args {
		tags = append(tags, a.Tag)
	}
	out = encodeString(out, string(tags))

	// Args in order.
	for i, a := range m.Args {
		data, err := encodeArg(a)
		if err != nil {
			return nil, fmt.Errorf("arg[%d]: %w", i, err)
		}
		out = append(out, data...)
	}
	return out, nil
}

// encodeArg serialises one argument per its tag. Returns nil for
// payload-less 1.1 tags.
func encodeArg(a Arg) ([]byte, error) {
	switch a.Tag {
	case TagInt32, TagChar:
		return encodeInt32(nil, a.Int32), nil
	case TagFloat32:
		return encodeFloat32(nil, a.Float32), nil
	case TagInt64:
		return encodeInt64(nil, a.Int64), nil
	case TagFloat64:
		return encodeFloat64(nil, a.Float64), nil
	case TagString, TagSymbol:
		return encodeString(nil, a.String), nil
	case TagBlob:
		return encodeBlob(nil, a.Blob), nil
	case TagTimetag:
		return encodeUint64(nil, a.Uint64), nil
	case TagRGBA32, TagMIDI:
		if len(a.Blob) != 4 {
			return nil, fmt.Errorf("osc: tag %c expects 4-byte payload, got %d", a.Tag, len(a.Blob))
		}
		return append([]byte(nil), a.Blob...), nil
	case TagTrue, TagFalse, TagNil, TagInfinitum, TagArrayBegin, TagArrayEnd:
		return nil, nil
	}
	return nil, fmt.Errorf("%w: 0x%02x (%q)", ErrTagUnknown, a.Tag, a.Tag)
}

// DecodeMessage parses an OSC Message from b. The entire slice must
// contain exactly one message (caller responsibility — for bundles use
// DecodeBundle).
func DecodeMessage(b []byte) (Message, error) {
	m := Message{}
	addr, n, err := decodeString(b, 0)
	if err != nil {
		return m, fmt.Errorf("address: %w", err)
	}
	m.Address = addr
	off := n

	tagStr, n, err := decodeString(b, off)
	if err != nil {
		return m, fmt.Errorf("type tag: %w", err)
	}
	off += n
	if len(tagStr) == 0 || tagStr[0] != ',' {
		return m, fmt.Errorf("%w: got %q", ErrCommaMissing, tagStr)
	}
	tags := tagStr[1:]

	for i := 0; i < len(tags); i++ {
		tag := tags[i]
		a := Arg{Tag: tag}
		switch tag {
		case TagInt32, TagChar:
			v, err := decodeInt32(b, off)
			if err != nil {
				return m, fmt.Errorf("arg[%d] (%c): %w", i, tag, err)
			}
			a.Int32 = v
			off += 4
		case TagFloat32:
			v, err := decodeFloat32(b, off)
			if err != nil {
				return m, fmt.Errorf("arg[%d] (%c): %w", i, tag, err)
			}
			a.Float32 = v
			off += 4
		case TagInt64:
			v, err := decodeInt64(b, off)
			if err != nil {
				return m, fmt.Errorf("arg[%d] (%c): %w", i, tag, err)
			}
			a.Int64 = v
			off += 8
		case TagFloat64:
			v, err := decodeFloat64(b, off)
			if err != nil {
				return m, fmt.Errorf("arg[%d] (%c): %w", i, tag, err)
			}
			a.Float64 = v
			off += 8
		case TagTimetag:
			v, err := decodeUint64(b, off)
			if err != nil {
				return m, fmt.Errorf("arg[%d] (%c): %w", i, tag, err)
			}
			a.Uint64 = v
			off += 8
		case TagString, TagSymbol:
			s, nc, err := decodeString(b, off)
			if err != nil {
				return m, fmt.Errorf("arg[%d] (%c): %w", i, tag, err)
			}
			a.String = s
			off += nc
		case TagBlob:
			blob, nc, err := decodeBlob(b, off)
			if err != nil {
				return m, fmt.Errorf("arg[%d] (%c): %w", i, tag, err)
			}
			a.Blob = blob
			off += nc
		case TagRGBA32, TagMIDI:
			if off+4 > len(b) {
				return m, fmt.Errorf("arg[%d] (%c): %w", i, tag, ErrTruncated)
			}
			a.Blob = append([]byte(nil), b[off:off+4]...)
			off += 4
		case TagTrue, TagFalse, TagNil, TagInfinitum, TagArrayBegin, TagArrayEnd:
			// payload-less — Arg.Tag alone carries the value
		default:
			m.Notes = append(m.Notes, ComplianceNote{
				Kind:   "osc_type_tag_unknown",
				Detail: fmt.Sprintf("arg[%d] tag 0x%02x (%q) unknown — skipping remainder", i, tag, tag),
			})
			// Unknown tag — we can't advance off safely. Stop and return
			// what we have so the caller sees the partial message + note.
			m.Args = append(m.Args, a)
			return m, nil
		}
		m.Args = append(m.Args, a)
	}
	return m, nil
}
