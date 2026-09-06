package probelsw08p

// Active keep-alive for SW-P-08 — spec-sanctioned, client-initiated.
//
// Two different things are called "keepalive" in this connector, and only one
// of them is in the spec:
//
//   - The 0x11 / 0x22 ping-pong that keepaliveAutoResponder answers is NOT in
//     SW-P-08. Byte 17 in the spec is Soft Key Assignment / Protect Device
//     Name Request. The pair is an observed testbed convention, and it is
//     MATRIX-initiated — we can only answer it, never start it. So a matrix
//     that does not send it leaves us with no liveness signal at all.
//
//   - DUAL CONTROLLER STATUS REQUEST (Command Byte 08, §3.1.5, zero payload)
//     IS in the spec, and it is issued by the remote device — us. §5
//     "Supporting dual controllers over IP" prescribes it verbatim as the way
//     to hold a link up:
//
//	"Continue to poll the idle controller with the appropriate
//	 active / idle status request message (to keep the connections open)."
//
// This file implements that second one. It is what makes the reader's
// dead-man deadline meaningful: without a probe of our own, a silent matrix
// is indistinguishable from a healthy idle one, and arming a deadline would
// tear down working links.
//
// Liveness does not depend on the matrix implementing cmd 09: SW-P-08 §2
// requires a DLE ACK for every good frame, so even a controller that ignores
// cmd 08 at the application layer still returns framing-level bytes — which
// is inbound traffic, and therefore proof of life. (Lawo VSM in server mode
// does exactly this: it never replies to cmd 8, but still ACKs it.)

import (
	"log/slog"
	"sync"
	"time"

	"dhs/internal/clock"
	"dhs/internal/probel-sw08p/codec"
	sw08session "dhs/internal/probel-sw08p/session"
)

// DefaultKeepalivePollSpacing is how often the client polls cmd 08. The spec
// gives no cadence, so this is chosen to be cheap (a 6-byte frame) while
// still detecting a dead link inside a useful window.
const DefaultKeepalivePollSpacing = 10 * time.Second

// keepaliveIdleMultiple is how many polls the matrix may miss before the link
// is judged dead — three, so one lost frame or a slow controller never tears
// down a healthy session.
const keepaliveIdleMultiple = 3

// minKeepaliveIdleWindow floors the dead-man window so ordinary scheduling
// jitter on a loaded host cannot trip it.
const minKeepaliveIdleWindow = 30 * time.Second

// keepalivePollState owns the prober goroutine for one session.
type keepalivePollState struct {
	stop     chan struct{}
	stopped  sync.WaitGroup
	stopOnce sync.Once
}

// idleWindowForPoll derives the reader's dead-man window from the poll
// spacing. Pure, so the arithmetic is unit-tested without a live session.
func idleWindowForPoll(spacing time.Duration) time.Duration {
	w := spacing * keepaliveIdleMultiple
	if w < minKeepaliveIdleWindow {
		return minKeepaliveIdleWindow
	}
	return w
}

// startKeepalivePoll launches the cmd 08 prober and arms the matching read
// deadline. spacing <= 0 disables both — the caller opted out, and with no
// probe running a deadline would be unsafe. clk is injected so tests drive
// the cadence deterministically instead of sleeping.
//
// Caller must hold p.mu.
func (p *Plugin) startKeepalivePoll(spacing time.Duration, clk clock.Clock) {
	if spacing <= 0 || p.client == nil || p.kaPoll != nil {
		return
	}
	if clk == nil {
		clk = clock.System()
	}
	// Deadline and probe are armed together and torn down together: the
	// probe is the only thing that makes silence meaningful.
	p.client.SetIdleTimeout(idleWindowForPoll(spacing))

	ka := &keepalivePollState{stop: make(chan struct{})}
	p.kaPoll = ka
	ka.stopped.Add(1)
	go p.keepalivePollLoop(ka, p.client, spacing, clk)
}

// stopKeepalivePoll signals the prober, waits for it, and disarms the
// deadline. Idempotent. Caller must hold p.mu.
func (p *Plugin) stopKeepalivePoll() {
	ka := p.kaPoll
	p.kaPoll = nil
	if ka == nil {
		return
	}
	if p.client != nil {
		p.client.SetIdleTimeout(0)
	}
	ka.stopOnce.Do(func() { close(ka.stop) })
	ka.stopped.Wait()
}

// keepalivePollLoop emits one cmd 08 per tick until the session ends. It uses
// Client.Write (raw, bypassing the single-flight Send path) so the probe never
// competes with a caller-driven request for the in-flight slot — the same
// trick the sw02p rotating poll uses.
func (p *Plugin) keepalivePollLoop(ka *keepalivePollState, cli *sw08session.Client, spacing time.Duration, clk clock.Clock) {
	defer ka.stopped.Done()

	t := clk.NewTicker(spacing)
	defer t.Stop()

	frame := codec.Pack(codec.EncodeDualControllerStatusRequest())
	for {
		select {
		case <-ka.stop:
			return
		case <-t.C():
			if err := cli.Write(frame); err != nil {
				// The socket is gone. The reader will surface it; log once
				// and stop probing rather than spinning on a dead link.
				p.logger.Debug("probel keepalive poll exiting",
					slog.String("err", err.Error()))
				return
			}
		}
	}
}
