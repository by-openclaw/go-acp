package acp1

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"dhs/internal/acp1/codec"
	"dhs/internal/devicemodel"
	"dhs/internal/export/canonical"
	"dhs/internal/transport"
)

// Server is the exported alias for the concrete ACP1 provider — lets
// cmd/acp-provider reach protocol-specific helpers (e.g. RunAnnounceDemo)
// via a type assertion without widening the cross-protocol
// provider.Provider interface. Mirrors the pattern used in acp2.
type Server = server

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

	// tcpRegistry is set when ServeTCP runs. Announces emitted via the
	// UDP broadcast path are also fanned to every live TCP session so
	// TCP-only consumers see value-change events.
	tcpRegistry *tcpSessionRegistry

	// an2Registry is set when ServeAN2 runs. Announces emitted via the
	// UDP / TCP broadcast paths are also wrapped in AN2 frames and
	// fanned to AN2 sessions that have called EnableProtocolEvents.
	an2Registry *an2SessionRegistry

	// slotMachine tracks pending insert/extract cascades per slot.
	// Initialised by newServer with InsertTimingReal; the producer CLI
	// can override via --insert-timing.
	slotMachine *slotStateMachine

	// dmLibrary, when non-nil, drives slot.load via the DM-library
	// resolver. Set via SetDMLibrary at startup.
	dmLibrary devicemodel.Resolver

	// adminListenHook / adminWriteDiscoveryHook are test-only injection
	// seams for ServeAdmin so its listen-failure, discovery-write-failure,
	// and accept-failure arms (all OS-syscall failures unreachable on a
	// healthy loopback) are deterministically testable. Nil in production.
	adminListenHook         func() (net.Listener, error)
	adminWriteDiscoveryHook func(path string, d *AdminDiscovery) error

	// preReadHook runs just before Serve enters readLoop. Test-only: a test
	// sets a past read deadline on conn so readLoop's first ReadFromUDP
	// returns a non-closed i/o-timeout error, exercising Serve's hard-error
	// return arm. Nil in production.
	preReadHook func(*net.UDPConn)

	// bcastDialErrHook, when it returns true, forces Serve's broadcast dial
	// to be treated as failed (driving the broadcast-disabled warn arm).
	// Test-only: a real broadcast dial almost never fails on a healthy host.
	bcastDialErrHook func() bool

	// acceptTCPHook, when non-nil, replaces ln.AcceptTCP in the ServeTCP /
	// ServeAN2 accept loops. Test-only injection seam: the accept-error warn
	// arm (a non-closed Accept failure while the context is live) is
	// otherwise only reachable under OS resource exhaustion. Production
	// leaves it nil.
	acceptTCPHook func(*net.TCPListener) (*net.TCPConn, error)

	// oscillatableTargetsHook, when non-nil, overrides the target list in
	// RunStatusPlayAll. Test-only injection seam (see play.go) — production
	// leaves it nil.
	oscillatableTargetsHook func() []objectKey

	// setStatusHook, when non-nil, replaces setSlotStatus in the cascade /
	// play paths. Test-only injection seam: the multi-phase cascade and
	// frame-status play guards (a setSlotStatus failure on the 2nd/3rd
	// phase) can only otherwise be hit by a concurrent frame mutation
	// race. Production leaves this nil → setStatus calls setSlotStatus.
	setStatusHook func(slot, state uint8) error
}

// setStatus dispatches to the injected hook (tests) or the real
// setSlotStatus (production). The cascade and frame-status-play paths route
// through here so their later-phase failure guards are deterministically
// reachable.
func (s *server) setStatus(slot, state uint8) error {
	if s.setStatusHook != nil {
		return s.setStatusHook(slot, state)
	}
	return s.setSlotStatus(slot, state)
}

// errBroadcastDialForced is the synthetic error the bcastDialErrHook injects
// (test-only) to drive Serve's broadcast-disabled warn path.
var errBroadcastDialForced = errors.New("acp1 provider: broadcast dial forced-failed (test)")

// SetInsertTiming switches the cascade timing for new transitions.
// Existing in-flight cascades keep their starting timing.
func (s *server) SetInsertTiming(t InsertTiming) {
	if s.slotMachine != nil {
		s.slotMachine.SetTiming(t)
	}
}

