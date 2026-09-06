package cerebrumnb

// Edge-path coverage for the 24/7 liveness machinery: the guard clauses and
// fallbacks that keep a supervised watch from panicking or leaking when it is
// driven in an unusual order (nil clock, nil conn, double start, cancel during
// backoff). These are exactly the paths a long-running process eventually hits.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"dhs/internal/cerebrum-nb/codec"
	"dhs/internal/clock"
	"dhs/internal/transport"
	"dhs/internal/transport/ws"
)

func TestStartKeepAliveGuards(t *testing.T) {
	s := &Session{
		logger:     slog.New(slog.NewTextHandler(discard{}, nil)),
		compliance: &Profile{},
		stopRX:     make(chan struct{}),
		done:       make(chan struct{}),
	}
	// Non-positive interval arms nothing.
	s.startKeepAlive(0, clock.NewFake(time.Time{}))
	if s.ka != nil {
		t.Fatal("interval 0 must not start a prober")
	}
	s.startKeepAlive(-time.Second, clock.NewFake(time.Time{}))
	if s.ka != nil {
		t.Fatal("negative interval must not start a prober")
	}

	// Start once, then a second start must be a no-op (no leaked goroutine).
	fake := clock.NewFake(time.Time{})
	s.startKeepAlive(time.Minute, fake)
	first := s.ka
	if first == nil {
		t.Fatal("prober did not start")
	}
	s.startKeepAlive(time.Minute, fake)
	if s.ka != first {
		t.Fatal("a second startKeepAlive replaced the running prober")
	}
	s.stopKeepAlive()
}

// The prober must return when the session dies, not only when explicitly
// stopped — otherwise a lost session leaks its prober for the process's life.
func TestKeepAliveLoopExitsOnSessionDone(t *testing.T) {
	s := &Session{
		logger:     slog.New(slog.NewTextHandler(discard{}, nil)),
		compliance: &Profile{},
		stopRX:     make(chan struct{}),
		done:       make(chan struct{}),
	}
	fake := clock.NewFake(time.Time{})
	s.startKeepAlive(time.Minute, fake)
	ka := s.ka
	if ka == nil {
		t.Fatal("prober did not start")
	}
	s.markLost(errors.New("link died"))

	done := make(chan struct{})
	go func() { ka.stopped.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("prober did not exit when the session was marked lost")
	}
}

// And it must return on stopRX (the close path) too.
func TestKeepAliveLoopExitsOnStopRX(t *testing.T) {
	s := &Session{
		logger:     slog.New(slog.NewTextHandler(discard{}, nil)),
		compliance: &Profile{},
		stopRX:     make(chan struct{}),
		done:       make(chan struct{}),
	}
	fake := clock.NewFake(time.Time{})
	s.startKeepAlive(time.Minute, fake)
	ka := s.ka
	close(s.stopRX)

	done := make(chan struct{})
	go func() { ka.stopped.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("prober did not exit on stopRX")
	}
}

func TestSessionLiveEdgePaths(t *testing.T) {
	s := &Session{done: make(chan struct{})}

	// No frame yet -> not live (and conn is nil, so the window falls back).
	if s.SessionLive() {
		t.Error("SessionLive must be false before any frame arrives")
	}
	// A recent frame with a nil conn uses the default window -> live.
	s.noteRX()
	if !s.SessionLive() {
		t.Error("SessionLive must be true right after a frame, even with a nil conn")
	}
	// An ancient frame is stale.
	s.lastRX.Store(time.Now().Add(-24 * time.Hour).UnixNano())
	if s.SessionLive() {
		t.Error("SessionLive must be false for a frame outside the window")
	}
	// Once lost, never live again.
	s.noteRX()
	s.markLost(errors.New("gone"))
	if s.SessionLive() {
		t.Error("SessionLive must be false once the session is lost")
	}
}

