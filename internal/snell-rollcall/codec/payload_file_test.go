package codec

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// TestFile_RoundTrip pins FILE_STR, the structure every file operation
// carries. Field widths are from RC3FILE.H under #pragma pack(2): two 16-bit
// handles, a signed 32-bit offset, and a signed 16-bit extra.
func TestFile_RoundTrip(t *testing.T) {
	in := File{SrcHandle: 0x0102, FileHandle: 0x0304, Offset: 0x05060708, Extra: -1}

	got := in.AppendTo(nil)
	if want := mustHex(t, "0102 0304 05060708 ffff"); !bytes.Equal(got, want) {
		t.Errorf("encoded %x, want %x", got, want)
	}
	if len(got) != FileSize {
		t.Errorf("encoded %d bytes, want %d", len(got), FileSize)
	}

	back, err := DecodeFile(got)
	if err != nil {
		t.Fatalf("DecodeFile: %v", err)
	}
	if back != in {
		t.Errorf("round trip %+v -> %+v", in, back)
	}

	if _, err := DecodeFile(make([]byte, FileSize-1)); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
	if got := in.String(); !strings.Contains(got, "src=258") {
		t.Errorf("String() = %q", got)
	}
}

// TestFile_Err covers the error the Extra field carries on a reply.
//
// Only a positive value is an error. Zero is success, and a negative one is
// the field being used for something else: on a request it carries a byte
// count or the open flags, and the binary flag sets the sign bit.
func TestFile_Err(t *testing.T) {
	if err := (File{}).Err(); err != nil {
		t.Errorf("a zero extra is success, got %v", err)
	}
	if err := (File{Extra: -1}).Err(); err != nil {
		t.Errorf("a negative extra is not an error, got %v", err)
	}

	err := File{Extra: FileErrNoEntry}.Err()
	if err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Errorf("err = %v, want it to name the missing file", err)
	}
}

// TestFileErrorName pins the errno values the file service uses. They are C
// numbers, so a wrong one turns "no space left" into "access denied" in an
// operator's log.
func TestFileErrorName(t *testing.T) {
	tests := []struct {
		code int16
		want string
	}{
		{0, "ok"},
		{2, "no such file or directory"},
		{13, "access denied"},
		{17, "already exists"},
		{22, "invalid argument"},
		{24, "no file handles left"},
		{28, "no space"},
		{129, "bad file type"},
		{99, "error(99)"},
	}
	for _, tc := range tests {
		if got := FileErrorName(tc.code); got != tc.want {
			t.Errorf("FileErrorName(%d) = %q, want %q", tc.code, got, tc.want)
		}
	}
}

// TestFileOpen covers the open request, whose path travels after the structure
// as a NUL-terminated string rather than in a fixed field.
func TestFileOpen(t *testing.T) {
	const path = "TEMPLATE.ZIP"

	got, err := AppendFileOpen(nil, 7, OpenReadOnly|OpenBinary, path)
	if err != nil {
		t.Fatalf("AppendFileOpen: %v", err)
	}
	// The flags sit in the last field, not the offset: the vendor server
	// reads them from there.
	want := mustHex(t, "0007 0000 00000000 8000"+"54454d504c4154452e5a495000")
	if !bytes.Equal(got, want) {
		t.Errorf("encoded\n got %x\nwant %x", got, want)
	}

	f, back, err := DecodeFileOpen(got)
	if err != nil {
		t.Fatalf("DecodeFileOpen: %v", err)
	}
	if back != path {
		t.Errorf("path = %q, want %q", back, path)
	}
	if f.SrcHandle != 7 {
		t.Errorf("source handle = %d, want 7", f.SrcHandle)
	}
	if f.OpenFlags() != OpenReadOnly|OpenBinary {
		t.Errorf("flags = %04X, want %04X", f.OpenFlags(), OpenReadOnly|OpenBinary)
	}

	if _, _, err := DecodeFileOpen(make([]byte, 4)); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
}

