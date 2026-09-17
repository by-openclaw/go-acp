package acp1

// SessionDone — the death signal, exposed at the Plugin so a supervisor can
// wait on it without knowing which transport mode is active.
//
// The multiplexing clients (TCPClient, AN2Client) already close a channel when
// their reader goroutine exits; this just surfaces it through the Plugin.

import "dhs/internal/consumer"

// readerDoner is implemented by the clients that carry announcements on the
// same socket as replies, and therefore have a reader goroutine whose exit
// means the session is over. Declared here rather than widening clientIface
// because the UDP path deliberately does NOT have one.
type readerDoner interface {
	ReaderDone() <-chan struct{}
}

// SessionDone implements consumer.SessionDoneAccessor for the connection-
// oriented modes (TCP direct, AN2). It returns nil for UDP, where there is no
// session to lose: the socket is connectionless, a silent device is
// indistinguishable from an idle one, and a nil channel blocks forever, which
// is the correct "this never fires" answer for a select.
func (p *Plugin) SessionDone() <-chan struct{} {
	p.mu.Lock()
	c := p.client
	p.mu.Unlock()
	if rd, ok := c.(readerDoner); ok {
		return rd.ReaderDone()
	}
	return nil
}

// Compile-time proof the Plugin satisfies the optional capability.
var _ consumer.SessionDoneAccessor = (*Plugin)(nil)
