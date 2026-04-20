package acp2

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"

	iacp2 "acp/internal/protocol/acp2"
)

// session is one TCP connection. Holds the conn, a write mutex so
// replies + announces don't interleave, and the consumer's
// AN2.EnableProtocolEvents subscription set (announces only fan out
// to sessions that opted in — spec §3.3.4).
type session struct {
	srv  *server
	conn net.Conn

	writeMu sync.Mutex
	enabled map[iacp2.AN2Proto]bool
}

func newSession(srv *server, conn net.Conn) *session {
	return &session{srv: srv, conn: conn}
}

// run reads AN2 frames until the connection closes. Every frame is
// dispatched inline through handleFrame; fatal errors close the conn.
func (s *session) run() {
	defer func() { _ = s.conn.Close() }()

	remote := s.conn.RemoteAddr().String()
	s.srv.logger.Info("acp2 session accepted", slog.String("remote", remote))

	for {
		frame, err := iacp2.ReadAN2Frame(s.conn)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				s.srv.logger.Debug("acp2 session closed", slog.String("remote", remote))
				return
			}
			s.srv.logger.Warn("acp2 session read error",
				slog.String("remote", remote),
				slog.String("err", err.Error()),
			)
			return
		}
		s.handleFrame(frame)
	}
}

// handleFrame dispatches one incoming AN2 frame via handlers.go.
func (s *session) handleFrame(f *iacp2.AN2Frame) {
	s.dispatch(f)
}
