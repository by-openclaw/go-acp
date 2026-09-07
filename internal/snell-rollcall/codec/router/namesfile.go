package router

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
)

// Name field widths. A controller publishes the same set of names at two
// widths, plus an alternate set carried at the wider one.
const (
	NameWidth8  = 8
	NameWidth32 = 32
)

// Sentinel errors.
var (
	// ErrShortFile means a names or mappings file was smaller than the
	// entity counts require.
	ErrShortFile = errors.New("router: file shorter than its declared contents")

	// ErrBadWidth means a name width other than 8 or 32 was asked for.
	ErrBadWidth = errors.New("router: name width must be 8 or 32")
)

// NamesFile is a decoded collated names file: every source name followed by
// every destination name, each in a fixed-width zero-padded field.
//
// There is no header. A client knows where the destination names begin because
// it already read how many sources there are, which is why decoding needs the
// counts passed in.
type NamesFile struct {
	Width int
	Srcs  []string
	Dsts  []string
}

// DecodeNamesFile reads a collated names file holding numSrcs source names
// followed by numDsts destination names, each width bytes wide.
//
// A file longer than the counts require is accepted and the surplus ignored: a
// controller may pad, and refusing would make a whole matrix unnameable over a
// trailing byte.
func DecodeNamesFile(b []byte, width, numSrcs, numDsts int) (NamesFile, error) {
	if width != NameWidth8 && width != NameWidth32 {
		return NamesFile{}, fmt.Errorf("%w: %d", ErrBadWidth, width)
	}
	if numSrcs < 0 || numDsts < 0 {
		return NamesFile{}, fmt.Errorf("%w: negative counts", ErrShortFile)
	}

	want := (numSrcs + numDsts) * width
	if len(b) < want {
		return NamesFile{}, fmt.Errorf("%w: need %d bytes for %d+%d names of %d, have %d",
			ErrShortFile, want, numSrcs, numDsts, width, len(b))
	}

	out := NamesFile{Width: width, Srcs: make([]string, numSrcs), Dsts: make([]string, numDsts)}
	off := 0
	for i := range numSrcs {
		out.Srcs[i] = trimName(b[off : off+width])
		off += width
	}
	for i := range numDsts {
		out.Dsts[i] = trimName(b[off : off+width])
		off += width
	}
	return out, nil
}

// AppendTo appends the file's wire form. A name too long for the field is
// truncated, which is what the controller itself does when it writes the file.
func (f NamesFile) AppendTo(dst []byte) ([]byte, error) {
	if f.Width != NameWidth8 && f.Width != NameWidth32 {
		return nil, fmt.Errorf("%w: %d", ErrBadWidth, f.Width)
	}
	for _, n := range f.Srcs {
		dst = appendName(dst, n, f.Width)
	}
	for _, n := range f.Dsts {
		dst = appendName(dst, n, f.Width)
	}
	return dst, nil
}

// CRC returns the checksum a controller publishes alongside the filename, so a
// client can tell whether its cached copy is still current.
//
// It is not a checksum over the file. Each name is hashed on its own, padded to
// its field width and followed by its zero-based index in the file as a 32-bit
// little-endian value, and those hashes are added together with 32-bit wrapping
// ("Full Control Command Set" §CMD_ASSOC_NAMES_8_FILENAME).
//
// Two properties follow, and both are deliberate. Including the index
// distinguishes the same name appearing in two positions. Summing rather than
// chaining means a controller can update the checksum when one name changes
// without re-reading the file.
func (f NamesFile) CRC() uint32 {
	var sum uint32
	idx := uint32(0)
	for _, n := range f.Srcs {
		sum += NameCRC(n, f.Width, idx)
		idx++
	}
	for _, n := range f.Dsts {
		sum += NameCRC(n, f.Width, idx)
		idx++
	}
	return sum
}

// NameCRC returns the contribution one name makes to a names-file checksum.
//
// The hash is the standard IEEE CRC-32, the one with polynomial 0x04C11DB7
// that the specification names, over the padded name followed by the index.
func NameCRC(name string, width int, index uint32) uint32 {
	buf := make([]byte, width+4)
	copy(buf, truncateName(name, width))
	binary.LittleEndian.PutUint32(buf[width:], index)
	return crc32.ChecksumIEEE(buf)
}

// MappingsFile is a decoded association mappings file.
//
// An association groups one entry per level, so that routing by association
// routes every level at once. The file holds the source associations first and
// then the destination ones, each a run of numLevels 32-bit little-endian
// entity numbers, one-based, with zero meaning the association does not reach
// that level.
type MappingsFile struct {
	Levels int
	Srcs   [][]uint32
	Dsts   [][]uint32
}

// DecodeMappingsFile reads an association mappings file.
func DecodeMappingsFile(b []byte, numLevels, numSrcAssocs, numDstAssocs int) (MappingsFile, error) {
	if numLevels < 0 || numSrcAssocs < 0 || numDstAssocs < 0 {
		return MappingsFile{}, fmt.Errorf("%w: negative counts", ErrShortFile)
	}

	want := (numSrcAssocs + numDstAssocs) * numLevels * 4
	if len(b) < want {
		return MappingsFile{}, fmt.Errorf("%w: need %d bytes for %d+%d associations over %d levels, have %d",
			ErrShortFile, want, numSrcAssocs, numDstAssocs, numLevels, len(b))
	}

	out := MappingsFile{Levels: numLevels}
	off := 0
	read := func(n int) [][]uint32 {
		rows := make([][]uint32, n)
		for i := range rows {
			row := make([]uint32, numLevels)
			for v := range row {
				row[v] = binary.LittleEndian.Uint32(b[off : off+4])
				off += 4
			}
			rows[i] = row
		}
		return rows
	}
	out.Srcs = read(numSrcAssocs)
	out.Dsts = read(numDstAssocs)
	return out, nil
}

