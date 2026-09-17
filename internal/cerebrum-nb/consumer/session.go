package cerebrumnb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dhs/internal/cerebrum-nb/codec"
	"dhs/internal/clock"
	"dhs/internal/metrics"
	"dhs/internal/transport"
	"dhs/internal/transport/ws"
)

// Session is the live WebSocket session against one Cerebrum host. It
// owns:
//   - the *ws.Conn (single goroutine readLoop dispatches RX);
//   - the mtid allocator + pending-request map;
//   - the per-event subscriber channel set;
//   - the compliance Profile.
type Session struct {
	logger     *slog.Logger
	conn       *ws.Conn
	host       string
	port       int
	compliance *Profile
	// rec, when non-nil, records every TX/RX XML document (the ws text
	// payload — the wire truth above the RFC 6455 framing) to the
	// standard JSONL wire-trace, same shape as every other connector's
	// --capture (#242). transport.Recorder methods are nil-safe.
	rec *transport.Recorder

	mtidNext atomic.Uint32

	// met counts every XML document in and out — the ws text payload, the
	// same wire truth --capture records, not the RFC 6455 framing around
	// it. Nil until SetMetrics.
	met *metrics.Connector

	mu       sync.Mutex
	pending  map[string]chan *codec.Frame
	subs     []*Subscription
	apiVer   string
	loggedIn bool

	closeOnce sync.Once
	closeErr  error
	stopRX    chan struct{}

	// --- 24/7 liveness (see keepalive.go) ---------------------------------
	//
	// lastRX is the unix-nano timestamp of the most recent frame received
	// from the server. It is the input to SessionLive and the evidence a
	// watcher uses to prove it is still being fed.
	lastRX atomic.Int64

	// done is closed exactly once, when the session dies (read error, idle
	// timeout, peer close, or local close). A supervisor blocks on it to
	// drive reconnection; lostErr says why.
	done     chan struct{}
	lostOnce sync.Once
	lostMu   sync.Mutex
	lostErr  error

	// ka owns the keep-alive prober goroutine, when one is running.
	ka *keepAlive
}

// Done returns a channel closed when the session dies, for whatever reason.
// A 24/7 watcher selects on this to notice a dead connection instead of
// blocking forever on an event stream that will never produce another frame.
func (s *Session) Done() <-chan struct{} { return s.done }

// Err reports why the session died, or nil while it is alive. The error
// always wraps transport.ErrConnectionLost so callers can dispatch with
// errors.Is without string-matching.
func (s *Session) Err() error {
	s.lostMu.Lock()
	defer s.lostMu.Unlock()
	return s.lostErr
}

// LastRx returns the time of the most recent frame from the server, or the
// zero time if nothing has arrived yet.
func (s *Session) LastRx() time.Time {
	ns := s.lastRX.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// noteRX records that a frame arrived. Called on EVERY inbound frame.
func (s *Session) noteRX() { s.lastRX.Store(time.Now().UnixNano()) }

// markLost records the cause of death and closes Done. Idempotent — the
// first cause wins, because it is the one that explains the others.
func (s *Session) markLost(err error) {
	s.lostOnce.Do(func() {
		s.lostMu.Lock()
		s.lostErr = err
		s.lostMu.Unlock()
		close(s.done)
	})
}

// EventFunc is the cerebrum-nb-specific event callback. It receives
// every dispatched RX Frame matching a Subscription's predicate. Must
// not block.
type EventFunc func(*codec.Frame)

// Subscription is a registered listener. Returned by Session.Subscribe*;
// pass to Session.Cancel to stop receiving events.
type Subscription struct {
	id   uint32
	kind codec.FrameKind // 0 = match all event kinds
	fn   EventFunc
}

// Profile is the cerebrum-nb compliance profile. Each Event() call
// records a named deviation; CLI surfaces them in --debug mode.
type Profile struct {
	mu     sync.Mutex
	counts map[string]int
}

// Event records one deviation by name + optional details. Counts are
// kept; a name maps to its first detail string only (for log brevity).
func (p *Profile) Event(name string, details ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.counts == nil {
		p.counts = make(map[string]int)
	}
	p.counts[name]++
	_ = details // logging happens at the call site; profile keeps counts
}

// Counts returns a copy of the count map.
func (p *Profile) Counts() map[string]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string]int, len(p.counts))
	for k, v := range p.counts {
		out[k] = v
	}
	return out
}

