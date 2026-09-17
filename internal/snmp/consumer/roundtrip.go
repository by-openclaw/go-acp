package consumer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"dhs/internal/snmp/codec"
)

// roundTrip sends one request and waits for its answer, retrying.
//
// UDP loses datagrams, so a manager that did not retry would report a
// device down because one packet was dropped on a busy switch. The
// request-id is the SAME across retries — a retry is the same request,
// not a new one — so a late answer to the first attempt still matches
// and is not thrown away as unsolicited.
func (s *Session) roundTrip(ctx context.Context, p *codec.PDU) (*codec.PDU, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, net.ErrClosed
	}
	s.nextID++
	p.RequestID = s.nextID
	conn := s.conn
	s.mu.Unlock()

	req := codec.Message{Version: s.opts.Version, Community: s.opts.Community, PDU: p}
	raw, err := codec.Encode(req)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := 0; attempt <= s.opts.Retries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		resp, err := s.attempt(ctx, conn, raw, p.RequestID)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !errors.Is(err, ErrTimeout) {
			// A socket that is gone, or a datagram we could not send,
			// will not be better on the next attempt.
			return nil, err
		}
		s.logger.Debug("snmp: retrying",
			slog.Int("attempt", attempt+1), slog.String("to", conn.RemoteAddr().String()))
	}
	return nil, fmt.Errorf("%w after %d attempt(s)", lastErr, s.opts.Retries+1)
}

// attempt is one send and one wait.
func (s *Session) attempt(ctx context.Context, conn net.Conn, raw []byte, id int32) (*codec.PDU, error) {
	deadline := s.clk.Now().Add(s.opts.Timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("snmp: set deadline: %w", err)
	}
	if _, err := conn.Write(raw); err != nil {
		return nil, fmt.Errorf("snmp: send: %w", err)
	}

	buf := make([]byte, codec.MaxMessageSize)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			var nerr net.Error
			if errors.As(err, &nerr) && nerr.Timeout() {
				return nil, ErrTimeout
			}
			return nil, fmt.Errorf("snmp: receive: %w", err)
		}

		resp, ok := s.acceptable(buf[:n], id)
		if !ok {
			// Somebody else's datagram, or a late answer to a request
			// we have given up on. Keep waiting for ours rather than
			// treating it as this request's failure — the deadline on
			// the socket still bounds the wait.
			continue
		}
		return resp, nil
	}
}

// acceptable decides whether a received datagram answers this request.
//
// Everything it rejects is COUNTED rather than logged and forgotten: a
// manager that quietly drops mismatched replies looks identical to one
// talking to a device that never answers, and the difference is the
// whole diagnosis.
func (s *Session) acceptable(raw []byte, id int32) (*codec.PDU, bool) {
	m, err := codec.Decode(raw)
	if err != nil {
		s.prof.Note(UnsolicitedResponse)
		s.logger.Debug("snmp: undecodable reply", slog.String("err", err.Error()))
		return nil, false
	}
	if m.PDU == nil || m.PDU.Type != codec.PDUTypeResponse {
		s.prof.Note(UnsolicitedResponse)
		return nil, false
	}
	if m.PDU.RequestID != id {
		s.prof.Note(UnsolicitedResponse)
		s.logger.Debug("snmp: reply for another request",
			slog.Int64("got", int64(m.PDU.RequestID)), slog.Int64("want", int64(id)))
		return nil, false
	}
	// An answer in the wrong version or under the wrong community is
	// absorbed rather than refused — some agents answer v2c requests as
	// v1, and refusing would mean refusing the device — but it is a
	// deviation and it is counted.
	if m.Version != s.opts.Version {
		s.prof.Note(VersionMismatch)
	}
	if m.Community != s.opts.Community {
		s.prof.Note(CommunityMismatch)
	}
	return m.PDU, true
}

