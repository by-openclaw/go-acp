package rollcall

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

// The proxy can front a real frame rather than the tree this provider serves:
// our own IPShare, in the sense the vendor RollProxy is one. The proxy unit
// and its virtual nodes are still answered here; everything a client sends to
// the frame's route is carried to the frame over a connection of the client's
// own, and everything the frame sends comes back the same way.
//
// One connection to the frame per client, rather than one shared by all of
// them. A shared connection is what the vendor box holds, and it is why the
// vendor box has to renumber sessions: the frame allocates its indices per
// connection, so two clients' sessions collide on one. A connection per client
// keeps the frame's numbering and the client's numbering exactly what each
// chose, and a frame's IPShare stamps a client port per connection, so each
// client is also a client the frame can see. The price is one of the frame's
// connection slots per client, which is what a client costs it directly.
//
// What crosses each leg is what the vendor's own library does, read from
// IPShare.c and IPShClient.c under assets/Protocol/Source rather than guessed:
//
//   - A message forwarded over a bridge hop has its destination route shifted
//     up a nibble (the hop just taken drops off) and, coming back, its source
//     route shifted down with the bridge's own unit inserted at the top. That
//     is codec Forward and ForwardSource (spec 5.3). The virtual nodes are the
//     hops, so a client's 2100-0C-01 reaches the frame as 0000-0C-01, and the
//     frame's 0000-0C-01 reaches the client as 2100-0C-01.
//   - An IPShare client zeroes its own source device — net, unit and port —
//     on everything it sends ("commtrol and IP Proxy seem to prefer this"),
//     and a frame spoofs the source of whatever arrives to its own unit and
//     the connection's port regardless. Only the session index survives, and
//     it is the one the client chose.
//   - A frame zeroes the destination device on everything it sends to an
//     IPShare client, and the client end writes its own address back in. A
//     client of this proxy is 0000-00-00, as a client of the vendor proxy is —
//     neither assigns one — so what is written back is the zero device with
//     the client's session index, which is what the vendor proxy's replies
//     carry (measured 2026-09-16: every reply to 0000-00-00:<index>).
//
// Nothing inside a payload is touched. The address carried in a DeviceInfo is
// never rewritten as a message crosses a bridge (spec 11.3.4); a client
// composes the route itself, which is what codec.Address.Compose is for.

// relayDialTimeout bounds one attempt to reach the frame. It is a LAN peer,
// and a client is waiting on its own three-second reply timer meanwhile.
const relayDialTimeout = 5 * time.Second

// errRelayClosed says the client this relay served has gone.
var errRelayClosed = errors.New("rollcall: the client of this relay has gone")

// relayLink carries one client's routed traffic to the frame and back.
type relayLink struct {
	p    *Provider
	addr string

	mu sync.Mutex
	// down is the client's link, learned from the first frame intercepted.
	down *session.Link
	// up is the connection to the frame, dialed when first needed and dialed
	// again if it goes away: a frame that dropped us is redialed on the next
	// routed request rather than probed while nobody wants it.
	up     *session.Link
	closed bool
}

// newRelayLink builds the relay for one client, before its link exists: the
// intercept is part of the link's configuration, so the relay has to be.
func newRelayLink(p *Provider, addr string) *relayLink {
	return &relayLink{p: p, addr: addr}
}

// intercept takes every frame a client addresses to the frame's route.
//
// The proxy unit and its virtual nodes are left to the ordinary path: they are
// served here. Only a resolved address on the frame's subnet is carried.
func (r *relayLink) intercept(l *session.Link, f codec.Frame) bool {
	role, _, _, _ := r.p.proxy.resolve(f.Dst)
	if role != roleFrame {
		return false
	}

	r.mu.Lock()
	if r.down == nil {
		r.down = l
	}
	r.mu.Unlock()

	r.toFrame(l, f)
	return true
}

// toFrame carries one client frame to the frame.
//
// A frame that cannot be reached refuses the request in the client's own
// terms: Nack, with a reason, rather than the silence that would leave the
// client waiting out its timer and striking the session.
func (r *relayLink) toFrame(down *session.Link, f codec.Frame) {
	up, err := r.upstream()
	if errors.Is(err, errRelayClosed) {
		// The client's last frames — its Terms, typically — can still be on
		// the read loop as its link ends. Nobody is left to answer.
		r.p.log.Debug("rollcall: dropped a routed frame from a client that has gone",
			"type", f.Type.String())
		return
	}
	if err != nil {
		r.p.log.Warn("rollcall: the frame behind the proxy is unreachable",
			"frame", r.addr, "type", f.Type.String(), "err", err)
		r.refuse(down, f, "frame unreachable")
		return
	}

	if err := up.SendFrame(r.p.proxy.outbound(f)); err != nil {
		r.p.log.Warn("rollcall: could not relay to the frame",
			"frame", r.addr, "type", f.Type.String(), "err", err)
		r.refuse(down, f, "frame connection lost")
	}
}

