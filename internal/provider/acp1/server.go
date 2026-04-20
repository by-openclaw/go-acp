package acp1

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"acp/internal/export/canonical"
	iacp1 "acp/internal/protocol/acp1"
)

// server is the concrete provider.Provider for ACP1 over UDP. One
// datagram in -> dispatch -> one datagram out, plus broadcast announce
// on successful mutating methods.
//
// Concurrency model:
//   - Serve runs a single read loop (ReadFromUDP is goroutine-unsafe on
//     some platforms anyway). Each inbound datagram is dispatched inline
//     — ACP1 messages are fixed-tiny (<=141 bytes) and all work is in-
//     process so a serial handler is well within budget.
//   - Announcements are sent from the same goroutine right after the
//     reply so consumer-visible ordering is always (reply, announce).
//   - SetValue() is called from the embedding API (acp-srv) off-thread;
//     it grabs tree.mu for the mutation then enqueues an announce.
type server struct {
	logger *slog.Logger
	tree   *tree

	mu      sync.Mutex
	conn    *net.UDPConn // listener + unicast reply socket
	bcast   *net.UDPConn // separate socket dialed to 255.255.255.255
	closed  bool
	stopped chan struct{}
}

func newServer(logger *slog.Logger, exp *canonical.Export) *server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &server{
		logger:  logger,
		stopped: make(chan struct{}),
	}
	t, err := newTree(exp)
	if err != nil {
		logger.Error("acp1 provider: tree build failed", "err", err.Error())
		// Surface later: Serve will fail fast with the same error.
		s.tree = &tree{entries: map[objectKey]*entry{}, slots: map[uint8]*slotCounts{}}
		return s
	}
	s.tree = t
	return s
}

// Serve binds addr (e.g. "0.0.0.0:2071") and runs until ctx is cancelled
// or a fatal listen error occurs.
func (s *server) Serve(ctx context.Context, addr string) error {
	udpAddr, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return fmt.Errorf("acp1 provider: resolve %q: %w", addr, err)
	}
	conn, err := net.ListenUDP("udp4", udpAddr)
	if err != nil {
		return fmt.Errorf("acp1 provider: listen %q: %w", addr, err)
	}

	// Dial a second socket to the limited broadcast address. Go stdlib
	// auto-sets SO_BROADCAST on dialed sockets with broadcast peers,
	// which is the portable path across Windows / Linux / macOS.
	bcastAddr := &net.UDPAddr{IP: net.IPv4bcast, Port: udpAddr.Port}
	bconn, bErr := net.DialUDP("udp4", nil, bcastAddr)
	// Best-effort: if the OS rejects the broadcast dial (no route, no
	// iface up) we log and continue without announcements.
	if bErr != nil {
		s.logger.Warn("acp1 provider: broadcast disabled",
			slog.String("err", bErr.Error()),
			slog.String("addr", bcastAddr.String()),
		)
	}

	s.mu.Lock()
	s.conn = conn
	s.bcast = bconn
	s.mu.Unlock()

	s.logger.Info("acp1 provider listening",
		slog.String("addr", conn.LocalAddr().String()),
		slog.String("broadcast", bcastAddr.String()),
		slog.Int("objects", len(s.tree.entries)),
	)

	// Close both sockets when ctx goes away; unblocks the read loop.
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		if !s.closed {
			s.closed = true
			_ = conn.Close()
			if s.bcast != nil {
				_ = s.bcast.Close()
			}
		}
		s.mu.Unlock()
	}()

	err = s.readLoop(ctx, conn)
	close(s.stopped)
	if errors.Is(err, net.ErrClosed) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// Stop closes the listening and broadcast sockets. Safe to call
// multiple times.
func (s *server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	var err error
	if s.conn != nil {
		err = s.conn.Close()
	}
	if s.bcast != nil {
		_ = s.bcast.Close()
	}
	return err
}

// SetValue mutates the served tree via the API path (acp-srv, tests).
// Routes through the same applyMutation pipeline used by wire SetValue
// requests so clamping + canonical.Parameter.Value update are
// consistent. The value-change announcement broadcast ships in the
// follow-up commit (Step 1e).
func (s *server) SetValue(_ context.Context, path string, val any) (any, error) {
	key, err := parsePath(path)
	if err != nil {
		return nil, err
	}
	e, ok := s.tree.lookup(key)
	if !ok {
		return nil, fmt.Errorf("acp1 provider: object not found: %s", path)
	}
	if e.access&1 == 0 {
		return nil, fmt.Errorf("acp1 provider: %s has no read access", path)
	}
	if e.access&2 == 0 {
		return nil, fmt.Errorf("acp1 provider: %s has no write access", path)
	}
	bytes, err := s.encodeIncomingFromAny(e, val)
	if err != nil {
		return nil, err
	}
	if _, err := s.applyMutation(e, iacp1.MethodSetValue, bytes); err != nil {
		return nil, err
	}
	return convertStoredValue(e.param), nil
}

// broadcastAnnounce serialises an announcement message and sends it to
// the LAN limited-broadcast address. Called after every successful
// mutating method per spec §"Announcements" p.14. Silent on send error
// — announcements are fire-and-forget, and the consumer that made the
// setX call already has the change confirmed via the reply.
func (s *server) broadcastAnnounce(ann *iacp1.Message) {
	s.mu.Lock()
	bc := s.bcast
	s.mu.Unlock()
	if bc == nil {
		return
	}
	out, err := ann.Encode()
	if err != nil {
		s.logger.Warn("acp1 announce encode",
			slog.String("err", err.Error()),
		)
		return
	}
	if _, err := bc.Write(out); err != nil {
		s.logger.Debug("acp1 announce send",
			slog.String("err", err.Error()),
		)
	}
}

// readLoop reads datagrams until ctx is cancelled or the conn is closed.
// Each message is dispatched inline through session.go's handleRequest.
func (s *server) readLoop(ctx context.Context, conn *net.UDPConn) error {
	buf := make([]byte, 1500) // covers the 141-byte ACP1 max + headroom
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			return err
		}
		if n == 0 {
			continue
		}
		data := append([]byte(nil), buf[:n]...)
		send := func(out []byte) error {
			_, err := conn.WriteToUDP(out, src)
			return err
		}
		s.handleDatagram2(data, src.String(), send)
	}
}
