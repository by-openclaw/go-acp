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

	"dhs/internal/export/canonical"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

func TestTemplateIsServedInTheVendorsFormat(t *testing.T) {
	// The Control Panel will not draw a unit without this file, and what it
	// expects is the vendor's own layout: an archive holding one entry called
	// Template.tpl, spelled that way, containing sections keyed by card type,
	// command set and user level. Taken from the archives the Centra simulator
	// ships, every one of which is exactly that.
	//
	// Handed anything else, a panel reports "Failed to process the template:
	// No pages for the requested command set version and/or RollCall level",
	// because it finds an archive, believes it, and cannot parse it.
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcFile)

	body := readFile(t, sess, TemplateFileName)
	if len(body) == 0 {
		t.Fatal("the template came back empty")
	}

	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("the template is not a zip archive: %v", err)
	}
	if len(zr.File) != 1 || zr.File[0].Name != templateEntryName {
		t.Fatalf("archive holds %v, want one %s", names(zr), templateEntryName)
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
	tpl := string(text)

	if !strings.Contains(tpl, "version=12") {
		t.Errorf("template carries no version line: %.40q", tpl)
	}
	// The section names the card type and its command set, and offers every
	// user level. A panel connected at supervisor asks for level 4.
	if !strings.Contains(tpl, "[23:1:15:0]") {
		t.Errorf("no section for this card type and command set: %s", tpl)
	}
	if !strings.Contains(tpl, "Size=0,0,") {
		t.Error("the page has no size")
	}
	// Every command in the menu is reachable from the drawing.
	for _, want := range []string{"gain", "enable", "name", "status"} {
		if !strings.Contains(tpl, want) {
			t.Errorf("the template does not mention %q", want)
		}
	}
	// A control line is caption then seven numbers, the last four being the
	// rectangle.
	if !strings.Contains(tpl, "Ctl0=") {
		t.Error("the page has no controls")
	}
}

// names lists what an archive holds, for a failure message.
func names(zr *zip.Reader) []string {
	out := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		out = append(out, f.Name)
	}
	return out
}

func TestTemplateIsTheSameBytesEveryTime(t *testing.T) {
	// A client caches the archive by checksum. Rebuilding it must not produce
	// different bytes for the same tree, or every restart re-fetches it.
	first := buildTemplate(buildModel(testTree(), "dhs rollcall").port(1))
	second := buildTemplate(buildModel(testTree(), "dhs rollcall").port(1))

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

func TestTemplateDrawsLinesThatNameThemselvesOddly(t *testing.T) {
	// A parameter with no identifier and one whose style is neither a number,
	// a checkbox nor an editable string: both have to appear on the page, or
	// the drawing silently omits a control the menu offers.
	blank := &canonical.Parameter{
		Header: canonical.Header{Number: 1, Path: "frame.card1.", Access: canonical.AccessRead},
		Type:   canonical.ParamString, Value: "",
	}
	// An enumeration becomes a list, which is neither a number, a checkbox nor
	// an editable string, and so takes the drawing's last branch.
	listy := &canonical.Parameter{
		Header: canonical.Header{
			Number: 2, Identifier: "mode", Path: "frame.card1.mode",
			Access: canonical.AccessRead,
		},
		Type: canonical.ParamEnum, Value: int64(0), Minimum: 0, Maximum: 3,
	}
	card := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "card1", Path: "frame.card1",
			Children: []canonical.Element{blank, listy},
		},
	}
	root := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "frame", Path: "frame",
			Children: []canonical.Element{card},
		},
	}

	body := buildTemplate(buildModel(&canonical.Export{Root: root}, "dhs rollcall").port(1))
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("not an archive: %v", err)
	}
	f, err := zr.File[0].Open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()
	text, _ := io.ReadAll(f)
	tpl := string(text)

	if !strings.Contains(tpl, "line ") {
		t.Errorf("a line with no name is not drawn:\n%s", tpl)
	}
	if !strings.Contains(tpl, "mode") {
		t.Errorf("a read-only line is not drawn:\n%s", tpl)
	}
}

func TestEachCardServesItsOwnTemplate(t *testing.T) {
	// Two cards can carry different menus. One archive for the frame, with its
	// sections keyed by card type, gave the second card the first one's page —
	// and a panel drew the first card's controls against the second card's
	// commands. A real frame serves a template per node, because each node's
	// file service is rooted at its own directory.
	s := newServed(t, testTree())

	one := templateOf(t, s, 1)
	two := templateOf(t, s, 2)

	if one == two {
		t.Fatal("both cards served the same page")
	}
	if !strings.Contains(one, "card1") || strings.Contains(one, "card2") {
		t.Errorf("card 1 was given the wrong page:\n%s", one)
	}
	if !strings.Contains(two, "card2") || strings.Contains(two, "card1") {
		t.Errorf("card 2 was given the wrong page:\n%s", two)
	}
}

func TestANumberCarriesItsPresetButton(t *testing.T) {
	// The vendor puts a preset beside every scrollbar. A card without them can
	// be driven but not put back, which is half a control.
	s := newServed(t, testTree())
	tpl := templateOf(t, s, 1)

	if !strings.Contains(tpl, ",-14,") {
		t.Errorf("no preset button on a page with a number:\n%s", tpl)
	}
	if !strings.Contains(tpl, ",-9,") {
		t.Errorf("no scrollbar on a page with a number:\n%s", tpl)
	}
}

// templateOf reads one card's template as text.
func templateOf(t *testing.T, s *served, port uint8) string {
	t.Helper()

	sess := s.open(port, codec.SvcFile)
	body := readFile(t, sess, TemplateFileName)
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("port %d: not an archive: %v", port, err)
	}
	f, err := zr.File[0].Open()
	if err != nil {
		t.Fatalf("port %d: open: %v", port, err)
	}
	defer func() { _ = f.Close() }()
	text, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("port %d: read: %v", port, err)
	}
	return string(text)
}
