// Package dtp implements RollCall Data Transfer Params: the compact,
// self-describing parameter block the Full Control Command Set uses to carry
// more than one value in a single command.
//
// A block is a count byte followed by that many items, each introduced by a
// type byte. Integers are variable-length so that small numbers cost one byte,
// which matters because the whole block has to fit the payload of one RollCall
// packet alongside everything else.
//
// The format is designed to be extended: a receiver that meets a block with
// more items than it expects must use the ones it understands rather than
// reject the message ("Full Control Command Set" §Data Transfer Params). Decode
// implements that rule literally, returning the items it decoded alongside the
// error that stopped it.
//
// This package is stdlib-only and imports nothing from dhs (ADR-0006).
package dtp

import (
	"errors"
	"fmt"
)

// Type is the item type byte.
type Type uint8

// Item types. Further types may be added by the vendor, so a decoder must
// treat an unknown one as the end of what it understands, not as corruption.
const (
	TypeUintArray Type = 1
	TypeBitmap    Type = 2
	TypeFalse     Type = 3 // a boolean false; the type byte is the whole item
	TypeTrue      Type = 4 // a boolean true; likewise
	TypeUint      Type = 5
	TypeString    Type = 6
)

func (t Type) String() string {
	switch t {
	case TypeUintArray:
		return "uint-array"
	case TypeBitmap:
		return "bitmap"
	case TypeFalse:
		return "false"
	case TypeTrue:
		return "true"
	case TypeUint:
		return "uint"
	case TypeString:
		return "string"
	default:
		return fmt.Sprintf("type(%d)", uint8(t))
	}
}

// Limits of the wire format.
const (
	// MaxItems is the largest number of items a block can hold. The count
	// occupies only bits 0..6 of the first byte; bit 7 marks a string-safe
	// block.
	MaxItems = 127

	// MaxStringLen is the largest string an item can hold: its length is a
	// single byte.
	MaxStringLen = 255

	// maxVarUintBytes bounds a variable-length integer at the five bytes a
	// 32-bit value needs, so a malformed block cannot make the decoder run
	// off the end of a payload.
	maxVarUintBytes = 5
)

// stringSafeFlag marks a block whose interior bytes have been escaped so the
// block can travel in a string parameter.
const stringSafeFlag = 0x80

// Sentinel errors.
var (
	// ErrShortBuffer means the block ended in the middle of an item.
	ErrShortBuffer = errors.New("dtp: short buffer")

	// ErrUnknownItemType means an item carried a type this build does not
	// know. The item cannot be skipped, because its length is defined by its
	// type, so decoding stops there. Whatever came before is still valid and
	// is returned: that is the format's forward-compatibility rule.
	ErrUnknownItemType = errors.New("dtp: unknown item type")

	// ErrTooManyItems means a block would exceed MaxItems.
	ErrTooManyItems = errors.New("dtp: too many items")

	// ErrStringTooLong means a string item would exceed MaxStringLen.
	ErrStringTooLong = errors.New("dtp: string too long")

	// ErrVarUintOverflow means a variable-length integer did not terminate
	// within the bytes a 32-bit value can occupy.
	ErrVarUintOverflow = errors.New("dtp: variable-length integer overflows 32 bits")

	// ErrBadEscape means a string-safe block ended on an escape byte, or
	// carried an escape sequence that is not defined.
	ErrBadEscape = errors.New("dtp: malformed string-safe escape")
)

// Item is one parameter. Which field carries the value is decided by Type; the
// others are zero.
type Item struct {
	Type Type

	Uint   uint32   // TypeUint
	Uints  []uint32 // TypeUintArray
	Bitmap Bitmap   // TypeBitmap
	Str    string   // TypeString
}

// Bool reports the value of a boolean item, and whether the item was one.
func (i Item) Bool() (value, ok bool) {
	switch i.Type {
	case TypeTrue:
		return true, true
	case TypeFalse:
		return false, true
	default:
		return false, false
	}
}

func (i Item) String() string {
	switch i.Type {
	case TypeUint:
		return fmt.Sprintf("uint(%d)", i.Uint)
	case TypeUintArray:
		return fmt.Sprintf("uints%v", i.Uints)
	case TypeBitmap:
		return i.Bitmap.String()
	case TypeFalse:
		return "false"
	case TypeTrue:
		return "true"
	case TypeString:
		return fmt.Sprintf("%q", i.Str)
	default:
		return i.Type.String()
	}
}

