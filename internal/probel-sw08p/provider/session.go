package probelsw08p

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"

	"acp/internal/probel-sw08p/codec"
)

// session is one connected SW-P-08 client (typically a controller).
// It owns the TCP socket, accumulates bytes, feeds complete frames to
// the server's command dispatcher, and serialises writes via writeMu.
type session struct {
	srv  *server
	conn net.Conn

	writeMu sync.Mutex
	closeMu sync.Mutex
	closed  bool
}

func newSession(srv *server, conn net.Conn) *session {
	return &session{srv: srv, conn: conn}
}

// run reads frames until EOF or ctx cancellation. Each frame is ACK'd
// and passed to dispatch. Dispatch is a stub in the scaffold commit:
// every command replies with DLE NAK so controllers see the session
// is alive but refusing until per-command handlers land.
func (s *session) run(ctx context.Context) {
	defer func() {
		_ = s.conn.Close()
	}()

	s.srv.logger.Info("probel session opened",
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
				s.srv.logger.Debug("probel session read",
					slog.String("remote", s.remoteAddr()),
					slog.String("err", err.Error()))
			}
			s.srv.logger.Info("probel session closed",
				slog.String("remote", s.remoteAddr()))
			return
		}
		buf = append(buf, tmp[:n]...)

		for len(buf) >= 2 {
			if codec.IsACK(buf) {
				s.srv.logger.Debug("probel session rx ACK",
					slog.String("remote", s.remoteAddr()))
				buf = buf[2:]
				continue
			}
			if codec.IsNAK(buf) {
				s.srv.logger.Warn("probel session rx NAK",
					slog.String("remote", s.remoteAddr()))
				buf = buf[2:]
				continue
			}
			if buf[0] != codec.DLE {
				s.srv.logger.Warn("probel session desync: dropping byte",
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
				s.srv.logger.Warn("probel session bad frame",
					slog.String("remote", s.remoteAddr()),
					slog.String("err", derr.Error()))
				_ = s.write(codec.PackNAK())
				s.srv.profile.Note(InboundFrameDecodeFailed)
				buf = buf[2:]
				continue
			}
			s.srv.logger.Info("probel session rx",
				slog.String("remote", s.remoteAddr()),
				slog.Int("cmd", int(f.ID)),
				slog.Int("payload_len", len(f.Payload)),
				slog.Int("wire_len", consumed),
				slog.String("hex", codec.HexDump(buf[:consumed])),
			)
			buf = buf[consumed:]
			// SW-P-08 §2 — always ACK a well-framed message, then
			// let dispatch decide whether to also send a functional reply.
			_ = s.write(codec.PackACK())
			s.dispatch(f)
		}
	}
}

// dispatch routes a decoded frame to the server's command handler table
// (handlers.go), sends the reply (if any) back on the originating
// session, and fans tallies out to every OTHER live session.
func (s *session) dispatch(f codec.Frame) {
	res, err := s.srv.handle(f)
	if err != nil {
		s.srv.logger.Warn("probel dispatch",
			slog.String("remote", s.remoteAddr()),
			slog.Int("cmd", int(f.ID)),
			slog.String("err", err.Error()),
		)
		s.srv.profile.Note(HandlerRejected)
		return
	}
	if res.reply != nil {
		raw := codec.Pack(*res.reply)
		s.srv.logger.Info("probel session tx",
			slog.String("remote", s.remoteAddr()),
			slog.Int("cmd", int(res.reply.ID)),
			slog.Int("payload_len", len(res.reply.Payload)),
			slog.Int("wire_len", len(raw)),
			slog.String("hex", codec.HexDump(raw)),
		)
		if werr := s.write(raw); werr != nil {
			s.srv.logger.Warn("probel session reply write",
				slog.String("remote", s.remoteAddr()),
				slog.String("err", werr.Error()))
		}
	}
	for _, tally := range res.tallies {
		s.srv.fanOutTally(s, tally)
	}
}

// write serialises outbound bytes against concurrent writers. All
// replies and tally fan-outs go through here.
func (s *session) write(raw []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.isClosed() {
		return net.ErrClosed
	}
	_, err := s.conn.Write(raw)
	return err
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

func (s *session) isClosed() bool {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	return s.closed
}

func (s *session) remoteAddr() string {
	if s.conn == nil || s.conn.RemoteAddr() == nil {
		return ""
	}
	return s.conn.RemoteAddr().String()
}
