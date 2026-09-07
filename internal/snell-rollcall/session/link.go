package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"

	"dhs/internal/clock"
	"dhs/internal/metrics"
	"dhs/internal/plugin"
	"dhs/internal/snell-rollcall/codec"
)

// Link is one RollCall connection: a byte stream with frames on it, and the
// sessions multiplexed over it by index.
//
// It never opens a socket. A caller dials through the injected transport and
// hands the net.Conn over, which is what lets every test in this package run
// over net.Pipe with no port, no listener and no timing.
type Link struct {
	conn net.Conn
	cfg  Config
	log  *slog.Logger
	clk  clock.Clock
	met  *metrics.Connector

	// writeMu serialises writes. Frames are small and written whole, so a
	// mutex is cheaper and clearer than a writer goroutine with a queue.
	writeMu sync.Mutex

	mu       sync.RWMutex
	local    codec.Address // our address, learned from the gateway if stamped
	remote   codec.Address // the peer at the other end, learned by Handshake
	sessions map[int16]*Session
	nextIdx  int16
	closed   bool
	closeErr error

	// blind carries replies to traffic sent outside any session: the first
	// GETDEVINFO, keepalives before a session exists, blind control on index
	// zero. It is a channel like any other, so those requests obey the same
	// one-in-flight rule.
	blind *channel

	// unsolicited receives frames addressed to no session and answering no
	// request, such as an Iam announcement from a peer.
	unsolicited chan codec.Frame

	done     chan struct{}
	doneOnce sync.Once
	wg       sync.WaitGroup

	rxFrames atomic.Uint64
	txFrames atomic.Uint64
	resyncs  atomic.Uint64
}

// NewLink wraps an established connection.
//
// The caller owns the dial and keeps ownership of nothing else: Close shuts
// the connection down. Deps supplies the logger, clock and metrics, so a Link
// cannot read the wall clock or invent a counter set of its own.
func NewLink(conn net.Conn, cfg Config, deps plugin.Deps) *Link {
	deps = deps.WithDefaults()

	l := &Link{
		conn:        conn,
		cfg:         cfg,
		log:         deps.Logger,
		clk:         deps.Clock,
		met:         deps.Metrics,
		sessions:    make(map[int16]*Session),
		nextIdx:     1,
		unsolicited: make(chan codec.Frame, 16),
		done:        make(chan struct{}),
		local: codec.Address{
			Net:   cfg.Local.Net,
			Unit:  cfg.Local.Unit,
			Port:  cfg.Local.Port,
			Index: codec.IndexUnknown,
		},
		// Until the handshake names the peer, the only address we can reach
		// it on is the broadcast one, which every unit on the segment
		// answers to.
		remote: codec.Broadcast(),
	}
	l.blind = newChannel("blind", l.clk, cfg.replyTimeout(), cfg.maxStrikes())

	l.wg.Add(1)
	go l.readLoop()
	return l
}

// LocalAddress returns our address on this link.
//
// A TCP client starts with zeros and learns its real address from the first
// reply a gateway stamps, so this changes once early in a link's life and then
// stays put.
func (l *Link) LocalAddress() codec.Address {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.local
}

// SetLocalAddress records the address a gateway assigned us.
func (l *Link) SetLocalAddress(a codec.Address) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.local.Net, l.local.Unit, l.local.Port = a.Net, a.Unit, a.Port
}

// RemoteAddress returns the peer at the other end of the link, which is the
// broadcast address until Handshake learns the real one.
func (l *Link) RemoteAddress() codec.Address {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.remote
}

// SetRemoteAddress records the peer's address.
func (l *Link) SetRemoteAddress(a codec.Address) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.remote = a.Device()
}

// Handshake is the first thing a client sends on a new connection: GetDevInfo
// for port zero, addressed to the unconnected broadcast address.
//
// It exists because a TCP client knows neither address. Its own is assigned by
// the gateway, which stamps a port drawn from its connection slots over the
// zeroed source we send; and the gateway's own address is whatever the reply
// comes from. Both are learned here, and both are needed before any session
// can be opened.
//
// The reply also carries the peer's service mask, which is what decides
// whether a 32-bit session can be asked for at all.
func (l *Link) Handshake(ctx context.Context) (codec.DeviceInfo, error) {
	req := codec.Frame{
		Dst:  codec.Broadcast(),
		Src:  addrWithIndex(l.LocalAddress(), codec.IndexUnknown),
		Type: codec.MsgGetDevInfo,
	}

	reply, err := l.exchange(ctx, l.blind, req)
	if err != nil {
		return codec.DeviceInfo{}, err
	}
	if reply.Type != codec.MsgRetDevInfo {
		return codec.DeviceInfo{}, protocolError("handshake", reply)
	}

	info, err := codec.DecodeDeviceInfo(reply.Payload)
	if err != nil {
		return codec.DeviceInfo{}, err
	}

	// Take the addresses from the frame header, not from the payload. The
	// address inside a DeviceInfo is never rewritten as a message crosses a
	// bridge, so its route is meaningless (spec 11.3.4).
	l.SetRemoteAddress(reply.Src)
	if reply.Dst.Unit != 0 {
		l.SetLocalAddress(reply.Dst)
	}
	return info, nil
}

// Unsolicited returns frames that answered no request and belonged to no
// session: announcements, mostly. It is buffered and lossy by design — a
// caller that is not draining it is not interested, and an announcement is
// repeated every few seconds anyway.
func (l *Link) Unsolicited() <-chan codec.Frame { return l.unsolicited }

// Done is closed when the link stops. Err says why.
func (l *Link) Done() <-chan struct{} { return l.done }

