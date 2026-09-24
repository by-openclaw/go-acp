package consumer

import (
	"log/slog"
	"net"

	"dhs/internal/snmp/codec"
)

// InformRequest, the receiving half.
//
// An InformRequest is a notification that is ACKNOWLEDGED. That single
// difference is the reason it exists: a trap is fire-and-forget, so a
// sender never learns that the alarm it sent during a link flap went
// into a dropped datagram, while an inform is retried until the
// receiver answers. RFC 3416 §4.2.7: the receiver answers with a
// Response carrying the same request-id and the same varbinds.
//
// Which makes half-supporting it worse than not supporting it. A
// receiver that reads informs and never answers leaves every sender in
// the plant retrying the same alarm until it gives up, once per
// notification, forever — so the answer is sent from the same socket
// the inform arrived on, before the handler is called, and a failure
// to answer is counted rather than dropped.

// Compliance events the inform path records.
const (
	// InformUnacknowledged is an inform this receiver could not answer:
	// the reply did not encode, or the socket would not take it. The
	// sender will retry, and an operator should know why.
	InformUnacknowledged = "snmp_inform_not_acknowledged"
	// InformV3Unsealed is a v3 inform that arrived authenticated and
	// could not be answered the same way — no engine, or a user this
	// receiver cannot seal as. Answering it in the clear would be
	// refused by the sender, so nothing is sent.
	InformV3Unsealed = "snmp_inform_v3_reply_unsealed"
)

// acknowledge answers an InformRequest.
//
// The response is the request's own request-id and varbinds with the
// PDU type changed, which is what RFC 3416 §4.2.7 asks for and what
// every sender matches on. A v3 inform is answered sealed as the user
// it arrived as, because a sender that authenticated the notification
// will not accept an unauthenticated acknowledgement of it.
func (l *Listener) acknowledge(conn net.PacketConn, to *net.UDPAddr, m codec.Message, user string) {
	if m.PDU == nil {
		return
	}
	resp := codec.PDU{
		Type:      codec.PDUTypeResponse,
		RequestID: m.PDU.RequestID,
		VarBinds:  m.PDU.VarBinds,
	}

	var (
		raw []byte
		err error
	)
	if m.Version == codec.Version3 {
		if l.opts.Engine == nil {
			l.prof.Note(InformV3Unsealed)
			return
		}
		raw, err = l.opts.Engine.Seal(codec.Message{
			Version: codec.Version3,
			V3: &codec.V3{
				ID:      m.V3.ID,
				MaxSize: codec.DefaultMaxSize,
				// The acknowledgement is scoped to the SENDER's engine,
				// which is the authoritative one for a notification —
				// the receiver is authoritative for nothing here.
				ContextEngineID: m.V3.ContextEngineID,
				ContextName:     m.V3.ContextName,
			},
			PDU: &resp,
		}, user)
	} else {
		raw, err = codec.Encode(codec.Message{
			Version: m.Version, Community: m.Community, PDU: &resp,
		})
	}
	if err != nil {
		l.prof.Note(InformUnacknowledged)
		l.logger.Warn("snmp: could not build the inform acknowledgement",
			slog.String("to", to.String()), slog.String("err", err.Error()))
		return
	}
	if _, err := conn.WriteTo(raw, to); err != nil {
		l.prof.Note(InformUnacknowledged)
		l.logger.Warn("snmp: could not send the inform acknowledgement",
			slog.String("to", to.String()), slog.String("err", err.Error()))
		return
	}
	l.logger.Debug("snmp: inform acknowledged",
		slog.String("to", to.String()), slog.Int64("request_id", int64(resp.RequestID)))
}
