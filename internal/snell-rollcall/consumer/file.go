package rollcall

import (
	"context"
	"fmt"
	"io"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

// The file service is how two things in this protocol actually work.
//
// A router publishes every source and destination name in a file, named by a
// filename and a checksum, because sending sixty-five thousand names as
// individual commands would take longer than anyone will wait. And the vendor's
// Control Panel will not render a device at all without reading TEMPLATE.ZIP
// from it, which the menu service cannot supply.
//
// So this is not an optional extra: without it a router has no labels and a
// producer has no user interface.

// defaultReadChunk is how much to ask for when the peer does not say.
//
// A frame carries 420 payload bytes and a read reply spends ten of them on its
// own structure, so this is what fits without fragmenting. A peer states its
// own maximum in the reply to an open, and that figure wins when it is
// smaller: the server clamps a larger request rather than refusing it, so
// asking for more than it allows simply wastes the excess on every block.
const defaultReadChunk = codec.MaxPayload - codec.FileSize

// ReadFile reads a whole file from a slot.
//
// The file is opened in binary mode, always. Text mode makes the peer translate
// line endings, which corrupts an archive or a names file, and it does so
// silently: the read succeeds and the bytes are wrong.
func (p *Plugin) ReadFile(ctx context.Context, slot int, path string) ([]byte, error) {
	s, err := p.fileSession(ctx, uint8(slot))
	if err != nil {
		return nil, err
	}

	handle, chunk, err := p.openFile(ctx, s, path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := p.closeFile(context.WithoutCancel(ctx), s, handle); cerr != nil {
			p.log.Debug("rollcall: closing a file failed",
				"path", path, "slot", slot, "err", cerr)
		}
	}()

	var out []byte
	for {
		chunk, err := p.readAt(ctx, s, handle, int32(len(out)), chunk)
		if err != nil {
			return nil, fmt.Errorf("rollcall: read %q at %d: %w", path, len(out), err)
		}
		if len(chunk) == 0 {
			return out, nil
		}
		out = append(out, chunk...)

		if len(out) > codec.MaxPayload*100000 {
			return nil, fmt.Errorf("rollcall: %q is longer than any file this protocol carries", path)
		}
	}
}

// openFile opens a path and returns the server's handle and the block size it
// wants reads to use.
func (p *Plugin) openFile(ctx context.Context, s *session.Session, path string) (int16, int, error) {
	payload, err := codec.AppendFileOpen(nil, 1, codec.OpenReadOnly|codec.OpenBinary, path)
	if err != nil {
		return 0, 0, err
	}

	reply, err := s.Do(ctx, codec.MsgFileOpen, payload)
	if err != nil {
		return 0, 0, fmt.Errorf("rollcall: open %q: %w", path, err)
	}
	f, err := codec.DecodeFile(reply.Payload)
	if err != nil {
		return 0, 0, fmt.Errorf("rollcall: open %q: %w", path, err)
	}
	if err := f.Err(); err != nil {
		return 0, 0, fmt.Errorf("rollcall: open %q: %w", path, err)
	}

	// The reply overwrites both numeric fields: the offset now carries the
	// peer's maximum block size and the extra carries the error. Honour the
	// size it gave, and fall back to what a frame holds when it gave none.
	chunk := f.BlockSize()
	if chunk <= 0 || chunk > defaultReadChunk {
		chunk = defaultReadChunk
	}
	return f.FileHandle, chunk, nil
}

// readAt reads one block from an offset, and returns an empty slice at the end
// of the file.
func (p *Plugin) readAt(ctx context.Context, s *session.Session, handle int16, off int32, count int) ([]byte, error) {
	// Offset says where to read from and Extra says how many bytes to read.
	// Both change meaning in the reply, where they become the number read and
	// the error.
	req := codec.FileReadRequest(1, handle, off, count)

	reply, err := s.Do(ctx, codec.MsgFileRead, req.AppendTo(nil))
	if err != nil {
		return nil, err
	}
	f, err := codec.DecodeFile(reply.Payload)
	if err != nil {
		return nil, err
	}
	if err := f.Err(); err != nil {
		return nil, err
	}

	// What follows the structure is the data. The offset field on a reply
	// says how much was read, but the payload is what actually arrived, so
	// the payload wins: a peer that disagrees with itself must not make us
	// read past what it sent.
	data := reply.Payload[codec.FileSize:]
	if n := int(f.Offset); n >= 0 && n < len(data) {
		data = data[:n]
	}
	return data, nil
}

func (p *Plugin) closeFile(ctx context.Context, s *session.Session, handle int16) error {
	req := codec.File{SrcHandle: 1, FileHandle: handle}
	_, err := s.Do(ctx, codec.MsgFileClose, req.AppendTo(nil))
	return err
}

// ListDir lists a directory on a slot.
func (p *Plugin) ListDir(ctx context.Context, slot int, path string) ([]codec.DirEntry, error) {
	s, err := p.fileSession(ctx, uint8(slot))
	if err != nil {
		return nil, err
	}

	payload, err := codec.AppendFilePath(nil, 1, path)
	if err != nil {
		return nil, err
	}

	var out []codec.DirEntry
	err = session.Walk(ctx, s, codec.MsgFileDir, payload, func(_ int, f codec.Frame) error {
		if f.Type != codec.MsgRetFileDir {
			return nil
		}
		entry, err := codec.DecodeDirEntry(f.Payload)
		if err != nil {
			return err
		}
		out = append(out, entry)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("rollcall: list %q: %w", path, err)
	}
	return out, nil
}

// fileSession returns a session that has the file service.
//
// It is a separate session from the control one because the services are
// negotiated together and all-or-nothing: asking for the file service on the
// control session would mean a unit without one could not be controlled at
// all.
func (p *Plugin) fileSession(ctx context.Context, port uint8) (*session.Session, error) {
	l, err := p.conn()
	if err != nil {
		return nil, err
	}

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, fmt.Errorf("rollcall: link closed")
	}
	if s, ok := l.fileSessions[port]; ok {
		l.mu.Unlock()
		return s, nil
	}
	l.mu.Unlock()

	peer := l.sess.RemoteAddress()
	peer.Port = port

	s, err := session.Call(ctx, l.sess, peer, codec.SvcFile,
		codec.LevelSupervisor, p.identity())
	if err != nil {
		return nil, fmt.Errorf("rollcall: file service on port %02X: %w", port, err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		_ = s.Close()
		return nil, fmt.Errorf("rollcall: link closed")
	}
	if existing, ok := l.fileSessions[port]; ok {
		go func() { _ = s.Close() }()
		return existing, nil
	}
	l.fileSessions[port] = s
	return s, nil
}

// ReadFileTo streams a file into a writer, for callers that would rather not
// hold a whole archive in memory.
func (p *Plugin) ReadFileTo(ctx context.Context, slot int, path string, w io.Writer) (int64, error) {
	s, err := p.fileSession(ctx, uint8(slot))
	if err != nil {
		return 0, err
	}

	handle, size, err := p.openFile(ctx, s, path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = p.closeFile(context.WithoutCancel(ctx), s, handle) }()

	var total int64
	for {
		chunk, err := p.readAt(ctx, s, handle, int32(total), size)
		if err != nil {
			return total, fmt.Errorf("rollcall: read %q at %d: %w", path, total, err)
		}
		if len(chunk) == 0 {
			return total, nil
		}
		n, err := w.Write(chunk)
		total += int64(n)
		if err != nil {
			return total, err
		}
	}
}
