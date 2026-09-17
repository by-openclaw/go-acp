package acp1

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"dhs/internal/acp1/codec"
	"dhs/internal/transport"
)

// Spec ceilings: ACP1 messages are at most ~141 bytes including the
// 7-byte header. Pad up to 1024 to leave room for future evolutions and
// catch malformed streams cheaply rather than blowing the socket open.
const (
	tcpMaxFrameBytes = 1024

	// MLEN floor per spec §"ACP Header" p.10: a valid TCP-mode message
	// has MLEN > 8. We still accept 8 (one-byte error reply edge case).
	tcpMinMLEN = 8

	// Max concurrent sessions per source IP. DoS guard - real
	// controllers only ever open one session per provider; anything
	// beyond this threshold is either misbehaving or hostile.
	tcpMaxSessionsPerIP = 32

	// Per-session send buffer. Bounded so a slow consumer cannot
	// pin announce traffic for the whole server. Drop on full + log.
	tcpSendChanCap = 256
)

// ServeTCP runs the ACP1 Mode B (TCP direct) listener. Each accepted
// connection becomes a session with its own reader/writer goroutine.
// Replies and announces multiplex on the same socket per spec §10.
//
// Concurrency model differs from UDP:
//   - One acceptor goroutine.
//   - Per-session: one reader, one writer (single send goroutine fed
//     from a bounded channel). handleRequest is called inline from the
//     reader; replies + announces are queued on the per-session
//     channel. The writer drains the channel and writes MLEN-framed
//     payloads to the socket.
//   - Server-wide: a fan-out registry forwards every announce produced
//     by any session (or the broadcast path) to every active TCP
//     session's channel.
//
// Stops cleanly when ctx is cancelled. ErrClosed is treated as a
// successful shutdown.
func (s *server) ServeTCP(ctx context.Context, addr string) error {
	// tcp4 preserved: ACP1 is IPv4-only. ListenTCPRaw rather than ListenTCP
	// because the session, registry, writer and frame-reader signatures below
	// all take *net.TCPConn, which only AcceptTCP provides. The socket policy
	// is applied per accepted connection further down, where an injected
	// listener is covered too.
	ln, err := transport.ListenTCPRaw(ctx, "tcp4", addr)
	if err != nil {
		return fmt.Errorf("acp1 provider tcp: listen %q: %w", addr, err)
	}
	defer func() { _ = ln.Close() }()

	s.logger.Info("acp1 provider tcp listening", slog.String("addr", ln.Addr().String()))

	reg := newTCPSessionRegistry(s.logger)

	// Goroutine to fan announces produced by the existing UDP path
	// into every active TCP session. Hooked through s.broadcastAnnounce
	// indirectly via the announceTap (set on the server below).
	s.tcpRegistry = reg

	// Stop the listener when ctx fires; unblocks Accept.
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	accept := ln.AcceptTCP
	if s.acceptTCPHook != nil {
		accept = func() (*net.TCPConn, error) { return s.acceptTCPHook(ln) }
	}
	for {
		conn, err := accept()
		if err != nil {
			if isClosed(err) || errors.Is(ctx.Err(), context.Canceled) {
				return nil
			}
			s.logger.Warn("acp1 tcp accept failed", slog.String("err", err.Error()))
			continue
		}
		// NoDelay: ACP1 messages are <=141 bytes and latency-sensitive.
		// Keepalive: the OS-level dead-peer probe, without which a half-open
		// client session holds a goroutine and a socket here for ever.
		_ = transport.ApplySocketOptions(conn, transport.SocketOptions{NoDelay: true})
		ip := remoteIP(conn.RemoteAddr())
		if !reg.tryAdd(ip) {
			s.logger.Warn("acp1 tcp refusing session: per-ip cap exceeded",
				slog.String("ip", ip),
				slog.Int("max", tcpMaxSessionsPerIP))
			_ = conn.Close()
			continue
		}
		go s.serveTCPSession(ctx, conn, ip, reg)
	}
}

// serveTCPSession runs reader+writer for one accepted connection. Reads
// one MLEN-framed message at a time, dispatches via handleRequest,
// pushes reply (and announce, if any) to the per-session send channel.
// The writer goroutine pulls from the channel and writes back.
func (s *server) serveTCPSession(ctx context.Context, conn *net.TCPConn, ip string, reg *tcpSessionRegistry) {
	send := make(chan []byte, tcpSendChanCap)
	sess := reg.register(ip, conn, send)
	defer func() {
		_ = conn.Close()
		reg.remove(ip, sess.id)
	}()

	// Writer
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		s.runTCPWriter(ctx, conn, send)
	}()

	// Reader
	for {
		payload, err := readMLENFrame(conn)
		if err != nil {
			if isClosed(err) || errors.Is(err, io.EOF) {
				break
			}
			s.logger.Warn("acp1 tcp read", slog.String("err", err.Error()))
			break
		}
		req, err := codec.Decode(payload)
		if err != nil {
			s.logger.Warn("acp1 tcp decode", slog.String("err", err.Error()))
			continue
		}
		reply, ann := s.handleRequest(req)
		if reply != nil {
			b, err := reply.Encode()
			if err == nil {
				enqueue(sess.send, b, "reply", s.logger)
			}
		}
		if ann != nil {
			// Route through broadcastAnnounceSkip so the Broadcasts gate
			// (#257) and UDP+AN2 bridges all apply uniformly. Pass this
			// session's id so the announce is NOT pushed back on the
			// same socket the request arrived on — strict ACP1 Mode B
			// peers (VSM Studio) RST when they see a server-initiated
			// MTID=0 frame between their reply and their next request.
			s.broadcastAnnounceSkip(ann, sess.id)
		}
	}

	close(send)
	<-writerDone
}

