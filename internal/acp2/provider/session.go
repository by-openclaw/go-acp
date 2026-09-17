package acp2

import (
	"dhs/internal/acp2/codec"
	"dhs/internal/transport"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
)

// session is one TCP connection. Holds the conn, a write mutex so
// replies + announces don't interleave, and the consumer's
// AN2.EnableProtocolEvents subscription set (announces only fan out
// to sessions that opted in — spec §3.3.4).
type session struct {
	srv  *server
	conn net.Conn

	writeMu sync.Mutex
	enabled map[codec.AN2Proto]bool

	// idle reaps a client that has gone silent, so the provider stops
	// holding a goroutine and a socket for every consumer that vanished
	// without an RST. Off unless configured.
	idle transport.Idle
}

func newSession(srv *server, conn net.Conn) *session {
	s := &session{srv: srv, conn: conn}
	s.idle.Set(srv.idleTimeout())
	return s
}

// an2HeaderBytes is the fixed AN2 frame header size (magic u16 + proto
// + slot + mtid + type + dlen u16 — CLAUDE.md "AN2 frame header").
// ReadAN2Frame returns Payload without it, so rx byte accounting adds
// it back for a true on-wire count.
const an2HeaderBytes = 8

// run reads AN2 frames until the connection closes. Every frame is
// dispatched inline through handleFrame; fatal errors close the conn.
//
// Per spec acp2_protocol.docx line 313 ("Should handle single request
// at a time") this loop is the single-request-at-a-time gate for one
// session: ReadAN2Frame blocks, dispatch processes synchronously
// (replyACP2 returns before we loop), then the next frame is read.
// No per-request goroutines are spawned. Pipelined requests on the
// same TCP connection queue in the kernel socket buffer and execute
// in arrival order. See provider/session_serial_test.go for the
// pinning test.
func (s *session) run() {
	defer func() { _ = s.conn.Close() }()

	remote := s.conn.RemoteAddr().String()
	s.srv.logger.Info("acp2 session accepted", slog.String("remote", remote))

	for {
		// Re-arm before every frame: any inbound AN2 traffic refreshes it,
		// so a consumer that is talking at all is never reaped.
		if err := s.idle.Arm(s.conn); err != nil {
			return
		}
		frame, err := codec.ReadAN2Frame(s.conn)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				s.srv.logger.Debug("acp2 session closed", slog.String("remote", remote))
				return
			}
			s.srv.metrics.ObserveDecodeError()
			s.srv.logger.Warn("acp2 session read error",
				slog.String("remote", remote),
				slog.String("err", err.Error()),
			)
			return
		}
		// Attributed by AN2 frame Type; the 8-byte header is not counted
		// in Payload, so add it back for a true on-wire byte count.
		s.srv.metrics.ObserveCmdRx(uint8(frame.Type), len(frame.Payload)+an2HeaderBytes)
		s.handleFrame(frame)
	}
}

// handleFrame dispatches one incoming AN2 frame via handlers.go.
func (s *session) handleFrame(f *codec.AN2Frame) {
	s.dispatch(f)
}