// Walk visits every object under root, calling fn for each.
//
// It is the verb an operator actually uses: a device's own MIB may not
// be to hand, and walking is how its tree is discovered. Under v2c it
// uses GETBULK, which is the difference between one round trip per
// object and one per twenty-five — on the 842-object Snell frame, three
// seconds versus a minute and a half on a fabric with 4ms latency.
//
// fn returning an error stops the walk and is returned; nothing else
// stops it early, because a walk that gave up half way looks exactly
// like a device with half a tree.
func (s *Session) Walk(ctx context.Context, root codec.OID,
	fn func(codec.VarBind) error) error {
	cursor := root
	for {
		binds, err := s.walkStep(ctx, cursor)
		if err != nil {
			return err
		}
		if len(binds) == 0 {
			return nil
		}

		for _, vb := range binds {
			// The end of the subtree, and the end of the whole tree,
			// are both ends of this walk.
			if !vb.Name.HasPrefix(root) || vb.Value.Type == codec.TypeEndOfMIBView {
				return nil
			}
			// RFC 3416 requires GETNEXT to return a strictly greater
			// name. An agent that repeats one walks a manager in a
			// circle forever, so the walk stops and says so.
			if vb.Name.Compare(cursor) <= 0 {
				s.prof.Note(TruncatedWalk)
				return fmt.Errorf("snmp: agent did not advance past %s", cursor)
			}
			if err := fn(vb); err != nil {
				return err
			}
			cursor = vb.Name
		}
	}
}

// walkStep is one hop of a walk: a GETBULK under v2c, a GETNEXT under
// v1, which has no bulk.
func (s *Session) walkStep(ctx context.Context, from codec.OID) ([]codec.VarBind, error) {
	if s.opts.Version == codec.Version1 {
		binds, err := s.GetNext(ctx, from)
		if err != nil {
			// v1 says "past the end of the tree" with noSuchName, which
			// is the end of a walk rather than a failure of one.
			if isEndOfTree(err) {
				return nil, nil
			}
			return nil, err
		}
		return binds, nil
	}

	resp, err := s.roundTrip(ctx, &codec.PDU{
		Type:           codec.PDUTypeGetBulk,
		NonRepeaters:   0,
		MaxRepetitions: s.opts.MaxRepetitions,
		VarBinds:       []codec.VarBind{{Name: from, Value: codec.Null()}},
	})
	if err != nil {
		return nil, err
	}
	if err := statusError(resp); err != nil {
		return nil, err
	}
	if len(resp.VarBinds) > s.opts.MaxRepetitions {
		// More than was asked for. Taking the extras would be trusting
		// a count the agent already disregarded, so they are dropped
		// and the deviation is counted.
		s.prof.Note(BulkOverrun)
		resp.VarBinds = resp.VarBinds[:s.opts.MaxRepetitions]
	}
	return resp.VarBinds, nil
}

// isEndOfTree reports whether a v1 error means the walk is over rather
// than that something went wrong. v1 has no endOfMibView, so noSuchName
// is the only way it can say "there is nothing after this".
func isEndOfTree(err error) bool {
	return errors.Is(err, &StatusError{Status: codec.NoSuchName})
}

// WalkAll is Walk collecting the result, for a caller that wants the
// subtree rather than a callback.
//
// It is bounded: a device with a table that grows while it is walked
// would otherwise fill memory, and an agent that never says endOfMibView
// is a real failure mode rather than a hypothetical one.
func (s *Session) WalkAll(ctx context.Context, root codec.OID, limit int) ([]codec.VarBind, error) {
	if limit <= 0 {
		limit = DefaultWalkLimit
	}
	var out []codec.VarBind
	err := s.Walk(ctx, root, func(vb codec.VarBind) error {
		if len(out) >= limit {
			return fmt.Errorf("snmp: walk of %s exceeded %d objects", root, limit)
		}
		out = append(out, vb)
		return nil
	})
	if err != nil {
		return out, err
	}
	return out, nil
}

// DefaultWalkLimit bounds WalkAll. The largest tree in docs/testbed.md
// is the Snell frame's 842 objects, so this leaves room for a device an
// order of magnitude larger before it is in the way.
const DefaultWalkLimit = 20000

// Deadline exists so the timeout is visible to a caller composing its
// own: a poller that gives a session two seconds and itself one is a
// poller that reports timeouts it caused.
func (s *Session) Timeout() time.Duration { return s.opts.Timeout }