// Err returns why the link closed, or nil while it is running.
func (l *Link) Err() error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.closeErr
}

// Stats reports frame counts and how many bytes were discarded to regain
// framing. A non-zero resync count is worth a compliance event: it means the
// peer sent something that was not a frame.
func (l *Link) Stats() (rx, tx, resyncs uint64) {
	return l.rxFrames.Load(), l.txFrames.Load(), l.resyncs.Load()
}

// Close shuts the link down and every session on it.
func (l *Link) Close() error {
	l.closeWith(ErrLinkClosed)
	l.wg.Wait()
	return nil
}

// closeWith stops the link once, recording the first reason given.
func (l *Link) closeWith(err error) {
	l.doneOnce.Do(func() {
		l.mu.Lock()
		l.closed = true
		l.closeErr = err
		sessions := make([]*Session, 0, len(l.sessions))
		for _, s := range l.sessions {
			sessions = append(sessions, s)
		}
		l.mu.Unlock()

		// Waking every waiter matters more than the order it happens in: a
		// caller blocked on a request should learn now, not at its own
		// timeout.
		l.blind.closeWith(err)
		for _, s := range sessions {
			s.linkClosed(err)
		}

		close(l.done)
		_ = l.conn.Close()
	})
}

// send writes one frame.
func (l *Link) send(f codec.Frame) error {
	buf, err := f.Encode()
	if err != nil {
		return err
	}

	l.writeMu.Lock()
	_, err = l.conn.Write(buf)
	l.writeMu.Unlock()

	if err != nil {
		l.closeWith(fmt.Errorf("rollcall: write: %w", err))
		return err
	}
	l.txFrames.Add(1)
	l.met.ObserveTx(len(buf), 0)
	return nil
}

// exchange sends a request on a channel and waits for its reply, holding the
// one-in-flight slot for the whole round trip.
func (l *Link) exchange(ctx context.Context, ch *channel, f codec.Frame) (codec.Frame, error) {
	p, err := ch.acquire(ctx, f.Type)
	if err != nil {
		return codec.Frame{}, err
	}
	if err := l.send(f); err != nil {
		ch.release(false)
		return codec.Frame{}, err
	}
	return ch.await(ctx, p)
}

// readLoop decodes frames until the connection ends.
func (l *Link) readLoop() {
	defer l.wg.Done()

	r := codec.NewReader(l.conn)
	for {
		f, err := r.ReadFrame()
		if err != nil {
			if errors.Is(err, io.EOF) {
				l.closeWith(ErrLinkClosed)
			} else {
				l.met.ObserveDecodeError()
				l.closeWith(fmt.Errorf("rollcall: read: %w", err))
			}
			return
		}

		l.rxFrames.Add(1)
		l.resyncs.Store(r.Resyncs())
		l.met.ObserveRx(f.Size())

		// The payload aliases the reader's buffer, which the next read
		// overwrites. Everything downstream may outlive that: a push waits in
		// a queue until the application takes it, an announcement waits until
		// somebody drains it, and even a reply sits in a buffered channel
		// until its caller wakes. Copying here is what makes all of them safe
		// at the cost of one short-lived allocation per frame.
		f.Payload = append([]byte(nil), f.Payload...)
		l.dispatch(f)
	}
}

// dispatch routes one received frame.
//
// The destination session index is what identifies the session, and it is our
// index rather than the peer's: we chose it, we put it in the source of
// everything we send, and the peer echoes it back here. Matching on the peer's
// index instead is the mistake that produced SP_INVSESS during the audit and
// lost every push.
func (l *Link) dispatch(f codec.Frame) {
	if s := l.session(f.Dst.Index); s != nil {
		s.receive(f)
		return
	}

	// Not for a session. A reply to something sent outside one is matched
	// head-of-queue on the blind channel, because there is no index to
	// correlate by and the protocol only ever has one such request in flight.
	if f.Type.ValidBlindReply() && l.blind.deliver(f) {
		return
	}

	// On a link that serves, everything else is a request.
	if l.cfg.Handler != nil {
		if f.Type == codec.MsgCall {
			l.serveCall(f)
			return
		}
		l.cfg.Handler.Unsolicited(l, f)
		return
	}
	l.deliverUnsolicited(f)
}

func (l *Link) deliverUnsolicited(f codec.Frame) {
	select {
	case l.unsolicited <- f:
	default:
		// Nobody is listening. Announcements repeat, so dropping one costs
		// nothing; stalling the read loop would cost everything.
		l.log.Debug("rollcall: dropped unsolicited frame",
			"type", f.Type.String(), "src", f.Src.String())
	}
}

func (l *Link) session(idx int16) *Session {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.sessions[idx]
}

// register allocates the session index we publish as our own and makes the
// session findable under it.
//
// Both happen under one lock. Allocating an index and registering separately
// leaves a window in which the peer's acknowledgement, which is addressed to
// that index, arrives before anything is listening on it.
//
// Zero, 0xFE and 0xFF are reserved for blind, logging and unconnected traffic
// respectively, so allocation starts at one and skips them.
func (l *Link) register(s *Session) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return l.closeErr
	}
	for range 0xFD {
		idx := l.nextIdx
		l.nextIdx++
		if l.nextIdx >= codec.IndexLogging {
			l.nextIdx = 1
		}
		if _, taken := l.sessions[idx]; !taken {
			s.localIndex = idx
			l.sessions[idx] = s
			return nil
		}
	}
	return errors.New("rollcall: no free session index on this link")
}

func (l *Link) removeSession(idx int16) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.sessions, idx)
}

// SessionCount reports how many sessions are open, which is what a provider
// checks before answering a Call with Busy.
func (l *Link) SessionCount() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.sessions)
}