// newSession dials the Cerebrum WebSocket and starts the RX goroutine.
// Login is performed by the caller via session.login. rec may be nil
// (no capture).
// newSession dials and starts the read loop. met is taken here rather than
// through a setter because the read loop is running before this returns —
// a connector assigned afterwards would be a data race, and would miss the
// LOGIN exchange besides.
func newSession(ctx context.Context, logger *slog.Logger, urlStr string, tlsOpts transport.TLSOptions, rec *transport.Recorder, met *metrics.Connector) (*Session, error) {
	// The POSTURE is injected; the *tls.Config is built once in the
	// transport layer. This connector used to assemble its own, with no
	// MinVersion — see internal/transport/tls.go for why that is now a
	// transport-level decision rather than a per-protocol one.
	tlsCfg, err := tlsOpts.Client()
	if err != nil {
		return nil, fmt.Errorf("cerebrum-nb: tls config: %w", err)
	}
	opts := &ws.DialOptions{TLSConfig: tlsCfg}
	conn, err := ws.Dial(ctx, urlStr, opts)
	if err != nil {
		return nil, fmt.Errorf("cerebrum-nb: ws dial %s: %w", urlStr, err)
	}
	host, port := splitURLHostPort(urlStr)
	s := &Session{
		logger:     logger,
		conn:       conn,
		host:       host,
		port:       port,
		compliance: &Profile{},
		rec:        rec,
		pending:    map[string]chan *codec.Frame{},
		stopRX:     make(chan struct{}),
		met:        met,
		done:       make(chan struct{}),
	}
	s.mtidNext.Store(1)
	// Liveness is ON by default. A watcher that runs for months must not
	// depend on the operator remembering a flag to avoid hanging forever on
	// a half-open socket; --keepalive / --keepalive-timeout tune it, and
	// consumer.DisableInterval / DisableTimeout turn it off deliberately.
	s.conn.SetIdleTimeout(defaultKeepAliveTimeout)
	go s.readLoop()
	s.startKeepAlive(defaultKeepAliveInterval, clock.System())
	return s, nil
}

// RemoteHostPort returns the (host, port) the session is talking to.
func (s *Session) RemoteHostPort() (string, int) { return s.host, s.port }

// APIVersion returns the raw login_reply api_ver string (e.g. "0.13").
// Empty when login has not completed.
func (s *Session) APIVersion() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.apiVer
}

// APIVersionMajor returns the integer major component of the
// login_reply api_ver — useful as consumer.DeviceInfo.ProtocolVersion.
// Note: Cerebrum currently ships api_ver="0.x"; major can legitimately
// be 0. Use APIVersion() for display.
func (s *Session) APIVersionMajor() int {
	s.mu.Lock()
	v := s.apiVer
	s.mu.Unlock()
	dot := strings.IndexByte(v, '.')
	if dot < 0 {
		dot = len(v)
	}
	n, _ := strconv.Atoi(v[:dot])
	return n
}

// LoggedIn reports whether the session has completed login successfully.
func (s *Session) LoggedIn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loggedIn
}

// nextMTID allocates the next mtid. Wraps to 1 on overflow (0 is
// reserved as "unset").
func (s *Session) nextMTID() uint32 {
	for {
		v := s.mtidNext.Add(1) - 1
		if v == 0 {
			continue
		}
		return v
	}
}

