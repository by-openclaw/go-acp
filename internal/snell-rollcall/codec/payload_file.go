package codec

import (
	"encoding/binary"
	"fmt"
)

// Wire sizes of the file service structures (RC3FILE.H).
const (
	// FileSize is FILE_STR, the structure every file operation carries.
	FileSize = 10

	// FileInfoSize is FILEINFOHDR_STR, one directory entry's metadata.
	FileInfoSize = 10

	// FileNameSize is FILENAMESIZE: the fixed filename field in a directory
	// entry. It is a DOS 8.3 name with its terminator.
	FileNameSize = 13

	// DirEntrySize is MODULE_FILEINFO_STR as it appears on the wire.
	//
	// The header plus the filename is 23 bytes of content, but the structure
	// is compiled under #pragma pack(2) and so is padded to an even size, and
	// senders use sizeof. The pad byte's content is undefined, exactly like
	// the one in GetNext, and must never be reported as a deviation.
	DirEntrySize = 24

	// MaxFileName is the longest path the file service accepts, from
	// RC3FILE.H MAX_FILENAME. Paths travel as NUL-terminated strings in the
	// payload rather than in the fixed field, which is only used by directory
	// listings.
	MaxFileName = 131
)

// File open flags (RC3FILE.H FileOpenFlags). They mirror the C runtime's
// open(2) flags, because that is what the vendor implementation passes them to.
const (
	OpenReadOnly  uint16 = 0x0000
	OpenWriteOnly uint16 = 0x0001
	OpenReadWrite uint16 = 0x0002
	OpenAppend    uint16 = 0x0008
	OpenCreate    uint16 = 0x0100
	OpenTruncate  uint16 = 0x0200
	OpenExclusive uint16 = 0x0400
	OpenText      uint16 = 0x4000

	// OpenBinary must be set for anything that is not line-oriented text.
	// Without it the peer translates line endings, which corrupts a names
	// file or a template archive silently.
	OpenBinary uint16 = 0x8000
)

// File attribute flags (RC3FILE.H AttributeFlags).
const (
	AttrNormal   uint16 = 0x0000
	AttrReadOnly uint16 = 0x0001
	AttrHidden   uint16 = 0x0002
	AttrSystem   uint16 = 0x0004
	AttrVolumeID uint16 = 0x0008
	AttrSubdir   uint16 = 0x0010
	AttrArchive  uint16 = 0x0020

	// AttrDOSFileTime says the time field is in MS-DOS date and time format
	// rather than seconds. It is a flag about the encoding of another field,
	// which is why it sits oddly high among the attribute bits.
	AttrDOSFileTime uint16 = 0x1000
	AttrRecord      uint16 = 0x2000
	AttrWriteOnly   uint16 = 0x4000
)

// File service error values (RC3FILE.H ErrorValues). They are C errno numbers,
// carried in the Extra field of a reply.
const (
	FileErrNoEntry     int16 = 2   // no such file or directory
	FileErrAccess      int16 = 13  // mode incompatible with the request
	FileErrExists      int16 = 17  // create-exclusive on a file that is there
	FileErrInvalid     int16 = 22  // an invalid flag combination
	FileErrTooManyOpen int16 = 24  // no file handles left
	FileErrNoSpace     int16 = 28  // no space
	FileErrBadType     int16 = 129 // bad runtime file type
)

// FileErrorName renders a file service error value.
func FileErrorName(v int16) string {
	switch v {
	case 0:
		return "ok"
	case FileErrNoEntry:
		return "no such file or directory"
	case FileErrAccess:
		return "access denied"
	case FileErrExists:
		return "already exists"
	case FileErrInvalid:
		return "invalid argument"
	case FileErrTooManyOpen:
		return "no file handles left"
	case FileErrNoSpace:
		return "no space"
	case FileErrBadType:
		return "bad file type"
	default:
		return fmt.Sprintf("error(%d)", v)
	}
}

// File is FILE_STR, the structure every file operation carries.
//
// The two handles are what make the service work across a network where both
// ends allocate independently: SrcHandle is the requester's own reference,
// echoed back untouched so a reply can be matched, and FileHandle is the
// server's, valid only on that server.
//
// Offset and Extra are overloaded by operation, and they do not merely differ
// between operations: they change meaning between a request and its own reply.
// The vendor server says so in as many words (FileServer.c, HandleSpFILEREAD:
// "NOTE that meaning of rOffset and rExtra differs between SP_FILEREAD and
// SP_RETFILEREAD"), and getting it wrong reads a file as a stream of empty
// blocks rather than failing.
//
//	message         Offset                  Extra
//	FileOpen        unused                  the open flags
//	RetFileOpen     the peer's block size   the error
//	FileRead        where to read from      how many bytes to read
//	RetFileRead     how many were read      the error
//	FileWrite       where to write to       how many bytes to write
//	FileRet         how many were written   the error
//
// The block size a peer returns from an open is its own maximum, and it caps
// what a read may ask for: the server clamps a larger request rather than
// refusing it, so a client that ignores the figure simply wastes the excess.
type File struct {
	SrcHandle  int16
	FileHandle int16
	Offset     int32
	Extra      int16
}