// Constructors, so a caller never has to set Type and a value field
// separately and cannot get them out of step.

// Uint returns a uint item.
func Uint(v uint32) Item { return Item{Type: TypeUint, Uint: v} }

// Uints returns a uint-array item.
func Uints(v ...uint32) Item { return Item{Type: TypeUintArray, Uints: v} }

// Bool returns a boolean item. The value lives in the type byte, so a boolean
// costs one byte on the wire.
func Bool(v bool) Item {
	if v {
		return Item{Type: TypeTrue}
	}
	return Item{Type: TypeFalse}
}

// String returns a string item.
func String(s string) Item { return Item{Type: TypeString, Str: s} }

// Bits returns a bitmap item.
func Bits(b Bitmap) Item { return Item{Type: TypeBitmap, Bitmap: b} }

// Params is a decoded block.
type Params []Item

func (p Params) String() string {
	if len(p) == 0 {
		return "[]"
	}
	s := "["
	for i, it := range p {
		if i > 0 {
			s += " "
		}
		s += it.String()
	}
	return s + "]"
}

// Append appends the wire form of p to dst.
//
// When stringSafe is set, the block's interior bytes are escaped so it can be
// carried in a string parameter, and the count byte is marked. See
// AppendStringValue for the wrapper that goes with it.
func Append(dst []byte, p Params, stringSafe bool) ([]byte, error) {
	if len(p) > MaxItems {
		return nil, fmt.Errorf("%w: %d items, limit %d", ErrTooManyItems, len(p), MaxItems)
	}

	count := byte(len(p))
	if stringSafe {
		count |= stringSafeFlag
	}

	body := make([]byte, 0, 16)
	for i, it := range p {
		var err error
		if body, err = appendItem(body, it); err != nil {
			return nil, fmt.Errorf("item %d: %w", i, err)
		}
	}

	dst = append(dst, count)
	if !stringSafe {
		return append(dst, body...), nil
	}
	return appendEscaped(dst, body), nil
}

// Encode returns the wire form of p.
func Encode(p Params, stringSafe bool) ([]byte, error) {
	return Append(nil, p, stringSafe)
}

func appendItem(dst []byte, it Item) ([]byte, error) {
	switch it.Type {
	case TypeFalse, TypeTrue:
		return append(dst, byte(it.Type)), nil

	case TypeUint:
		dst = append(dst, byte(it.Type))
		return appendVarUint(dst, it.Uint), nil

	case TypeUintArray:
		dst = append(dst, byte(it.Type))
		dst = appendVarUint(dst, uint32(len(it.Uints)))
		for _, v := range it.Uints {
			dst = appendVarUint(dst, v)
		}
		return dst, nil

	case TypeBitmap:
		dst = append(dst, byte(it.Type))
		dst = appendVarUint(dst, uint32(it.Bitmap.NumBits))
		return append(dst, it.Bitmap.bytes()...), nil

	case TypeString:
		if len(it.Str) > MaxStringLen {
			return nil, fmt.Errorf("%w: %d bytes, limit %d",
				ErrStringTooLong, len(it.Str), MaxStringLen)
		}
		dst = append(dst, byte(it.Type), byte(len(it.Str)))
		return append(dst, it.Str...), nil

	default:
		return nil, fmt.Errorf("%w: %d", ErrUnknownItemType, uint8(it.Type))
	}
}

// Decode reads a block.
//
// On an unknown item type it returns the items decoded so far together with
// ErrUnknownItemType, because the format requires a receiver to work with the
// parameters it understands rather than reject the whole message. A caller that
// wants only complete blocks checks the error; one following the extension rule
// uses the items and ignores it.
func Decode(b []byte) (Params, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("%w: empty block", ErrShortBuffer)
	}

	count := int(b[0] &^ stringSafeFlag)
	body := b[1:]

	if b[0]&stringSafeFlag != 0 {
		var err error
		if body, err = unescape(body); err != nil {
			return nil, err
		}
	}

	out := make(Params, 0, count)
	off := 0
	for i := 0; i < count; i++ {
		it, n, err := decodeItem(body[off:])
		if err != nil {
			if errors.Is(err, ErrUnknownItemType) {
				return out, fmt.Errorf("item %d: %w", i, err)
			}
			return nil, fmt.Errorf("item %d: %w", i, err)
		}
		out = append(out, it)
		off += n
	}
	return out, nil
}

