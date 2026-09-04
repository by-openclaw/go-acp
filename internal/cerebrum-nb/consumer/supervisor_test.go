package cerebrumnb

// Supervisor tests: the RECOVERY half of the 24/7 fix.
//
// keepalive_test.go proves a dead session is detected. These prove something
// acts on that detection — reconnecting and, critically, re-subscribing, since
// a Cerebrum subscription lives on the server and does not survive the socket.

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"dhs/internal/cerebrum-nb/codec"
	"dhs/internal/clock"
	"dhs/internal/transport"
)

// newTestSession builds a Session that is not attached to a socket. Enough for
// the supervisor, which only ever observes Done() and calls close().
func newTestSession() *Session {
	return &Session{
		logger:     slog.New(slog.NewTextHandler(discard{}, nil)),
		compliance: &Profile{},
		pending:    map[string]chan *codec.Frame{},
		stopRX:     make(chan struct{}),
		done:       make(chan struct{}),
	}
}

// killTestSession simulates the link dying with a typed reason.
func killTestSession(s *Session, reason error) { s.markLost(reason) }

func TestBackoffNextDoublesAndClamps(t *testing.T) {
	bo := Backoff{Initial: time.Second, Max: 30 * time.Second}.withDefaults()
	got := []time.Duration{}
	d := bo.Initial
	for i := 0; i < 8; i++ {
		got = append(got, d)
		d = bo.next(d)
	}
	want := []time.Duration{
		1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
		16 * time.Second, 30 * time.Second, 30 * time.Second, 30 * time.Second,
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("attempt %d delay = %v, want %v", i+1, got[i], want[i])
		}
	}
}

func TestBackoffDefaults(t *testing.T) {
	bo := Backoff{}.withDefaults()
	if bo.Initial != defaultBackoffInitial || bo.Max != defaultBackoffMax {
		t.Fatalf("defaults = %v/%v, want %v/%v",
			bo.Initial, bo.Max, defaultBackoffInitial, defaultBackoffMax)
	}
	// A Max below Initial is nonsense; it must not produce a shrinking schedule.
	bo2 := Backoff{Initial: 10 * time.Second, Max: time.Second}.withDefaults()
	if bo2.Max < bo2.Initial {
		t.Fatalf("Max %v < Initial %v", bo2.Max, bo2.Initial)
	}
}

// The headline: when the link dies, the supervisor reconnects AND re-runs
// Setup, so subscriptions are re-established on the new session.
func TestSupervisorReconnectsAndResubscribes(t *testing.T) {
	var mu sync.Mutex
	var dials, setups int
	sessions := make(chan *Session, 4)

	sup := &Supervisor{
		Clock:   clock.System(),
		Backoff: Backoff{Initial: time.Millisecond, Max: 2 * time.Millisecond},
		Dial: func(context.Context) (*Session, error) {
			mu.Lock()
			dials++
			mu.Unlock()
			s := newTestSession()
			sessions <- s
			return s, nil
		},
		Setup: func(context.Context, *Session) error {
			mu.Lock()
			setups++
			mu.Unlock()
			return nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- sup.Run(ctx) }()

	// First session, then kill it twice, expecting a fresh one each time.
	for i := 0; i < 3; i++ {
		var s *Session
		select {
		case s = <-sessions:
		case <-time.After(5 * time.Second):
			t.Fatalf("no session on round %d — supervisor did not reconnect", i+1)
		}
		if i < 2 {
			killTestSession(s, transport.ErrConnectionLost)
		}
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned %v, want nil on ctx cancel", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}

	mu.Lock()
	defer mu.Unlock()
	if dials < 3 {
		t.Errorf("dials = %d, want >= 3 (initial + 2 reconnects)", dials)
	}
	// The whole point: Setup runs on EVERY session, or the watcher comes
	// back connected but subscribed to nothing.
	if setups != dials {
		t.Errorf("setups = %d but dials = %d — every reconnect must re-subscribe", setups, dials)
	}
}

// A failure on the very first connect is the operator's problem and must be
// returned immediately, not hidden behind an infinite retry loop.
func TestSupervisorInitialConnectFailureIsReturned(t *testing.T) {
	boom := errors.New("dial refused")
	sup := &Supervisor{
		Clock: clock.System(),
		Dial:  func(context.Context) (*Session, error) { return nil, boom },
		Setup: func(context.Context, *Session) error { return nil },
	}
	err := sup.Run(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("Run = %v, want the dial error %v", err, boom)
	}
}

// A finite attempt budget must be honoured and reported as a typed
// connection-lost error so the CLI exits with the contract's runtime code.
func TestSupervisorGivesUpAfterMaxAttempts(t *testing.T) {
	var mu sync.Mutex
	dials := 0
	first := make(chan *Session, 1)

	sup := &Supervisor{
		Clock:   clock.System(),
		Backoff: Backoff{Initial: time.Millisecond, Max: time.Millisecond, MaxAttempts: 3},
		Dial: func(context.Context) (*Session, error) {
			mu.Lock()
			dials++
			n := dials
			mu.Unlock()
			if n == 1 {
				s := newTestSession()
				first <- s
				return s, nil
			}
			return nil, errors.New("still down")
		},
		Setup: func(context.Context, *Session) error { return nil },
	}

	runErr := make(chan error, 1)
	go func() { runErr <- sup.Run(context.Background()) }()

	killTestSession(<-first, transport.ErrConnectionLost)

	select {
	case err := <-runErr:
		if !errors.Is(err, transport.ErrConnectionLost) {
			t.Fatalf("Run = %v, want it to wrap transport.ErrConnectionLost", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not give up within the attempt budget")
	}

	mu.Lock()
	defer mu.Unlock()
	if dials != 4 { // 1 initial + 3 attempts
		t.Errorf("dials = %d, want 4 (initial + MaxAttempts=3)", dials)
	}
}

// Cancelling during an active watch is an orderly stop, never an error —
// otherwise Ctrl-C would exit non-zero and look like a fault.
func TestSupervisorCtxCancelIsOrderly(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	sup := &Supervisor{
		Clock: clock.System(),
		Dial: func(context.Context) (*Session, error) {
			s := newTestSession()
			once.Do(func() { close(started) })
			return s, nil
		},
		Setup: func(context.Context, *Session) error { return nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- sup.Run(ctx) }()

	<-started
	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run = %v, want nil on cancel", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// A Setup failure must not leak the half-built session it was handed.
func TestSupervisorClosesSessionWhenSetupFails(t *testing.T) {
	setupErr := errors.New("subscribe refused")
	var built *Session
	sup := &Supervisor{
		Clock: clock.System(),
		Dial: func(context.Context) (*Session, error) {
			built = newTestSession()
			return built, nil
		},
		Setup: func(context.Context, *Session) error { return setupErr },
	}
	if err := sup.Run(context.Background()); !errors.Is(err, setupErr) {
		t.Fatalf("Run = %v, want %v", err, setupErr)
	}
	select {
	case <-built.Done():
	default:
		t.Fatal("a session whose Setup failed was left open")
	}
}

func TestSupervisorRequiresDialAndSetup(t *testing.T) {
	if err := (&Supervisor{}).Run(context.Background()); err == nil {
		t.Fatal("Run with no Dial/Setup must error rather than nil-panic")
	}
}
