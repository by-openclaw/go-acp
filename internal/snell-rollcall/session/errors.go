package session

import (
	"errors"
	"fmt"

	"dhs/internal/snell-rollcall/codec"
)

// Sentinel errors. Call sites use errors.Is and errors.As, never string
// matching (root CLAUDE.md).
var (
	// ErrLinkClosed means the link is no longer usable. It is returned to
	// everything still waiting when a link shuts down, so a caller blocked on
	// a request learns immediately rather than at its own timeout.
	ErrLinkClosed = errors.New("rollcall: link closed")

	// ErrSessionClosed means the session was terminated. The link may still
	// be fine and other sessions on it unaffected.
	ErrSessionClosed = errors.New("rollcall: session closed")

	// ErrTimeout means a reply did not arrive within the active-message
	// deadline, including any extension the peer asked for with Wait.
	ErrTimeout = errors.New("rollcall: reply timeout")

	// ErrSessionDead means the session exceeded its strike count. Nothing
	// further may be sent on it; the caller reconnects.
	ErrSessionDead = errors.New("rollcall: session failed too many times")

	// ErrLinkDead means the link's keepalive went unanswered often enough
	// that the peer is assumed gone.
	ErrLinkDead = errors.New("rollcall: link is not answering")

	// ErrBusy means the peer refused a Call because it has no session left.
	// It is worth retrying; a Nack is not.
	ErrBusy = errors.New("rollcall: peer is busy")

	// ErrRefused means the peer refused a Call outright. Services are
	// all-or-nothing, so the usual cause is asking for a service bit the
	// peer does not provide.
	ErrRefused = errors.New("rollcall: call refused")

	// ErrInvalidSession means the peer did not recognise the session index we
	// used. During the audit this was produced by sending our own index where
	// the peer's belonged, which silently lost every push.
	ErrInvalidSession = errors.New("rollcall: peer rejected the session index")

	// ErrNotUnderstood means the peer does not implement the message type,
	// which is different from refusing to act on it.
	ErrNotUnderstood = errors.New("rollcall: peer does not implement this message")

	// ErrUnexpectedReply means a reply arrived whose type cannot answer the
	// request that was in flight.
	ErrUnexpectedReply = errors.New("rollcall: unexpected reply type")
)

// ProtocolError is a refusal the peer sent us, carrying the frame it came in
// so a caller can inspect the payload rather than guess.
type ProtocolError struct {
	// Op names what was being attempted, e.g. "call" or "get".
	Op string

	// Type is the message the peer answered with.
	Type codec.PacketType

	// Detail is any text the peer supplied. Nack and Busy may carry one.
	Detail string

	// Err is the sentinel this maps to.
	Err error
}

func (e *ProtocolError) Error() string {
	s := fmt.Sprintf("rollcall: %s: %s", e.Op, e.Type)
	if e.Detail != "" {
		s += fmt.Sprintf(" (%s)", e.Detail)
	}
	return s
}

func (e *ProtocolError) Unwrap() error { return e.Err }

// protocolError maps a refusal message to its sentinel.
//
// The distinction that matters is between Nack and InvCmd: Nack means the peer
// understood and will not, InvCmd means it did not understand at all
// (spec 9.15). A client that treats them alike either retries something that
// can never work, or gives up on a peer that simply wanted a different
// message.
func protocolError(op string, f codec.Frame) error {
	e := &ProtocolError{Op: op, Type: f.Type}
	switch f.Type {
	case codec.MsgBusy:
		e.Err = ErrBusy
	case codec.MsgNack:
		e.Err = ErrRefused
	case codec.MsgInvCmd:
		e.Err = ErrNotUnderstood
	case codec.MsgInvSess:
		e.Err = ErrInvalidSession
	default:
		e.Err = ErrUnexpectedReply
	}
	e.Detail = refusalText(f)
	return e
}

// refusalText reads the optional text a refusal may carry. Nack and Ack may
// carry a string, and Busy may carry an ID_STR naming who holds the session;
// none is required, and the frame is well formed without one.
func refusalText(f codec.Frame) string {
	switch f.Type {
	case codec.MsgBusy:
		if id, err := codec.DecodeID(f.Payload); err == nil {
			return id.Name
		}
	case codec.MsgNack, codec.MsgAck:
		if len(f.Payload) > 0 {
			s, _ := codec.CString(f.Payload)
			return s
		}
	}
	return ""
}
