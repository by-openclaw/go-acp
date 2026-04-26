package cerebrumnb

import (
	"context"
	"crypto/tls"
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

	"acp/internal/cerebrum-nb/codec"
	"acp/internal/cerebrum-nb/codec/ws"
)

// PingInterval is how often the consumer pings Cerebrum as a fallback
// keep-alive when no <poll> traffic is in flight. RFC 6455 ping; the
// server replies with Pong.
const PingInterval = 30 * time.Second

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

	mtidNext atomic.Uint32

	mu         sync.Mutex
	pending    map[string]chan *codec.Frame
	subs       []*Subscription
	apiVer     string
	loggedIn   bool

	closeOnce sync.Once
	closeErr  error
	stopRX    chan struct{}
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
// Login is performed by the caller via session.login.
func newSession(ctx context.Context, logger *slog.Logger, urlStr string, useTLS, insecure bool) (*Session, error) {
	opts := &ws.DialOptions{}
	if useTLS && insecure {
		opts.TLSConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
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
		pending:    map[string]chan *codec.Frame{},
		stopRX:     make(chan struct{}),
	}
	s.mtidNext.Store(1)
	go s.readLoop()
	go s.pingLoop()
	return s, nil
}

// RemoteHostPort returns the (host, port) the session is talking to.
func (s *Session) RemoteHostPort() (string, int) { return s.host, s.port }

// APIVersionMajor returns the integer major component of the
// login_reply api_ver — useful as protocol.DeviceInfo.ProtocolVersion.
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

	if err := s.conn.WriteText(ctx, payload); err != nil {
		return nil, fmt.Errorf("cerebrum-nb: write: %w", err)
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
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				s.logger.Debug("read", slog.String("err", err.Error()))
			}
			return
		}
		if op != ws.OpText {
			// Cerebrum doesn't speak Binary; log and drop.
			s.logger.Debug("dropping non-text frame", slog.Int("opcode", int(op)))
			continue
		}
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
		s.mu.Lock()
		ch, ok := s.pending[f.MTID]
		s.mu.Unlock()
		if ok {
			select {
			case ch <- f:
			default:
				s.logger.Warn("dropped reply (channel full)", slog.String("mtid", f.MTID))
			}
			// Replies that also need to fan out to subscribers fall through.
			// Per spec, ack/nack/busy/login_reply/poll_reply only ever match
			// the in-flight request; events have no mtid waiter so we treat
			// them as exclusive to the subscriber path below.
			switch f.Kind {
			case codec.KindAck, codec.KindNack, codec.KindBusy,
				codec.KindLoginReply, codec.KindPollReply:
				return
			}
		}
	}
	// 2. Fan out to OnEvent subscribers.
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

// pingLoop emits a Ping every PingInterval as fallback keep-alive.
// Cerebrum doesn't require it (TCP keep-alive could be enough) but a
// Ping is cheap and surfaces dead connections fast.
func (s *Session) pingLoop() {
	t := time.NewTicker(PingInterval)
	defer t.Stop()
	for {
		select {
		case <-s.stopRX:
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := s.conn.Ping(ctx, nil)
			cancel()
			if err != nil {
				s.logger.Debug("ping failed", slog.String("err", err.Error()))
				return
			}
		}
	}
}

// close tears down the session. Idempotent.
func (s *Session) close() error {
	s.closeOnce.Do(func() {
		close(s.stopRX)
		s.closeErr = s.conn.Close(1000, "client closing")
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
	port := 0
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