// refuse answers a client frame that could not be carried.
//
// A broadcast is not answered: nobody answers a broadcast, and the client is
// not waiting for one.
func (r *relayLink) refuse(down *session.Link, f codec.Frame, reason string) {
	if f.Type.Broadcastable() {
		return
	}
	err := down.SendFrame(codec.Frame{
		Dst:     f.Src,
		Src:     f.Dst,
		Type:    codec.MsgNack,
		Payload: append([]byte(reason), 0),
	})
	if err != nil {
		r.p.log.Debug("rollcall: could not refuse a relayed request",
			"type", f.Type.String(), "err", err)
	}
}

// fromFrame carries one frame from the frame to the client. It runs on the
// frame connection's read loop and takes everything: nothing on that
// connection is for the proxy itself.
func (r *relayLink) fromFrame(_ *session.Link, f codec.Frame) bool {
	r.mu.Lock()
	down := r.down
	r.mu.Unlock()
	if down == nil {
		return true
	}

	// A client of the proxy has the zero address: the handshake assigned none.
	if err := down.SendFrame(r.p.proxy.inbound(f, codec.Address{})); err != nil {
		r.p.log.Debug("rollcall: could not relay to the client",
			"type", f.Type.String(), "err", err)
	}
	return true
}

// upstream returns the connection to the frame, dialing it if there is none.
//
// The dial happens on the client's read loop, which stalls that client for the
// duration. A LAN peer answers in milliseconds and the client was waiting on
// this very answer, so that is the honest place for the wait to be; the
// timeout is what keeps a frame that has gone away from holding the client
// past its own timer.
func (r *relayLink) upstream() (*session.Link, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil, errRelayClosed
	}
	if r.up != nil {
		select {
		case <-r.up.Done():
			r.up = nil
		default:
			return r.up, nil
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), relayDialTimeout)
	defer cancel()
	conn, err := r.p.net.Dial(ctx, "tcp", r.addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", r.addr, err)
	}

	// A frame does not probe its clients and this proxy does not probe the
	// frame: the client's own keepalives cross the relay like everything else,
	// and a frame that drops an idle connection is redialed when it is wanted.
	up := session.NewLink(conn, session.Config{
		Intercept:         r.fromFrame,
		KeepaliveInterval: -1,
	}, r.p.deps)
	r.up = up
	r.p.log.Debug("rollcall: relay connected to the frame", "frame", r.addr)

	r.p.wg.Add(1)
	go func() {
		defer r.p.wg.Done()
		<-up.Done()
		r.mu.Lock()
		if r.up == up {
			r.up = nil
		}
		r.mu.Unlock()
		r.p.log.Debug("rollcall: relay connection to the frame ended",
			"frame", r.addr, "err", up.Err())
	}()
	return up, nil
}

// close ends the relay with its client: the frame connection goes with it,
// which is what frees the frame's connection slot and, with it, the sessions
// the client held there.
func (r *relayLink) close() {
	r.mu.Lock()
	up := r.up
	r.up = nil
	r.closed = true
	r.mu.Unlock()

	if up != nil {
		_ = up.Close()
	}
}

// outbound rewrites a client's frame for the frame's own segment: the route is
// consumed hop by hop, and the source device is zeroed the way an IPShare
// client zeroes its own. The session indices at both ends are kept.
func (t *proxyTopology) outbound(f codec.Frame) codec.Frame {
	for range t.nodes {
		f.Dst = f.Dst.Forward()
	}
	f.Src.Net, f.Src.Unit, f.Src.Port = 0, 0, 0
	return f
}

// inbound rewrites the frame's frame for the client at client: the route is
// composed hop by hop from the far end back, and the destination — which a
// frame zeroes on everything it sends an IPShare client, and which some frames
// echo instead — is written back as the client's own address. A broadcast
// keeps the broadcast address, because a client recognises an announcement by
// it.
func (t *proxyTopology) inbound(f codec.Frame, client codec.Address) codec.Frame {
	for i := len(t.nodes) - 1; i >= 0; i-- {
		f.Src = f.Src.ForwardSource(t.nodes[i].local.Unit)
	}
	if !f.Type.Broadcastable() || !f.Dst.IsBroadcast() {
		f.Dst.Net, f.Dst.Unit, f.Dst.Port = 0, client.Unit, client.Port
	}
	return f
}

// probeFrame reaches the frame once to learn what it is: its unit, which is
// what the route is stamped with, and its identity, which is what the last
// virtual node lists as its far side. It is the vendor proxy's "Connected"
// column, asked once at configuration rather than polled.
func (p *Provider) probeFrame(ctx context.Context, addr string) (codec.DeviceInfo, codec.Address, error) {
	ctx, cancel := context.WithTimeout(ctx, relayDialTimeout)
	defer cancel()

	conn, err := p.net.Dial(ctx, "tcp", addr)
	if err != nil {
		return codec.DeviceInfo{}, codec.Address{}, fmt.Errorf("rollcall: dial the frame at %s: %w", addr, err)
	}
	l := session.NewLink(conn, session.Config{KeepaliveInterval: -1}, p.deps)
	defer func() { _ = l.Close() }()

	info, err := l.Handshake(ctx)
	if err != nil {
		return codec.DeviceInfo{}, codec.Address{}, fmt.Errorf("rollcall: handshake with the frame at %s: %w", addr, err)
	}
	// The unit comes from the frame header, not the payload: a DeviceInfo's
	// address is whatever the unit was built to say, the header's is what it
	// answers as.
	return info, l.RemoteAddress(), nil
}
