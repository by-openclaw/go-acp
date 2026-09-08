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

	// announce is our presence on the network. A RollCall client is a node
	// like any other and the specification requires it to say so.
	announce *session.Announcer

	// gateway is what the peer said it was during the handshake.
	gateway codec.DeviceInfo

	mu sync.Mutex
	// sessions are keyed by the node's whole address, not by a port: on a
	// controller the nodes differ by unit, and keying by port would give every
	// one of them the same session.
	sessions map[codec.Address]*session.Session

	// fileSessions are separate because services are negotiated together and
	// all-or-nothing: asking for the file service on the control session
	// would make a unit without one uncontrollable.
	fileSessions map[codec.Address]*session.Session

	// mapSess is the one session that may ask for the device map, for the same
	// reason. A unit answers a request belonging to a service the session did
	// not negotiate with SP_INVSESS, not by ignoring it: measured against the
	// vendor Centra, which refuses GETDEVLIST that way on a control session.
	mapSess *session.Session

	// netSessions are the far-side enumerations, one per bridge. A bridge
	// offers Map and Net alike and the same message means a different list on
	// each, so the two cannot share a session.
	netSessions map[codec.Address]*session.Session

	// portSess is the same story for the port service. A real IQ frame keeps
	// its cards behind SP_GETDEVLIST and ignores that request on a session
	// that did not negotiate Ports, so a frame full of cards enumerates as one
	// gateway and nothing else.
	portSess *session.Session

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
		sessions:     make(map[codec.Address]*session.Session),
		fileSessions: make(map[codec.Address]*session.Session),
		netSessions:  make(map[codec.Address]*session.Session),
	}
	l.keep = session.NewKeepalive(context.WithoutCancel(ctx), sl, nil)

	// Say we are here, and keep saying it. Spec 9.31 requires every unit on
	// the network to broadcast Iam at about fifteen second intervals, and a
	// client is a unit: the vendor's own Control Panel appears in a frame's
	// port list as one. That list is how an operator sees who is attached, and
	// a firmware upgrade requires every client disconnected first — so a
	// client nobody can see is a client nobody knows to disconnect.
	//
	// The address is the one the gateway just assigned us, not the empty one
	// the identity carries before a handshake has happened.
	l.announce = session.NewAnnouncer(context.WithoutCancel(ctx), sl,
		session.Identity{Info: p.announceIdentity(sl.LocalAddress())})

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
	sessions := make([]*session.Session, 0, len(l.sessions)+len(l.fileSessions)+1)
	if l.mapSess != nil {
		sessions = append(sessions, l.mapSess)
		l.mapSess = nil
	}
	if l.portSess != nil {
		sessions = append(sessions, l.portSess)
		l.portSess = nil
	}
	for a, s := range l.netSessions {
		sessions = append(sessions, s)
		delete(l.netSessions, a)
	}
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
func (p *Plugin) session(ctx context.Context, slot int) (*session.Session, error) {
	peer, err := p.slotAddress(ctx, slot)
	if err != nil {
		return nil, err
	}
	return p.sessionAt(ctx, peer)
}

// sessionAt opens or returns the session for one node address.
//
// Sessions are keyed by the whole address rather than by a port, because on a
// controller the nodes differ by unit and keying by port would hand every one
// of them the same session.
func (p *Plugin) sessionAt(ctx context.Context, peer codec.Address) (*session.Session, error) {
	l, err := p.conn()
	if err != nil {
		return nil, err
	}
	peer = peer.Device()

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, consumer.ErrNotConnected
	}
	if s, ok := l.sessions[peer]; ok {
		l.mu.Unlock()
		return s, nil
	}
	l.mu.Unlock()

	s, err := p.call(ctx, l, peer, p.advertisedBy(peer, l))
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
	// reply. Keep theirs and close ours, so a node never has two.
	if existing, ok := l.sessions[peer]; ok {
		go func() { _ = s.Close() }()
		return existing, nil
	}
	l.sessions[peer] = s
	return s, nil
}

// mapSession returns the session enumeration runs on.
//
// It asks for the map service alone. A gateway that does not advertise one is
// asked on the control session instead, because some units answer a list on
// any session and the walk is worth attempting before it is given up on.
func (p *Plugin) mapSession(ctx context.Context) (*session.Session, error) {
	l, err := p.conn()
	if err != nil {
		return nil, err
	}
	gateway := l.sess.RemoteAddress().Device()

	if !l.gateway.ID.Services.Has(codec.SvcMap) {
		return p.sessionAt(ctx, gateway)
	}

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, consumer.ErrNotConnected
	}
	if s := l.mapSess; s != nil {
		l.mu.Unlock()
		return s, nil
	}
	l.mu.Unlock()

	// The map service is asked for on its own, without the long-string bit.
	// What the map carries is fixed-width whichever generation is in force, so
	// the bit buys nothing here, and the vendor Centra refuses the pair while
	// accepting the map service alone.
	s, err := session.Call(ctx, l.sess, gateway, codec.SvcMap, codec.LevelSupervisor, p.identity())
	if err != nil {
		// A gateway that will not open a map session may still answer a list
		// on the control one. Trying is cheaper than reporting a device with
		// no discoverable nodes.
		p.log.Debug("rollcall: no map session; enumerating on the control session",
			"gateway", gateway.String(), "err", err)
		return p.sessionAt(ctx, gateway)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		_ = s.Close()
		return nil, consumer.ErrNotConnected
	}
	if existing := l.mapSess; existing != nil {
		go func() { _ = s.Close() }()
		return existing, nil
	}
	l.mapSess = s
	return s, nil
}

