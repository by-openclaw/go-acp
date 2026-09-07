package session

import (
	"context"
	"fmt"

	"dhs/internal/snell-rollcall/codec"
)

// A link is the calling side by default: it opens sessions and drives
// requests. This file is the answering side, which is the same machine with
// the roles exchanged.
//
// The difference is where a frame goes when it matches nothing. On the calling
// side, a frame that answers no request is an announcement and may be dropped;
// on the answering side it is a request, and dropping it means a client waits
// out its timeout for something we chose not to read.

// Handler answers what arrives on a link that serves rather than calls.
//
// Every method is called from the link's read loop, so an implementation must
// not block: a slow answer stalls every session on the link, including the
// keepalives that decide whether the link is alive at all. Work that takes
// time belongs on its own goroutine, with the reply sent when it finishes.
type Handler interface {
	// Call decides whether to accept a session. Returning a nil error accepts
	// it; returning one refuses with the message that error names, which is
	// Busy when there are no sessions left and Nack when the services asked
	// for cannot be supplied.
	Call(l *Link, req codec.Frame, conn codec.Connect) error

	// Request answers a message on an established session.
	Request(s *Session, req codec.Frame)

	// Unsolicited handles a message that belongs to no session: the first
	// GetDevInfo of a connection, a keepalive on the unconnected index, an
	// announcement from a peer.
	Unsolicited(l *Link, req codec.Frame)
}

// Refusal is an error that names the message a handler wants sent back.
//
// It exists so a handler can say "no, and here is why" in one return value:
// the distinction between Busy, Nack and InvCmd is the whole of what a client
// learns from a refusal, and flattening them to a bare error would lose it.
type Refusal struct {
	Type    codec.PacketType
	Payload []byte
	Reason  string
}

func (r *Refusal) Error() string {
	if r.Reason == "" {
		return "rollcall: refused with " + r.Type.String()
	}
	return "rollcall: refused with " + r.Type.String() + ": " + r.Reason
}

// RefuseBusy refuses a call because there is no session left. It is worth
// retrying, which is what distinguishes it from a refusal.
func RefuseBusy(reason string) error {
	return &Refusal{Type: codec.MsgBusy, Reason: reason}
}

// RefuseNack refuses a call or a request outright, optionally with text the
// client can show a user.
//
// The text matters more than it looks: the router document asks a client to
// display it and revert its own value, so a route that could not be made says
// why rather than silently failing.
func RefuseNack(reason string) error {
	r := &Refusal{Type: codec.MsgNack, Reason: reason}
	if reason != "" {
		r.Payload = append([]byte(reason), 0)
	}
	return r
}

// RefuseInvalidCommand refuses a message the server does not implement, which
// is different from refusing to act on one it does (spec 9.15).
func RefuseInvalidCommand() error {
	return &Refusal{Type: codec.MsgInvCmd}
}

// refusalFrame turns an error into the message to send back.
func refusalFrame(err error) (codec.PacketType, []byte) {
	var r *Refusal
	if ok := asRefusal(err, &r); ok {
		return r.Type, r.Payload
	}
	// A handler that failed for its own reasons rather than deciding to
	// refuse. Nack is the honest answer: we understood and could not.
	return codec.MsgNack, nil
}

func asRefusal(err error, target **Refusal) bool {
	for err != nil {
		if r, ok := err.(*Refusal); ok {
			*target = r
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// Accept turns an inbound Call into a session.
//
// The indices are the mirror of what a caller does. The client chose its own
// and put it in the source of the Call, so that is what every push must be
// addressed to; we choose ours and put it in the source of the acknowledgement,
// so that is what every later request from the client will carry. Getting them
// the wrong way round produces SP_INVSESS on the client and loses every push.
func Accept(l *Link, req codec.Frame, conn codec.Connect) (*Session, error) {
	s := &Session{
		link:        l,
		peer:        req.Src.Device(),
		remoteIndex: req.Src.Index,
		services:    conn.Services,
		userLevel:   conn.UserLevel,
		pushes:      make(chan Push, l.cfg.pushQueue()),
		server:      true,
		local:       req.Dst.Device(),
	}
	s.front = newChannel("front", l.clk, l.cfg.replyTimeout(), l.cfg.maxStrikes())
	s.back = newChannel("back", l.clk, l.cfg.replyTimeout(), l.cfg.maxStrikes())

	if err := l.register(s); err != nil {
		return nil, err
	}

	// The acknowledgement carries our index as its source, which is what the
	// client will address everything to from now on.
	ack := codec.Frame{
		Dst:  addrWithIndex(req.Src, s.remoteIndex),
		Src:  addrWithIndex(req.Dst, s.localIndex),
		Type: codec.MsgAck,
	}
	if err := l.send(ack); err != nil {
		l.removeSession(s.localIndex)
		return nil, err
	}
	return s, nil
}

// Answer sends a reply to a request on this session.
//
// A reply carries no flags: it travels on the channel the request came in on,
// and the front channel is where a client's requests live.
func (s *Session) Answer(typ codec.PacketType, payload []byte) error {
	return s.link.send(s.frame(0, typ, payload))
}

// Refuse answers a request with the message an error names.
func (s *Session) Refuse(err error) error {
	typ, payload := refusalFrame(err)
	return s.Answer(typ, payload)
}

// IsServer reports whether this session was accepted rather than opened.
func (s *Session) IsServer() bool { return s.server }

// serveCall handles an inbound Call on a link that has a handler.
func (l *Link) serveCall(f codec.Frame) {
	conn, err := codec.DecodeConnect(f.Payload)
	if err != nil {
		l.replyTo(f, codec.MsgNack, nil)
		return
	}

	if err := l.cfg.Handler.Call(l, f, conn); err != nil {
		typ, payload := refusalFrame(err)
		l.replyTo(f, typ, payload)
		return
	}
	// The handler accepted and is responsible for having called Accept, which
	// sends the acknowledgement.
}

// SendFrame writes one frame, for a server that has to address a reply itself
// rather than answering on a session.
//
// The handshake is the case that needs it: the reply's destination is the
// address being assigned to the client, which is neither where the request
// came from nor anywhere a session knows about.
func (l *Link) SendFrame(f codec.Frame) error { return l.send(f) }

// replyTo sends a frame back to whoever sent req, swapping the addresses.
func (l *Link) replyTo(req codec.Frame, typ codec.PacketType, payload []byte) {
	err := l.send(codec.Frame{
		Dst:     req.Src,
		Src:     req.Dst,
		Type:    typ,
		Payload: payload,
	})
	if err != nil {
		l.log.Debug("rollcall: could not answer a request",
			"type", req.Type.String(), "err", err)
	}
}

// Terminate ends a session from the serving side without sending Term.
//
// A client that has gone away is not listening, and a server that keeps its
// half of a dead session is how a unit runs out of them. This is what a
// handler calls when it sees the client's own Term.
func (s *Session) Terminate(reason error) {
	if reason == nil {
		reason = ErrSessionClosed
	}
	if !s.beginClose() {
		return
	}
	s.shutdown(reason)
}

// Describe renders the session for a log line: who, which services, what
// level.
func (s *Session) Describe() string {
	return fmt.Sprintf("%s svc=%s level=%s local=%d remote=%d",
		s.peer, s.services, s.userLevel, s.localIndex, s.remoteIndex)
}

// ServeContext runs until the link stops, so a caller can wait on a served
// link the way it waits on a server.
func (l *Link) ServeContext(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-l.done:
		return l.Err()
	}
}