// TestFileOpen_BinaryMatters records why the binary flag is not optional. A
// peer that opens in text mode translates line endings, which silently
// corrupts a names file or a template archive.
func TestFileOpen_BinaryMatters(t *testing.T) {
	if OpenBinary != 0x8000 {
		t.Errorf("OpenBinary = %04X, want 8000", OpenBinary)
	}
	if OpenText != 0x4000 {
		t.Errorf("OpenText = %04X, want 4000", OpenText)
	}
	if OpenBinary&OpenText != 0 {
		t.Error("binary and text must be distinct bits")
	}

	// The flags are the C runtime's, because that is what the vendor passes
	// them to.
	tests := map[uint16]string{
		0x0000: "read only", 0x0001: "write only", 0x0002: "read write",
		0x0008: "append", 0x0100: "create", 0x0200: "truncate", 0x0400: "exclusive",
	}
	got := map[uint16]string{
		OpenReadOnly: "read only", OpenWriteOnly: "write only", OpenReadWrite: "read write",
		OpenAppend: "append", OpenCreate: "create", OpenTruncate: "truncate",
		OpenExclusive: "exclusive",
	}
	for v, name := range tests {
		if got[v] != name {
			t.Errorf("flag %04X is %q, want %q", v, got[v], name)
		}
	}
}

func TestFilePath(t *testing.T) {
	got, err := AppendFilePath(nil, 3, "/RC_Files")
	if err != nil {
		t.Fatalf("AppendFilePath: %v", err)
	}
	if want := mustHex(t, "0003 0000 00000000 0000"+"2f52435f46696c657300"); !bytes.Equal(got, want) {
		t.Errorf("encoded\n got %x\nwant %x", got, want)
	}

	_, path, err := DecodeFileOpen(got)
	if err != nil {
		t.Fatalf("DecodeFileOpen: %v", err)
	}
	if path != "/RC_Files" {
		t.Errorf("path = %q", path)
	}
}

func TestFileRename(t *testing.T) {
	got, err := AppendFileRename(nil, 1, "OLD.TXT", "NEW.TXT")
	if err != nil {
		t.Fatalf("AppendFileRename: %v", err)
	}

	f, from, to, err := DecodeFileRename(got)
	if err != nil {
		t.Fatalf("DecodeFileRename: %v", err)
	}
	if f.SrcHandle != 1 || from != "OLD.TXT" || to != "NEW.TXT" {
		t.Errorf("= %+v %q %q", f, from, to)
	}

	// A rename with only one path is malformed but must not panic: the
	// second name simply comes back empty.
	short, err := AppendFilePath(nil, 1, "OLD.TXT")
	if err != nil {
		t.Fatalf("AppendFilePath: %v", err)
	}
	_, from, to, err = DecodeFileRename(short)
	if err != nil {
		t.Fatalf("DecodeFileRename: %v", err)
	}
	if from != "OLD.TXT" || to != "" {
		t.Errorf("= %q %q, want the second name empty", from, to)
	}

	if _, _, _, err := DecodeFileRename(make([]byte, 4)); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
}

func TestFilePaths_TooLong(t *testing.T) {
	long := strings.Repeat("x", MaxFileName)

	if _, err := AppendFileOpen(nil, 1, 0, long); !errors.Is(err, ErrStringTooLong) {
		t.Errorf("open err = %v, want ErrStringTooLong", err)
	}
	if _, err := AppendFilePath(nil, 1, long); !errors.Is(err, ErrStringTooLong) {
		t.Errorf("path err = %v, want ErrStringTooLong", err)
	}
	if _, err := AppendFileRename(nil, 1, long, long); !errors.Is(err, ErrStringTooLong) {
		t.Errorf("rename err = %v, want ErrStringTooLong", err)
	}

	// The longest acceptable path is one short of the limit, leaving room for
	// the terminator.
	ok := strings.Repeat("x", MaxFileName-1)
	if _, err := AppendFileOpen(nil, 1, 0, ok); err != nil {
		t.Errorf("a %d-byte path must fit: %v", len(ok), err)
	}
}

// TestFileInfo covers a directory entry's metadata, including the flag that
// says how to read the time field. A reader that assumes one encoding gets
// dates decades out on the devices that use the other.
func TestFileInfo(t *testing.T) {
	in := FileInfo{Time: 0x11223344, Attrib: AttrReadOnly | AttrArchive, Length: 1024}

	got := in.AppendTo(nil)
	if want := mustHex(t, "11223344 0021 00000400"); !bytes.Equal(got, want) {
		t.Errorf("encoded %x, want %x", got, want)
	}
	if len(got) != FileInfoSize {
		t.Errorf("encoded %d bytes, want %d", len(got), FileInfoSize)
	}

	back, err := DecodeFileInfo(got)
	if err != nil {
		t.Fatalf("DecodeFileInfo: %v", err)
	}
	if back != in {
		t.Errorf("round trip %+v -> %+v", in, back)
	}

	if !back.ReadOnly() {
		t.Error("the read-only attribute was lost")
	}
	if back.IsDir() {
		t.Error("a file is not a directory")
	}
	if back.DOSTime() {
		t.Error("the DOS time flag was not set")
	}

	dir := FileInfo{Attrib: AttrSubdir | AttrDOSFileTime}
	if !dir.IsDir() || !dir.DOSTime() {
		t.Errorf("= %+v, want a directory with a DOS time", dir)
	}

	if _, err := DecodeFileInfo(make([]byte, FileInfoSize-1)); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
}

