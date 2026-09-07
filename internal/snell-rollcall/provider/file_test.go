package rollcall

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"dhs/internal/clock"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

func TestTemplateIsServedAndIsAnArchive(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcFile)

	body := readFile(t, sess, TemplateFileName)
	if len(body) == 0 {
		t.Fatal("the template came back empty")
	}

	// The Control Panel will not render a device without this file, and what
	// it expects is an archive. A blob that is not one is no better than none.
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("the template is not a zip archive: %v", err)
	}
	if len(zr.File) != 1 || zr.File[0].Name != templateEntryName {
		t.Fatalf("archive holds %d entries", len(zr.File))
	}

	f, err := zr.File[0].Open()
	if err != nil {
		t.Fatalf("open entry: %v", err)
	}
	defer func() { _ = f.Close() }()

	text, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read entry: %v", err)
	}
	// It describes the tree it was built from, which is the whole point of
	// generating it rather than shipping a fixed one.
	for _, want := range []string{"card1", "gain", "card2", "level"} {
		if !strings.Contains(string(text), want) {
			t.Errorf("the template does not mention %q", want)
		}
	}
}

func TestTemplateIsTheSameBytesEveryTime(t *testing.T) {
	// A client caches the archive by checksum. Rebuilding it must not produce
	// different bytes for the same tree, or every restart re-fetches it.
	first := buildTemplate(buildModel(testTree(), "dhs rollcall"))
	second := buildTemplate(buildModel(testTree(), "dhs rollcall"))

	if !bytes.Equal(first, second) {
		t.Error("two builds of one tree produced different archives")
	}
}

func TestAFileIsReadInBlocks(t *testing.T) {
	s := newServed(t, testTree())

	// Longer than one frame holds, so the read has to come back in pieces and
	// the client's offsets have to line up with what the server sends.
	body := bytes.Repeat([]byte("0123456789"), 200)
	s.p.AddFile("names.dat", body)

	sess := s.open(1, codec.SvcFile)
	got := readFile(t, sess, "names.dat")

	if !bytes.Equal(got, body) {
		t.Errorf("read %d bytes, want %d", len(got), len(body))
	}
}

func TestAnOpenReportsTheBlockSizeAndTheLength(t *testing.T) {
	s := newServed(t, testTree())
	s.p.AddFile("short.txt", []byte("hello"))

	sess := s.open(1, codec.SvcFile)
	payload, err := codec.AppendFileOpen(nil, 1, codec.OpenReadOnly|codec.OpenBinary, "short.txt")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	reply := do(t, sess, codec.MsgFileOpen, payload)
	f, err := codec.DecodeFile(reply.Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := f.Err(); err != nil {
		t.Fatalf("open: %v", err)
	}
	if f.BlockSize() != maxBlock {
		t.Errorf("block size = %d, want %d", f.BlockSize(), maxBlock)
	}

	// The vendor server sends the directory header after the structure, and a
	// client that reads the length from there is reading what it was given.
	info, err := codec.DecodeFileInfo(reply.Payload[codec.FileSize:])
	if err != nil {
		t.Fatalf("decode info: %v", err)
	}
	if info.Length != 5 {
		t.Errorf("length = %d, want 5", info.Length)
	}
	if !info.ReadOnly() {
		t.Error("a generated file should be read-only")
	}
}

func TestAPathIsMatchedHoweverItIsWritten(t *testing.T) {
	s := newServed(t, testTree())
	s.p.AddFile("names.dat", []byte("x"))

	sess := s.open(1, codec.SvcFile)
	for _, path := range []string{
		"names.dat", "NAMES.DAT", "/names.dat", "\\device\\root\\names.dat", "root/Names.Dat",
	} {
		if got := readFile(t, sess, path); string(got) != "x" {
			t.Errorf("%q did not find the file", path)
		}
	}
}

func TestAMissingFileIsAnErrnoNotARefusal(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcFile)

	payload, err := codec.AppendFileOpen(nil, 1, codec.OpenBinary, "absent.bin")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	// A file that is not there is an answer, not a broken service: the reply
	// carries the errno and the client decides what to do.
	reply := do(t, sess, codec.MsgFileOpen, payload)
	f, err := codec.DecodeFile(reply.Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if f.Err() == nil {
		t.Fatal("opening a file that is not there should report an error")
	}
	if f.Extra != codec.FileErrNoEntry {
		t.Errorf("errno = %d, want %d", f.Extra, codec.FileErrNoEntry)
	}
}

func TestAReadOnAnUnknownHandleIsReported(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcFile)

	req := codec.FileReadRequest(1, 99, 0, 16)
	reply := do(t, sess, codec.MsgFileRead, req.AppendTo(nil))
	f, err := codec.DecodeFile(reply.Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if f.Err() == nil {
		t.Error("a read on a handle nobody issued should report an error")
	}
	if !hasEvent(s.p, EventUnknownFileHandle) {
		t.Error("the handle should have been recorded as a compliance event")
	}
}

func TestClosingAnUnknownHandleIsRefused(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcFile)

	req := codec.File{SrcHandle: 1, FileHandle: 99}
	if got := refused(t, sess, codec.MsgFileClose, req.AppendTo(nil)); got.Type != codec.MsgNack {
		t.Errorf("answered %s, want Nack", got.Type)
	}
}