func newServer(logger *slog.Logger, exp *canonical.Export) *server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &server{
		logger:      logger,
		stopped:     make(chan struct{}),
		slotMachine: newSlotStateMachine(InsertTimingReal),
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
//
// SO_REUSEADDR is set so the listener coexists with ACP1 clients on
// the same host that also bind 2071 to receive broadcast announces
// (this is how Cerebrum, VSM Studio, SynapseSetUp and other Axon
// controllers operate per ACP1 §"Announcements" p.14). Real Axon
// racks are dedicated devices on their own IP, so the spec is silent
// on SO_REUSEADDR — but emulators and dev rigs running on the same
// host as those controllers must use it or fail to bind.
func (s *server) Serve(ctx context.Context, addr string) error {
	udpAddr, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return fmt.Errorf("acp1 provider: resolve %q: %w", addr, err)
	}
	// SO_REUSEADDR is set before bind — the only window in which it takes
	// effect — so an emulator or dev rig can share the port with a
	// controller already listening on it. The type assertion that used to
	// sit here now lives in transport, tested once.
	conn, err := transport.ListenUDPAddr(ctx, "udp4", udpAddr.String(),
		transport.UDPBindOptions{ReuseAddr: true})
	if err != nil {
		return fmt.Errorf("acp1 provider: listen %q: %w", addr, err)
	}

	// Dial a second socket to the limited broadcast address. Go stdlib
	// auto-sets SO_BROADCAST on dialed sockets with broadcast peers,
	// which is the portable path across Windows / Linux / macOS.
	//
	// VIP-aware source selection (#263): when --bind <ip> selects a
	// specific local address, we pin the broadcast dial's LocalAddr
	// to it so the kernel routes via the matching iface and consumers
	// see the right `From:` per emulator. Without this, multiple
	// emulators on different VIPs would share whichever IP the kernel
	// picks for the route, collapsing the per-emulator distinction.
	bcastAddr := &net.UDPAddr{IP: net.IPv4bcast, Port: udpAddr.Port}
	var localAddr *net.UDPAddr
	if udpAddr.IP != nil && !udpAddr.IP.IsUnspecified() {
		localAddr = &net.UDPAddr{IP: udpAddr.IP, Port: 0}
	}
	bconn, bErr := net.DialUDP("udp4", localAddr, bcastAddr)
	if s.bcastDialErrHook != nil && s.bcastDialErrHook() {
		if bconn != nil {
			_ = bconn.Close()
		}
		bconn, bErr = nil, errBroadcastDialForced
	}
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

	if s.preReadHook != nil {
		s.preReadHook(conn)
	}
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
	if _, err := s.applyMutation(e, codec.MethodSetValue, bytes); err != nil {
		return nil, err
	}
	return convertStoredValue(e.param), nil
}

// broadcastAnnounce serialises an announcement message and sends it to
// the LAN limited-broadcast address. Called after every successful
// mutating method per spec §"Announcements" p.14. Silent on send error
// — announcements are fire-and-forget, and the consumer that made the
// setX call already has the change confirmed via the reply.
//
// Gated by the Broadcasts field (slot 0 / control / id 4): when the
// served tree has Broadcasts=Off, every spontaneous announce is
// suppressed. Replies to active requests stay on regardless — only
// this path is gated. (#257)
func (s *server) broadcastAnnounce(ann *codec.Message) {
	s.broadcastAnnounceSkip(ann, 0)
}

// broadcastAnnounceSkip is the TCP-session-aware variant. skipTCPSessionID
// names the TCP session that originated the change so the announce is
// NOT echoed back on that same socket — strict peers (e.g. VSM Studio)
// RST when they see a server-pushed MTID=0 frame between their own
// reply and their next request. UDP / AN2 / admin / demo callers pass 0.
func (s *server) broadcastAnnounceSkip(ann *codec.Message, skipTCPSessionID uint64) {
	if !s.tree.broadcastsEnabled() {
		s.logger.Debug("acp1 announce gated by Broadcasts=Off",
			slog.Int("objgroup", int(ann.ObjGroup)),
			slog.Int("objid", int(ann.ObjID)),
		)
		return
	}
	out, err := ann.Encode()
	if err != nil {
		s.logger.Warn("acp1 announce encode",
			slog.String("err", err.Error()),
		)
		return
	}
	// Fan-out to every live TCP and AN2 session. Standalone TCP/AN2
	// providers (no UDP listener) still announce here.
	s.broadcastTCPAnnounce(out, skipTCPSessionID)
	s.broadcastAN2Announce(out)

	s.mu.Lock()
	bc := s.bcast
	closed := s.closed
	s.mu.Unlock()
	if bc == nil || closed {
		// No UDP listener, or we're shutting down. Skipping a broadcast on a
		// closing socket keeps Ctrl-C output clean — without this, every
		// in-flight --play tick would spray a Warn as its write races the
		// socket close.
		return
	}
	if _, err := bc.Write(out); err != nil {
		// A closed socket during shutdown is expected, not a fault.
		if errors.Is(err, net.ErrClosed) {
			s.logger.Debug("acp1 announce skipped: socket closed during shutdown")
			return
		}
		s.logger.Warn("acp1 announce send",
			slog.String("err", err.Error()),
			slog.String("dst", bc.RemoteAddr().String()),
		)
		return
	}
	s.logger.Info("acp1 announce broadcast",
		slog.Uint64("mtid", uint64(ann.MTID)),
		slog.Int("mtype", int(ann.MType)),
		slog.Int("mcode", int(ann.MCode)),
		slog.Int("objgroup", int(ann.ObjGroup)),
		slog.Int("objid", int(ann.ObjID)),
		slog.Int("maddr", int(ann.MAddr)),
		slog.Int("bytes", len(out)),
		slog.String("dst", bc.RemoteAddr().String()),
	)
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
