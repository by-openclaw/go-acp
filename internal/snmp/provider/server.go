package provider

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"dhs/internal/plugin"
	"dhs/internal/provider"
	"dhs/internal/snmp/codec"
	"dhs/internal/transport"
)

// DefaultPort is where a manager looks for an agent. It is a default and
// not an assumption: Cerebrum's own agent answers on 1161 and vendor
// devices vary, so every caller can override it and the CLI does.
const DefaultPort = 161

// Server is the agent on a UDP socket.
//
// One socket, one read loop, each datagram dispatched inline. SNMP
// requests are small and every answer comes out of an in-process tree,
// so a serial handler is well inside budget — and a serial handler is
// also what keeps the responses in the order the requests arrived, which
// a manager correlating by request-id does not require but an operator
// reading a capture very much wants.
//
// The session type is [provider.NoConn]: there is no per-connection
// state on a datagram protocol, so the connection set stays empty and
// Base contributes the bind, the stop sequence and the counters.
type Server struct {
	provider.Base[*provider.NoConn]

	agent  *Agent
	logger *slog.Logger

	mu   sync.Mutex
	conn *net.UDPConn
}

// NewServer builds the agent's server. deps carries the transport the
// socket is bound through, the clock and the metrics connector, so
// nothing here reaches for a global.
func NewServer(mib *MIB, communities Communities, deps plugin.Deps) *Server {
	deps = deps.WithDefaults()
	s := &Server{
		agent:  NewAgent(mib, communities, deps.Logger),
		logger: plugin.LoggerOrDefault(deps.Logger),
	}
	s.Init(deps)
	return s
}

// Agent exposes the request handler, so a caller can answer a datagram
// it obtained some other way — a test, or a transport this package does
// not own.
func (s *Server) Agent() *Agent { return s.agent }

// Serve binds addr and answers until ctx is done or Stop is called.
//
// SO_REUSEADDR is set: an agent and a manager frequently share a host in
// this lab, and 161 is a well-known port that something else may already
// hold. Without it a dev rig cannot bind at all.
func (s *Server) Serve(ctx context.Context, addr string) error {
	conn, err := s.ListenUDP(ctx, "udp4", addr, transport.UDPBindOptions{ReuseAddr: true})
	if err != nil {
		return fmt.Errorf("snmp agent: listen %q: %w", addr, err)
	}
	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()

	s.logger.Info("snmp agent listening",
		slog.String("addr", conn.LocalAddr().String()),
		slog.Int("objects", s.agent.mib.Len()))

	// Closing the socket is what unblocks the read below, so ctx has to
	// reach it. Stop is idempotent, so a later explicit Stop is a no-op.
	go func() {
		<-ctx.Done()
		_ = s.Stop()
	}()

	return shutdownIsNotAFailure(s.readLoop(conn))
}

// shutdownIsNotAFailure translates the read loop's exit.
//
// Closing the socket is HOW this server stops, so the error that ends
// the loop on a clean shutdown is net.ErrClosed. Reporting that to a
// supervisor would mean every ordinary stop looked like a crash.
func shutdownIsNotAFailure(err error) error {
	if errors.Is(err, net.ErrClosed) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// packetConn is the socket readLoop drives. *net.UDPConn satisfies it.
//
// It exists so the reply path can be exercised: a write to a manager
// that has gone away is the ordinary case on a datagram protocol — the
// poller was restarted, the route changed — and an agent that stopped
// serving everybody else over one of them would be a worse agent than
// one that never answered at all. There is no way to make a real socket
// refuse one write on demand.
type packetConn interface {
	ReadFromUDP(b []byte) (int, *net.UDPAddr, error)
	WriteToUDP(b []byte, addr *net.UDPAddr) (int, error)
}

// readLoop is the socket half, kept apart from Serve so the bind and the
// dispatch can be read separately.
func (s *Server) readLoop(conn packetConn) error {
	buf := make([]byte, codec.MaxMessageSize)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			return err
		}
		s.Metrics().ObserveRx(n)

		started := s.Clock().Now()
		reply, ok := s.handle(buf[:n], from)
		if !ok {
			continue
		}
		if _, err := conn.WriteToUDP(reply, from); err != nil {
			// A write that fails is this one manager's problem, not the
			// agent's: the socket is still good for everybody else.
			s.logger.Warn("snmp agent: reply failed",
				slog.String("to", from.String()), slog.String("err", err.Error()))
			continue
		}
		s.Metrics().ObserveTx(len(reply), s.Clock().Now().Sub(started))
	}
}

// handle turns one datagram into the datagram that answers it, or into
// nothing at all.
//
// A datagram that will not decode is COUNTED and dropped, never
// answered. An agent that replied to malformed input with an error would
// be a reflection amplifier on a port that takes traffic from anybody.
func (s *Server) handle(raw []byte, from *net.UDPAddr) ([]byte, bool) {
	req, err := codec.Decode(raw)
	if err != nil {
		s.Metrics().ObserveDecodeError()
		s.logger.Debug("snmp agent: undecodable datagram",
			slog.String("from", from.String()), slog.String("err", err.Error()))
		return nil, false
	}

	// The agent hands back the datagram it already encoded, so the
	// reply path makes one pass over the varbinds rather than two.
	_, out, send := s.agent.Respond(req)
	return out, send
}

// Addr is the address the agent is bound to, or nil before Serve.
//
// It overrides Base.Addr, which reports the TCP LISTENER and so answers
// nil for every packet provider in the tree. Without this a caller — or
// a test — that binds ":0" has no way to learn which port the OS chose,
// which is the one thing Addr exists for.
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return nil
	}
	return s.conn.LocalAddr()
}

// Stop closes the socket and clears it. Idempotent; Base does the rest.
func (s *Server) Stop() error {
	s.mu.Lock()
	s.conn = nil
	s.mu.Unlock()
	return s.Base.Stop()
}
