package emberplus

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"acp/internal/protocol/compliance"
	"acp/internal/protocol/emberplus/glow"
	"acp/internal/protocol/emberplus/s101"
	"acp/internal/transport"
)

// Session manages a single TCP connection to an Ember+ provider.
type Session struct {
	conn     net.Conn
	reader   *s101.Reader
	writer   *s101.Writer
	logger   *slog.Logger
	mu       sync.Mutex
	closed   bool

	// Callbacks for received elements.
	onElement func([]glow.Element)

	// Callbacks for invocation results (function invoke).
	invocations   map[int32]chan *glow.InvocationResult
	invocationsMu sync.Mutex
	nextInvID     int32

	// Keep-alive.
	keepAliveInterval time.Duration
	keepAliveDone     chan struct{}

	// Session-health tracking. lastRX is touched on every frame
	// arriving from the provider (keep-alive responses count) so
	// the dead-man timer can declare the session dead when no
	// traffic has been seen for deadManThreshold. onStateChange is
	// the plugin-side notifier fired on true/false transitions;
	// guarded against double-fire by the plugin, not by Session.
	lastRX            time.Time
	lastRXMu          sync.RWMutex
	onStateChange     func(connected bool, reason string)
	deadManThreshold  time.Duration
	deadManDone       chan struct{}

	// profile records tolerance events (spec deviations absorbed on
	// the fly) per connection. Set by the Plugin via SetProfile.
	profile *compliance.Profile

	// recorder captures every raw S101 frame (including BOF/EOF/CRC)
	// into JSONL when the caller passed --capture. Plugin sets it
	// before Connect via SetRecorder.
	recorder *transport.Recorder
}

// SetRecorder attaches a traffic recorder. Call before Connect.
// After connection, every S101 frame (tx + rx) is written to the
// recorder, tagged proto="emberplus" and dir="tx" / "rx".
func (s *Session) SetRecorder(r *transport.Recorder) {
	s.mu.Lock()
	s.recorder = r
	s.mu.Unlock()
}

// SetProfile attaches a compliance profile for this session. Events
// observed in the read loop (e.g. multi-frame reassembly) are noted
// into it. Safe to call once before Connect.
func (s *Session) SetProfile(p *compliance.Profile) {
	s.mu.Lock()
	s.profile = p
	s.mu.Unlock()
}

// noteCompliance records a tolerance event in the session's profile,
// if one is attached. Safe to call under any lock state — Profile.Note
// is self-synchronised.
func (s *Session) noteCompliance(event string) {
	s.mu.Lock()
	p := s.profile
	s.mu.Unlock()
	p.Note(event)
}

// NewSession creates a session (not yet connected).
func NewSession(logger *slog.Logger) *Session {
	return &Session{
		logger:            logger,
		keepAliveInterval: 10 * time.Second,
		deadManThreshold:  30 * time.Second, // 3× keep-alive interval
		invocations:       make(map[int32]chan *glow.InvocationResult),
	}
}

// SetOnStateChange registers a callback fired on session transitions.
// Called once with connected=true after Connect succeeds, and once
// with connected=false on keep-alive timeout or read-loop exit (TCP
// EOF, decode error, etc.). Caller is responsible for de-duplicating
// back-to-back false fires.
func (s *Session) SetOnStateChange(fn func(connected bool, reason string)) {
	s.mu.Lock()
	s.onStateChange = fn
	s.mu.Unlock()
}

func (s *Session) touchRX() {
	s.lastRXMu.Lock()
	s.lastRX = time.Now()
	s.lastRXMu.Unlock()
}

func (s *Session) rxAge() time.Duration {
	s.lastRXMu.RLock()
	defer s.lastRXMu.RUnlock()
	if s.lastRX.IsZero() {
		return 0
	}
	return time.Since(s.lastRX)
}

// fireStateChange dispatches to the registered callback if any.
// Holds no lock across the user callback.
func (s *Session) fireStateChange(connected bool, reason string) {
	s.mu.Lock()
	fn := s.onStateChange
	s.mu.Unlock()
	if fn != nil {
		fn(connected, reason)
	}
}

