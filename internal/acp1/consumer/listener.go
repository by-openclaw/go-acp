package acp1

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"dhs/internal/transport"
	"dhs/internal/acp1/codec"
)

// Listener receives ACP1 announcements broadcast by rack controllers on
// the LAN and dispatches them to registered subscribers.
//
// Announcements per spec §"Reply Message Matrix" p. 14:
//   - MTID=0 always
//   - MTYPE=0 for status / card-event / frame-status changes (MCODE=0)
//   - MTYPE=2 for value-change echoes of set-family methods (MCODE 1..4)
//
// The listener runs one goroutine per instance that loops on
// UDPListener.Receive, decodes each datagram, and iterates registered
// subscriptions. Subscriptions are matched by (slot, group, id) with -1
// or "" meaning wildcard. Multiple subscriptions can match one event —
// they are all invoked in registration order.
//
// RawEventFunc is the low-level callback used internally by the plugin
// wiring. The public consumer.EventFunc is wrapped around this at the
// Plugin.Subscribe layer so higher-level code sees decoded consumer.Event
// values with resolved labels, not raw Messages.
type RawEventFunc func(msg *codec.Message)

// Listener is safe for concurrent Subscribe/Unsubscribe and for one
// Receive goroutine. Stop is idempotent.
type Listener struct {
	logger *slog.Logger
	conn   *transport.UDPListener

	mu   sync.Mutex
	subs []subscription
	next int // monotonic id for unsubscribe handle

	// cancel signals the receive goroutine to exit. Set by Start.
	cancel context.CancelFunc
	done   chan struct{}
}

// subscription is one registered filter + callback.
type subscription struct {
	id    int    // unsubscribe handle
	slot  int    // -1 = any
	group string // "" = any ("control", "status", ...)
	objID int    // -1 = any
	fn    RawEventFunc
}

// SubHandle is an opaque unsubscribe handle.
type SubHandle int

// NewListener binds a listening UDP socket to the given port. Typical
// usage: port = acp1.DefaultPort (2071). The listener is not running
// until Start is called.
func NewListener(logger *slog.Logger, port int) (*Listener, error) {
	if logger == nil {
		logger = slog.Default()
	}
	conn, err := transport.ListenUDP(context.Background(), port)
	if err != nil {
		return nil, err
	}
	return &Listener{
		logger: logger,
		conn:   conn,
		done:   make(chan struct{}),
	}, nil
}

// Start spawns the receive goroutine. Returns immediately. Call Stop to
// terminate. Safe to call once per Listener.
func (l *Listener) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	l.cancel = cancel
	go l.loop(ctx)
}

// Stop terminates the receive goroutine and closes the socket. Safe to
// call multiple times. Blocks until the goroutine exits so callers can
// rely on no further callback invocations after Stop returns.
func (l *Listener) Stop() {
	if l.cancel != nil {
		l.cancel()
	}
	if l.conn != nil {
		_ = l.conn.Close()
	}
	select {
	case <-l.done:
	default:
	}
}

// Subscribe registers a filter + callback. Returns a SubHandle that can
// be passed to Unsubscribe. slot < 0, group == "", or objID < 0 act as
// wildcards in their respective axes.
func (l *Listener) Subscribe(slot int, group string, objID int, fn RawEventFunc) SubHandle {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.next++
	h := l.next
	l.subs = append(l.subs, subscription{
		id:    h,
		slot:  slot,
		group: group,
		objID: objID,
		fn:    fn,
	})
	return SubHandle(h)
}

// Unsubscribe removes a previously-registered subscription. A handle
// that no longer exists is a silent no-op.
func (l *Listener) Unsubscribe(h SubHandle) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.subs {
		if l.subs[i].id == int(h) {
			l.subs = append(l.subs[:i], l.subs[i+1:]...)
			return
		}
	}
}

// loop is the receive goroutine. Survives transient socket errors
// (e.g. Windows UDP ICMP-unreachable blowback) by logging them and
// retrying. Exits only when the context is cancelled or the socket is
// permanently closed via Stop.
func (l *Listener) loop(ctx context.Context) {
	defer close(l.done)
	// Panic isolation lives at the only call site that can panic — the
	// per-dispatch recover below (l.dispatch is wrapped). Receive / Decode /
	// logging never panic, so a top-level loop recover would be unreachable
	// and is intentionally omitted.

	for {
		// Honour cancellation before each receive.
		if ctx.Err() != nil {
			return
		}

		raw, _, err := l.conn.Receive(ctx, codec.MaxPacket)
		if err != nil {
			// Deliberate shutdown: ctx cancelled or socket closed.
			if ctx.Err() != nil {
				return
			}
			// Note: a closed socket does NOT surface as a bare net.ErrClosed
			// here — transport.UDPListener.Receive wraps it (ErrReadFailed),
			// so it falls through to the transient-retry path below, which
			// then exits once Stop() cancels the context. (A former
			// errors.Is(err, net.ErrClosed) check was therefore unreachable.)

			// Transient error — log at debug and keep looping. A short
			// sleep avoids tight-spin if the same error recurs.
			l.logger.Debug("acp1 listener receive error (retrying)", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Millisecond):
			}
			continue
		}

		// Trace every received datagram. This is how we diagnose
		// "change X in the emulator but no event" — if the packet
		// arrives but is skipped, the log tells us why.
		l.logger.Debug("acp1 listener: rx",
			"len", len(raw),
			"hex", fmt.Sprintf("%x", raw))

		msg, derr := codec.Decode(raw)
		if derr != nil {
			l.logger.Debug("acp1 listener: malformed datagram",
				"err", derr, "bytes", len(raw))
			continue
		}
		if !msg.IsAnnouncement() {
			l.logger.Debug("acp1 listener: non-announcement, skipped",
				"mtid", msg.MTID, "mtype", msg.MType,
				"slot", msg.MAddr, "grp", msg.ObjGroup, "id", msg.ObjID)
			continue
		}
		l.logger.Debug("acp1 listener: announcement",
			"slot", msg.MAddr, "grp", msg.ObjGroup, "id", msg.ObjID,
			"mtype", msg.MType, "mcode", msg.MCode,
			"val", fmt.Sprintf("%x", msg.Value))

		// Dispatch panic-guarded so one broken subscriber cannot take
		// the whole listener down. Inline so we don't lose the defer
		// stack on a crash.
		func() {
			defer func() {
				if r := recover(); r != nil {
					l.logger.Error("acp1 listener subscriber panic", "err", r)
				}
			}()
			l.dispatch(msg)
		}()
	}
}

// dispatch iterates subscriptions under the lock and invokes matching
// callbacks. Callbacks are invoked synchronously inside the receive
// goroutine — subscribers must not block or the listener stops draining
// the socket.
func (l *Listener) dispatch(msg *codec.Message) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range l.subs {
		if s.slot >= 0 && s.slot != int(msg.MAddr) {
			continue
		}
		if s.group != "" && s.group != msg.ObjGroup.String() {
			continue
		}
		if s.objID >= 0 && s.objID != int(msg.ObjID) {
			continue
		}
		s.fn(msg)
	}
}