// runTCPWriter pumps the per-session send channel onto the socket. Extracted
// from serveTCPSession so the writer's ctx-exit, channel-close, and
// write-error arms are directly testable. Returns when ctx is cancelled, the
// channel closes, or a write fails.
func (s *server) runTCPWriter(ctx context.Context, conn *net.TCPConn, send chan []byte) {
	for {
		select {
		case <-ctx.Done():
			return
		case payload, ok := <-send:
			if !ok {
				return
			}
			if err := writeMLENFrame(conn, payload); err != nil {
				if !isClosed(err) {
					s.logger.Warn("acp1 tcp write", slog.String("err", err.Error()))
				}
				return
			}
		}
	}
}

// broadcastTCPAnnounce sends an already-encoded announce to every TCP
// session except the one identified by skipID. Pass skipID=0 to fan
// out to every session (UDP-bridged announces have no originator). The
// originating TCP session is excluded so strict peers don't see a
// server-pushed MTID=0 frame between their own reply and their next
// request.
func (s *server) broadcastTCPAnnounce(b []byte, skipID uint64) {
	if s.tcpRegistry == nil {
		return
	}
	s.tcpRegistry.broadcast(b, skipID)
}

// tcpSessionRegistry tracks live TCP sessions with per-IP caps and
// announce fan-out. Methods are safe for concurrent access.
type tcpSessionRegistry struct {
	logger *slog.Logger

	mu       sync.RWMutex
	sessions map[uint64]*tcpSession
	perIP    map[string]int
}

type tcpSession struct {
	id   uint64
	ip   string
	conn *net.TCPConn
	send chan []byte
}

func newTCPSessionRegistry(logger *slog.Logger) *tcpSessionRegistry {
	if logger == nil {
		logger = slog.Default()
	}
	return &tcpSessionRegistry{
		logger:   logger,
		sessions: map[uint64]*tcpSession{},
		perIP:    map[string]int{},
	}
}

// tryAdd reserves a per-IP slot. Returns false if the IP has already
// hit tcpMaxSessionsPerIP. Caller must call remove when the session
// closes (whether tryAdd succeeded or not is irrelevant — remove is a
// no-op for absent sessions).
func (r *tcpSessionRegistry) tryAdd(ip string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.perIP[ip] >= tcpMaxSessionsPerIP {
		return false
	}
	r.perIP[ip]++
	return true
}

func (r *tcpSessionRegistry) register(ip string, conn *net.TCPConn, send chan []byte) *tcpSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := sessionID(conn)
	sess := &tcpSession{id: id, ip: ip, conn: conn, send: send}
	r.sessions[id] = sess
	return sess
}

func (r *tcpSessionRegistry) remove(ip string, id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.perIP[ip] > 0 {
		r.perIP[ip]--
	}
	delete(r.sessions, id)
}

// broadcast pushes payload onto every live session's send channel,
// skipping the session whose id matches skipID. The originating
// session has already received its correlated reply, so re-pushing the
// announce on the same socket interleaves a server-initiated MTID=0
// frame between the client's reply and its next request — strict
// peers (e.g. VSM Studio) RST when they see that pattern. Pass
// skipID=0 (no real session ever has id 0) to fan out to every
// session.
//
// Drop-on-full per session — never block one slow consumer behind
// another.
func (r *tcpSessionRegistry) broadcast(payload []byte, skipID uint64) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for id, sess := range r.sessions {
		if id == skipID {
			continue
		}
		select {
		case sess.send <- payload:
		default:
			r.logger.Warn("acp1 tcp broadcast drop (slow consumer)",
				slog.String("ip", sess.ip))
		}
	}
}

// activeSessions returns a count snapshot; useful for tests.
func (r *tcpSessionRegistry) activeSessions() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.sessions)
}

func enqueue(ch chan []byte, payload []byte, kind string, logger *slog.Logger) {
	select {
	case ch <- payload:
	default:
		logger.Warn("acp1 tcp send drop", slog.String("kind", kind))
	}
}

func writeMLENFrame(conn *net.TCPConn, payload []byte) error {
	if len(payload) > tcpMaxFrameBytes {
		return fmt.Errorf("acp1 tcp: outbound frame %d > max %d", len(payload), tcpMaxFrameBytes)
	}
	frame := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(payload)))
	copy(frame[4:], payload)
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := conn.Write(frame)
	return err
}

func readMLENFrame(conn *net.TCPConn) ([]byte, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		return nil, err
	}
	mlen := binary.BigEndian.Uint32(lenBuf[:])
	if mlen < tcpMinMLEN {
		return nil, fmt.Errorf("acp1 tcp: MLEN %d below minimum %d", mlen, tcpMinMLEN)
	}
	if int(mlen) > tcpMaxFrameBytes {
		return nil, fmt.Errorf("acp1 tcp: MLEN %d > max %d", mlen, tcpMaxFrameBytes)
	}
	payload := make([]byte, mlen)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// remoteIP extracts the IP portion of a TCP RemoteAddr, dropping the
// port and any IPv6 zone. Used as the per-IP cap key.
func remoteIP(addr net.Addr) string {
	host := addr.String()
	if idx := strings.LastIndex(host, ":"); idx >= 0 {
		host = host[:idx]
	}
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	return host
}

// sessionIDCounter is the monotonic source for session keys.
// Cheaper than reflecting pointer addresses and stable under GC.
var sessionIDCounter uint64

var sessionIDMu sync.Mutex

func sessionID(_ *net.TCPConn) uint64 {
	sessionIDMu.Lock()
	defer sessionIDMu.Unlock()
	sessionIDCounter++
	return sessionIDCounter
}

func isClosed(err error) bool {
	return errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "use of closed")
}