func TestAClosedHandleIsGone(t *testing.T) {
	s := newServed(t, testTree())
	s.p.AddFile("a.txt", []byte("abc"))

	sess := s.open(1, codec.SvcFile)
	handle := openFileOn(t, sess, "a.txt")

	req := codec.File{SrcHandle: 1, FileHandle: handle}
	if got := do(t, sess, codec.MsgFileClose, req.AppendTo(nil)); got.Type != codec.MsgAck {
		t.Errorf("close answered %s, want Ack", got.Type)
	}
	if got := refused(t, sess, codec.MsgFileClose, req.AppendTo(nil)); got.Type != codec.MsgNack {
		t.Errorf("closing twice answered %s, want Nack", got.Type)
	}
}

func TestASessionTakesItsOpenFilesWithIt(t *testing.T) {
	s := newServed(t, testTree())
	s.p.AddFile("a.txt", []byte("abc"))

	sess := s.open(1, codec.SvcFile)
	handle := openFileOn(t, sess, "a.txt")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sess.Term(ctx, codec.TermUser, "done"); err != nil {
		t.Fatalf("term: %v", err)
	}
	s.raw(codec.MsgKeepAlive, nil) // barrier: the read loop is sequential

	st := s.p.linkState(providerLink(t, s.p))
	if _, ok := st.handle(handle); ok {
		t.Error("a file stayed open after the session that opened it ended")
	}
}

func TestADirectoryListingNamesWhatIsOnOffer(t *testing.T) {
	s := newServed(t, testTree())
	s.p.AddFile("names.dat", []byte("x"))

	sess := s.open(1, codec.SvcFile)
	payload, err := codec.AppendFilePath(nil, 1, "/")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var names []string
	err = session.Walk(context.Background(), sess, codec.MsgFileDir, payload,
		func(_ int, f codec.Frame) error {
			entry, err := codec.DecodeDirEntry(f.Payload)
			if err != nil {
				return err
			}
			names = append(names, entry.Name)
			return nil
		})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	if len(names) != 2 || names[0] != "NAMES.DAT" || names[1] != "TEMPLATE.ZIP" {
		t.Errorf("listing = %v, want [NAMES.DAT TEMPLATE.ZIP]", names)
	}
}

func TestFileRefusals(t *testing.T) {
	s := newServed(t, testTree())

	noFiles := s.open(1, codec.SvcMenus)
	if got := refused(t, noFiles, codec.MsgFileDir, nil); got.Type != codec.MsgNack {
		t.Errorf("a session without the file service listed anyway: %s", got.Type)
	}

	sess := s.open(1, codec.SvcFile)
	for _, tc := range []struct {
		name string
		typ  codec.PacketType
	}{
		{"a short open", codec.MsgFileOpen},
		{"a short read", codec.MsgFileRead},
		{"a short close", codec.MsgFileClose},
		{"a short listing", codec.MsgFileDir},
	} {
		if got := refused(t, sess, tc.typ, []byte{0x01}); got.Type != codec.MsgNack {
			t.Errorf("%s answered %s, want Nack", tc.name, got.Type)
		}
	}
}

func TestFilesAreListedByName(t *testing.T) {
	p := New(testDeps(clock.NewFake(time.Time{})), testTree())
	p.AddFile("one.txt", []byte("1"))

	names := p.Files()
	if len(names) != 2 {
		t.Fatalf("Files() = %v, want two", names)
	}
	if _, ok := p.file("ONE.TXT"); !ok {
		t.Error("a file added under a lower-case name was not found")
	}
	if _, ok := p.file("missing"); ok {
		t.Error("a file that was never added was found")
	}
}

func TestCleanPath(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"template.zip", "TEMPLATE.ZIP"},
		{"/template.zip", "TEMPLATE.ZIP"},
		{"\\dev\\root\\template.zip", "TEMPLATE.ZIP"},
		{"a/b/c.dat", "C.DAT"},
		{"", ""},
	} {
		if got := cleanPath(tc.in); got != tc.want {
			t.Errorf("cleanPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// openFileOn opens a file and returns the server's handle.
func openFileOn(t *testing.T, s *session.Session, path string) int16 {
	t.Helper()

	payload, err := codec.AppendFileOpen(nil, 1, codec.OpenReadOnly|codec.OpenBinary, path)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	f, err := codec.DecodeFile(do(t, s, codec.MsgFileOpen, payload).Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := f.Err(); err != nil {
		t.Fatalf("open %q: %v", path, err)
	}
	return f.FileHandle
}

// readFile reads a whole file the way a client does: open, read until nothing
// comes back, close.
func readFile(t *testing.T, s *session.Session, path string) []byte {
	t.Helper()

	handle := openFileOn(t, s, path)

	var out []byte
	for {
		req := codec.FileReadRequest(1, handle, int32(len(out)), maxBlock)
		reply := do(t, s, codec.MsgFileRead, req.AppendTo(nil))

		f, err := codec.DecodeFile(reply.Payload)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if err := f.Err(); err != nil {
			t.Fatalf("read at %d: %v", len(out), err)
		}
		data := reply.Payload[codec.FileSize:]
		if int(f.Offset) != len(data) {
			t.Fatalf("the reply said %d bytes and carried %d", f.Offset, len(data))
		}
		if len(data) == 0 {
			break
		}
		out = append(out, data...)
	}

	closeReq := codec.File{SrcHandle: 1, FileHandle: handle}
	do(t, s, codec.MsgFileClose, closeReq.AppendTo(nil))
	return out
}
