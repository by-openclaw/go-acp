package session

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"dhs/internal/snell-rollcall/codec"
)

// Keepalive probes an idle link and closes it when the peer stops answering.
//
// It has to probe, because silence carries no information. Measured against
// the live stack during the audit: an idle connection receives exactly zero
// unsolicited frames over ninety seconds, and the session survives that
// silence intact. So a quiet link is indistinguishable from a dead one until
// something is sent.
//
// KeepAlive is answered with Ack by both a real device and the proxy, on both
// an unconnected and a connected session, in four milliseconds. It is the
// specification's own link probe and costs an empty payload, where GetDevInfo
// costs a forty-byte reply; GetDevInfo remains the fallback for a peer that
// ignores the type.
type Keepalive struct {
	link    *Link
	session *Session
	misses  atomic.Int64
	probes  atomic.Uint64
}

// NewKeepalive starts probing a link.
//
// When s is non-nil the probe is sent on that session, which proves the
// session still exists as well as the socket. On the unconnected index a
// successful probe proves only that the peer's stack is alive, and the session
// is what actually breaks.
func NewKeepalive(ctx context.Context, l *Link, s *Session) *Keepalive {
	k := &Keepalive{link: l, session: s}

	interval := l.cfg.keepaliveInterval()
	if interval <= 0 {
		return k // explicitly disabled
	}

	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		k.loop(ctx, interval)
	}()
	return k
}

// Misses reports how many consecutive probes went unanswered.
func (k *Keepalive) Misses() int { return int(k.misses.Load()) }

// Probes reports how many probes have been sent.
func (k *Keepalive) Probes() uint64 { return k.probes.Load() }

func (k *Keepalive) loop(ctx context.Context, interval time.Duration) {
	t := k.link.clk.NewTicker(interval)
	defer t.Stop()

	max := k.link.cfg.maxKeepaliveMisses()
	for {
		select {
		case <-ctx.Done():
			return
		case <-k.link.done:
			return
		case <-t.C():
			if err := k.probe(ctx); err != nil {
				if k.misses.Add(1) >= int64(max) {
					k.link.closeWith(fmt.Errorf("%w: %d probes unanswered", ErrLinkDead, max))
					return
				}
				continue
			}
			k.misses.Store(0)
		}
	}
}

// Probe sends one keepalive and waits for the answer.
//
// It goes through the ordinary send queue because it is an active message like
// any other. Injecting it beside a pending request would have it matched
// head-of-queue against that request's reply, which is a correctness bug
// rather than a shortcut.
func (k *Keepalive) Probe(ctx context.Context) error { return k.probe(ctx) }

func (k *Keepalive) probe(ctx context.Context) error {
	k.probes.Add(1)

	if k.session != nil {
		_, err := k.session.Do(ctx, codec.MsgKeepAlive, nil)
		return err
	}

	req := codec.Frame{
		Dst:  addrWithIndex(k.link.RemoteAddress(), codec.IndexUnknown),
		Src:  addrWithIndex(k.link.LocalAddress(), codec.IndexUnknown),
		Type: codec.MsgKeepAlive,
	}
	reply, err := k.link.exchange(ctx, k.link.blind, req)
	if err != nil {
		return err
	}
	if reply.Type != codec.MsgAck {
		return protocolError("keepalive", reply)
	}
	return nil
}