// deadManLoop watches lastRX. If the provider has been silent for
// longer than deadManThreshold, it declares the session dead,
// closes the connection (which unblocks readLoop via EOF), and
// fires a disconnected state change with a clear reason.
//
// Owned by the Session; terminates when deadManDone closes
// (Disconnect path) or the threshold fires.
func (s *Session) deadManLoop() {
	tick := s.deadManThreshold / 3
	if tick < time.Second {
		tick = time.Second
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-s.deadManDone:
			return
		case <-ticker.C:
			age := s.rxAge()
			if age == 0 {
				continue
			}
			if age > s.deadManThreshold {
				s.logger.Warn("emberplus: session dead-man",
					"rx_age", age, "threshold", s.deadManThreshold)
				reason := fmt.Sprintf("keep-alive timeout (no rx for %s)", age.Truncate(time.Second))
				// Trigger state change first, then close the
				// connection so readLoop exits cleanly.
				s.fireStateChange(false, reason)
				_ = s.Disconnect()
				return
			}
		}
	}
}

// NextInvocationID returns a unique invocation ID for function calls.
func (s *Session) NextInvocationID() int32 {
	s.invocationsMu.Lock()
	defer s.invocationsMu.Unlock()
	s.nextInvID++
	return s.nextInvID
}

// RegisterInvocation registers a channel to receive the result for an invocation ID.
func (s *Session) RegisterInvocation(id int32, ch chan *glow.InvocationResult) {
	s.invocationsMu.Lock()
	s.invocations[id] = ch
	s.invocationsMu.Unlock()
}

// UnregisterInvocation removes an invocation result channel.
func (s *Session) UnregisterInvocation(id int32) {
	s.invocationsMu.Lock()
	delete(s.invocations, id)
	s.invocationsMu.Unlock()
}

// deliverInvocationResult dispatches an InvocationResult to the waiting caller.
func (s *Session) deliverInvocationResult(result *glow.InvocationResult) {
	s.invocationsMu.Lock()
	ch, ok := s.invocations[result.InvocationID]
	s.invocationsMu.Unlock()
	if ok {
		select {
		case ch <- result:
		default:
		}
	}
}

// Connect dials the Ember+ provider and starts the read loop.
func (s *Session) Connect(ctx context.Context, host string, port int) error {
	addr := fmt.Sprintf("%s:%d", host, port)
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return WrapS101(fmt.Sprintf("connect %s", addr), err)
	}

	s.mu.Lock()
	s.conn = conn
	s.reader = s101.NewReader(conn)
	s.writer = s101.NewWriter(conn)
	s.closed = false
	s.keepAliveDone = make(chan struct{})
	s.deadManDone = make(chan struct{})
	rec := s.recorder
	if rec != nil {
		s.writer.SetTap(func(b []byte) { rec.Record("emberplus", "tx", b) })
		s.reader.SetTap(func(b []byte) { rec.Record("emberplus", "rx", b) })
	}
	s.mu.Unlock()

	// Prime lastRX so the dead-man doesn't fire immediately if the
	// provider is slow to send its first frame.
	s.touchRX()

	// Start read loop.
	go s.readLoop()

	// Start keep-alive.
	go s.keepAliveLoop()

	// Start dead-man watchdog.
	go s.deadManLoop()

	s.logger.Info("emberplus: connected", "host", host, "port", port)

	// Fire state-change last so the plugin's reaction (starting
	// subscriptions etc.) runs AFTER all Session goroutines are up.
	s.fireStateChange(true, "connected")
	return nil
}

// Disconnect closes the TCP connection. Intentional disconnect does
// NOT fire the state-change callback — the caller already knows.
// Only unsolicited disconnects (dead-man, read error) fire state.
func (s *Session) Disconnect() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	kaDone := s.keepAliveDone
	dmDone := s.deadManDone
	s.keepAliveDone = nil
	s.deadManDone = nil
	conn := s.conn
	s.mu.Unlock()

	if kaDone != nil {
		closeChanSafe(kaDone)
	}
	if dmDone != nil {
		closeChanSafe(dmDone)
	}
	if conn != nil {
		return conn.Close()
	}
	return nil
}

