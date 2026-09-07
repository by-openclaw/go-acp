package rollcall

import (
	"fmt"
	"sort"
	"strings"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

// The file service is not an extra here. The vendor's Control Panel reads
// TEMPLATE.ZIP over it before it draws anything, so a provider without a file
// service is a device the Control Panel will not render at all; a router
// publishes its source and destination names the same way, because sending
// sixty-five thousand names as individual commands would take longer than
// anyone will wait.
//
// What is served is an in-memory set of files rather than a directory on disk.
// A gateway exposing its own filesystem to every client that connects is a
// hazard nobody asked for, and the two files that matter are generated anyway.

// maxBlock is the largest read this server will answer.
//
// It is what a frame holds once the file structure has taken its share, and it
// is what an open reports back so a client sizes its reads to it.
const maxBlock = codec.MaxPayload - codec.FileSize

// openFile is one handle a client holds.
type openFile struct {
	name  string
	body  []byte
	owner int16
}

// fileRequest serves the file operations a client may use.
func (p *Provider) fileRequest(s *session.Session, req codec.Frame) error {
	if !s.Services().Has(codec.SvcFile) {
		return session.RefuseNack("this session did not ask for the file service")
	}

	st := p.sessionState(s)
	if st == nil {
		return session.RefuseNack("unknown session")
	}

	switch req.Type {
	case codec.MsgFileOpen:
		return p.fileOpen(s, st, req)
	case codec.MsgFileRead:
		return p.fileRead(s, st, req)
	case codec.MsgFileClose:
		return p.fileClose(s, st, req)
	default:
		return p.fileDir(s, req)
	}
}

// fileOpen answers an open with a handle, the block size to read in, and what
// the directory says about the file.
//
// The reply carries both structures because the vendor server does: it sends
// FILE_STR followed by FILEINFOHDR_STR, and a client that reads the length
// from the second is reading what the vendor put there.
func (p *Provider) fileOpen(s *session.Session, st *linkState, req codec.Frame) error {
	f, path, err := codec.DecodeFileOpen(req.Payload)
	if err != nil {
		return session.RefuseNack("malformed file open")
	}

	body, ok := p.file(cleanPath(path))
	if !ok {
		// Not an error at the protocol level: the reply says which errno, and
		// the client decides. Refusing the message instead would tell it the
		// service is broken rather than that the file is absent.
		reply := codec.File{SrcHandle: f.SrcHandle, Extra: codec.FileErrNoEntry}.AppendTo(nil)
		reply = codec.FileInfo{Attrib: codec.AttrReadOnly}.AppendTo(reply)
		return s.Answer(codec.MsgRetFileOpen, reply)
	}

	handle := st.openFile(s.LocalIndex(), cleanPath(path), body)

	reply := codec.File{
		SrcHandle:  f.SrcHandle,
		FileHandle: handle,
		Offset:     maxBlock,
	}.AppendTo(nil)
	reply = codec.FileInfo{
		Time:   int32(p.clk.Now().Unix()),
		Attrib: codec.AttrReadOnly,
		Length: int32(len(body)),
	}.AppendTo(reply)
	return s.Answer(codec.MsgRetFileOpen, reply)
}

// fileRead answers a read with the bytes at an offset.
//
// Offset says where to read from and Extra how many bytes to read; both change
// meaning in the reply, where they become the number read and the error. A
// request for more than a frame holds is clamped rather than refused, which is
// what the vendor server does.
func (p *Provider) fileRead(s *session.Session, st *linkState, req codec.Frame) error {
	f, err := codec.DecodeFile(req.Payload)
	if err != nil {
		return session.RefuseNack("malformed file read")
	}

	open, ok := st.handle(f.FileHandle)
	if !ok {
		p.fire(EventUnknownFileHandle, fmt.Sprintf(
			"a read named handle %d, which this link never issued", f.FileHandle))
		reply := codec.File{SrcHandle: f.SrcHandle, FileHandle: f.FileHandle,
			Extra: codec.FileErrNoEntry}.AppendTo(nil)
		return s.Answer(codec.MsgRetFileRead, reply)
	}

	count := int(f.Extra)
	if count <= 0 || count > maxBlock {
		count = maxBlock
	}

	var data []byte
	if off := int(f.Offset); off >= 0 && off < len(open.body) {
		end := off + count
		if end > len(open.body) {
			end = len(open.body)
		}
		data = open.body[off:end]
	}

	// A read past the end returns nothing and no error, which is how the end
	// of a file is reported: there is no separate end-of-file message.
	reply := codec.File{
		SrcHandle:  f.SrcHandle,
		FileHandle: f.FileHandle,
		Offset:     int32(len(data)),
	}.AppendTo(nil)
	return s.Answer(codec.MsgRetFileRead, append(reply, data...))
}

// fileClose releases a handle, answering with an acknowledgement as the vendor
// server does rather than with a file structure.
func (p *Provider) fileClose(s *session.Session, st *linkState, req codec.Frame) error {
	f, err := codec.DecodeFile(req.Payload)
	if err != nil {
		return session.RefuseNack("malformed file close")
	}
	if !st.closeHandle(f.FileHandle) {
		p.fire(EventUnknownFileHandle, fmt.Sprintf(
			"a close named handle %d, which this link never issued", f.FileHandle))
		return session.RefuseNack("no such file handle")
	}
	return s.Answer(codec.MsgAck, nil)
}

// fileDir lists what is on offer, as a multi-packet transfer.
func (p *Provider) fileDir(s *session.Session, req codec.Frame) error {
	if _, err := codec.DecodeFile(req.Payload); err != nil {
		return session.RefuseNack("malformed directory request")
	}

	names := p.Files()
	sort.Strings(names)

	now := int32(p.clk.Now().Unix())
	items := make([][]byte, 0, len(names))
	for _, name := range names {
		body, _ := p.file(name)
		entry, _ := codec.DirEntry{
			Info: codec.FileInfo{
				Time:   now,
				Attrib: codec.AttrReadOnly,
				Length: int32(len(body)),
			},
			// The name field is thirteen bytes, so a longer one is cut here
			// rather than refused: a listing that fails because one file has a
			// long name is worse than one that shows a shortened name.
			Name: codec.TruncateFixed(name, codec.FileNameSize),
		}.AppendTo(nil) // the name is cut to the field above, so this cannot fail
		items = append(items, entry)
	}
	return p.beginTransfer(s, req.Type, codec.MsgRetFileDir, items)
}

// cleanPath reduces a client's path to the name we store it under.
//
// Clients name the same file several ways: with a leading separator, with a
// device root in front of it, in either case. Everything before the last
// separator is dropped, and the comparison is case-insensitive, because the
// filesystems this protocol grew up on were.
func cleanPath(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		path = path[i+1:]
	}
	return strings.ToUpper(path)
}

// openFile records a handle and returns it.
//
// Handles start at one because zero is what an uninitialised structure carries,
// and a client that sent one would otherwise be given a real file.
func (st *linkState) openFile(owner int16, name string, body []byte) int16 {
	st.mu.Lock()
	defer st.mu.Unlock()

	st.nextH++
	h := st.nextH
	st.handles[h] = openFile{name: name, body: body, owner: owner}
	return h
}

// handle returns what a handle refers to.
func (st *linkState) handle(h int16) (openFile, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	f, ok := st.handles[h]
	return f, ok
}

// closeHandle releases a handle, reporting whether it existed.
func (st *linkState) closeHandle(h int16) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.handles[h]; !ok {
		return false
	}
	delete(st.handles, h)
	return true
}

// closeHandlesOf releases everything a session left open, which is what a
// client that disconnects without closing its files relies on.
func (st *linkState) closeHandlesOf(owner int16) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for h, f := range st.handles {
		if f.owner == owner {
			delete(st.handles, h)
		}
	}
}