func TestLastRxZeroAndSet(t *testing.T) {
	s := &Session{done: make(chan struct{})}
	if got := s.LastRx(); !got.IsZero() {
		t.Fatalf("LastRx = %v before any frame, want the zero time", got)
	}
	s.noteRX()
	if got := s.LastRx(); got.IsZero() {
		t.Fatal("LastRx is still zero after noteRX")
	}
}

func TestSupervisorLogFallbacks(t *testing.T) {
	// No logger at all -> a discard logger, never nil.
	s := &Supervisor{}
	if s.log() == nil {
		t.Fatal("log() returned nil with no logger configured")
	}
	// Logger field only.
	explicit := slog.New(slog.NewTextHandler(discard{}, nil))
	s = &Supervisor{Logger: explicit}
	if s.log() != explicit {
		t.Fatal("log() ignored the Logger field")
	}
	// LoggerFn wins over Logger when it yields one.
	fn := slog.New(slog.NewTextHandler(discard{}, nil))
	s = &Supervisor{Logger: explicit, LoggerFn: func() *slog.Logger { return fn }}
	if s.log() != fn {
		t.Fatal("LoggerFn must take precedence over Logger")
	}
	// A LoggerFn that yields nil falls back to Logger.
	s = &Supervisor{Logger: explicit, LoggerFn: func() *slog.Logger { return nil }}
	if s.log() != explicit {
		t.Fatal("a nil from LoggerFn must fall back to Logger")
	}
}

func TestErrTextAndCloseSessionNil(t *testing.T) {
	if got := errText(nil); got != "unknown" {
		t.Fatalf("errText(nil) = %q, want \"unknown\"", got)
	}
	if got := errText(errors.New("boom")); got != "boom" {
		t.Fatalf("errText = %q, want \"boom\"", got)
	}
	// closeSession must tolerate nil — the supervisor calls it on paths where
	// the session was never built.
	(&Supervisor{}).closeSession(nil)
}

// Cancelling while the supervisor is backing off is an orderly stop, not a
// failure: Run returns nil rather than a connection error.
func TestSupervisorCancelDuringBackoff(t *testing.T) {
	first := make(chan *Session, 1)
	calls := 0
	sup := &Supervisor{
		Clock:   clock.System(),
		Backoff: Backoff{Initial: time.Hour, Max: time.Hour}, // long enough to sit in
		Dial: func(context.Context) (*Session, error) {
			calls++
			if calls == 1 {
				s := newTestSession()
				first <- s
				return s, nil
			}
			return nil, errors.New("still down")
		},
		Setup: func(context.Context, *Session) error { return nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- sup.Run(ctx) }()

	killTestSession(<-first, transport.ErrConnectionLost)
	time.Sleep(50 * time.Millisecond) // let it enter the backoff sleep
	cancel()

	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run = %v, want nil when cancelled during backoff", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel during backoff")
	}
}