// roundTrip sends payload and blocks for the matching ack/nack/busy
// or login_reply / poll_reply (any frame whose mtid matches). Returns
// the matched Frame; turns NACK into a NackError. Times out per ctx.
func (s *Session) roundTrip(ctx context.Context, mtid uint32, payload []byte) (*codec.Frame, error) {
	ch := make(chan *codec.Frame, 1)
	mtidStr := strconv.FormatUint(uint64(mtid), 10)

	s.mu.Lock()
	if _, dup := s.pending[mtidStr]; dup {
		s.mu.Unlock()
		s.compliance.Event("cerebrum_mtid_reused")
		return nil, fmt.Errorf("cerebrum-nb: mtid %s already in flight", mtidStr)
	}
	s.pending[mtidStr] = ch
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pending, mtidStr)
		s.mu.Unlock()
	}()

	// Raw TX at debug level — the wire truth for diagnostics; --debug on the
	// CLI promises "verbose RX/TX XML logging" and this is that promise.
	// Redact credentials before the frame reaches ANY sink: the LOGIN frame
	// carries the NB password in cleartext, and the log now persists daily
	// files (and may forward them to a remote collector), while the capture
	// is the artefact operators attach to vendor bug reports. See redact.go.
	safe := redactSecrets(payload)
	s.logger.Debug("tx", slog.String("xml", string(safe)))
	s.rec.Record("cerebrum-nb", "tx", safe)
	if err := s.conn.WriteText(ctx, payload); err != nil {
		return nil, fmt.Errorf("cerebrum-nb: write: %w", err)
	}
	if s.met != nil {
		s.met.ObserveTx(len(payload), 0)
	}

	select {
	case f := <-ch:
		switch f.Kind {
		case codec.KindNack:
			s.recordNack(f.Nack)
			return f, f.Nack
		case codec.KindBusy:
			s.compliance.Event("cerebrum_busy_received")
		}
		return f, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// recordNack fires the matching cerebrum_nack_<code> compliance event.
func (s *Session) recordNack(n *codec.NackError) {
	if n == nil {
		return
	}
	name := "cerebrum_nack_unknown"
	if n.ID >= 0 {
		name = "cerebrum_nack_" + strings.ToLower(n.Code)
	}
	s.compliance.Event(name)
}

// login sends <login>, waits for login_reply / nack, and stores api_ver.
func (s *Session) login(ctx context.Context, user, pass string) error {
	mtid := s.nextMTID()
	payload := codec.EncodeLogin(mtid, user, pass)
	f, err := s.roundTrip(ctx, mtid, payload)
	if err != nil {
		return fmt.Errorf("cerebrum-nb: login: %w", err)
	}
	if f.Kind != codec.KindLoginReply {
		return fmt.Errorf("cerebrum-nb: login: unexpected reply %s", f.Kind)
	}
	s.mu.Lock()
	s.loggedIn = true
	s.apiVer = f.LoginReply.APIVer
	s.mu.Unlock()
	s.logger.Info("logged in",
		slog.String("user", user),
		slog.String("api_ver", f.LoginReply.APIVer),
		slog.String("host", s.host),
	)
	return nil
}

// Poll sends <poll/> and returns the parsed reply. Fires
// cerebrum_server_inactive when CONNECTED_SERVER_ACTIVE='0'.
func (s *Session) Poll(ctx context.Context) (*codec.PollReply, error) {
	mtid := s.nextMTID()
	f, err := s.roundTrip(ctx, mtid, codec.EncodePoll(mtid))
	if err != nil {
		return nil, err
	}
	if f.Kind != codec.KindPollReply {
		return nil, fmt.Errorf("cerebrum-nb: poll: unexpected %s", f.Kind)
	}
	if !f.PollReply.ConnectedServerActive {
		s.compliance.Event("cerebrum_server_inactive")
	}
	return f.PollReply, nil
}

// Action sends <action><body/></action> and returns nil on ack, the
// NACK error on nack, or wraps a transport error.
func (s *Session) Action(ctx context.Context, body codec.ActionBody) error {
	mtid := s.nextMTID()
	payload := codec.EncodeAction(mtid, body)
	f, err := s.roundTrip(ctx, mtid, payload)
	if err != nil {
		return err
	}
	switch f.Kind {
	case codec.KindAck:
		return nil
	case codec.KindBusy:
		return fmt.Errorf("cerebrum-nb: action: server busy (mtid %d)", mtid)
	default:
		return fmt.Errorf("cerebrum-nb: action: unexpected reply %s", f.Kind)
	}
}

// Subscribe sends <subscribe> with the given items and returns nil on
// ack. After ack, matching events flow into all registered callbacks.
func (s *Session) Subscribe(ctx context.Context, items []codec.SubItem) error {
	mtid := s.nextMTID()
	f, err := s.roundTrip(ctx, mtid, codec.EncodeSubscribe(mtid, items))
	if err != nil {
		return err
	}
	if f.Kind != codec.KindAck {
		return fmt.Errorf("cerebrum-nb: subscribe: unexpected %s", f.Kind)
	}
	return nil
}

// Obtain sends <obtain> and waits for the ack. Snapshot events arrive
// asynchronously through the dispatcher; Obtain itself does not collect
// them — register an OnEvent callback before calling.
func (s *Session) Obtain(ctx context.Context, items []codec.SubItem) error {
	mtid := s.nextMTID()
	f, err := s.roundTrip(ctx, mtid, codec.EncodeObtain(mtid, items))
	if err != nil {
		return err
	}
	if f.Kind != codec.KindAck {
		return fmt.Errorf("cerebrum-nb: obtain: unexpected %s", f.Kind)
	}
	return nil
}

// UnsubscribeAll sends <unsubscribe_all/> and waits for ack. Clears
// every server-side subscription on this connection.
func (s *Session) UnsubscribeAll(ctx context.Context) error {
	mtid := s.nextMTID()
	f, err := s.roundTrip(ctx, mtid, codec.EncodeUnsubscribeAll(mtid))
	if err != nil {
		return err
	}
	if f.Kind != codec.KindAck {
		return fmt.Errorf("cerebrum-nb: unsubscribe_all: unexpected %s", f.Kind)
	}
	return nil
}

// OnEvent registers fn to receive every async RX event whose Kind
// equals kind. Pass codec.KindUnknown for "any event".
//
// The returned Subscription can be passed to Cancel to stop receiving.
func (s *Session) OnEvent(kind codec.FrameKind, fn EventFunc) *Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub := &Subscription{
		id:   s.mtidNext.Add(0), // borrow the counter for unique IDs (no wire effect)
		kind: kind,
		fn:   fn,
	}
	s.subs = append(s.subs, sub)
	return sub
}

