package ccp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"dhs/internal/ccp/codec"
	"dhs/internal/metrics"
)

// session is one TCP connection to a panel (§1.0.0: the panel is the
// server, port 2071, at least two clients — Cerebrum can hold one while
// dhs holds another). Replies go only to the asker; button and GPI frames
// go to every client.
type session struct {
	conn    net.Conn
	writeMu sync.Mutex
	reqMu   sync.Mutex // one request in flight: replies carry no tag

	mu      sync.Mutex
	pending *pendingReq
	onEvent func(codec.Frame)
	onBad   func(error)
	done    chan struct{}
	closeMu sync.Once
	err     error
	m       *metrics.Connector
}

type pendingReq struct {
	cmd    byte
	expect byte
	ch     chan codec.Frame
}

func newSession(conn net.Conn, m *metrics.Connector, onEvent func(codec.Frame), onBad func(error)) *session {
	s := &session{conn: conn, onEvent: onEvent, onBad: onBad, done: make(chan struct{}), m: m}
	go s.readLoop()
	return s
}

// do sends one client command and waits for its reply: the command's own
// response, an Acknowledge, or an Error naming the command (§1.2.0, §1.3.3).
func (s *session) do(ctx context.Context, cmd []byte) (codec.Frame, error) {
	if len(cmd) == 0 {
		return codec.Frame{}, errors.New("ccp: empty command")
	}
	s.reqMu.Lock()
	defer s.reqMu.Unlock()
	p := &pendingReq{cmd: cmd[0], expect: codec.ExpectedReply(cmd[0]), ch: make(chan codec.Frame, 1)}
	s.mu.Lock()
	s.pending = p
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.pending = nil
		s.mu.Unlock()
	}()
	start := time.Now()
	if err := s.write(cmd); err != nil {
		return codec.Frame{}, err
	}
	select {
	case f := <-p.ch:
		if s.m != nil {
			s.m.ObserveTx(0, time.Since(start))
		}
		if f.Cmd == codec.RspError {
			pe, _ := codec.ParseError(f)
			if s.m != nil {
				s.m.ObserveNAK()
			}
			return f, pe
		}
		return f, nil
	case <-s.done:
		return codec.Frame{}, fmt.Errorf("ccp: connection closed: %v", s.err)
	case <-ctx.Done():
		if s.m != nil {
			s.m.ObserveTimeout()
		}
		return codec.Frame{}, fmt.Errorf("ccp: no reply to %s: %w", codec.CommandNames[cmd[0]], ctx.Err())
	}
}

func (s *session) write(b []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := s.conn.Write(b)
	if err == nil && s.m != nil {
		s.m.ObserveTx(len(b), 0)
	}
	return err
}

func (s *session) readLoop() {
	buf := make([]byte, 0, 8192)
	tmp := make([]byte, 4096)
	for {
		n, err := s.conn.Read(tmp)
		if n > 0 {
			if s.m != nil {
				s.m.ObserveRx(n)
			}
			buf = append(buf, tmp[:n]...)
			for {
				f, used, ferr := codec.Next(buf)
				if ferr != nil {
					// No length field: an unknown byte cannot be skipped
					// safely. Drop the rest of what was read and report.
					if s.m != nil {
						s.m.ObserveDecodeError()
					}
					if s.onBad != nil {
						s.onBad(ferr)
					}
					buf = buf[:0]
					break
				}
				if used == 0 {
					break
				}
				buf = buf[used:]
				s.route(f)
			}
			if len(buf) == 0 {
				buf = make([]byte, 0, 8192)
			}
		}
		if err != nil {
			s.close(err)
			return
		}
	}
}

// route hands a frame to the waiting request when it answers it, and to
// the event callback otherwise.
func (s *session) route(f codec.Frame) {
	s.mu.Lock()
	p := s.pending
	s.mu.Unlock()
	if p != nil {
		match := f.Cmd == p.expect
		if f.Cmd == codec.RspError && len(f.Data) == 3 && f.Data[0] != codec.ErrPSU && f.Data[1] == p.cmd {
			match = true
		}
		if match {
			select {
			case p.ch <- f:
			default:
			}
			return
		}
	}
	if s.onEvent != nil {
		s.onEvent(f)
	}
}

func (s *session) close(err error) {
	s.closeMu.Do(func() {
		s.err = err
		close(s.done)
		_ = s.conn.Close()
	})
}
