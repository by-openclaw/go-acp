package rollcall

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"

	"dhs/internal/consumer"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

// link holds one connection and the sessions opened over it.
//
// Sessions are per port rather than per device, because a menu walk is bound
// to the port it was opened on and a gateway's cards each have their own. They
// are opened on demand and kept, since opening one costs a round trip and
// closing one is what a unit runs out of.
type link struct {
	sess *session.Link
	keep *session.Keepalive

	// gateway is what the peer said it was during the handshake.
	gateway codec.DeviceInfo

	mu       sync.Mutex
	sessions map[uint8]*session.Session // control and menu, by port

	// fileSessions are separate because services are negotiated together and
	// all-or-nothing: asking for the file service on the control session
	// would make a unit without one uncontrollable.
	fileSessions map[uint8]*session.Session

	closed bool
}

// Connect opens the link and learns who is on the other end.
//
// It is callable more than once: a reconnect closes what came before, so a
// caller that reconnects does not leak the sessions it had.
func (p *Plugin) Connect(ctx context.Context, ip string, port int) error {
	if port == 0 {
		port = DefaultPort
	}
	addr := net.JoinHostPort(ip, strconv.Itoa(port))

	// Anything from a previous connection goes first, including its sessions,
	// which are terminated properly rather than abandoned. Disconnect cannot
	// fail; it reports an error only to satisfy the neutral contract.
	_ = p.Disconnect()

	conn, err := p.net.Dial(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("rollcall: dial %s: %w", addr, err)
	}

	sl := session.NewLink(conn, session.Config{}, p.deps)

	// The first message on a new connection. It learns our address, which the
	// gateway assigns, and the gateway's own, which we cannot know; and its
	// service mask, which decides which generation we may ask for.
	info, err := sl.Handshake(ctx)
	if err != nil {
		_ = sl.Close()
		return fmt.Errorf("rollcall: handshake with %s: %w", addr, err)
	}

	l := &link{
		sess:         sl,
		gateway:      info,
		sessions:     make(map[uint8]*session.Session),
		fileSessions: make(map[uint8]*session.Session),
	}
	l.keep = session.NewKeepalive(context.WithoutCancel(ctx), sl, nil)

	p.mu.Lock()
	p.link = l
	p.addr = ip
	p.port = port
	p.trees = make(map[int]*slotTree)
	p.events = make(chan struct{})
	p.mu.Unlock()

	p.log.Info("rollcall: connected",
		"addr", addr,
		"gateway", info.Address.String(),
		"name", info.ID.Name,
		"type", codec.UnitTypeName(info.ID.TypeID),
		"services", info.ID.Services.String(),
		"generation", generationName(info.ID.Services))

	go p.pumpBackChannel()
	return nil
}

// generationName says which wire generation a peer can speak, for a log line.
func generationName(s codec.Service) string {
	if s.LongStrings() {
		return "32-bit"
	}
	return "16-bit"
}

// Disconnect closes the link and every session on it.
//
// Sessions are terminated rather than dropped. Servers do not time idle
// sessions out, so one we abandon is held until the unit reboots, and a unit
// that runs out of sessions stops answering anybody.
func (p *Plugin) Disconnect() error {
	p.mu.Lock()
	l := p.link
	p.link = nil
	events := p.events
	p.mu.Unlock()

	if l == nil {
		return nil
	}
	// Only this call can close it: the link is taken under the lock and set to
	// nil in the same breath, so a second Disconnect returns above rather than
	// arriving here, and the channel is created fresh by each Connect.
	close(events)
	l.close()
	return nil
}

func (l *link) close() {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.closed = true
	sessions := make([]*session.Session, 0, len(l.sessions)+len(l.fileSessions))
	for _, s := range l.sessions {
		sessions = append(sessions, s)
	}
	for _, s := range l.fileSessions {
		sessions = append(sessions, s)
	}
	l.sessions = nil
	l.fileSessions = nil
	l.mu.Unlock()

	for _, s := range sessions {
		_ = s.Close()
	}
	_ = l.sess.Close()
}

// conn returns the current link, or the neutral not-connected error.
func (p *Plugin) conn() (*link, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.link == nil {
		return nil, consumer.ErrNotConnected
	}
	return p.link, nil
}

// session returns the session for a port, opening one if there is none.
//
// The 32-bit generation is asked for whenever the peer advertises it, because
// it is the only one that can express a router's command space and its menu
// requests are stateless. A peer that refuses the whole call because of that
// bit is retried without it: services are all-or-nothing, so a partial grant
// is not a thing that can happen.
func (p *Plugin) session(ctx context.Context, port uint8) (*session.Session, error) {
	l, err := p.conn()
	if err != nil {
		return nil, err
	}

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, consumer.ErrNotConnected
	}
	if s, ok := l.sessions[port]; ok {
		l.mu.Unlock()
		return s, nil
	}
	l.mu.Unlock()

	peer := l.sess.RemoteAddress()
	peer.Port = port

	wantLong := l.gateway.ID.Services.LongStrings()
	s, err := p.call(ctx, l, peer, wantLong)
	if err != nil {
		return nil, err
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		_ = s.Close()
		return nil, consumer.ErrNotConnected
	}
	// Another caller may have opened one while we were waiting for the
	// reply. Keep theirs and close ours, so a port never has two.
	if existing, ok := l.sessions[port]; ok {
		go func() { _ = s.Close() }()
		return existing, nil
	}
	l.sessions[port] = s
	return s, nil
}

// call opens one session, falling back a generation if the peer refuses.
func (p *Plugin) call(ctx context.Context, l *link, peer codec.Address, wantLong bool) (*session.Session, error) {
	s, err := session.Call(ctx, l.sess, peer, sessionServices(wantLong),
		codec.LevelSupervisor, p.identity())
	if err == nil {
		return s, nil
	}
	if !wantLong {
		return nil, fmt.Errorf("rollcall: call %s: %w", peer, err)
	}

	// The peer advertised long strings and then refused a call that asked for
	// them. That is a deviation worth naming, because the two statements
	// cannot both be true, and the fallback hides it from the caller.
	p.fire(EventLongStringsRefused, fmt.Sprintf(
		"%s advertises SV_LONGSTR but refused a call requesting it: %v", peer, err))

	s, err = session.Call(ctx, l.sess, peer, sessionServices(false),
		codec.LevelSupervisor, p.identity())
	if err != nil {
		return nil, fmt.Errorf("rollcall: call %s: %w", peer, err)
	}
	return s, nil
}

// Uses32Bit reports whether the session for a port speaks the 32-bit
// generation. Callers that need to know which message types will be used ask
// this rather than inferring it from the device.
func (p *Plugin) Uses32Bit(ctx context.Context, slot int) (bool, error) {
	s, err := p.session(ctx, uint8(slot))
	if err != nil {
		return false, err
	}
	return s.Uses32Bit(), nil
}