// The prober's tick path: a successful Ping keeps the session alive, and a
// Ping that cannot be written is terminal — the socket is gone, and the
// watcher must be told immediately rather than waiting out the idle deadline.
func TestKeepAliveLoopPingSuccessThenWriteFailure(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		for {
			if _, err := fc.readClientFrame(); err != nil {
				return
			}
		}
	})
	p, sess := dialFake(t, fs)

	// Replace the default system-clock prober with a fake-clock one.
	sess.stopKeepAlive()
	fake := clock.NewFake(time.Time{})
	sess.startKeepAlive(time.Minute, fake)
	waitForFakeTicker(t, fake)

	// Tick once: the Ping goes out and the session stays alive.
	fake.Advance(time.Minute)
	select {
	case <-sess.Done():
		t.Fatalf("session died on a successful keepalive ping: %v", sess.Err())
	case <-time.After(200 * time.Millisecond):
	}

	// Kill the socket, then tick again: the write fails and the session is
	// marked lost with the typed sentinel.
	_ = p.Disconnect()
	for i := 0; i < 50; i++ {
		fake.Advance(time.Minute)
		select {
		case <-sess.Done():
			if err := sess.Err(); !errors.Is(err, transport.ErrConnectionLost) {
				t.Fatalf("Err = %v, want it to wrap transport.ErrConnectionLost", err)
			}
			return
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatal("a failing keepalive ping never marked the session lost")
}

// Session death racing context cancellation is an orderly stop: the operator
// asked to stop, so Run must not report it as a connection failure.
func TestSupervisorSessionDeathRacingCancel(t *testing.T) {
	first := make(chan *Session, 1)
	sup := &Supervisor{
		Clock:   clock.System(),
		Backoff: Backoff{Initial: time.Millisecond, Max: time.Millisecond},
		Dial: func(context.Context) (*Session, error) {
			s := newTestSession()
			select {
			case first <- s:
			default:
			}
			return s, nil
		},
		Setup: func(context.Context, *Session) error { return nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- sup.Run(ctx) }()

	s := <-first
	cancel()                                    // operator stops...
	killTestSession(s, errors.New("link died")) // ...as the link drops

	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run = %v, want nil when cancellation races session death", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
}

// Cancelling while a reconnect ATTEMPT is in flight (not while sleeping) must
// also end cleanly rather than surfacing the dial error.
func TestSupervisorCancelDuringReconnectAttempt(t *testing.T) {
	first := make(chan *Session, 1)
	var cancel context.CancelFunc
	calls := 0

	sup := &Supervisor{
		Clock:   clock.System(),
		Backoff: Backoff{Initial: time.Millisecond, Max: time.Millisecond},
		Setup:   func(context.Context, *Session) error { return nil },
	}
	sup.Dial = func(context.Context) (*Session, error) {
		calls++
		if calls == 1 {
			s := newTestSession()
			first <- s
			return s, nil
		}
		// Cancel from inside the dial, then fail: Run must treat the
		// cancellation as the reason, not the dial error.
		cancel()
		return nil, errors.New("dial failed while cancelling")
	}

	var ctx context.Context
	ctx, cancel = context.WithCancel(context.Background())
	// Dial cancels on its second call, but the failure paths below return
	// without reaching it — release the context on every path.
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- sup.Run(ctx) }()

	killTestSession(<-first, transport.ErrConnectionLost)
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run = %v, want nil when cancelled mid-attempt", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
}

func waitForFakeTicker(t *testing.T, f *clock.Fake) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if f.Waiters() > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("prober never armed its ticker")
}

// The ping-write failure arm, isolated. Disconnecting the plugin stops the
// prober before it can fail, so this builds a session around a socket that is
// already dead and drives one tick — no readLoop to race the outcome.
func TestKeepAliveLoopMarksLostWhenPingWriteFails(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		for {
			if _, err := fc.readClientFrame(); err != nil {
				return
			}
		}
	})
	ctx, cancel := ctx2s(t)
	defer cancel()

	url := fmt.Sprintf("ws://%s:%d/", fs.host(), fs.port())
	conn, err := ws.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	s := &Session{
		logger:     slog.New(slog.NewTextHandler(discard{}, nil)),
		conn:       conn,
		host:       fs.host(),
		compliance: &Profile{},
		pending:    map[string]chan *codec.Frame{},
		stopRX:     make(chan struct{}),
		done:       make(chan struct{}),
	}
	// Kill the socket. No readLoop is running, so nothing else can mark the
	// session lost — the next ping write is the only possible cause.
	_ = conn.Close(1000, "test")

	fake := clock.NewFake(time.Time{})
	s.startKeepAlive(time.Minute, fake)
	waitForFakeTicker(t, fake)
	fake.Advance(time.Minute)

	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("a failing keepalive ping did not mark the session lost")
	}
	if err := s.Err(); !errors.Is(err, transport.ErrConnectionLost) {
		t.Fatalf("Err = %v, want it to wrap transport.ErrConnectionLost", err)
	}
	if n := s.compliance.Counts()["cerebrum_keepalive_failed"]; n != 1 {
		t.Errorf("cerebrum_keepalive_failed fired %d times, want 1", n)
	}
}

