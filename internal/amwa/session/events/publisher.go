package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	stdhttp "net/http"
	"sync"
	"sync/atomic"
	"time"

	"acp/internal/amwa/codec/is07"
	httpsession "acp/internal/amwa/session/http"
)

// Publisher is the IS-07 WebSocket server attached to a Node. One
// Publisher fronts many concurrent subscribers; each subscriber
// declares interest via command_subscription and receives any
// matching state events the Publisher emits.
//
// Lifetime: created by Node, registered as an http.Handler under the
// per-version Events API base path (e.g.
// `/x-nmos/events/v1.0/ws`), and Closed when the Node shuts down.
//
// Thread-safety: Publish, Handler, and Close are safe to call from
// any goroutine concurrently.
type Publisher struct {
	codec    is07.Codec
	logger   *slog.Logger
	hbEvery  time.Duration

	mu          sync.RWMutex
	clients     map[uint64]*subscription
	closed      bool
	clientSeq   atomic.Uint64
	stop        chan struct{}
	hbWg        sync.WaitGroup
}

// PublisherOptions configures Publisher creation.
type PublisherOptions struct {
	// Codec is the IS-07 wire codec to use; defaults to
	// is07.Default() when nil.
	Codec is07.Codec

	// Logger receives per-connection events; defaults to slog.Default().
	Logger *slog.Logger

	// HeartbeatInterval is how often the Publisher emits an unsolicited
	// MessageHealth to every connected client. 0 disables the
	// background heartbeat (clients can still drive heartbeats via
	// CommandHealth probes). Default 5s per IS-07 §3.
	HeartbeatInterval time.Duration
}

// NewPublisher constructs a Publisher.
func NewPublisher(opts PublisherOptions) *Publisher {
	c := opts.Codec
	if c == nil {
		c = is07.Default()
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	hb := opts.HeartbeatInterval
	if hb == 0 {
		hb = 5 * time.Second
	}
	p := &Publisher{
		codec:   c,
		logger:  logger,
		hbEvery: hb,
		clients: make(map[uint64]*subscription),
		stop:    make(chan struct{}),
	}
	if hb > 0 {
		p.hbWg.Add(1)
		go p.heartbeatLoop()
	}
	return p
}

// Handler returns an http.Handler that completes the WS upgrade and
// serves IS-07 traffic for the lifetime of the connection.
//
// The Node mounts this at the spec's wire path:
//
//	mux.Handle("/x-nmos/events/v1.0/ws", pub.Handler())
func (p *Publisher) Handler() stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		ws, err := httpsession.AcceptWebSocket(w, r)
		if err != nil {
			stdhttp.Error(w, err.Error(), stdhttp.StatusBadRequest)
			return
		}
		p.serve(r.Context(), ws, r.RemoteAddr)
	})
}

// Publish fans an event message to every subscriber whose set
// includes the event's source_id. EventBoolean / EventNumber /
// EventString / EventObject are the expected variants — Publish
// rejects any other Message kind so callers don't accidentally fan a
// health/reboot through the source-filter.
func (p *Publisher) Publish(m is07.Message) error {
	if m == nil {
		return errors.New("nmos/is07/publisher: nil message")
	}
	var src string
	switch e := m.(type) {
	case is07.EventBoolean:
		src = e.Identity.SourceID
	case is07.EventNumber:
		src = e.Identity.SourceID
	case is07.EventString:
		src = e.Identity.SourceID
	case is07.EventObject:
		src = e.Identity.SourceID
	default:
		return fmt.Errorf("nmos/is07/publisher: Publish requires a state event, got %T", m)
	}
	body, err := p.codec.EncodeMessage(m)
	if err != nil {
		return err
	}
	p.mu.RLock()
	clients := make([]*subscription, 0, len(p.clients))
	for _, c := range p.clients {
		if c.matches(src) {
			clients = append(clients, c)
		}
	}
	p.mu.RUnlock()
	for _, c := range clients {
		if err := c.ws.SendText(body); err != nil {
			p.logger.Debug("publish: drop client",
				"client", c.id, "err", err.Error())
			c.markFailed()
		}
	}
	return nil
}

// Close terminates the heartbeat loop, drops all subscribers, and
// stops accepting new ones. Idempotent.
func (p *Publisher) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	close(p.stop)
	for _, c := range p.clients {
		_ = c.ws.Close()
	}
	p.clients = nil
	p.mu.Unlock()
	p.hbWg.Wait()
	return nil
}

// SubscriberCount is the number of clients currently connected.
func (p *Publisher) SubscriberCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.clients)
}

// serve runs the per-connection message loop until the client closes
// or sends an unrecognised frame.
func (p *Publisher) serve(ctx context.Context, ws *httpsession.WebSocket, remote string) {
	id := p.clientSeq.Add(1)
	sub := &subscription{
		id:      id,
		ws:      ws,
		sources: make(map[string]struct{}),
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		_ = ws.Close()
		return
	}
	p.clients[id] = sub
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.clients, id)
		p.mu.Unlock()
		_ = ws.Close()
	}()

	p.logger.Info("nmos/is07: ws client connected",
		"client", id, "remote", remote)

	for {
		if err := ctx.Err(); err != nil {
			return
		}
		body, err := ws.ReadText()
		if err != nil {
			if errors.Is(err, httpsession.ErrWebSocketClosed) {
				p.logger.Info("nmos/is07: ws client disconnected", "client", id)
				return
			}
			p.logger.Warn("nmos/is07: ws read error", "client", id, "err", err.Error())
			return
		}
		cmd, err := p.codec.DecodeCommand(body)
		if err != nil {
			p.logger.Warn("nmos/is07: bad command", "client", id, "err", err.Error())
			continue
		}
		switch c := cmd.(type) {
		case is07.CommandSubscription:
			sub.set(c.Sources)
			p.logger.Info("nmos/is07: subscription updated",
				"client", id, "sources", len(c.Sources))
		case is07.CommandHealth:
			resp := is07.MessageHealth{
				Timing: is07.Timing{
					CreationTimestamp: is07Now(),
					OriginTimestamp:   c.Timestamp,
				},
			}
			body, err := p.codec.EncodeMessage(resp)
			if err != nil {
				p.logger.Warn("nmos/is07: encode health", "err", err.Error())
				continue
			}
			if err := ws.SendText(body); err != nil {
				p.logger.Warn("nmos/is07: send health", "err", err.Error())
				return
			}
		}
	}
}

// heartbeatLoop emits unsolicited MessageHealth at p.hbEvery to every
// connected client.
func (p *Publisher) heartbeatLoop() {
	defer p.hbWg.Done()
	t := time.NewTicker(p.hbEvery)
	defer t.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-t.C:
			now := is07Now()
			msg := is07.MessageHealth{
				Timing: is07.Timing{
					CreationTimestamp: now,
					OriginTimestamp:   now,
				},
			}
			body, err := p.codec.EncodeMessage(msg)
			if err != nil {
				continue
			}
			p.mu.RLock()
			snap := make([]*subscription, 0, len(p.clients))
			for _, c := range p.clients {
				snap = append(snap, c)
			}
			p.mu.RUnlock()
			for _, c := range snap {
				_ = c.ws.SendText(body)
			}
		}
	}
}
