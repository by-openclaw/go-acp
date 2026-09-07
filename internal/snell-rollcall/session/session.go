package session

import (
	"context"
	"fmt"
	"sync"

	"dhs/internal/snell-rollcall/codec"
)

// Session is one negotiated conversation with a peer: a set of services at a
// user level, with a front channel we drive and a back channel the peer does.
//
// Two indices matter and confusing them is the defect this type exists to
// prevent. localIndex is ours: we chose it, it goes in the source of every
// message we send, and the peer must put it in the destination of every push.
// remoteIndex is the peer's, learned from its acknowledgement, and it goes in
// the destination of every message we send. Sending our own index where the
// peer's belongs produces SP_INVSESS and, on the back channel, silently loses
// every push.
type Session struct {
	link *Link

	// peer is the device this session is with, without an index.
	peer codec.Address

	localIndex  int16
	remoteIndex int16

	services  codec.Service
	userLevel codec.UserLevel

	front *channel
	back  *channel

	// pushes carries back-channel messages to the application. Each one is a
	// request the peer expects acknowledged, and the acknowledgement is what
	// asks for the next, so a full queue is back pressure rather than an
	// error.
	pushes chan Push

	mu     sync.Mutex
	closed bool

	closeOnce sync.Once
}

// Push is one back-channel message and the means to acknowledge it.
type Push struct {
	Frame codec.Frame

	// ack acknowledges the push. It is called by the delivery loop once the
	// application has taken the message, not by the application itself.
	ack func() error
}

// Services returns the service mask this session negotiated.
func (s *Session) Services() codec.Service { return s.services }

// UserLevel returns the level this session was opened at. Menus and commands
// above it are hidden rather than absent, so the level is part of what
// identifies a cached device model.
func (s *Session) UserLevel() codec.UserLevel { return s.userLevel }

// LocalIndex is the session index we publish as our own.
func (s *Session) LocalIndex() int16 { return s.localIndex }

// RemoteIndex is the session index the peer assigned, which every message we
// send must carry as its destination.
func (s *Session) RemoteIndex() int16 { return s.remoteIndex }

// Peer returns the address of the device on the other end, carrying the peer's
// session index.
func (s *Session) Peer() codec.Address {
	a := s.peer
	a.Index = s.remoteIndex
	return a
}

// Pushes returns the back-channel stream. It is empty until the back channel
// is enabled.
func (s *Session) Pushes() <-chan Push { return s.pushes }

// Uses32Bit reports whether this session negotiated the long-string
// generation. It decides which message types every later request uses, and it
// is a property of the session rather than of the device: the same unit serves
// both, one session each.
func (s *Session) Uses32Bit() bool { return s.services.LongStrings() }

