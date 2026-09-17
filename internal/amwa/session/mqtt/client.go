package mqtt

// Client — a resilient QoS-0 publisher. One goroutine owns the
// connection: it dials, CONNECTs, replays every retained topic it has
// been asked to hold (the broker forgets nothing, but a publish that
// happened DURING an outage would otherwise be lost), then drains the
// publish queue and keeps the keepalive alive. On any error it backs
// off and starts over. Publish never blocks the caller: an event
// source's state change must not stall on a broker.

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"dhs/internal/transport"
)

// Options configures one broker connection.
type Options struct {
	// Addr is the broker's host:port. Required.
	Addr string
	// ClientID names this session at the broker. Required.
	ClientID string
	// Username/Password are optional (IS-07 lab brokers run open).
	Username string
	Password string
	// KeepAlive is the §3.1.2.10 interval. Zero means 30s.
	KeepAlive time.Duration
	// Logger receives connection lifecycle events. Nil = slog.Default.
	Logger *slog.Logger
}

type message struct {
	topic   string
	payload []byte
	retain  bool
}

// Client is one broker session.
type Client struct {
	opts Options
	log  *slog.Logger

	mu sync.Mutex
	// retained is the last value per retained topic, replayed after a
	// reconnect so an outage cannot swallow a state change for good.
	retained map[string]message
	queue    chan message
	cancel   context.CancelFunc
	done     chan struct{}

	// dialer opens the broker connection each session establishes. Injected
	// rather than built inline so the pipe is substitutable — which matters
	// more here than anywhere else, because session() is the body of a
	// reconnect loop and every retry asks it for a fresh connection.
	dialer transport.Dialer
}

// New starts a client; the connection is managed in the background.
func New(opts Options) (*Client, error) {
	if opts.Addr == "" || opts.ClientID == "" {
		return nil, fmt.Errorf("mqtt: Addr and ClientID are required")
	}
	if opts.KeepAlive == 0 {
		opts.KeepAlive = 30 * time.Second
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		opts:     opts,
		log:      log,
		retained: map[string]message{},
		queue:    make(chan message, 256),
		cancel:   cancel,
		done:     make(chan struct{}),
		// Same 10 s connect bound as before, plus the SO_KEEPALIVE this
		// client never set. MQTT has its own PINGREQ keep-alive, but that
		// only detects a broker still speaking MQTT; a half-open socket
		// needs the OS probe underneath it.
		dialer: transport.TCPDialer{Timeout: 10 * time.Second},
	}
	go c.run(ctx)
	return c, nil
}

// Publish queues one message. Retained messages are also remembered
// for replay after a reconnect. A full queue drops the OLDEST queued
// message rather than blocking the caller — for state messages the
// newest value is the one that matters.
func (c *Client) Publish(topic string, payload []byte, retain bool) {
	m := message{topic: topic, payload: append([]byte(nil), payload...), retain: retain}
	if retain {
		c.mu.Lock()
		c.retained[topic] = m
		c.mu.Unlock()
	}
	for {
		select {
		case c.queue <- m:
			return
		default:
			select {
			case <-c.queue:
			default:
			}
		}
	}
}

// Close publishes nothing further and closes the connection cleanly.
func (c *Client) Close() {
	c.cancel()
	<-c.done
}

func (c *Client) run(ctx context.Context) {
	defer close(c.done)
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		err := c.session(ctx)
		if ctx.Err() != nil {
			return
		}
		c.log.Warn("mqtt: session ended, reconnecting", "broker", c.opts.Addr, "err", err, "backoff", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

// session runs one connect-publish-ping loop until an error.
func (c *Client) session(ctx context.Context) error {
	conn, err := c.dialer.DialContext(ctx, "tcp", c.opts.Addr)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	if _, err := conn.Write(connectPacket(c.opts.ClientID, uint16(c.opts.KeepAlive/time.Second), c.opts.Username, c.opts.Password)); err != nil {
		return err
	}
	ack := make([]byte, 4)
	if _, err := readFull(conn, ack); err != nil {
		return fmt.Errorf("reading CONNACK: %w", err)
	}
	if err := parseConnack(ack); err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Time{})
	c.log.Info("mqtt: connected", "broker", c.opts.Addr, "client", c.opts.ClientID)

	// Replay the retained set: the broker still holds the values from
	// before the outage, but anything published INTO the outage only
	// lives here.
	c.mu.Lock()
	replay := make([]message, 0, len(c.retained))
	for _, m := range c.retained {
		replay = append(replay, m)
	}
	c.mu.Unlock()
	for _, m := range replay {
		if err := c.send(conn, publishPacket(m.topic, m.payload, m.retain)); err != nil {
			return err
		}
	}

	// The broker answers PINGREQ with PINGRESP; draining those (and
	// tolerating nothing else) keeps the read side honest without a
	// full packet reader.
	readErr := make(chan error, 1)
	go func() {
		buf := make([]byte, 512)
		for {
			if _, err := conn.Read(buf); err != nil {
				readErr <- err
				return
			}
		}
	}()

	ping := time.NewTicker(c.opts.KeepAlive)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = c.send(conn, disconnectPacket())
			return ctx.Err()
		case err := <-readErr:
			return err
		case m := <-c.queue:
			if err := c.send(conn, publishPacket(m.topic, m.payload, m.retain)); err != nil {
				return err
			}
		case <-ping.C:
			if err := c.send(conn, pingreqPacket()); err != nil {
				return err
			}
		}
	}
}

func (c *Client) send(conn net.Conn, pkt []byte) error {
	if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	_, err := conn.Write(pkt)
	return err
}

func readFull(conn net.Conn, buf []byte) (int, error) {
	read := 0
	for read < len(buf) {
		n, err := conn.Read(buf[read:])
		read += n
		if err != nil {
			return read, err
		}
	}
	return read, nil
}
