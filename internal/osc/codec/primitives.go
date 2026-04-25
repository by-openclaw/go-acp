package codec

import (
	"encoding/binary"
	"fmt"
	"math"
)

// padTo4 returns the number of NUL pad bytes required to bring n up to a
// 4-byte boundary. OSC spec §OSC-string and §OSC-blob both require this.
func padTo4(n int) int {
	rem := n % 4
	if rem == 0 {
		return 0
	}
	return 4 - rem
}

// encodeString appends an OSC-string (NUL-terminated + NUL-padded to a
// 4-byte boundary) to dst and returns the new slice.
func encodeString(dst []byte, s string) []byte {
	dst = append(dst, s...)
	dst = append(dst, 0x00)
	for i := 0; i < padTo4(len(s)+1); i++ {
		dst = append(dst, 0x00)
	}
	return dst
}

// decodeString extracts an OSC-string from b starting at offset off.
// Returns the string (without the terminating NUL), bytes consumed
// (including alignment pad), and an error.
func decodeString(b []byte, off int) (string, int, error) {
	if off >= len(b) {
		return "", 0, fmt.Errorf("%w: string at offset %d", ErrTruncated, off)
	}
	// Find the first NUL.
	end := -1
	for i := off; i < len(b); i++ {
		if b[i] == 0x00 {
			end = i
			break
		}
	}
	if end < 0 {
		return "", 0, fmt.Errorf("%w: scanning from offset %d", ErrStringNotTerminated, off)
	}
	s := string(b[off:end])
	// Bytes consumed = s + NUL + pad-to-4.
	consumed := (end - off) + 1 + padTo4(end-off+1)
	if off+consumed > len(b) {
		return "", 0, fmt.Errorf("%w: string pad at offset %d", ErrTruncated, off)
	}
	return s, consumed, nil
}

// encodeBlob appends an OSC-blob (int32 size + bytes + pad) to dst.
func encodeBlob(dst []byte, data []byte) []byte {
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(data)))
	dst = append(dst, size[:]...)
	dst = append(dst, data...)
	for i := 0; i < padTo4(len(data)); i++ {
		dst = append(dst, 0x00)
	}
	return dst
}

// decodeBlob extracts an OSC-blob from b starting at offset off.
func decodeBlob(b []byte, off int) ([]byte, int, error) {
	if off+4 > len(b) {
		return nil, 0, fmt.Errorf("%w: blob size at offset %d", ErrTruncated, off)
	}
	size := int32(binary.BigEndian.Uint32(b[off:]))
	if size < 0 {
		return nil, 0, fmt.Errorf("%w: size=%d", ErrBlobTooLarge, size)
	}
	start := off + 4
	end := start + int(size)
	if end > len(b) {
		return nil, 0, fmt.Errorf("%w: blob body at offset %d size %d", ErrTruncated, off, size)
	}
	data := make([]byte, size)
	copy(data, b[start:end])
	consumed := 4 + int(size) + padTo4(int(size))
	if off+consumed > len(b) {
		return nil, 0, fmt.Errorf("%w: blob pad at offset %d", ErrTruncated, off)
	}
	return data, consumed, nil
}

// Helpers for fixed-width big-endian primitives used by the arg codec.
func encodeInt32(dst []byte, v int32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(v))
	return append(dst, b[:]...)
}
func decodeInt32(b []byte, off int) (int32, error) {
	if off+4 > len(b) {
		return 0, fmt.Errorf("%w: int32 at offset %d", ErrTruncated, off)
	}
	return int32(binary.BigEndian.Uint32(b[off:])), nil
}

func encodeInt64(dst []byte, v int64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(v))
	return append(dst, b[:]...)
}
func decodeInt64(b []byte, off int) (int64, error) {
	if off+8 > len(b) {
		return 0, fmt.Errorf("%w: int64 at offset %d", ErrTruncated, off)
	}
	return int64(binary.BigEndian.Uint64(b[off:])), nil
}

func encodeFloat32(dst []byte, v float32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], math.Float32bits(v))
	return append(dst, b[:]...)
}
func decodeFloat32(b []byte, off int) (float32, error) {
	if off+4 > len(b) {
		return 0, fmt.Errorf("%w: float32 at offset %d", ErrTruncated, off)
	}
	return math.Float32frombits(binary.BigEndian.Uint32(b[off:])), nil
}

func encodeFloat64(dst []byte, v float64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], math.Float64bits(v))
	return append(dst, b[:]...)
}
func decodeFloat64(b []byte, off int) (float64, error) {
	if off+8 > len(b) {
		return 0, fmt.Errorf("%w: float64 at offset %d", ErrTruncated, off)
	}
	return math.Float64frombits(binary.BigEndian.Uint64(b[off:])), nil
}

// encodeUint64 / decodeUint64 — used for OSC Timetag (u64 NTP format).
func encodeUint64(dst []byte, v uint64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	return append(dst, b[:]...)
}
func decodeUint64(b []byte, off int) (uint64, error) {
	if off+8 > len(b) {
		return 0, fmt.Errorf("%w: uint64 at offset %d", ErrTruncated, off)
	}
	return binary.BigEndian.Uint64(b[off:]), nil
}