// Cancel removes a previously-registered event subscription.
func (s *Session) Cancel(sub *Subscription) {
	if sub == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, x := range s.subs {
		if x == sub {
			s.subs = append(s.subs[:i], s.subs[i+1:]...)
			return
		}
	}
}

// readLoop pulls frames off the WebSocket and dispatches them to either
// the pending-request map (matched by mtid) or the OnEvent subscriber
// list (free-standing events). Exits on close or read error.
func (s *Session) readLoop() {
	for {
		select {
		case <-s.stopRX:
			return
		default:
		}
		op, payload, err := s.conn.ReadMessage(context.Background())
		if err != nil {
			// The session is over. Classify WHY, then publish it on Done so
			// a supervisor can reconnect. Returning silently here — the old
			// behaviour — is precisely what made a 24/7 watcher go quiet
			// without crashing: the reader vanished and nobody was told.
			s.markLost(s.classifyReadErr(err))
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				s.logger.Debug("read", slog.String("err", err.Error()))
			}
			return
		}
		s.noteRX()
		if s.met != nil {
			s.met.ObserveRx(len(payload))
		}
		if op != ws.OpText {
			// Cerebrum doesn't speak Binary; log and drop.
			s.logger.Debug("dropping non-text frame", slog.Int("opcode", int(op)))
			continue
		}
		// Raw RX at debug level — see the tx twin in roundTrip.
		// RX is redacted too: a server echo or an error frame can quote the
		// offending request back at us, credentials included.
		safeRX := redactSecrets(payload)
		s.logger.Debug("rx", slog.String("xml", string(safeRX)))
		s.rec.Record("cerebrum-nb", "rx", safeRX)
		f, err := codec.Decode(payload)
		if err != nil {
			s.logger.Warn("decode failed",
				slog.String("err", err.Error()),
				slog.Int("len", len(payload)))
			s.compliance.Event("cerebrum_decode_failed")
			continue
		}
		if f.CaseChanged {
			s.compliance.Event("cerebrum_case_normalized")
		}
		s.dispatch(f)
	}
}

