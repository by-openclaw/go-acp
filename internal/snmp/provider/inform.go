package provider

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"dhs/internal/snmp/codec"
)

// InformRequest, the sending half.
//
// A trap is fire-and-forget: it goes out once, and a sender never
// learns that the alarm it sent during a link flap went into a dropped
// datagram. An inform is acknowledged, so it can be retried until the
// receiver answers — which is the only way a notification over UDP
// ever becomes something you can rely on having arrived.
//
// That makes the retry the feature, not an implementation detail. The
// same request-id goes out on every attempt (RFC 3416 §4.2.7: the
// acknowledgement carries it back), so a receiver that answered a
// retry late is still recognised, and a receiver that got two copies
// can tell they were one event.

// DefaultInformTimeout and DefaultInformRetries are what one inform
// costs before it is given up on: three attempts a second apart. Chosen
// to outlast a spanning-tree reconvergence on this plant's fabric
// rather than from taste.
const (
	DefaultInformTimeout = time.Second
	DefaultInformRetries = 2
)

// ErrInformNotAcknowledged is an inform no receiver answered. The
// notification did not arrive — or its acknowledgement did not — and
// either way nothing downstream should assume it was seen.
var ErrInformNotAcknowledged = errors.New("snmp: inform was not acknowledged")

// InformResult is what one destination did with one inform.
type InformResult struct {
	// Dest is the manager this describes.
	Dest TrapDestination
	// Attempts is how many datagrams it took, 1 when the first was
	// answered.
	Attempts int
	// Err is nil when the receiver acknowledged.
	Err error
}

// SendInform emits n as an InformRequest to every destination and
// waits for each acknowledgement.
//
// Every destination is attempted even when an earlier one fails, for
// the same reason Send does it: a receiver that is down must not stop
// the others hearing about an alarm. The results come back one per
// destination, in order, so a caller can say WHICH manager did not
// answer rather than only that something did not.
func (s *TrapSender) SendInform(ctx context.Context, n Notification) []InformResult {
	dests := s.Destinations()
	out := make([]InformResult, 0, len(dests))
	for _, d := range dests {
		attempts, err := s.informOne(ctx, d, n)
		out = append(out, InformResult{Dest: d, Attempts: attempts, Err: err})
	}
	return out
}

// nextRequestID hands out the next notification's request-id. An
// inform keeps its own across retries, which is how a late
// acknowledgement is still recognised as this one's.
func (s *TrapSender) nextRequestID() int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	return s.nextID
}

// informOne sends one inform and waits for its acknowledgement,
// retrying with the SAME request-id.
func (s *TrapSender) informOne(ctx context.Context, d TrapDestination, n Notification) (int, error) {
	if d.Version == codec.Version1 {
		// v1 has no InformRequest-PDU at all — it was introduced with
		// v2. Saying so is better than sending a trap and letting the
		// caller believe it was acknowledged.
		return 0, fmt.Errorf("snmp: %s is a v1 destination and v1 has no InformRequest", d.Addr)
	}

	id := s.nextRequestID()
	msg, err := n.Message(d.Version, d.Community, id)
	if err != nil {
		return 0, err
	}
	// The one difference from a trap on the wire.
	msg.PDU.Type = codec.PDUTypeInform
	if d.Version == codec.Version3 && msg.V3 != nil {
		// Reportable, unlike a trap: somebody IS waiting for the
		// answer, so a receiver that refuses the message should say so
		// rather than leave the sender retrying into silence.
		msg.V3.Flags |= codec.FlagReportable
	}

	raw, err := s.sealOrEncode(d, msg)
	if err != nil {
		return 0, err
	}
	conn, err := s.connFor(ctx, d.Addr)
	if err != nil {
		return 0, err
	}

	var lastErr error
	for attempt := 1; attempt <= DefaultInformRetries+1; attempt++ {
		if err := ctx.Err(); err != nil {
			return attempt - 1, err
		}
		acked, err := s.informAttempt(ctx, conn, raw, id, d)
		if acked {
			return attempt, nil
		}
		lastErr = err
		if err != nil && !errors.Is(err, errInformTimeout) {
			s.drop(d.Addr)
			return attempt, err
		}
		s.logger.Debug("snmp: inform unanswered, retrying",
			slog.String("to", d.Addr), slog.Int("attempt", attempt))
	}
	return DefaultInformRetries + 1, fmt.Errorf("%w by %s after %d attempt(s): %v",
		ErrInformNotAcknowledged, d.Addr, DefaultInformRetries+1, lastErr)
}

// errInformTimeout is one unanswered attempt, which is a retry rather
// than a failure.
var errInformTimeout = errors.New("snmp: no acknowledgement yet")

// informAttempt sends once and waits once.
func (s *TrapSender) informAttempt(ctx context.Context, conn net.Conn, raw []byte,
	id int32, d TrapDestination) (bool, error) {
	deadline := time.Now().Add(DefaultInformTimeout)
	if t, ok := ctx.Deadline(); ok && t.Before(deadline) {
		deadline = t
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return false, err
	}
	if _, err := conn.Write(raw); err != nil {
		return false, err
	}

	buf := make([]byte, codec.MaxMessageSize)
	for {
		nRead, err := conn.Read(buf)
		if err != nil {
			var nerr net.Error
			if errors.As(err, &nerr) && nerr.Timeout() {
				return false, errInformTimeout
			}
			return false, err
		}
		if s.acknowledges(buf[:nRead], id, d) {
			return true, nil
		}
		// Somebody else's datagram on this socket. Keep waiting for
		// ours; the deadline still bounds it.
	}
}

// acknowledges reports whether a datagram is the acknowledgement for
// this inform.
func (s *TrapSender) acknowledges(raw []byte, id int32, d TrapDestination) bool {
	if d.Version == codec.Version3 && s.engine != nil {
		// The receiver seals its answer as the user the inform came
		// from, so it opens with the same engine that sealed the
		// request.
		m, _, err := s.engine.Open(raw)
		if err != nil {
			s.logger.Debug("snmp: unopenable answer to an inform",
				slog.String("from", d.Addr), slog.String("err", err.Error()))
			return false
		}
		return m.PDU != nil && m.PDU.Type == codec.PDUTypeResponse && m.PDU.RequestID == id
	}
	m, err := codec.Decode(raw)
	if err != nil {
		return false
	}
	return m.PDU != nil && m.PDU.Type == codec.PDUTypeResponse && m.PDU.RequestID == id
}

// sealOrEncode renders a message for a destination: sealed as the
// destination's user under v3, encoded with its community otherwise.
func (s *TrapSender) sealOrEncode(d TrapDestination, msg codec.Message) ([]byte, error) {
	if d.Version == codec.Version3 {
		if s.engine == nil {
			return nil, fmt.Errorf(
				"snmp: %s is a v3 destination and this sender has no USM engine", d.Addr)
		}
		return s.engine.Seal(msg, d.User)
	}
	return codec.Encode(msg)
}
