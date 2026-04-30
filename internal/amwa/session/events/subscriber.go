package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"acp/internal/amwa/codec/is07"
	httpsession "acp/internal/amwa/session/http"
)

// MessageHandler receives every IS-07 sender-to-receiver wire frame
// the Subscriber decodes. The handler runs in the Subscriber's read
// goroutine — long-running work belongs elsewhere.
type MessageHandler func(is07.Message)

// Subscriber is the IS-07 WebSocket client used by the Controller
// to consume state events from a Node.
//
// Lifetime: Connect -> Subscribe -> Run -> Close. Connect dials and
// completes the upgrade. Subscribe sends the source-id set. Run
// blocks reading frames until Close or context cancellation.
type Subscriber struct {
	codec   is07.Codec
	logger  *slog.Logger
	hbEvery time.Duration

	mu      sync.Mutex
	ws      *httpsession.WebSocket
	closed  bool
}

// SubscriberOptions configures Subscriber creation.
type SubscriberOptions struct {
	// Codec is the IS-07 wire codec to use; defaults to
	// is07.Default() when nil.
	Codec is07.Codec

	// Logger receives per-frame events at debug level + connection
	// events at info; defaults to slog.Default().
	Logger *slog.Logger

	// HeartbeatInterval controls how often the Subscriber sends
	// CommandHealth to the Publisher. 0 disables (relying on the
	// Publisher's own heartbeat). Default 5s per IS-07 §3.
	HeartbeatInterval time.Duration
}

// NewSubscriber returns a new Subscriber. Use Connect to dial.
func NewSubscriber(opts SubscriberOptions) *Subscriber {
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
	return &Subscriber{codec: c, logger: logger, hbEvery: hb}
}

// Connect dials the Node's IS-07 WS endpoint and completes the RFC
// 6455 handshake. wsURL is typically `ws://<node>:<port>/x-nmos/events/v1.0/ws`
// — exact path is published in the IS-04 Sender's `controls` URN.
func (s *Subscriber) Connect(ctx context.Context, wsURL string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ws != nil {
		return errors.New("nmos/is07/subscriber: already connected")
	}
	ws, err := httpsession.DialWebSocket(ctx, wsURL, nil)
	if err != nil {
		return err
	}
	s.ws = ws
	s.closed = false
	return nil
}

// Subscribe sends a command_subscription with the given source IDs.
// Replaces any prior subscription on the same WS — IS-07
// command_subscription semantics are full replacement, not append.
func (s *Subscriber) Subscribe(sources []string) error {
	s.mu.Lock()
	ws := s.ws
	s.mu.Unlock()
	if ws == nil {
		return errors.New("nmos/is07/subscriber: not connected")
	}
	cmd := is07.CommandSubscription{Sources: append([]string{}, sources...)}
	if cmd.Sources == nil {
		cmd.Sources = []string{}
	}
	body, err := s.codec.EncodeCommand(cmd)
	if err != nil {
		return err
	}
	return ws.SendText(body)
}

// Run reads frames in a loop and invokes h for every decoded message.
// Returns nil on Close, the underlying error otherwise. Heartbeat
// goroutine is started when hbEvery > 0; it stops when Run returns.
func (s *Subscriber) Run(ctx context.Context, h MessageHandler) error {
	s.mu.Lock()
	ws := s.ws
	hb := s.hbEvery
	s.mu.Unlock()
	if ws == nil {
		return errors.New("nmos/is07/subscriber: not connected")
	}
	if h == nil {
		h = func(is07.Message) {}
	}

	stop := make(chan struct{})
	var hbDone sync.WaitGroup
	if hb > 0 {
		hbDone.Add(1)
		go func() {
			defer hbDone.Done()
			t := time.NewTicker(hb)
			defer t.Stop()
			for {
				select {
				case <-stop:
					return
				case <-ctx.Done():
					return
				case <-t.C:
					cmd := is07.CommandHealth{Timestamp: is07Now()}
					body, err := s.codec.EncodeCommand(cmd)
					if err != nil {
						continue
					}
					if err := ws.SendText(body); err != nil {
						return
					}
				}
			}
		}()
	}
	defer func() {
		close(stop)
		hbDone.Wait()
	}()

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		body, err := ws.ReadText()
		if err != nil {
			if errors.Is(err, httpsession.ErrWebSocketClosed) {
				return nil
			}
			// Close() was called concurrently — the read errored
			// because we tore the connection down, not because the
			// peer misbehaved. Surface as graceful exit.
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return nil
			}
			return fmt.Errorf("nmos/is07/subscriber: read: %w", err)
		}
		msg, err := s.codec.DecodeMessage(body)
		if err != nil {
			s.logger.Warn("nmos/is07: bad frame", "err", err.Error())
			continue
		}
		h(msg)
	}
}

// Close drops the underlying WS connection. Idempotent.
func (s *Subscriber) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.ws != nil {
		err := s.ws.Close()
		s.ws = nil
		return err
	}
	return nil
}