func (f File) String() string {
	return fmt.Sprintf("src=%d file=%d off=%d extra=%d",
		f.SrcHandle, f.FileHandle, f.Offset, f.Extra)
}

// Err returns the error a reply carries, or nil. A negative or zero Extra is
// not an error: only the documented errno values are.
func (f File) Err() error {
	if f.Extra <= 0 {
		return nil
	}
	return fmt.Errorf("rollcall file: %s", FileErrorName(f.Extra))
}

// AppendTo appends the 10-byte wire form.
func (f File) AppendTo(dst []byte) []byte {
	var b [FileSize]byte
	binary.BigEndian.PutUint16(b[0:2], uint16(f.SrcHandle))
	binary.BigEndian.PutUint16(b[2:4], uint16(f.FileHandle))
	binary.BigEndian.PutUint32(b[4:8], uint32(f.Offset))
	binary.BigEndian.PutUint16(b[8:10], uint16(f.Extra))
	return append(dst, b[:]...)
}

// DecodeFile reads a FILE_STR from the front of b.
func DecodeFile(b []byte) (File, error) {
	if err := need(b, FileSize, "File", ""); err != nil {
		return File{}, err
	}
	return File{
		SrcHandle:  int16(binary.BigEndian.Uint16(b[0:2])),
		FileHandle: int16(binary.BigEndian.Uint16(b[2:4])),
		Offset:     int32(binary.BigEndian.Uint32(b[4:8])),
		Extra:      int16(binary.BigEndian.Uint16(b[8:10])),
	}, nil
}

// AppendFileOpen builds the payload of a file open: the structure with the
// flags in Extra, then the path as a NUL-terminated string.
//
// The flags go in Extra, not Offset. The vendor server reads them from there
// (FileServer.c, HandleSpFILEOPEN: "OpenModeFlags = FileSpecIn->rExtra") and
// overwrites both fields in its reply with the block size and the error.
//
// Set OpenBinary for anything that is not line-oriented text. Without it the
// peer translates line endings, which corrupts a names file or a template
// archive without reporting anything.
func AppendFileOpen(dst []byte, srcHandle int16, flags uint16, path string) ([]byte, error) {
	if len(path) > MaxFileName-1 {
		return nil, fmt.Errorf("%w: path %d bytes, limit %d",
			ErrStringTooLong, len(path), MaxFileName-1)
	}
	dst = File{SrcHandle: srcHandle, Extra: int16(flags)}.AppendTo(dst)
	dst = append(dst, path...)
	return append(dst, 0), nil
}

// OpenFlags reads the open flags from a file-open request.
//
// They are a 16-bit set carried in a signed field, so the binary flag arrives
// as a negative number. Reading it as signed would make the most important
// flag in the service look like a malformed request.
func (f File) OpenFlags() uint16 { return uint16(f.Extra) }

// BlockSize reads the maximum read size from an open reply. Zero means the
// peer did not say, and a caller should use its own default.
func (f File) BlockSize() int {
	if f.Offset <= 0 {
		return 0
	}
	return int(f.Offset)
}

// FileReadRequest builds a read: where to start, and how many bytes.
func FileReadRequest(srcHandle, fileHandle int16, offset int32, count int) File {
	if count > MaxPayload {
		count = MaxPayload
	}
	return File{
		SrcHandle:  srcHandle,
		FileHandle: fileHandle,
		Offset:     offset,
		Extra:      int16(count),
	}
}

// AppendFileWrite builds a write: where to put the data, how much there is,
// and then the data.
func AppendFileWrite(dst []byte, srcHandle, fileHandle int16, offset int32, data []byte) ([]byte, error) {
	if len(data) > MaxPayload-FileSize {
		return nil, fmt.Errorf("%w: %d bytes of file data in one write",
			ErrPayloadTooLong, len(data))
	}
	dst = File{
		SrcHandle:  srcHandle,
		FileHandle: fileHandle,
		Offset:     offset,
		Extra:      int16(len(data)),
	}.AppendTo(dst)
	return append(dst, data...), nil
}

// DecodeFileOpen reads a file open request: the structure and the path.
func DecodeFileOpen(b []byte) (File, string, error) {
	f, err := DecodeFile(b)
	if err != nil {
		return File{}, "", err
	}
	path, _ := CString(b[FileSize:])
	return f, path, nil
}

// AppendFilePath builds the payload of an operation that names a path rather
// than a handle: directory listing, delete, and make directory.
func AppendFilePath(dst []byte, srcHandle int16, path string) ([]byte, error) {
	if len(path) > MaxFileName-1 {
		return nil, fmt.Errorf("%w: path %d bytes, limit %d",
			ErrStringTooLong, len(path), MaxFileName-1)
	}
	dst = File{SrcHandle: srcHandle}.AppendTo(dst)
	dst = append(dst, path...)
	return append(dst, 0), nil
}

