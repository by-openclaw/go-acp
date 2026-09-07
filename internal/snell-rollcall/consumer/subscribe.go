package rollcall

import (
	"context"
	"fmt"

	"dhs/internal/consumer"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

// Subscribe registers a listener for value changes on a slot.
//
// Two messages are needed and the second is easy to miss: opening the back
// channel makes a session able to receive pushes, and asking for change
// reporting is what makes a device send value pushes at all. With only the
// first, a subscription looks live and nothing ever arrives.
//
// On enable the device flushes every value that has changed, so a burst
// follows immediately. That is the specification's behaviour rather than a
// fault, and it is what makes the first callback arrive without anything
// having moved.
func (p *Plugin) Subscribe(req consumer.ValueRequest, fn consumer.EventFunc) error {
	if fn == nil {
		return fmt.Errorf("rollcall: subscribe needs a callback")
	}

	ctx := context.Background()
	s, err := p.session(ctx, uint8(req.Slot))
	if err != nil {
		return err
	}

	// Resolving the request needs the menu, so a subscription by label walks
	// on a miss like every other addressed call.
	//
	// A subscription to the whole slot walks too, even though it needs no
	// resolution: the menu is what gives a push its label and its path, and
	// an event stream of bare command numbers is far less use than one that
	// names what changed. It is best effort, because a device with no menu
	// service can still push values.
	var command uint32
	if req.Path != "" || req.Label != "" || req.ID != 0 {
		t, err := p.tree(ctx, req.Slot)
		if err != nil {
			return err
		}
		line, err := t.resolve(req)
		if err != nil {
			return err
		}
		command = line.Command
	} else if _, err := p.tree(ctx, req.Slot); err != nil {
		p.log.Debug("rollcall: subscribing without a menu; pushes will carry no labels",
			"slot", req.Slot, "err", err)
	}

	p.mu.Lock()
	p.subs[subKey{slot: req.Slot, command: command}] = fn
	p.mu.Unlock()

	if err := s.EnableBackChannel(ctx, true); err != nil {
		p.mu.Lock()
		delete(p.subs, subKey{slot: req.Slot, command: command})
		p.mu.Unlock()
		return err
	}

	go p.deliver(req.Slot, s)
	return nil
}

// Unsubscribe removes a listener.
//
// The back channel itself is left open. A slot usually has more than one
// subscription, and closing the channel for one of them would silence the
// others; the device is told to stop only when the last goes.
func (p *Plugin) Unsubscribe(req consumer.ValueRequest) error {
	ctx := context.Background()

	var command uint32
	if req.Path != "" || req.Label != "" || req.ID != 0 {
		t, err := p.tree(ctx, req.Slot)
		if err != nil {
			return err
		}
		line, err := t.resolve(req)
		if err != nil {
			return err
		}
		command = line.Command
	}

	p.mu.Lock()
	delete(p.subs, subKey{slot: req.Slot, command: command})
	remaining := 0
	for k := range p.subs {
		if k.slot == req.Slot {
			remaining++
		}
	}
	p.mu.Unlock()

	if remaining > 0 {
		return nil
	}

	s, err := p.session(ctx, uint8(req.Slot))
	if err != nil {
		// Nothing left to tell; the subscription is gone either way.
		return nil //nolint:nilerr // an unreachable device needs no unsubscribe
	}
	return s.DisableBackChannel(ctx)
}

// deliver pumps one session's pushes into the registered callbacks.
func (p *Plugin) deliver(slot int, s *session.Session) {
	p.mu.RLock()
	stop := p.events
	p.mu.RUnlock()

	for {
		select {
		case <-stop:
			return
		case push, ok := <-s.Pushes():
			if !ok {
				return
			}
			p.dispatch(slot, push.Frame)

			// The acknowledgement goes after the callbacks have run, because
			// it is what asks the device for the next push. Sending it first
			// throws away the only flow control the protocol offers, and a
			// fast device would then outrun a slow listener.
			if err := push.Ack(); err != nil {
				p.log.Debug("rollcall: could not acknowledge a push",
					"slot", slot, "err", err)
				return
			}
		}
	}
}

// dispatch turns one push into an event and delivers it.
func (p *Plugin) dispatch(slot int, f codec.Frame) {
	switch f.Type {
	case codec.MsgRetFStat, codec.MsgSetParam:
		fs, err := codec.DecodeFuncStatus(f.Payload)
		if err != nil {
			p.log.Debug("rollcall: undecodable value push", "slot", slot, "err", err)
			return
		}
		p.emit(slot, uint32(fs.Command), fs.Mode, fs.Value, fs.Text, fs.Data)

	case codec.MsgRetValue:
		v, err := codec.DecodeValue(f.Payload)
		if err != nil {
			p.log.Debug("rollcall: undecodable value push", "slot", slot, "err", err)
			return
		}
		p.emit(slot, v.Command, v.Mode, v.Val, v.Text, v.Data)

	case codec.MsgDispData:
		d, err := codec.DecodeDisp(f.Payload)
		if err != nil {
			return
		}
		p.emitDisplay(slot, d)

	case codec.MsgFuncStyleChg:
		// A style change means a line became hidden or disabled. The cached
		// menu is now stale, so it is dropped and the next call walks again
		// rather than reporting an access it no longer has.
		p.invalidate(slot)

	case codec.MsgFuncListChg:
		// The whole menu changed, which happens when a card is reconfigured.
		p.invalidate(slot)
	}
}

// emit delivers a value change to whichever callbacks want it.
func (p *Plugin) emit(slot int, command uint32, mode codec.Mode, num int32, text string, data []byte) {
	p.mu.RLock()
	specific := p.subs[subKey{slot: slot, command: command}]
	all := p.subs[subKey{slot: slot}]
	t := p.trees[slot]
	p.mu.RUnlock()

	if specific == nil && all == nil {
		return
	}

	var line *menuLine
	if t != nil {
		if i, ok := t.byCmd[command]; ok {
			line = &t.lines[i]
		}
	}
	if line == nil {
		// A device pushing a command its own menu does not list is the
		// device's business; it knows its command set better than a cached
		// walk does, so the event is delivered under its number.
		p.fire(EventUnsolicitedCommand, fmt.Sprintf(
			"slot %d pushed command %d, which the walked menu does not list", slot, command))
	}

	ev := consumer.Event{
		Slot:      slot,
		ID:        int(command),
		Value:     p.valueFrom(line, mode, num, text, data),
		Timestamp: p.clk.Now(),
	}
	if line != nil {
		ev.Label = line.Text
		ev.Path = pathString(line.path)
	}

	if specific != nil {
		specific(ev)
	}
	// A listener registered for the whole slot hears it too. When the command
	// is zero the two lookups found the same registration, so delivering
	// again would call one callback twice for one change.
	if all != nil && command != 0 {
		all(ev)
	}
}

// emitDisplay delivers a status-line change.
//
// The four display lines are a unit's front panel, and the two negative line
// numbers are an error and a warning rather than positions. A line outside
// both ranges is kept under its own number and reported, because it means the
// device is using the service in a way the specification does not describe.
func (p *Plugin) emitDisplay(slot int, d codec.Disp) {
	p.mu.RLock()
	all := p.subs[subKey{slot: slot}]
	p.mu.RUnlock()

	if all == nil {
		return
	}
	if d.Line > 3 || d.Line < codec.DisplayLineWarning {
		p.fire(EventDisplayLineOutOfRange, fmt.Sprintf(
			"slot %d sent display line %d", slot, d.Line))
	}

	all(consumer.Event{
		Slot:      slot,
		ID:        int(d.Line),
		Label:     displayLabel(d.Line),
		Value:     consumer.Value{Kind: consumer.KindString, Str: d.Text},
		Timestamp: p.clk.Now(),
	})
}

// displayLabel names a status line. The negative numbers carry priority rather
// than position, which is why they are named rather than numbered.
func displayLabel(line int16) string {
	switch line {
	case codec.DisplayLineError:
		return "error"
	case codec.DisplayLineWarning:
		return "warning"
	default:
		return fmt.Sprintf("display%d", line)
	}
}

func pathString(path []string) string {
	out := ""
	for i, part := range path {
		if i > 0 {
			out += "."
		}
		out += part
	}
	return out
}

// invalidate drops a slot's cached menu so the next call walks again.
func (p *Plugin) invalidate(slot int) {
	p.mu.Lock()
	delete(p.trees, slot)
	p.mu.Unlock()
}

// pumpBackChannel forwards announcements the link receives outside any
// session, which is how a unit says it has appeared or gone.
func (p *Plugin) pumpBackChannel() {
	p.mu.RLock()
	l := p.link
	stop := p.events
	p.mu.RUnlock()

	if l == nil {
		return
	}
	for {
		select {
		case <-stop:
			return
		case <-l.sess.Done():
			return
		case f := <-l.sess.Unsolicited():
			if f.Type == codec.MsgIam {
				if info, err := codec.DecodeDeviceInfo(f.Payload); err == nil {
					p.log.Debug("rollcall: announcement",
						"unit", info.Address.String(),
						"name", info.ID.Name,
						"status", info.Status.Status.String())
				}
			}
		}
	}
}