// Call opens a session with a peer.
//
// Services are all-or-nothing: a peer that cannot supply every requested bit
// refuses the whole call rather than granting a subset. A client that wants to
// fall back from the 32-bit generation therefore issues a second Call without
// the long-string bit; it must not expect a partial grant.
func Call(ctx context.Context, l *Link, peer codec.Address, services codec.Service,
	level codec.UserLevel, caller codec.DeviceInfo) (*Session, error) {

	if !level.Valid() {
		return nil, fmt.Errorf("rollcall: call: user level %d is not one a client may request", level)
	}

	payload, err := codec.Connect{
		Services:  services,
		UserLevel: level,
		Caller:    caller,
	}.AppendTo(nil)
	if err != nil {
		return nil, err
	}

	s := &Session{
		link:        l,
		peer:        peer.Device(),
		remoteIndex: codec.IndexUnknown,
		services:    services,
		userLevel:   level,
		pushes:      make(chan Push, l.cfg.pushQueue()),
	}
	s.front = newChannel("front", l.clk, l.cfg.replyTimeout(), l.cfg.maxStrikes())
	s.back = newChannel("back", l.clk, l.cfg.replyTimeout(), l.cfg.maxStrikes())

	// Registered before the Call goes out: the acknowledgement is addressed
	// to our index, so the session has to be findable when it arrives.
	if err := l.register(s); err != nil {
		return nil, err
	}
	idx := s.localIndex

	// The destination index is the unconnected one: there is no session yet,
	// and 255 is what says so. Writing "unknown" as -1 puts 0xFFFF on the
	// wire, which a proxy answers and a real device faults on.
	req := codec.Frame{
		Dst:     addrWithIndex(peer, codec.IndexUnknown),
		Src:     addrWithIndex(l.LocalAddress(), idx),
		Type:    codec.MsgCall,
		Payload: payload,
	}

	reply, err := l.exchange(ctx, s.front, req)
	if err != nil {
		l.removeSession(idx)
		return nil, err
	}
	if reply.Type != codec.MsgAck {
		l.removeSession(idx)
		return nil, protocolError("call", reply)
	}

	// The peer's index arrives as the source of its acknowledgement. It is
	// sticky for the life of the session.
	s.remoteIndex = reply.Src.Index
	s.peer = reply.Src.Device()

	// A gateway rewrites our zeroed source into the address it assigned us.
	// Learning it here is what lets every later frame carry a real source.
	if l.LocalAddress().Unit == 0 && reply.Dst.Unit != 0 {
		l.SetLocalAddress(reply.Dst)
	}
	return s, nil
}

// Do sends a request on the front channel and returns the reply.
//
// It fills in the addressing, so a caller supplies only the type and payload
// and cannot get the indices the wrong way round.
func (s *Session) Do(ctx context.Context, typ codec.PacketType, payload []byte) (codec.Frame, error) {
	return s.do(ctx, s.front, 0, typ, payload)
}

// Reply answers a back-channel push. Providers use it; a consumer's pushes are
// acknowledged for it by the delivery loop.
func (s *Session) Reply(typ codec.PacketType, payload []byte) error {
	return s.link.send(s.frame(codec.FlagBackChannel, typ, payload))
}

// Push sends an unsolicited message on the back channel and waits for the
// peer to acknowledge it.
//
// Every push is an active message: the next may not be sent until this one is
// answered. That is why this blocks, and why a provider with a burst of
// changes queues them rather than writing them all to the socket.
func (s *Session) Push(ctx context.Context, typ codec.PacketType, payload []byte) error {
	_, err := s.do(ctx, s.back, codec.FlagBackChannel, typ, payload)
	return err
}

func (s *Session) do(ctx context.Context, ch *channel, flags uint8,
	typ codec.PacketType, payload []byte) (codec.Frame, error) {

	if s.isClosed() {
		return codec.Frame{}, ErrSessionClosed
	}

	reply, err := s.link.exchange(ctx, ch, s.frame(flags, typ, payload))
	if err != nil {
		return codec.Frame{}, err
	}
	switch reply.Type {
	case codec.MsgNack, codec.MsgInvCmd, codec.MsgInvSess, codec.MsgBusy:
		return reply, protocolError(typ.String(), reply)
	default:
		return reply, nil
	}
}

// frame builds an outbound frame with this session's addressing.
func (s *Session) frame(flags uint8, typ codec.PacketType, payload []byte) codec.Frame {
	return codec.Frame{
		Dst:     addrWithIndex(s.peer, s.remoteIndex),
		Src:     addrWithIndex(s.link.LocalAddress(), s.localIndex),
		Type:    typ,
		Flags:   flags,
		Payload: payload,
	}
}

// receive routes a frame the link matched to this session.
func (s *Session) receive(f codec.Frame) {
	if f.BackChannel() {
		// A back-channel frame is either the peer's answer to something we
		// pushed, or a push of its own.
		if s.back.busy() && s.back.deliver(f) {
			return
		}
		s.receivePush(f)
		return
	}
	if s.front.deliver(f) {
		return
	}
	// A front-channel frame answering nothing is the peer talking out of
	// turn. Announcements and display updates arrive this way from some
	// units, so it is surfaced rather than dropped.
	s.link.deliverUnsolicited(f)
}

