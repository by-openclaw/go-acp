// Package session is the RollCall protocol state machine.
//
// It sits below consumer and provider because it is the same machine on both
// sides with the roles exchanged: a link carrying frames, sessions multiplexed
// over it by index, one message in flight per channel, and a back channel
// whose pushes are themselves requests. The vendor's own Core/ directory is
// organised the same way for the same reason.
//
// It is transport-agnostic. A Link is handed a net.Conn and never opens one,
// so a test drives it over net.Pipe with no socket, no port and no listener.
// Time comes from an injected clock, so every timeout, probe and backoff in
// here is exercised by advancing a fake rather than by sleeping.
//
// # The active-message rule
//
// The rule that shapes everything else: one message may be in flight per
// channel per session, and the next may not be sent until the current one is
// answered (spec 4). Every request therefore queues, including the keepalive
// probe. Sending a probe beside a pending request is not a shortcut, it is a
// correctness bug — replies are matched head-of-queue, so the probe's ACK
// would be handed to whatever was waiting.
//
// A reply is expected within three seconds. A server that needs longer sends
// Wait, which extends the deadline. The vendor library never implements that
// and answers Nack instead, so a server asking for more time is abandoned
// mid-operation and its session leaks; we honour it.
//
// After five consecutive failures a session is dead. The vendor then drops it
// without sending Term, which is how units run out of sessions and why the
// library's own comments warn about it. This package always sends Term: on
// close, on error and on context cancellation.
//
// # Address rewriting
//
// A client on TCP does not know its own RollCall address, and says so by
// sending a zeroed source. The gateway stamps its own unit and a port drawn
// from its connection slots, and reverses that on the way back. This is
// library behaviour rather than anything in the specification, and a peer that
// skips it is rejected by real gateways and by the Control Panel.
//
// # What is not here
//
// The verbs. This package will hand you a session you can send a request on
// and receive a push from; deciding which requests to send is the consumer's
// job, and answering them is the provider's.
package session
