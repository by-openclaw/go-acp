package probelsw08p

// SessionDone — the death signal, exposed at the Plugin so a supervisor can
// wait on it without reaching into the codec client.
//
// sw08session.Client closes readerDone when its reader goroutine exits: a peer
// close, an I/O error, or the keep-alive poll's idle deadline firing. Until
// now nothing acted on it — the watch verb detected death and kept running,
// connected to nothing.

import "dhs/internal/consumer"

// SessionDone implements consumer.SessionDoneAccessor.
//
// Returns nil when there is no client yet (not connected), which blocks
// forever in a select — the correct "this never fires" answer.
func (p *Plugin) SessionDone() <-chan struct{} {
	p.mu.Lock()
	c := p.client
	p.mu.Unlock()
	if c == nil {
		return nil
	}
	return c.ReaderDone()
}

// Compile-time proof the Plugin satisfies the optional capability.
var _ consumer.SessionDoneAccessor = (*Plugin)(nil)