// portSession returns the session a port list runs on.
//
// The cards in a frame are ports of its gateway (spec 7.6) and they are
// reached through the port service, which is a different service from the map
// even though both answer with a list of devices. A unit that implements it
// properly ignores SP_GETDEVLIST asked on a session that did not negotiate
// Ports: measured against a real IQ 3U frame, which advertises the service and
// then says nothing at all, so a frame full of cards enumerated as one gateway.
//
// A peer that does not advertise Ports is asked on the map session, which is
// what the vendor Centra needs - it advertises Map and not Ports and answers a
// port list there anyway with the units it fronts.
func (p *Plugin) portSession(ctx context.Context) (*session.Session, error) {
	l, err := p.conn()
	if err != nil {
		return nil, err
	}
	if !l.gateway.ID.Services.Has(codec.SvcPorts) {
		return p.mapSession(ctx)
	}
	gateway := l.sess.RemoteAddress().Device()

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, consumer.ErrNotConnected
	}
	if s := l.portSess; s != nil {
		l.mu.Unlock()
		return s, nil
	}
	l.mu.Unlock()

	s, err := session.Call(ctx, l.sess, gateway, codec.SvcPorts, codec.LevelSupervisor, p.identity())
	if err != nil {
		// A unit that advertises the service and refuses a session for it can
		// still answer on the map session, and trying is cheaper than
		// reporting a frame with no cards.
		p.log.Debug("rollcall: no port session; enumerating on the map session",
			"gateway", gateway.String(), "err", err)
		return p.mapSession(ctx)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		_ = s.Close()
		return nil, consumer.ErrNotConnected
	}
	if existing := l.portSess; existing != nil {
		_ = s.Close()
		return existing, nil
	}
	l.portSess = s
	return s, nil
}

// netSession returns the session a bridge's far-side list runs on.
//
// Net and Map are different services that answer the same message with
// different lists, and spec 7.7 is explicit: a session that names both is read
// as Net, so the two must be asked for separately. A bridge offers both, which
// is exactly the case the rule exists for.
func (p *Plugin) netSession(ctx context.Context, bridge codec.Address) (*session.Session, error) {
	l, err := p.conn()
	if err != nil {
		return nil, err
	}
	node := bridge.Device()

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, consumer.ErrNotConnected
	}
	if s := l.netSessions[node]; s != nil {
		l.mu.Unlock()
		return s, nil
	}
	l.mu.Unlock()

	s, err := session.Call(ctx, l.sess, node, codec.SvcNet, codec.LevelSupervisor, p.identity())
	if err != nil {
		return nil, err
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		_ = s.Close()
		return nil, consumer.ErrNotConnected
	}
	if existing := l.netSessions[node]; existing != nil {
		_ = s.Close()
		return existing, nil
	}
	l.netSessions[node] = s
	return s, nil
}

// advertisedBy is what a node said it serves.
//
// Each node advertises for itself and they differ: on the vendor Centra the
// matrices offer Ports and the panel node does not, and only the gateway offers
// Map. What the enumeration said about a node is therefore better than what the
// gateway said about itself, and the gateway's own mask is the fallback for a
// node nothing has described.
func (p *Plugin) advertisedBy(peer codec.Address, l *link) codec.Service {
	p.mu.RLock()
	t := p.nodeCache
	p.mu.RUnlock()

	if t != nil {
		for i, addr := range t.addrs {
			if addr.SameDevice(peer) {
				return t.info[i].ID.Services
			}
		}
	}
	return l.gateway.ID.Services
}

// call opens one session, falling back a generation if the peer refuses.
func (p *Plugin) call(ctx context.Context, l *link, peer codec.Address,
	advertised codec.Service) (*session.Session, error) {

	wantLong := advertised.LongStrings()

	s, err := session.Call(ctx, l.sess, peer, sessionServices(advertised, wantLong),
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

	s, err = session.Call(ctx, l.sess, peer, sessionServices(advertised, false),
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
	s, err := p.session(ctx, slot)
	if err != nil {
		return false, err
	}
	return s.Uses32Bit(), nil
}
