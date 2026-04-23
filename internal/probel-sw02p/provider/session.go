package probelsw02p

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"

	"acp/internal/probel-sw02p/codec"
)

// session is one connected SW-P-02 client. It owns the TCP socket,
// accumulates bytes, and feeds complete frames to the server's command
// dispatcher (a no-op in the scaffold). Reply-emitting machinery
// (write serialiser, close-state check) is added alongside the first
// command commit that needs it.
type session struct {
	srv  *server
	conn net.Conn

	closeMu sync.Mutex
	closed  bool
}

func newSession(srv *server, conn net.Conn) *session {
	return &session{srv: srv, conn: conn}
}

// run reads frames until EOF or ctx cancellation. Each decoded frame
// is passed to the server dispatcher. No handler table is wired yet —
// every well-framed frame fires UnsupportedCommand and nothing more.
// Per-command commits replace the scaffold dispatcher.
func (s *session) run(ctx context.Context) {
	defer func() {
		_ = s.conn.Close()
	}()

	s.srv.logger.Info("probel-sw02p session opened",
		slog.String("remote", s.remoteAddr()),
	)

	buf := make([]byte, 0, codec.DefaultReadBufferSize)
	tmp := make([]byte, codec.DefaultReadBufferSize)
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		n, err := s.conn.Read(tmp)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				s.srv.logger.Debug("probel-sw02p session read",
					slog.String("remote", s.remoteAddr()),
					slog.String("err", err.Error()))
			}
			s.srv.logger.Info("probel-sw02p session closed",
				slog.String("remote", s.remoteAddr()))
			return
		}
		buf = append(buf, tmp[:n]...)

		for len(buf) >= 3 {
			if buf[0] != codec.SOM {
				s.srv.logger.Warn("probel-sw02p session desync: dropping byte",
					slog.String("remote", s.remoteAddr()),
					slog.String("byte", fmt.Sprintf("%02x", buf[0])))
				buf = buf[1:]
				continue
			}
			f, consumed, derr := codec.Unpack(buf)
			if errors.Is(derr, io.ErrUnexpectedEOF) {
				break
			}
			if derr != nil {
				s.srv.logger.Warn("probel-sw02p session bad frame",
					slog.String("remote", s.remoteAddr()),
					slog.String("err", derr.Error()))
				s.srv.profile.Note(InboundFrameDecodeFailed)
				s.srv.metrics.ObserveDecodeError()
				// Without per-command length info the scaffold cannot
				// resync precisely; drop one byte and keep looking.
				buf = buf[1:]
				continue
			}
			if s.srv.logger.Enabled(context.Background(), slog.LevelDebug) {
				s.srv.logger.Debug("probel-sw02p session rx",
					slog.String("remote", s.remoteAddr()),
					slog.Int("cmd", int(f.ID)),
					slog.Int("payload_len", len(f.Payload)),
					slog.Int("wire_len", consumed),
					slog.String("hex", codec.HexDump(buf[:consumed])),
				)
			}
			s.srv.metrics.ObserveCmdRx(uint8(f.ID), consumed)
			buf = buf[consumed:]
			// Scaffold dispatcher: every command is "unsupported" until
			// per-command commits wire real handlers.
			s.srv.profile.Note(UnsupportedCommand)
		}
	}
}

// close terminates the session's socket. Idempotent.
func (s *session) close() {
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		return
	}
	s.closed = true
	s.closeMu.Unlock()
	_ = s.conn.Close()
}

func (s *session) remoteAddr() string {
	if s.conn == nil || s.conn.RemoteAddr() == nil {
		return ""
	}
	return s.conn.RemoteAddr().String()
}