// closeChanSafe closes a channel unless it was already closed.
// Guards against the deadManLoop having already closed deadManDone
// on its own exit path.
func closeChanSafe(ch chan struct{}) {
	defer func() { _ = recover() }()
	close(ch)
}

// SetOnElement sets the callback for received Glow elements.
func (s *Session) SetOnElement(fn func([]glow.Element)) {
	s.mu.Lock()
	s.onElement = fn
	s.mu.Unlock()
}

// SendGetDirectory sends a GetDirectory command for the root.
func (s *Session) SendGetDirectory() error {
	payload := glow.EncodeGetDirectory()
	s.logger.Debug("emberplus: sending GetDirectory",
		"payload_len", len(payload),
		"payload_hex", fmt.Sprintf("%x", payload))
	return s.sendEmBER(payload)
}

// SendGetDirectoryFor sends a GetDirectory command for a specific path.
func (s *Session) SendGetDirectoryFor(path []int32) error {
	payload := glow.EncodeGetDirectoryFor(path)
	s.logger.Debug("emberplus: sending GetDirectoryFor",
		"path", path,
		"payload_len", len(payload),
		"payload_hex", fmt.Sprintf("%x", payload))
	return s.sendEmBER(payload)
}

// SendMatrixGetDirectory sends a GetDirectory command addressed at a
// matrix node. Per spec p.42, this implicitly subscribes the consumer
// to matrix-connection changes — the provider responds with the full
// targets / sources / connections collection and then streams
// disposition updates on further changes.
func (s *Session) SendMatrixGetDirectory(matrixPath []int32) error {
	payload := glow.EncodeMatrixGetDirectory(matrixPath)
	s.logger.Debug("emberplus: SendMatrixGetDirectory",
		"matrix_path", matrixPath,
		"payload_len", len(payload))
	return s.sendEmBER(payload)
}

// SendSubscribe sends a Subscribe command for a parameter path.
func (s *Session) SendSubscribe(path []int32) error {
	payload := glow.EncodeSubscribe(path)
	return s.sendEmBER(payload)
}

// SendUnsubscribe sends an Unsubscribe command.
func (s *Session) SendUnsubscribe(path []int32) error {
	payload := glow.EncodeUnsubscribe(path)
	return s.sendEmBER(payload)
}

// SendSetValue sends a set-value command for a parameter.
func (s *Session) SendSetValue(path []int32, value interface{}) error {
	payload := glow.EncodeSetValue(path, value)
	s.logger.Debug("emberplus: SendSetValue",
		"path", path,
		"value", value,
		"payload_len", len(payload),
		"payload_hex", fmt.Sprintf("%x", payload))
	return s.sendEmBER(payload)
}

// SendMatrixConnect sends a matrix connection command.
func (s *Session) SendMatrixConnect(matrixPath []int32, target int32, sources []int32, operation int64) error {
	payload := glow.EncodeMatrixConnect(matrixPath, target, sources, operation)
	s.logger.Debug("emberplus: SendMatrixConnect",
		"matrix_path", matrixPath,
		"target", target,
		"sources", sources,
		"payload_len", len(payload),
		"payload_hex", fmt.Sprintf("%x", payload))
	return s.sendEmBER(payload)
}

// SendInvoke sends a function invocation.
func (s *Session) SendInvoke(funcPath []int32, invocationID int32, args []interface{}) error {
	payload := glow.EncodeInvoke(funcPath, invocationID, args)
	s.logger.Debug("emberplus: SendInvoke",
		"path", funcPath,
		"invocation_id", invocationID,
		"args", args,
		"payload_len", len(payload),
		"payload_hex", fmt.Sprintf("%x", payload))
	return s.sendEmBER(payload)
}

