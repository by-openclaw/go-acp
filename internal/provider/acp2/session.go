package acp2

import (
	"errors"
	"io"
	"log/slog"
	"net"

	iacp2 "acp/internal/protocol/acp2"
)

// session is one TCP connection. Holds the conn, the read loop, and —
// as later commits add it — a write mutex + subscribed-protocols set
// for announce gating per spec §EnableProtocolEvents.
type session struct {
	srv  *server
	conn net.Conn
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

// handleFrame dispatches one incoming AN2 frame. The AN2 internal
// handshake ships in Step 2b; ACP2 dispatch in Step 2d/2e. For now
// the session logs what it sees so you can confirm the consumer is
// reaching the provider before real handlers arrive.
func (s *session) handleFrame(f *iacp2.AN2Frame) {
	s.srv.logger.Debug("acp2 frame (dispatch pending)",
		slog.String("proto", f.Proto.String()),
		slog.String("type", f.Type.String()),
		slog.Int("slot", int(f.Slot)),
		slog.Int("mtid", int(f.MTID)),
		slog.Int("dlen", len(f.Payload)),
	)
}