// A Supervisor with no Clock must fall back to the system clock rather than
// nil-panicking on the first backoff.
func TestSupervisorNilClockDefaults(t *testing.T) {
	sup := &Supervisor{
		Dial:  func(context.Context) (*Session, error) { return nil, errors.New("nope") },
		Setup: func(context.Context, *Session) error { return nil },
	}
	if err := sup.Run(context.Background()); err == nil {
		t.Fatal("Run returned nil despite a failing initial dial")
	}
}

// The optional callbacks are how the CLI explains a gap in the output: OnLost
// fires the moment the link drops, OnReconnected when rows resume. A silent
// gap was the operator's original complaint, so these must actually fire.
func TestSupervisorCallbacksFire(t *testing.T) {
	var (
		mu            sync.Mutex
		lostErr       error
		lostCalls     int
		reconnCalls   int
		reconnAttempt int
	)
	sessions := make(chan *Session, 4)
	sup := &Supervisor{
		Clock:   clock.System(),
		Backoff: Backoff{Initial: time.Millisecond, Max: 2 * time.Millisecond},
		Dial: func(context.Context) (*Session, error) {
			s := newTestSession()
			sessions <- s
			return s, nil
		},
		Setup: func(context.Context, *Session) error { return nil },
		OnLost: func(err error) {
			mu.Lock()
			defer mu.Unlock()
			lostCalls++
			lostErr = err
		},
		OnReconnected: func(attempt int, _ time.Duration) {
			mu.Lock()
			defer mu.Unlock()
			reconnCalls++
			reconnAttempt = attempt
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- sup.Run(ctx) }()

	cause := fmt.Errorf("%w: peer vanished", transport.ErrConnectionLost)
	killTestSession(<-sessions, cause) // first session dies
	<-sessions                         // second session established

	cancel()
	select {
	case <-runErr:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}

	mu.Lock()
	defer mu.Unlock()
	if lostCalls != 1 {
		t.Errorf("OnLost fired %d times, want 1", lostCalls)
	}
	if !errors.Is(lostErr, transport.ErrConnectionLost) {
		t.Errorf("OnLost got %v, want the typed cause", lostErr)
	}
	if reconnCalls != 1 {
		t.Errorf("OnReconnected fired %d times, want 1", reconnCalls)
	}
	if reconnAttempt != 1 {
		t.Errorf("OnReconnected attempt = %d, want 1", reconnAttempt)
	}
}

// errOnlyCtx is cancelled as far as Err() is concerned, but its Done() channel
// never becomes ready. That forces Run's select to take the session-death arm
// and THEN observe the cancellation — the ordering that decides whether a
// clean Ctrl-C is misreported as a connection failure. With a real context the
// two cases are both ready and select picks at random, so this path cannot be
// covered deterministically any other way.
type errOnlyCtx struct{ context.Context }

func (errOnlyCtx) Done() <-chan struct{} { return nil }
func (errOnlyCtx) Err() error            { return context.Canceled }

func TestSupervisorCancelObservedAfterSessionDeath(t *testing.T) {
	first := make(chan *Session, 1)
	sup := &Supervisor{
		Clock: clock.System(),
		Dial: func(context.Context) (*Session, error) {
			s := newTestSession()
			first <- s
			return s, nil
		},
		Setup: func(context.Context, *Session) error { return nil },
		OnLost: func(error) {
			t.Error("OnLost must not fire when the operator already cancelled")
		},
	}

	runErr := make(chan error, 1)
	go func() { runErr <- sup.Run(errOnlyCtx{context.Background()}) }()

	killTestSession(<-first, errors.New("link died"))

	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run = %v, want nil — a cancelled operator stop is not a failure", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
}