func (s *Session) sendEmBER(payload []byte) error {
	s.mu.Lock()
	w := s.writer
	s.mu.Unlock()
	if w == nil {
		return fmt.Errorf("emberplus: not connected")
	}
	frame := s101.NewEmBERFrame(payload)
	return w.WriteFrame(frame)
}

func (s *Session) readLoop() {
	// Multi-frame reassembly buffer. Large Glow messages may be split
	// across multiple S101 frames using FlagFirst/FlagLast.
	var multiframeBuf []byte

	for {
		s.mu.Lock()
		r := s.reader
		closed := s.closed
		s.mu.Unlock()
		if closed || r == nil {
			return
		}

		frame, err := r.ReadFrame()
		if err != nil {
			s.mu.Lock()
			closed = s.closed
			s.mu.Unlock()
			if !closed {
				s.logger.Debug("emberplus: read error", "err", err)
				// Unsolicited disconnect — notify plugin.
				s.fireStateChange(false, fmt.Sprintf("read error: %v", err))
			}
			return
		}
		// Any frame arrival — keep-alive response, EmBER payload,
		// anything — counts as the provider being alive.
		s.touchRX()

		if frame.IsKeepAlive() {
			s.logger.Debug("emberplus: keep-alive rx", "cmd", frame.Command)
			if frame.Command == s101.CmdKeepAliveReq {
				resp := s101.NewKeepAliveResponse()
				s.mu.Lock()
				w := s.writer
				s.mu.Unlock()
				if w != nil {
					if err := w.WriteFrame(resp); err != nil {
						s.logger.Debug("emberplus: keep-alive resp failed", "err", err)
					} else {
						s.logger.Debug("emberplus: keep-alive resp sent")
					}
				}
			}
			continue
		}

		if !frame.IsEmBER() || len(frame.Payload) == 0 {
			continue
		}

		// Multi-frame reassembly per S101 spec.
		var payload []byte
		switch {
		case frame.Flags == s101.FlagSingle:
			// Single-packet message — decode directly.
			payload = frame.Payload
		case frame.Flags&s101.FlagFirst != 0:
			// First fragment of a multi-packet message.
			multiframeBuf = append([]byte{}, frame.Payload...)
			s.logger.Debug("emberplus: multi-frame start", "len", len(frame.Payload))
			s.noteCompliance(MultiFrameReassembly)
			continue
		case frame.Flags&s101.FlagLast != 0:
			// Last fragment — assemble and decode.
			multiframeBuf = append(multiframeBuf, frame.Payload...)
			payload = multiframeBuf
			s.logger.Debug("emberplus: multi-frame complete", "total_len", len(payload))
			multiframeBuf = nil
		default:
			// Middle fragment — accumulate.
			multiframeBuf = append(multiframeBuf, frame.Payload...)
			s.logger.Debug("emberplus: multi-frame continue", "buf_len", len(multiframeBuf))
			continue
		}

		s.logger.Debug("emberplus: received EmBER frame",
			"payload_len", len(payload),
			"dtd", frame.DTD,
			"version", frame.Version,
			"hex", fmt.Sprintf("%x", payload))
		elements, err := glow.DecodeRoot(payload)
		if err != nil {
			s.logger.Debug("emberplus: glow decode error",
				"err", err,
				"payload_len", len(payload))
			continue
		}
		s.logger.Debug("emberplus: decoded glow elements", "count", len(elements))
		s.mu.Lock()
		fn := s.onElement
		s.mu.Unlock()
		if fn != nil && len(elements) > 0 {
			fn(elements)
		}
	}
}

func (s *Session) keepAliveLoop() {
	ticker := time.NewTicker(s.keepAliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.keepAliveDone:
			return
		case <-ticker.C:
			s.mu.Lock()
			w := s.writer
			closed := s.closed
			s.mu.Unlock()
			if closed || w == nil {
				return
			}
			req := s101.NewKeepAliveRequest()
			if err := w.WriteFrame(req); err != nil {
				s.logger.Debug("emberplus: keep-alive failed", "err", err)
				return
			}
		}
	}
}
