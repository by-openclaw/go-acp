package probelsw08p

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"acp/internal/probel-sw08p/codec"
)

// session is one connected SW-P-08 client (typically a controller).
// It owns the TCP socket, accumulates bytes, feeds complete frames to
// the server's command dispatcher, and serialises writes via writeMu.
//
// Frames are decoded + ACK'd on the read goroutine, then handed to
// a dedicated dispatcher goroutine via dispatchCh. The dispatcher
// serialises handlers per-session (so reply order is preserved) while
// freeing the read loop to keep pulling bytes off the socket. Without
// this split the read loop would block on tree-mutex contention during
// the connect-phase of the scale bench, producing multi-second tail
// latency (see memory/project_scale_bench_results_2026_04_22.md).
type session struct {
	srv  *server
	conn net.Conn

	writeMu sync.Mutex
	closeMu sync.Mutex
	closed  bool

	dispatchCh chan pendingFrame
	dispatchWG sync.WaitGroup
}

// pendingFrame carries everything dispatch() needs once the read loop
// has moved on. rxAt is captured before the ACK write so end-to-end
// latency measurement matches what the client sees.
type pendingFrame struct {
	f    codec.Frame
	rxAt time.Time
	n    int // wire length, for metrics
}

// dispatchChanCap bounds the per-session backlog between the read loop
// and the dispatcher. 256 frames is one ACP2 announce burst on the
// Probel side (§3.2.29) and absorbs a full salvo-fire staging round
// without the read loop having to block. If this ever fills up
// repeatedly it means the handler is the bottleneck — S2 (lock split)
// is the follow-up.
const dispatchChanCap = 256

func newSession(srv *server, conn net.Conn) *session {
	return &session{
		srv:        srv,
		conn:       conn,
		dispatchCh: make(chan pendingFrame, dispatchChanCap),
	}
}

// run reads frames until EOF or ctx cancellation. Each frame is decoded,
// ACK'd, then enqueued for the dispatcher goroutine (started here).
// The read loop never waits on handler or fan-out work.
func (s *session) run(ctx context.Context) {
	defer func() {
		_ = s.conn.Close()
		// Signal dispatcher no more frames coming; drain anything queued
		// so replies to already-ACK'd frames still land.
		close(s.dispatchCh)
		s.dispatchWG.Wait()
	}()

	s.startDispatcher(ctx)

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
				s.srv.metrics.ObserveDecodeError()
				buf = buf[2:]
				continue
			}
			// Per-frame trace goes at Debug — 4 M Info calls with
			// HexDump formatting was shown to be the dominant cost in
			// the 10× scale bench (see project_scale_bench_results).
			// feedback_logging.md: skip announce logs entirely.
			if s.srv.logger.Enabled(context.Background(), slog.LevelDebug) {
				s.srv.logger.Debug("probel session rx",
					slog.String("remote", s.remoteAddr()),
					slog.Int("cmd", int(f.ID)),
					slog.Int("payload_len", len(f.Payload)),
					slog.Int("wire_len", consumed),
					slog.String("hex", codec.HexDump(buf[:consumed])),
				)
			}
			s.srv.metrics.ObserveCmdRx(uint8(f.ID), consumed)
			rxAt := time.Now()
			buf = buf[consumed:]
			// SW-P-08 §2 — always ACK a well-framed message, then
			// hand off to the dispatcher. The handler + fan-out run on
			// the dispatcher goroutine so the read loop stays hot.
			_ = s.write(codec.PackACK())
			select {
			case s.dispatchCh <- pendingFrame{f: f, rxAt: rxAt, n: consumed}:
			case <-ctx.Done():
				return
			}
		}
	}
}

// startDispatcher launches the per-session dispatcher goroutine. It
// consumes pendingFrame values in FIFO order so reply ordering within
// a session matches wire order — SW-P-08 controllers assume it.
func (s *session) startDispatcher(ctx context.Context) {
	s.dispatchWG.Add(1)
	go func() {
		defer s.dispatchWG.Done()
		defer func() {
			if r := recover(); r != nil {
				s.srv.logger.Error("probel dispatcher panic",
					slog.String("remote", s.remoteAddr()),
					slog.Any("recover", r),
				)
			}
		}()
		for pf := range s.dispatchCh {
			if err := ctx.Err(); err != nil {
				return
			}
			s.dispatch(pf.f, pf.rxAt)
		}
	}()
}

// dispatch routes a decoded frame to the server's command handler table
// (handlers.go), sends the reply (if any) back on the originating
// session, and fans tallies out to every OTHER live session. rxAt is
// the timestamp at which the incoming frame finished decoding; used
// to measure handler rx→tx turnaround for metrics.
func (s *session) dispatch(f codec.Frame, rxAt time.Time) {
	res, err := s.srv.handle(f)
	if err != nil {
		s.srv.logger.Warn("probel dispatch",
			slog.String("remote", s.remoteAddr()),
			slog.Int("cmd", int(f.ID)),
			slog.String("err", err.Error()),
		)
		s.srv.profile.Note(HandlerRejected)
		s.srv.metrics.ObserveDecodeError()
		return
	}
	if res.reply != nil {
		raw := codec.Pack(*res.reply)
		if s.srv.logger.Enabled(context.Background(), slog.LevelDebug) {
			s.srv.logger.Debug("probel session tx",
				slog.String("remote", s.remoteAddr()),
				slog.Int("cmd", int(res.reply.ID)),
				slog.Int("payload_len", len(res.reply.Payload)),
				slog.Int("wire_len", len(raw)),
				slog.String("hex", codec.HexDump(raw)),
			)
		}
		if werr := s.write(raw); werr != nil {
			s.srv.logger.Warn("probel session reply write",
				slog.String("remote", s.remoteAddr()),
				slog.String("err", werr.Error()))
		}
		s.srv.metrics.ObserveCmdTx(uint8(res.reply.ID), len(raw), time.Since(rxAt))
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