// TestDirEntry_PaddedToTwentyFour pins the size question this structure
// raises. Its content is a 10-byte header and a 13-byte name, which is 23,
// but under #pragma pack(2) it is padded to an even size and senders use
// sizeof. Twenty-four bytes go on the wire.
func TestDirEntry_PaddedToTwentyFour(t *testing.T) {
	in := DirEntry{
		Info: FileInfo{Attrib: AttrNormal, Length: 4096},
		Name: "TEMPLATE.ZIP",
	}

	got, err := in.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	if len(got) != DirEntrySize {
		t.Fatalf("encoded %d bytes, want %d", len(got), DirEntrySize)
	}
	if DirEntrySize != FileInfoSize+FileNameSize+1 {
		t.Errorf("the size is not the content plus one pad byte")
	}
	// The pad is zeroed rather than left as whatever was in the buffer.
	if got[DirEntrySize-1] != 0 {
		t.Errorf("pad byte = %02X, want it zeroed", got[DirEntrySize-1])
	}

	back, err := DecodeDirEntry(got)
	if err != nil {
		t.Fatalf("DecodeDirEntry: %v", err)
	}
	if back.Name != in.Name || back.Info != in.Info {
		t.Errorf("round trip %+v -> %+v", in, back)
	}
}

// TestDirEntry_AcceptsTheTrimmedForm covers a peer that sends the 23 bytes of
// content without the structure's padding. It is not wrong, and the entry must
// still decode.
func TestDirEntry_AcceptsTheTrimmedForm(t *testing.T) {
	full, err := DirEntry{Name: "A.TXT"}.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}

	trimmed := full[:FileInfoSize+FileNameSize]
	got, err := DecodeDirEntry(trimmed)
	if err != nil {
		t.Fatalf("DecodeDirEntry on the trimmed form: %v", err)
	}
	if got.Name != "A.TXT" {
		t.Errorf("name = %q", got.Name)
	}

	if _, err := DecodeDirEntry(trimmed[:len(trimmed)-1]); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("err = %v, want ErrShortBuffer", err)
	}
}

func TestDirEntry_Errors(t *testing.T) {
	long := DirEntry{Name: strings.Repeat("n", FileNameSize)}
	if _, err := long.AppendTo(nil); !errors.Is(err, ErrStringTooLong) {
		t.Errorf("err = %v, want ErrStringTooLong", err)
	}
	// A DOS 8.3 name with its terminator is the longest that fits.
	ok := DirEntry{Name: "FILENAME.EXT"}
	if _, err := ok.AppendTo(nil); err != nil {
		t.Errorf("a %d-byte name must fit a %d-byte field: %v",
			len(ok.Name), FileNameSize, err)
	}
}

func TestDirEntry_String(t *testing.T) {
	file := DirEntry{Info: FileInfo{Length: 100}, Name: "A.TXT"}
	if got := file.String(); !strings.Contains(got, "file") || !strings.Contains(got, "100") {
		t.Errorf("String() = %q", got)
	}
	dir := DirEntry{Info: FileInfo{Attrib: AttrSubdir}, Name: "SUB"}
	if got := dir.String(); !strings.Contains(got, "dir") {
		t.Errorf("String() = %q", got)
	}
}

