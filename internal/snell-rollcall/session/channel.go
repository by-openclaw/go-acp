package session

import (
	"context"
	"fmt"
	"sync"
	"time"

	"dhs/internal/clock"
	"dhs/internal/snell-rollcall/codec"
)

// channel is one direction of one session, and it enforces the rule the whole
// protocol is built on: one message in flight at a time, and nothing else sent
// until it is answered (spec 4).
//
// Every operation that touches the free token or a pending entry's frame
// buffer happens under the mutex. All of them are non-blocking — the token
// channel holds one slot and the frame buffer is bounded with a default case —
// so holding the lock costs nothing and removes the race between a shutdown
// closing those channels and a delivery writing to them. Waiting for a reply
// happens on the caller's own goroutine with the lock released.
type channel struct {
	name string // "front" or "back", for errors and traces

	mu         sync.Mutex
	inFlight   *pending
	closed     bool
	err        error // why it closed, returned to later callers
	strikes    int
	maxStrikes int

	// free is a one-token semaphore. Holding the token is the right to send;
	// it is returned when the reply arrives or the attempt fails.
	//
	// It is never closed. Shutting down puts the token back instead, and each
	// waiter that takes it finds the channel closed, returns the reason and
	// passes the token on. Closing it would work too, but it would leave two
	// ways to learn the channel is finished and a window in which a waiter
	// takes a real token from a channel that is closing.
	free chan struct{}

	clk     clock.Clock
	timeout time.Duration
}

// pending is the message occupying the slot.
type pending struct {
	// req is what was sent, kept so a timeout can name it.
	req codec.PacketType

	// frames carries everything the link matched to this request. It is
	// buffered because a Wait arrives before the real reply and both have to
	// be delivered without the link's read loop blocking on us.
	frames chan codec.Frame
}

func newChannel(name string, clk clock.Clock, timeout time.Duration, maxStrikes int) *channel {
	c := &channel{
		name:       name,
		free:       make(chan struct{}, 1),
		clk:        clk,
		timeout:    timeout,
		maxStrikes: maxStrikes,
	}
	c.free <- struct{}{}
	return c
}

// acquire waits for the slot and installs a pending entry for req.
//
// It is the only way to send on a channel, which is what makes the
// one-in-flight rule structural rather than a convention every call site has
// to remember.
func (c *channel) acquire(ctx context.Context, req codec.PacketType) (*pending, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.free:
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		// Pass the token on so the next waiter also wakes and learns why.
		c.free <- struct{}{}
		return nil, c.err
	}
	p := &pending{req: req, frames: make(chan codec.Frame, 4)}
	c.inFlight = p
	return p, nil
}

// release clears the slot and hands the token back, unless the failure count
// has reached the point where the session is dead.
func (c *channel) release(ok bool) {
	c.mu.Lock()
	c.inFlight = nil
	if ok {
		c.strikes = 0
	} else {
		c.strikes++
	}
	dead := !c.closed && c.strikes >= c.maxStrikes
	if !c.closed && !dead {
		c.free <- struct{}{} // capacity 1, we hold the only token
	}
	c.mu.Unlock()

	if dead {
		c.closeWith(fmt.Errorf("%w: %s channel", ErrSessionDead, c.name))
	}
}

// await blocks for the reply to p, honouring the peer's request for more time.
//
// Wait is the reason this is a loop rather than a single select. A server that
// needs longer than the deadline says so, and the deadline restarts. The
// vendor library answers Nack to Wait and never implements the extension,
// which abandons the operation and leaves the peer's session behind.
func (c *channel) await(ctx context.Context, p *pending) (codec.Frame, error) {
	// A stoppable timer rather than After, because a Wait replaces the
	// deadline and an abandoned After keeps its timer armed until it fires.
	// One per extension on a busy link is a leak that grows with traffic.
	timer := c.clk.NewTicker(c.timeout)
	defer func() { timer.Stop() }()

	for {
		select {
		case <-ctx.Done():
			c.release(false)
			return codec.Frame{}, ctx.Err()

		case <-timer.C():
			c.release(false)
			return codec.Frame{}, fmt.Errorf("%w: %s after %s", ErrTimeout, p.req, c.timeout)

		case f, ok := <-p.frames:
			if !ok {
				c.release(false)
				return codec.Frame{}, c.closedErr()
			}
			if f.Type == codec.MsgWait {
				timer.Stop()
				timer = c.clk.NewTicker(waitDuration(f, c.timeout))
				continue
			}
			c.release(true)
			return f, nil
		}
	}
}

// waitDuration reads how much longer the peer asked for.
//
// A Wait carrying no time, or one whose payload will not decode, still means
// "I am working on it", so the deadline restarts at the normal timeout rather
// than the message being discarded.
func waitDuration(f codec.Frame, fallback time.Duration) time.Duration {
	if w, err := codec.DecodeWait(f.Payload); err == nil && w.Seconds > 0 {
		return time.Duration(w.Seconds) * time.Second
	}
	return fallback
}

// deliver hands a frame to whatever is in flight, and reports whether anything
// was waiting for it.
//
// It never blocks. A full buffer means the peer sent more replies than it was
// asked for, which is the peer's fault and must not stall the link's read
// loop.
func (c *channel) deliver(f codec.Frame) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed || c.inFlight == nil {
		return false
	}
	select {
	case c.inFlight.frames <- f:
		return true
	default:
		return false
	}
}

// busy reports whether a message is in flight.
func (c *channel) busy() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inFlight != nil
}

// closeWith shuts the channel down, waking anything waiting on it.
//
// err says why, and must not be nil: it is what every later caller is given,
// and "closed for no reason" is not something a caller can act on.
func (c *channel) closeWith(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return
	}
	c.closed = true
	c.err = err
	if c.inFlight != nil {
		close(c.inFlight.frames)
	}
	// Wake one waiter, which will wake the next in turn.
	select {
	case c.free <- struct{}{}:
	default:
	}
}

func (c *channel) closedErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}
