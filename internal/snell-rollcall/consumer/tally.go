package rollcall

import (
	"context"
	"fmt"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/codec/router"
)

// Tally is the reason the back channel exists on a router.
//
// A set does not tell a client what happened: the reply carries the crosspoint
// from before the change, and the new one arrives afterwards as a push. So a
// panel that shows what is routed has to subscribe, and a client that sets
// without subscribing is guessing.
//
// The pushes carry command numbers, not destinations, so what this file adds to
// the value stream is the arithmetic in reverse: which matrix, level and
// destination a command belongs to.

// TallyFunc is called for each crosspoint change. It must not block: it runs on
// the delivery loop, and the acknowledgement that asks for the next push waits
// behind it.
type TallyFunc func(Crosspoint)

// WatchRoutes reports every crosspoint change on a router.
//
// It reports changes and nothing else. The specification says enabling the back
// channel asks for what has already changed as well, but the vendor controller
// sends nothing until something moves: subscribing to a level and waiting
// delivers no crosspoints at all. So a caller drawing a panel reads the state
// it wants to show with Routes and keeps it current from here, rather than
// waiting for a picture that never arrives.
func (p *Plugin) WatchRoutes(ctx context.Context, r *RouterInterface, fn TallyFunc) error {
	if fn == nil {
		return fmt.Errorf("rollcall: watching routes needs a callback")
	}

	s, err := p.session(ctx, r.Slot)
	if err != nil {
		return err
	}

	index := r.destIndex()

	if err := s.EnableBackChannel(ctx, true); err != nil {
		return fmt.Errorf("rollcall: open the back channel on slot %d: %w", r.Slot, err)
	}

	p.mu.RLock()
	stop := p.events
	p.mu.RUnlock()

	go func() {
		for {
			select {
			case <-stop:
				return
			case push, ok := <-s.Pushes():
				if !ok {
					return
				}
				if xpt, ok := index.crosspoint(push.Frame); ok {
					fn(xpt)
				}
				// The acknowledgement goes after the callback, because it is
				// what asks for the next push: sending it first throws away
				// the only flow control the protocol has.
				if err := push.Ack(); err != nil {
					p.log.Debug("rollcall: could not acknowledge a tally push",
						"slot", r.Slot, "err", err)
					return
				}
			}
		}
	}()
	return nil
}

// destIndex is the reverse of the command arithmetic: which destination a
// command number belongs to.
type destIndex struct {
	levels []indexedLevel
}

type indexedLevel struct {
	matrix uint32
	level  uint32
	dsts   router.Table
}

// destIndex builds the reverse lookup for every level of every matrix.
func (r *RouterInterface) destIndex() destIndex {
	var idx destIndex
	for _, m := range r.Matrices {
		for _, lv := range m.Levels {
			idx.levels = append(idx.levels, indexedLevel{
				matrix: m.Number, level: lv.Number, dsts: lv.Dsts,
			})
		}
	}
	return idx
}

// crosspoint turns a pushed value into a crosspoint change, if it is one.
//
// A router pushes on the same channel as everything else, so a push that is not
// a routed-source command is not a fault: it may be a protect, a name, or a
// display line. Anything this cannot place is left alone.
func (i destIndex) crosspoint(f codec.Frame) (Crosspoint, bool) {
	if f.Type != codec.MsgRetValue && f.Type != codec.MsgSetValue {
		return Crosspoint{}, false
	}
	v, err := codec.DecodeValue(f.Payload)
	if err != nil {
		return Crosspoint{}, false
	}

	for _, lv := range i.levels {
		n, off, ok := lv.dsts.Index(router.Command(v.Command))
		if !ok || off != router.OffDestRoutedSrc {
			continue
		}
		xpt, err := decodeCrosspoint(v, router.SourcePin{
			Matrix: uint8(lv.matrix), Level: uint8(lv.level), Source: uint16(n),
		})
		if err != nil {
			return Crosspoint{}, false
		}
		return xpt, true
	}
	return Crosspoint{}, false
}

// Routes reads every crosspoint of one level.
//
// One command per destination, which is what the protocol offers: there is no
// bulk crosspoint read, only the bulk names. On a large level that is thousands
// of round trips, and there is no way around it — the controller will not
// replay the current state on the back channel, so a panel reads once here and
// keeps it current from WatchRoutes afterwards.
func (p *Plugin) Routes(ctx context.Context, r *RouterInterface,
	matrix, level uint32) ([]Crosspoint, error) {

	lv, err := r.Level(matrix, level)
	if err != nil {
		return nil, err
	}

	out := make([]Crosspoint, 0, lv.Dsts.Count)
	for d := uint32(1); d <= lv.Dsts.Count; d++ {
		xpt, err := p.Route(ctx, r, matrix, level, d)
		if err != nil {
			return out, err
		}
		out = append(out, xpt)
	}
	return out, nil
}