// TestFile_FieldsChangeMeaningBetweenRequestAndReply is the correction the
// vendor server forced.
//
// Offset and Extra do not merely differ between operations. They change
// meaning between a request and its own reply, which FileServer.c states
// outright: "NOTE that meaning of rOffset and rExtra differs between
// SP_FILEREAD and SP_RETFILEREAD". A client that reads them the same way in
// both directions asks for zero bytes every time and sees an empty file
// rather than an error.
func TestFile_FieldsChangeMeaningBetweenRequestAndReply(t *testing.T) {
	// A read asks from an offset for a count.
	req := FileReadRequest(1, 9, 4096, 400)
	if req.Offset != 4096 {
		t.Errorf("read offset = %d, want where to read from", req.Offset)
	}
	if req.Extra != 400 {
		t.Errorf("read extra = %d, want how many bytes to read", req.Extra)
	}

	// The same two fields in the reply are the count read and the error.
	reply := File{SrcHandle: 1, FileHandle: 9, Offset: 400, Extra: 0}
	if reply.Err() != nil {
		t.Errorf("a zero extra on a reply is success, got %v", reply.Err())
	}
	if reply.Offset != 400 {
		t.Errorf("reply offset = %d, want how many bytes were read", reply.Offset)
	}

	// A read is never asked for more than a frame can carry, whatever the
	// caller passes.
	big := FileReadRequest(1, 9, 0, 100000)
	if int(big.Extra) > MaxPayload {
		t.Errorf("read asked for %d bytes, more than a frame holds", big.Extra)
	}
}

// TestFileOpen_FlagsTravelInExtra pins the field the vendor server actually
// reads them from: "OpenModeFlags = FileSpecIn->rExtra". Putting them in the
// offset instead opens every file read-only in text mode, which succeeds and
// returns corrupted bytes.
func TestFileOpen_FlagsTravelInExtra(t *testing.T) {
	got, err := AppendFileOpen(nil, 7, OpenReadOnly|OpenBinary, "A.TXT")
	if err != nil {
		t.Fatalf("AppendFileOpen: %v", err)
	}

	f, path, err := DecodeFileOpen(got)
	if err != nil {
		t.Fatalf("DecodeFileOpen: %v", err)
	}
	if path != "A.TXT" {
		t.Errorf("path = %q", path)
	}
	if f.Offset != 0 {
		t.Errorf("offset = %d, want it unused on an open", f.Offset)
	}
	if f.OpenFlags() != OpenReadOnly|OpenBinary {
		t.Errorf("flags = %04X, want %04X", f.OpenFlags(), OpenReadOnly|OpenBinary)
	}

	// The binary flag is bit 15, so it arrives as a negative number in the
	// signed field. Reading it as signed would make the most important flag
	// in the service look like a malformed request.
	if f.Extra >= 0 {
		t.Errorf("extra = %d; the binary flag should have set the sign bit", f.Extra)
	}
	if f.OpenFlags()&OpenBinary == 0 {
		t.Error("the binary flag was lost in the signed field")
	}
}

// TestFileOpen_ReplyCarriesTheBlockSize pins what an open reply says. Both
// numeric fields are overwritten: the offset becomes the peer's maximum read
// size and the extra becomes the error.
func TestFileOpen_ReplyCarriesTheBlockSize(t *testing.T) {
	reply := File{SrcHandle: 7, FileHandle: 3, Offset: 410}
	if reply.BlockSize() != 410 {
		t.Errorf("block size = %d, want 410", reply.BlockSize())
	}
	if reply.Err() != nil {
		t.Errorf("err = %v, want success", reply.Err())
	}

	// A peer that says nothing leaves the caller to choose.
	if got := (File{}).BlockSize(); got != 0 {
		t.Errorf("block size = %d, want 0 when the peer did not say", got)
	}
	if got := (File{Offset: -1}).BlockSize(); got != 0 {
		t.Errorf("block size = %d, want 0 for a negative figure", got)
	}

	failed := File{Extra: FileErrNoEntry}
	if failed.Err() == nil {
		t.Error("an open that failed must report it")
	}
}

func TestFileWrite(t *testing.T) {
	data := []byte{0xDE, 0xAD, 0xBE, 0xEF}

	got, err := AppendFileWrite(nil, 1, 9, 1024, data)
	if err != nil {
		t.Fatalf("AppendFileWrite: %v", err)
	}

	f, err := DecodeFile(got)
	if err != nil {
		t.Fatalf("DecodeFile: %v", err)
	}
	if f.Offset != 1024 {
		t.Errorf("offset = %d, want where to write", f.Offset)
	}
	if int(f.Extra) != len(data) {
		t.Errorf("extra = %d, want the byte count %d", f.Extra, len(data))
	}
	if !bytes.Equal(got[FileSize:], data) {
		t.Errorf("data = %x, want %x", got[FileSize:], data)
	}

	// More data than a frame can carry is refused rather than truncated.
	if _, err := AppendFileWrite(nil, 1, 9, 0, make([]byte, MaxPayload)); !errors.Is(err, ErrPayloadTooLong) {
		t.Errorf("err = %v, want ErrPayloadTooLong", err)
	}
}