// AppendFileRename builds a rename: the structure, then the old and new paths
// as two NUL-terminated strings.
func AppendFileRename(dst []byte, srcHandle int16, from, to string) ([]byte, error) {
	if len(from)+len(to) > MaxFileName-2 {
		return nil, fmt.Errorf("%w: rename paths total %d bytes, limit %d",
			ErrStringTooLong, len(from)+len(to), MaxFileName-2)
	}
	dst = File{SrcHandle: srcHandle}.AppendTo(dst)
	dst = append(dst, from...)
	dst = append(dst, 0)
	dst = append(dst, to...)
	return append(dst, 0), nil
}

// DecodeFileRename reads a rename request.
func DecodeFileRename(b []byte) (f File, from, to string, err error) {
	if f, err = DecodeFile(b); err != nil {
		return File{}, "", "", err
	}
	rest := b[FileSize:]
	from, n := CString(rest)
	if n < len(rest) {
		to, _ = CString(rest[n:])
	}
	return f, from, to, nil
}

// FileInfo is FILEINFOHDR_STR: what a directory listing says about one entry.
type FileInfo struct {
	// Time is the modification time. Its encoding depends on the DOS
	// file-time attribute: with it, MS-DOS packed date and time; without it,
	// seconds. Devices differ, so a reader must check the flag rather than
	// assume.
	Time   int32
	Attrib uint16
	Length int32
}

// IsDir reports whether the entry is a directory.
func (f FileInfo) IsDir() bool { return f.Attrib&AttrSubdir != 0 }

// ReadOnly reports whether the entry may not be written.
func (f FileInfo) ReadOnly() bool { return f.Attrib&AttrReadOnly != 0 }

// DOSTime reports whether Time is in MS-DOS packed format rather than seconds.
func (f FileInfo) DOSTime() bool { return f.Attrib&AttrDOSFileTime != 0 }

// AppendTo appends the 10-byte wire form.
func (f FileInfo) AppendTo(dst []byte) []byte {
	var b [FileInfoSize]byte
	binary.BigEndian.PutUint32(b[0:4], uint32(f.Time))
	binary.BigEndian.PutUint16(b[4:6], f.Attrib)
	binary.BigEndian.PutUint32(b[6:10], uint32(f.Length))
	return append(dst, b[:]...)
}

// DecodeFileInfo reads a FILEINFOHDR_STR from the front of b.
func DecodeFileInfo(b []byte) (FileInfo, error) {
	if err := need(b, FileInfoSize, "FileInfo", ""); err != nil {
		return FileInfo{}, err
	}
	return fileInfoAt(b), nil
}

// fileInfoAt reads a FILEINFOHDR_STR from a slice the caller has already
// sized. A directory entry embeds one and validates the whole entry first.
func fileInfoAt(b []byte) FileInfo {
	return FileInfo{
		Time:   int32(binary.BigEndian.Uint32(b[0:4])),
		Attrib: binary.BigEndian.Uint16(b[4:6]),
		Length: int32(binary.BigEndian.Uint32(b[6:10])),
	}
}

// DirEntry is MODULE_FILEINFO_STR: one line of a directory listing, delivered
// as one item of a multi-packet transfer.
type DirEntry struct {
	Info FileInfo
	Name string
}

func (d DirEntry) String() string {
	kind := "file"
	if d.Info.IsDir() {
		kind = "dir"
	}
	return fmt.Sprintf("%s %q %d bytes", kind, d.Name, d.Info.Length)
}

// AppendTo appends the 24-byte wire form: the header, the fixed-width name,
// and the pad byte the structure's packing adds.
func (d DirEntry) AppendTo(dst []byte) ([]byte, error) {
	dst = d.Info.AppendTo(dst)
	dst, err := appendFixedString(dst, d.Name, FileNameSize)
	if err != nil {
		return nil, err
	}
	// The pad is zeroed rather than left as whatever was in the buffer, so
	// our own frames stay reproducible.
	return append(dst, 0), nil
}

// DecodeDirEntry reads a directory entry.
//
// The name is read as whatever follows the header rather than as a fixed
// thirteen-byte field, because that is what servers send. The structure
// declares the field, but the vendor's own controller sends the header
// followed by the name and its terminator and nothing more: an entry for "."
// arrives in twelve bytes, one for "TEMPLATE.ZIP" in twenty-three. Demanding
// the declared width makes every short entry unreadable, which is most of them.
//
// A name that fills its field without a terminator is accepted too, which is
// what a server that does send the full width produces for a thirteen-character
// name.
func DecodeDirEntry(b []byte) (DirEntry, error) {
	if err := need(b, FileInfoSize+1, "DirEntry", ""); err != nil {
		return DirEntry{}, err
	}
	name, _ := CString(b[FileInfoSize:])
	return DirEntry{
		Info: fileInfoAt(b),
		Name: name,
	}, nil
}
