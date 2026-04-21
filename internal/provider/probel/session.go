package probel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"

	iprobel "acp/internal/probel"
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

	buf := make([]byte, 0, iprobel.DefaultReadBufferSize)
	tmp := make([]byte, iprobel.DefaultReadBufferSize)
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
			if iprobel.IsACK(buf) || iprobel.IsNAK(buf) {
				buf = buf[2:]
				continue
			}
			if buf[0] != iprobel.DLE {
				s.srv.logger.Warn("probel session desync: dropping byte",
					slog.String("remote", s.remoteAddr()),
					slog.String("byte", fmt.Sprintf("%02x", buf[0])))
				buf = buf[1:]
				continue
			}
			f, consumed, derr := iprobel.Unpack(buf)
			if errors.Is(derr, io.ErrUnexpectedEOF) {
				break
			}
			if derr != nil {
				s.srv.logger.Warn("probel session bad frame",
					slog.String("remote", s.remoteAddr()),
					slog.String("err", derr.Error()))
				_ = s.write(iprobel.PackNAK())
				buf = buf[2:]
				continue
			}
			s.srv.logger.Info("probel session rx",
				slog.String("remote", s.remoteAddr()),
				slog.Int("cmd", int(f.ID)),
				slog.Int("payload_len", len(f.Payload)),
				slog.Int("wire_len", consumed),
				slog.String("hex", iprobel.HexDump(buf[:consumed])),
			)
			buf = buf[consumed:]
			// SW-P-88 §3.5 — always ACK a well-framed message, then
			// let dispatch decide whether to also send a functional reply.
			_ = s.write(iprobel.PackACK())
			s.dispatch(f)
		}
	}
}

// dispatch routes a decoded frame to the right command handler. Scaffold
// behaviour: log + no functional reply. Per-command PRs add cases.
func (s *session) dispatch(f iprobel.Frame) {
	s.srv.logger.Debug("probel dispatch (stub)",
		slog.String("remote", s.remoteAddr()),
		slog.Int("cmd", int(f.ID)),
	)
	// Intentionally no reply: scaffold supplies no command logic. The
	// ACK sent above keeps the peer happy at the framing layer.
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