// AppendTo appends the file's wire form.
func (m MappingsFile) AppendTo(dst []byte) []byte {
	write := func(rows [][]uint32) {
		for _, row := range rows {
			for v := range m.Levels {
				var n uint32
				if v < len(row) {
					n = row[v]
				}
				dst = binary.LittleEndian.AppendUint32(dst, n)
			}
		}
	}
	write(m.Srcs)
	write(m.Dsts)
	return dst
}

// CRC returns the checksum for a mappings file: the sum of the CRC-32 of each
// association's own run of entries. Unlike the names file there is no index
// mixed in, because an association's position is already implied by its
// contents.
func (m MappingsFile) CRC() uint32 {
	var sum uint32
	add := func(rows [][]uint32) {
		for _, row := range rows {
			buf := make([]byte, 0, m.Levels*4)
			for v := range m.Levels {
				var n uint32
				if v < len(row) {
					n = row[v]
				}
				buf = binary.LittleEndian.AppendUint32(buf, n)
			}
			sum += crc32.ChecksumIEEE(buf)
		}
	}
	add(m.Srcs)
	add(m.Dsts)
	return sum
}

// MultiChannel is one source's or destination's multi-channel configuration,
// read from the file a level publishes.
//
// Records are variable length, so the file has to be walked from the start:
// there is no index. All multi-byte fields are little-endian.
type MultiChannel struct {
	IsMultiChannel bool
	TrackTemplate  uint16 // one-based, zero for none
	AudioOnly      bool
	Channels       []uint16 // one-based entity per channel, zero for undefined
}

// MaxMultiChannelChannels is the ceiling the specification places on a
// multi-channel record.
const MaxMultiChannelChannels = 64

// DecodeMultiChannel reads one record and reports its size.
func DecodeMultiChannel(b []byte) (MultiChannel, int, error) {
	const fixed = 5 // isMultiChannel, numChannels, trackTemplate(2), isAudioOnly
	if len(b) < fixed {
		return MultiChannel{}, 0, fmt.Errorf("%w: record header needs %d bytes, have %d",
			ErrShortFile, fixed, len(b))
	}

	n := int(b[1])
	if n > MaxMultiChannelChannels {
		return MultiChannel{}, 0, fmt.Errorf("%w: %d channels, limit %d",
			ErrShortFile, n, MaxMultiChannelChannels)
	}
	if len(b) < fixed+2*n {
		return MultiChannel{}, 0, fmt.Errorf("%w: %d channels need %d bytes, have %d",
			ErrShortFile, n, fixed+2*n, len(b))
	}

	mc := MultiChannel{
		IsMultiChannel: b[0] != 0,
		TrackTemplate:  binary.LittleEndian.Uint16(b[2:4]),
		AudioOnly:      b[4] != 0,
		Channels:       make([]uint16, n),
	}
	for i := range n {
		mc.Channels[i] = binary.LittleEndian.Uint16(b[fixed+2*i : fixed+2*i+2])
	}
	return mc, fixed + 2*n, nil
}

// AppendTo appends the record's wire form.
func (m MultiChannel) AppendTo(dst []byte) ([]byte, error) {
	if len(m.Channels) > MaxMultiChannelChannels {
		return nil, fmt.Errorf("%w: %d channels, limit %d",
			ErrShortFile, len(m.Channels), MaxMultiChannelChannels)
	}
	dst = append(dst, boolByte(m.IsMultiChannel), byte(len(m.Channels)))
	dst = binary.LittleEndian.AppendUint16(dst, m.TrackTemplate)
	dst = append(dst, boolByte(m.AudioOnly))
	for _, c := range m.Channels {
		dst = binary.LittleEndian.AppendUint16(dst, c)
	}
	return dst, nil
}

// DecodeMultiChannelFile reads the whole file: one record per source followed
// by one per destination.
//
// The checksum of this file is taken over the file as a whole, not summed per
// record, because the configuration does not change while a system is running
// ("Full Control Command Set" §CMD_SRCDST_MC_DATA_FILENAME). Use
// crc32.ChecksumIEEE on the bytes.
func DecodeMultiChannelFile(b []byte, numSrcs, numDsts int) (srcs, dsts []MultiChannel, err error) {
	off := 0
	read := func(n int, what string) ([]MultiChannel, error) {
		out := make([]MultiChannel, n)
		for i := range n {
			mc, used, err := DecodeMultiChannel(b[off:])
			if err != nil {
				return nil, fmt.Errorf("%s %d: %w", what, i+1, err)
			}
			out[i] = mc
			off += used
		}
		return out, nil
	}
	if srcs, err = read(numSrcs, "source"); err != nil {
		return nil, nil, err
	}
	if dsts, err = read(numDsts, "destination"); err != nil {
		return nil, nil, err
	}
	return srcs, dsts, nil
}

func boolByte(v bool) byte {
	if v {
		return 1
	}
	return 0
}

// trimName reads a zero-padded fixed-width name field.
func trimName(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// truncateName cuts a name to its field width. The controller truncates when
// it writes the file, so a client computing a checksum must truncate the same
// way or it will never match.
func truncateName(s string, width int) string {
	if len(s) > width {
		return s[:width]
	}
	return s
}

// appendName writes a zero-padded fixed-width name field.
func appendName(dst []byte, s string, width int) []byte {
	field := make([]byte, width)
	copy(field, truncateName(s, width))
	return append(dst, field...)
}