// receivePush queues a back-channel push for the application.
//
// The acknowledgement is deliberately not sent here. It is what asks the peer
// for the next push, so sending it before the application has taken this one
// throws away the flow control the protocol provides. If the queue is full the
// push is dropped and not acknowledged, which stalls the peer rather than
// letting it run ahead of a reader that cannot keep up.
func (s *Session) receivePush(f codec.Frame) {
	p := Push{Frame: f, ack: func() error { return s.Reply(codec.MsgAck, nil) }}
	select {
	case s.pushes <- p:
	default:
		s.link.log.Warn("rollcall: back-channel queue full, push withheld",
			"type", f.Type.String(), "peer", s.peer.String())
	}
}

// Ack acknowledges a push. A consumer calls it once it has taken the message;
// until it does, the peer will not send another.
func (p Push) Ack() error { return p.ack() }

// EnableBackChannel turns the back channel on.
//
// Two messages are needed, and the second is easy to forget: BkChnReady opens
// the channel, and control-value pushes additionally require ReportChange.
// With only the first, a session looks subscribed and never receives a value.
//
// On enable the peer flushes every changed value, so a burst follows
// immediately; that is the specification's behaviour, not a fault.
func (s *Session) EnableBackChannel(ctx context.Context, reportValues bool) error {
	if _, err := s.Do(ctx, codec.MsgBkChnReady, []byte{codec.BackChannelEnable}); err != nil {
		return err
	}
	if !reportValues {
		return nil
	}
	return s.reportChange(ctx, codec.ReportAllCommands)
}

// DisableBackChannel closes the back channel again.
func (s *Session) DisableBackChannel(ctx context.Context) error {
	_, err := s.Do(ctx, codec.MsgBkChnReady, []byte{codec.BackChannelDisable})
	return err
}

// reportChange asks for value pushes. The wildcard command asks for all of
// them, which every server honours; per-command selection is optional and not
// every server implements it.
func (s *Session) reportChange(ctx context.Context, command uint16) error {
	_, err := s.Do(ctx, codec.MsgRepFChg, []byte{byte(command >> 8), byte(command)})
	return err
}

// Term ends the session.
//
// It is always sent, on close, on error and on cancellation. Servers do not
// time idle sessions out — the vendor's own comments warn that units "don't
// time them out very well (at all), and then run out of available sessions" —
// so a session we abandon is leaked until the unit reboots.
func (s *Session) Term(ctx context.Context, code codec.TermCode, reason string) error {
	var err error
	s.closeOnce.Do(func() {
		payload, perr := codec.TermSess{Code: code, Reason: reason}.AppendTo(nil)
		if perr != nil {
			// A reason too long for the field must not stop the session
			// being closed; the code alone is what the peer acts on.
			payload, _ = codec.TermSess{Code: code}.AppendTo(nil)
		}

		// Term is sent directly rather than through the channel. The slot may
		// be held by a request that will never be answered, and waiting for
		// it is how a session ends up abandoned instead of closed.
		err = s.link.send(s.frame(0, codec.MsgTerm, payload))
		s.shutdown(ErrSessionClosed)
	})
	return err
}

// Close terminates the session with the ordinary user code.
func (s *Session) Close() error {
	return s.Term(context.Background(), codec.TermUser, "")
}

// linkClosed shuts the session down because the link underneath it went away.
// No Term is sent: there is nothing left to send it on.
func (s *Session) linkClosed(err error) {
	s.closeOnce.Do(func() { s.shutdown(err) })
}

func (s *Session) shutdown(err error) {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()

	s.front.closeWith(err)
	s.back.closeWith(err)
	s.link.removeSession(s.localIndex)
	close(s.pushes)
}

func (s *Session) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// addrWithIndex returns a copy of a carrying the given session index.
func addrWithIndex(a codec.Address, idx int16) codec.Address {
	a.Index = idx
	return a
}