// IsStringSafe reports whether a block is string-safe encoded.
func IsStringSafe(b []byte) bool {
	return len(b) > 0 && b[0]&stringSafeFlag != 0
}

func decodeItem(b []byte) (Item, int, error) {
	if len(b) == 0 {
		return Item{}, 0, fmt.Errorf("%w: no type byte", ErrShortBuffer)
	}
	typ := Type(b[0])
	off := 1

	switch typ {
	case TypeFalse, TypeTrue:
		return Item{Type: typ}, off, nil

	case TypeUint:
		v, n, err := decodeVarUint(b[off:])
		if err != nil {
			return Item{}, 0, err
		}
		return Item{Type: typ, Uint: v}, off + n, nil

	case TypeUintArray:
		length, n, err := decodeVarUint(b[off:])
		if err != nil {
			return Item{}, 0, err
		}
		off += n
		// A length is bounded by what is left: each element is at least one
		// byte, so a longer array cannot be present and claiming one is a
		// malformed block rather than a reason to allocate.
		if int(length) > len(b)-off {
			return Item{}, 0, fmt.Errorf("%w: array of %d in %d bytes",
				ErrShortBuffer, length, len(b)-off)
		}
		vals := make([]uint32, 0, length)
		for range length {
			v, n, err := decodeVarUint(b[off:])
			if err != nil {
				return Item{}, 0, err
			}
			vals = append(vals, v)
			off += n
		}
		return Item{Type: typ, Uints: vals}, off, nil

	case TypeBitmap:
		bits, n, err := decodeVarUint(b[off:])
		if err != nil {
			return Item{}, 0, err
		}
		off += n
		nbytes := int((bits + 7) / 8)
		if len(b)-off < nbytes {
			return Item{}, 0, fmt.Errorf("%w: bitmap of %d bits needs %d bytes, %d left",
				ErrShortBuffer, bits, nbytes, len(b)-off)
		}
		bm := NewBitmap(int(bits))
		copy(bm.Bits, b[off:off+nbytes])
		return Item{Type: typ, Bitmap: bm}, off + nbytes, nil

	case TypeString:
		if len(b)-off < 1 {
			return Item{}, 0, fmt.Errorf("%w: no string length", ErrShortBuffer)
		}
		n := int(b[off])
		off++
		if len(b)-off < n {
			return Item{}, 0, fmt.Errorf("%w: string of %d in %d bytes",
				ErrShortBuffer, n, len(b)-off)
		}
		return Item{Type: typ, Str: string(b[off : off+n])}, off + n, nil

	default:
		return Item{}, 0, fmt.Errorf("%w: %d", ErrUnknownItemType, uint8(typ))
	}
}

// appendVarUint writes v seven bits at a time, least significant first, with
// bit 7 set on every byte but the last. Values below 128 take one byte.
func appendVarUint(dst []byte, v uint32) []byte {
	for v >= 0x80 {
		dst = append(dst, byte(v)|0x80)
		v >>= 7
	}
	return append(dst, byte(v))
}

// decodeVarUint reads one variable-length integer and reports its size.
func decodeVarUint(b []byte) (uint32, int, error) {
	var v uint32
	for i := 0; i < len(b); i++ {
		if i == maxVarUintBytes {
			return 0, 0, fmt.Errorf("%w: no terminator within %d bytes",
				ErrVarUintOverflow, maxVarUintBytes)
		}
		c := b[i]
		part := uint32(c & 0x7F)
		if i == maxVarUintBytes-1 && part > 0xF {
			return 0, 0, fmt.Errorf("%w: byte %d contributes bits above 32", ErrVarUintOverflow, i)
		}
		v |= part << (7 * uint(i))
		if c&0x80 == 0 {
			return v, i + 1, nil
		}
	}
	return 0, 0, fmt.Errorf("%w: value continues past the end of the block", ErrShortBuffer)
}

// VarUintSize returns how many bytes v occupies. Callers sizing a block before
// building it use this rather than encoding twice.
func VarUintSize(v uint32) int {
	n := 1
	for v >= 0x80 {
		v >>= 7
		n++
	}
	return n
}