// dispatch routes f to either a pending mtid waiter or every matching
// OnEvent callback.
func (s *Session) dispatch(f *codec.Frame) {
	// 1. Try to match an in-flight request by mtid.
	if f.MTID != "" {
		// Only Ack / Nack / Busy / LoginReply / PollReply /
		// DeviceConfigResult are valid terminal replies for an in-flight
		// request. ROUTING_CHANGE / CATEGORY_CHANGE / SALVO_CHANGE /
		// DEVICE_CHANGE rows can carry the same MTID as the originating
		// SUBSCRIBE (server streams the snapshot rows before the
		// WILDCARD_COMPLETE + ACK), but they are notifications, not
		// replies — routing them into the pending channel makes roundTrip
		// return the first row instead of the ACK, which then
		// mis-classifies the SUBSCRIBE as "unexpected ROUTING_CHANGE"
		// failure. Send only terminal replies to the pending waiter; let
		// everything else (including CONTINUE / WILDCARD_COMPLETE, which
		// are flow-control notifications, not request terminators) fan out
		// to subscribers.
		switch f.Kind {
		case codec.KindAck, codec.KindNack, codec.KindBusy,
			codec.KindLoginReply, codec.KindPollReply,
			codec.KindDeviceConfigResult:
			s.mu.Lock()
			ch, ok := s.pending[f.MTID]
			s.mu.Unlock()
			if ok {
				select {
				case ch <- f:
				default:
					s.logger.Warn("dropped reply (channel full)", slog.String("mtid", f.MTID))
				}
				return
			}
		}
	}
	// 2. Flow-control notifications (§1.4 CONTINUE after BUSY, §1.6
	// WILDCARD_COMPLETE end-of-snapshot). These are not terminal replies
	// to a request, so they fall through the mtid switch above and are
	// logged here before fanning out — a listener that registered an
	// OnEvent(KindContinue/KindWildcardComplete) still sees them.
	switch f.Kind {
	case codec.KindContinue:
		// Server signals it has drained its BUSY backlog and the client
		// may resume sending. Log + continue (no client-side throttle is
		// modelled today); subscribers may react.
		s.logger.Debug("flow-control: CONTINUE (resume after BUSY)", slog.String("mtid", f.MTID))
		s.compliance.Event("cerebrum_continue_received")
	case codec.KindWildcardComplete:
		// End of an OBTAIN/SUBSCRIBE wildcard snapshot — every matching
		// row has been sent. Subscribers use this as the "snapshot
		// complete" marker.
		s.logger.Debug("flow-control: WILDCARD_COMPLETE (snapshot done)", slog.String("mtid", f.MTID))
	}

	// 3. Fan out to OnEvent subscribers.
	if f.Kind == codec.KindUnknown {
		s.compliance.Event("cerebrum_unknown_notification")
		return
	}
	s.mu.Lock()
	subs := append([]*Subscription{}, s.subs...)
	s.mu.Unlock()
	for _, sub := range subs {
		if sub.kind != codec.KindUnknown && sub.kind != f.Kind {
			continue
		}
		if sub.fn != nil {
			sub.fn(f)
		}
	}
}

// close tears down the session. Idempotent.
func (s *Session) close() error {
	s.closeOnce.Do(func() {
		// Stop the prober BEFORE the socket goes away, so its final tick
		// cannot race the close and report a spurious "keepalive failed".
		s.stopKeepAlive()
		if s.stopRX != nil {
			close(s.stopRX)
		}
		// conn is nil when a dial failed partway, or when the supervisor
		// tears down a session it never finished building. Closing such a
		// session must be a no-op, not a panic on the cleanup path.
		if s.conn != nil {
			s.closeErr = s.conn.Close(1000, "client closing")
		}
		s.markLost(fmt.Errorf("%w: session closed by client", transport.ErrConnectionLost))
		_ = s.rec.Close() // nil-safe; flush the --capture wire-trace
	})
	return s.closeErr
}

// splitURLHostPort returns ("host", port) from urlStr ("ws://h:p/" etc.).
func splitURLHostPort(urlStr string) (string, int) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", 0
	}
	host := u.Hostname()
	var port int
	if p := u.Port(); p != "" {
		port, _ = strconv.Atoi(p)
	} else if u.Scheme == "wss" {
		port = 443
	} else {
		port = 80
	}
	return host, port
}

// splitDevicePath splits a "device.sub_device.object" Path into its
// three components. Either / both of sub_device and object may
// contain dotted segments — only the first dot is treated as the
// device boundary; the second dot splits sub_device from object. The
// rest is the object's dotted name (preserved verbatim).
func splitDevicePath(path string) (device, sub, obj string, err error) {
	first := strings.IndexByte(path, '.')
	if first < 0 {
		return "", "", "", fmt.Errorf("cerebrum-nb: path %q must be device.sub_device.object", path)
	}
	device = path[:first]
	rest := path[first+1:]
	second := strings.IndexByte(rest, '.')
	if second < 0 {
		return "", "", "", fmt.Errorf("cerebrum-nb: path %q must be device.sub_device.object", path)
	}
	sub = rest[:second]
	obj = rest[second+1:]
	if device == "" || sub == "" || obj == "" {
		return "", "", "", fmt.Errorf("cerebrum-nb: path %q has empty component", path)
	}
	return device, sub, obj, nil
}
