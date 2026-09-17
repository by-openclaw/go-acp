package rollcall

import (
	"context"
	"fmt"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

// A push is a request, not a notification: the client must acknowledge it
// before the next may be sent. So a change cannot be written to the socket
// from wherever it happened — it has to be queued, and one goroutine per
// subscriber has to send them in turn and wait for each acknowledgement.
//
// That is also why the queue is bounded and drops rather than blocks. The
// alternative is a client that stops acknowledging holding up whichever
// goroutine changed a value, which on a frame with a hundred cards means one
// slow panel stopping the whole device.

// pushQueue is how many changes a subscriber may fall behind by.
//
// A client that has not caught up within this many changes is not going to:
// what it needs then is the current value, which it gets by reading, not the
// hundredth stale one.
const pushQueue = 64

// pending is one message waiting to go to a subscriber.
//
// A value is carried as a value rather than as bytes because the two
// generations encode it differently, and one change may go to subscribers of
// both. Anything else is already encoded, because it is the same on both.
type pending struct {
	value   codec.Value
	isValue bool

	typ     codec.PacketType
	payload []byte
}

// subscriber is a session that asked for pushes, and the goroutine that sends
// them.
type subscriber struct {
	s     *session.Session
	queue chan pending
	done  chan struct{}
}

// setSubscribed turns pushes on or off for a session.
//
// Enabling twice is not an error and does not start a second pump: a client
// may re-enable the back channel after a reconnection without having told us
// the first one ended.
func (st *linkState) setSubscribed(s *session.Session, on bool) (*subscriber, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()

	sub, existing := st.subscribed[s.LocalIndex()]
	if !on {
		if existing {
			delete(st.subscribed, s.LocalIndex())
			close(sub.done)
		}
		return nil, false
	}
	if existing {
		return sub, false
	}

	sub = &subscriber{
		s:     s,
		queue: make(chan pending, pushQueue),
		done:  make(chan struct{}),
	}
	st.subscribed[s.LocalIndex()] = sub
	return sub, true
}

// subscribe turns pushes on and starts the goroutine that sends them.
func (p *Provider) subscribe(st *linkState, s *session.Session) *subscriber {
	sub, created := st.setSubscribed(s, true)
	if created {
		p.wg.Add(1)
		go p.pump(sub)
	}
	return sub
}

// subscribers returns everything on this link that wants pushes.
func (st *linkState) subscribers() []*subscriber {
	st.mu.Lock()
	defer st.mu.Unlock()

	out := make([]*subscriber, 0, len(st.subscribed))
	for _, sub := range st.subscribed {
		out = append(out, sub)
	}
	return out
}

// pump sends one subscriber's queue in order, waiting for each acknowledgement.
func (p *Provider) pump(sub *subscriber) {
	defer p.wg.Done()

	for {
		select {
		case <-sub.done:
			return
		case <-p.done:
			return
		case msg := <-sub.queue:
			typ, payload, err := encodePush(sub.s, msg)
			if err != nil {
				p.log.Debug("rollcall: a change would not encode for this session",
					"session", sub.s.Describe(), "err", err)
				continue
			}
			if err := sub.s.Push(context.Background(), typ, payload); err != nil {
				// A push that fails means the session is gone or the client
				// stopped answering. Either way there is nothing to send to,
				// so the pump ends rather than retrying into a dead socket.
				p.log.Debug("rollcall: a push was not acknowledged",
					"session", sub.s.Describe(), "err", err)
				return
			}
		}
	}
}

// encodePush renders a queued message for the generation a subscriber speaks.
func encodePush(s *session.Session, msg pending) (codec.PacketType, []byte, error) {
	if !msg.isValue {
		return msg.typ, msg.payload, nil
	}
	if s.Uses32Bit() {
		payload, err := msg.value.AppendTo(nil)
		return codec.MsgRetValue, payload, err
	}
	if msg.value.Command > 0xFFFF {
		// A command that does not fit cannot be named to this client at all,
		// so there is nothing to tell it. Its menu does not list the line
		// either, which is what makes this consistent rather than a silent
		// gap.
		return 0, nil, fmt.Errorf("command %d needs the long-string generation", msg.value.Command)
	}
	payload, _ := funcStatusOf(msg.value).AppendTo(nil)
	return codec.MsgSetParam, payload, nil
}

// enqueue hands a message to a subscriber, dropping it if the client is that
// far behind.
func (p *Provider) enqueue(sub *subscriber, msg pending) {
	select {
	case sub.queue <- msg:
	default:
		p.fire(EventPushDropped, fmt.Sprintf(
			"%s is more than %d changes behind; a push was dropped",
			sub.s.Describe(), pushQueue))
	}
}

// publish tells every subscriber to a slot that a value changed.
func (p *Provider) publish(ctx context.Context, slot uint8, v codec.Value) {
	p.publishExcept(ctx, nil, slot, v)
}

// publishExcept is publish, without telling the session that made the change.
//
// The one that wrote already has the answer in its reply, and pushing the same
// change back to it makes a client that echoes what it hears loop.
func (p *Provider) publishExcept(_ context.Context, except *session.Session, slot uint8, v codec.Value) {
	// A value belongs to the control service. Pushing one at a session that
	// asked for the map, or for files, sends it a message it has no reason to
	// understand: measured against a vendor Control Panel, which answers
	// SETPARAM on its map session with INVSESS every time.
	for _, sub := range p.slotSubscribers(slot, codec.SvcControl) {
		if except != nil && sub.s == except {
			continue
		}
		p.enqueue(sub, pending{value: v, isValue: true})
	}
}

// pushToSlot sends an already-encoded message to a slot's subscribers, which
// is what a display line is: the same bytes whichever generation is listening.
func (p *Provider) pushToSlot(_ context.Context, slot uint8, typ codec.PacketType, need codec.Service, payload []byte) {
	for _, sub := range p.slotSubscribers(slot, need) {
		p.enqueue(sub, pending{typ: typ, payload: payload})
	}
}

// slotSubscribers returns the subscribers listening to one slot that hold the
// service a message belongs to.
//
// Enabling the back channel is not itself a claim to be told everything. A
// session is told what its own services cover and nothing else, because a
// client that negotiated one service and is sent another has no way to place
// the message except to refuse it.
func (p *Provider) slotSubscribers(slot uint8, need codec.Service) []*subscriber {
	p.mu.RLock()
	states := make([]*linkState, 0, len(p.links))
	for _, st := range p.links {
		states = append(states, st)
	}
	p.mu.RUnlock()

	var out []*subscriber
	for _, st := range states {
		for _, sub := range st.subscribers() {
			if sub.s.LocalAddress().Port != slot {
				continue
			}
			if need != 0 && !sub.s.Services().Has(need) {
				continue
			}
			out = append(out, sub)
		}
	}
	return out
}

// flush sends a subscriber every value its slot holds.
//
// Enabling the back channel asks for this, and it is what makes a client's
// first screen correct without it having to read every object one at a time.
//
// It runs on its own goroutine and waits for room in the queue rather than
// dropping, because a whole menu is more than the queue holds and a client
// that is slow to acknowledge should be made to wait, not given a partial
// picture it cannot tell from a complete one.
func (p *Provider) flush(sub *subscriber) {
	defer p.wg.Done()

	prt := p.model.port(sub.s.LocalAddress().Port)
	if prt == nil {
		return
	}
	// The flush is a burst of values, so it goes only to a session that asked
	// for values. Without the control service there is nothing here to send.
	if !sub.s.Services().Has(codec.SvcControl) {
		return
	}
	for _, l := range prt.menu(sub.s.Uses32Bit()) {
		if l.Command == 0 {
			continue
		}
		// Every command has a value: the model seeds one for each line when
		// it is built, so there is no line here without one.
		v, _ := prt.value(l.Command)
		if !p.enqueueWaiting(sub, pending{value: v, isValue: true}) {
			return
		}
	}
}

// enqueueWaiting queues a message, waiting for room, and reports whether it
// got there before the subscriber or the provider stopped.
func (p *Provider) enqueueWaiting(sub *subscriber, msg pending) bool {
	select {
	case sub.queue <- msg:
		return true
	case <-sub.done:
	case <-p.done:
	}
	return false
}

// sessionState returns the bookkeeping for the link a session runs on.
func (p *Provider) sessionState(s *session.Session) *linkState {
	return p.linkState(s.Link())
}
