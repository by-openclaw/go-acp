package consumer

// Tests for the shared Supervisor — the RECOVERY half of the 24/7 contract.
//
// Detection is per-connector and tested there. These prove something ACTS on
// it: reconnect, and re-run Setup, since a subscription that lives on the
// server does not survive the socket.
//
// The session type is a local fake: nothing here should need a protocol.

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/transport"
)

// fakeSess is a session with the two things a supervisor observes: a channel
// that closes on death, and a reason.
type fakeSess struct {
	done   chan struct{}
	mu     sync.Mutex
	err    error
	closed int
}

func newFakeSess() *fakeSess { return &fakeSess{done: make(chan struct{})} }

func (s *fakeSess) kill(reason error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = reason
	}
	s.mu.Unlock()
	select {
	case <-s.done:
	default:
		close(s.done)
	}
}

func (s *fakeSess) reason() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *fakeSess) closeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// wire fills in the three lifecycle functions for a *fakeSess.
func wire(sup *Supervisor[*fakeSess]) *Supervisor[*fakeSess] {
	sup.Done = func(s *fakeSess) <-chan struct{} { return s.done }
	sup.Err = func(s *fakeSess) error { return s.reason() }
	sup.Close = func(s *fakeSess) {
		s.mu.Lock()
		s.closed++
		s.mu.Unlock()
	}
	return sup
}

func fastBackoff(maxAttempts int) Backoff {
	return Backoff{
		Initial:     time.Millisecond,
		Max:         time.Millisecond,
		MaxAttempts: maxAttempts,
	}
}

// --- Backoff --------------------------------------------------------------

func TestBackoffNextDoublesAndClamps(t *testing.T) {
	bo := Backoff{Initial: time.Second, Max: 30 * time.Second}.withDefaults()
	var got []time.Duration
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
	if bo.Initial != DefaultBackoffInitial || bo.Max != DefaultBackoffMax {
		t.Fatalf("defaults = %v/%v, want %v/%v",
			bo.Initial, bo.Max, DefaultBackoffInitial, DefaultBackoffMax)
	}
	// A Max below Initial is nonsense; it must not make a shrinking schedule.
	bo2 := Backoff{Initial: 10 * time.Second, Max: time.Second}.withDefaults()
	if bo2.Max < bo2.Initial {
		t.Fatalf("Max %v < Initial %v", bo2.Max, bo2.Initial)
	}
}

// --- required collaborators ----------------------------------------------

func TestSupervisorRequiresDialSetupAndDone(t *testing.T) {
	tests := []struct {
		name string
		sup  *Supervisor[*fakeSess]
	}{
		{"nothing", &Supervisor[*fakeSess]{}},
		{"no Setup", &Supervisor[*fakeSess]{
			Dial: func(context.Context) (*fakeSess, error) { return newFakeSess(), nil },
		}},
		{"no Done", &Supervisor[*fakeSess]{
			Dial:  func(context.Context) (*fakeSess, error) { return newFakeSess(), nil },
			Setup: func(context.Context, *fakeSess) error { return nil },
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.sup.Run(context.Background())
			if err == nil {
				t.Fatal("Run succeeded with a missing collaborator")
			}
			if !strings.Contains(err.Error(), "required") {
				t.Errorf("err = %v, want it to say what is required", err)
			}
		})
	}
}

// --- the headline ---------------------------------------------------------

// When the link dies the supervisor reconnects AND re-runs Setup, so
// server-side subscriptions are re-established on the new session.
func TestSupervisorReconnectsAndReRunsSetup(t *testing.T) {
	var mu sync.Mutex
	var dials, setups int
	sessions := make(chan *fakeSess, 4)

	reconnected := make(chan int, 1)
	sup := wire(&Supervisor[*fakeSess]{
		Clock:   clock.System(),
		Backoff: fastBackoff(0),
		Dial: func(context.Context) (*fakeSess, error) {
			mu.Lock()
			dials++
			mu.Unlock()
			s := newFakeSess()
			sessions <- s
			return s, nil
		},
		Setup: func(context.Context, *fakeSess) error {
			mu.Lock()
			setups++
			mu.Unlock()
			return nil
		},
		OnReconnected: func(attempt int, _ time.Duration) {
			select {
			case reconnected <- attempt:
			default:
			}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- sup.Run(ctx) }()

	first := <-sessions
	first.kill(transport.ErrConnectionLost)

	select {
	case <-sessions: // the reconnect happened
	case <-time.After(10 * time.Second):
		t.Fatal("no reconnect after the session died")
	}
	select {
	case <-reconnected:
	case <-time.After(10 * time.Second):
		t.Fatal("OnReconnected never fired")
	}

	cancel()
	if err := <-runErr; err != nil {
		t.Fatalf("Run after cancel = %v, want nil", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if dials < 2 || setups < 2 {
		t.Errorf("dials=%d setups=%d, want at least 2 of each", dials, setups)
	}
	if setups != dials {
		t.Errorf("Setup ran %d times for %d dials — it must run on every one",
			setups, dials)
	}
}

// OnLost carries the typed reason, so the gap is explained at the moment it
// starts rather than after the backoff.
func TestSupervisorOnLostCarriesTheReason(t *testing.T) {
	lost := make(chan error, 1)
	sessions := make(chan *fakeSess, 2)

	sup := wire(&Supervisor[*fakeSess]{
		Clock:   clock.System(),
		Backoff: fastBackoff(0),
		Dial: func(context.Context) (*fakeSess, error) {
			s := newFakeSess()
			sessions <- s
			return s, nil
		},
		Setup:  func(context.Context, *fakeSess) error { return nil },
		OnLost: func(err error) { lost <- err },
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = sup.Run(ctx) }()

	(<-sessions).kill(transport.ErrIdleTimeout)
	select {
	case err := <-lost:
		if !errors.Is(err, transport.ErrIdleTimeout) {
			t.Errorf("OnLost got %v, want the idle-timeout reason", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("OnLost never fired")
	}
}

// A connector with no Err function reports the reason as unknown rather than
// panicking — several clients record no cause.
func TestSupervisorToleratesNoErrFunc(t *testing.T) {
	sessions := make(chan *fakeSess, 2)
	lost := make(chan error, 1)

	sup := &Supervisor[*fakeSess]{
		Clock:   clock.System(),
		Backoff: fastBackoff(0),
		Dial: func(context.Context) (*fakeSess, error) {
			s := newFakeSess()
			sessions <- s
			return s, nil
		},
		Setup:  func(context.Context, *fakeSess) error { return nil },
		Done:   func(s *fakeSess) <-chan struct{} { return s.done },
		OnLost: func(err error) { lost <- err },
		// Err and Close deliberately nil.
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = sup.Run(ctx) }()

	(<-sessions).kill(errors.New("ignored"))
	select {
	case err := <-lost:
		if err != nil {
			t.Errorf("OnLost got %v, want nil when the connector records no reason", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("OnLost never fired")
	}
}

// --- failure paths --------------------------------------------------------

// The initial connect is the operator's problem to see immediately — a bad
// host or password must not disappear behind a backoff.
func TestSupervisorInitialConnectFailureIsReturned(t *testing.T) {
	want := errors.New("bad password")
	sup := wire(&Supervisor[*fakeSess]{
		Dial:  func(context.Context) (*fakeSess, error) { return nil, want },
		Setup: func(context.Context, *fakeSess) error { return nil },
	})
	if err := sup.Run(context.Background()); !errors.Is(err, want) {
		t.Fatalf("Run = %v, want %v", err, want)
	}
}

// A finite attempt budget is honoured and reported as a typed
// connection-lost error, so a CLI exits with the contract's runtime code.
func TestSupervisorGivesUpAfterMaxAttempts(t *testing.T) {
	var mu sync.Mutex
	dials := 0
	first := make(chan *fakeSess, 1)

	sup := wire(&Supervisor[*fakeSess]{
		Clock:   clock.System(),
		Backoff: fastBackoff(3),
		Dial: func(context.Context) (*fakeSess, error) {
			mu.Lock()
			dials++
			n := dials
			mu.Unlock()
			if n == 1 {
				s := newFakeSess()
				first <- s
				return s, nil
			}
			return nil, errors.New("still down")
		},
		Setup: func(context.Context, *fakeSess) error { return nil },
	})

	runErr := make(chan error, 1)
	go func() { runErr <- sup.Run(context.Background()) }()

	(<-first).kill(transport.ErrConnectionLost)

	select {
	case err := <-runErr:
		if !errors.Is(err, transport.ErrConnectionLost) {
			t.Fatalf("Run = %v, want it to wrap transport.ErrConnectionLost", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Run did not give up within the attempt budget")
	}

	mu.Lock()
	defer mu.Unlock()
	if dials != 4 { // 1 initial + 3 attempts
		t.Errorf("dials = %d, want 4 (initial + MaxAttempts=3)", dials)
	}
}

// A Setup failure closes the fresh session, so a half-built one is never
// handed back or leaked.
func TestSupervisorClosesSessionWhenSetupFails(t *testing.T) {
	sess := newFakeSess()
	sup := wire(&Supervisor[*fakeSess]{
		Dial:  func(context.Context) (*fakeSess, error) { return sess, nil },
		Setup: func(context.Context, *fakeSess) error { return errors.New("subscribe refused") },
	})
	if err := sup.Run(context.Background()); err == nil {
		t.Fatal("Run succeeded with a failing Setup")
	}
	if sess.closeCount() != 1 {
		t.Errorf("session closed %d times, want 1", sess.closeCount())
	}
}

// --- cancellation ---------------------------------------------------------

// Cancelling a healthy session is an orderly stop, not a failure.
func TestSupervisorCtxCancelIsOrderly(t *testing.T) {
	sess := newFakeSess()
	sup := wire(&Supervisor[*fakeSess]{
		Dial:  func(context.Context) (*fakeSess, error) { return sess, nil },
		Setup: func(context.Context, *fakeSess) error { return nil },
	})

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- sup.Run(ctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run after cancel = %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if sess.closeCount() == 0 {
		t.Error("cancel did not close the session")
	}
}

// racedCtx models the one window this test is about: the operator cancels
// between the select observing the session's death and the check that decides
// whether it was death or a clean stop. Done never becomes ready — so the
// select can only take the death branch — while Err already reports cancelled.
//
// Driving that with a real context and two goroutines makes the select a coin
// toss between two ready channels, which is a flaky test AND flaky coverage.
type racedCtx struct{ context.Context }

func (racedCtx) Done() <-chan struct{} { return nil }
func (racedCtx) Err() error            { return context.Canceled }

// Closing a session marks it lost too, so a stop that races the death signal
// must still be reported as orderly rather than as a connection failure.
func TestSupervisorCancelObservedAfterSessionDeath(t *testing.T) {
	sess := newFakeSess()
	sess.kill(transport.ErrConnectionLost)

	lost := false
	sup := wire(&Supervisor[*fakeSess]{
		Clock:   clock.System(),
		Backoff: fastBackoff(0),
		Dial:    func(context.Context) (*fakeSess, error) { return sess, nil },
		Setup:   func(context.Context, *fakeSess) error { return nil },
		OnLost:  func(error) { lost = true },
	})

	if err := sup.Run(racedCtx{context.Background()}); err != nil {
		t.Fatalf("Run = %v, want nil — cancel makes this an orderly stop", err)
	}
	if lost {
		t.Error("a cancelled run reported the session as lost")
	}
	if sess.closeCount() != 1 {
		t.Errorf("session closed %d times, want 1", sess.closeCount())
	}
}

// Cancelling while backing off is an orderly stop: Run returns nil rather
// than a connection error.
func TestSupervisorCancelDuringBackoff(t *testing.T) {
	first := make(chan *fakeSess, 1)
	var mu sync.Mutex
	calls := 0

	sup := wire(&Supervisor[*fakeSess]{
		Clock:   clock.System(),
		Backoff: Backoff{Initial: 5 * time.Second, Max: 5 * time.Second},
		Dial: func(context.Context) (*fakeSess, error) {
			mu.Lock()
			calls++
			n := calls
			mu.Unlock()
			if n == 1 {
				s := newFakeSess()
				first <- s
				return s, nil
			}
			return nil, errors.New("still down")
		},
		Setup: func(context.Context, *fakeSess) error { return nil },
	})

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- sup.Run(ctx) }()

	(<-first).kill(transport.ErrConnectionLost)
	time.Sleep(50 * time.Millisecond) // let it enter the backoff sleep
	cancel()

	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run = %v, want nil after cancel during backoff", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancel during backoff")
	}
}

// Cancelling while a reconnect ATTEMPT is in flight is also orderly — the
// dial fails because the context died, not because the peer is gone.
func TestSupervisorCancelDuringReconnectAttempt(t *testing.T) {
	first := make(chan *fakeSess, 1)
	dialing := make(chan struct{}, 1)
	var mu sync.Mutex
	calls := 0

	ctx, cancel := context.WithCancel(context.Background())
	sup := wire(&Supervisor[*fakeSess]{
		Clock:   clock.System(),
		Backoff: fastBackoff(0),
		Dial: func(c context.Context) (*fakeSess, error) {
			mu.Lock()
			calls++
			n := calls
			mu.Unlock()
			if n == 1 {
				s := newFakeSess()
				first <- s
				return s, nil
			}
			select {
			case dialing <- struct{}{}:
			default:
			}
			<-c.Done()
			return nil, c.Err()
		},
		Setup: func(context.Context, *fakeSess) error { return nil },
	})

	runErr := make(chan error, 1)
	go func() { runErr <- sup.Run(ctx) }()

	(<-first).kill(transport.ErrConnectionLost)
	<-dialing
	cancel()

	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run = %v, want nil after cancel during a reconnect attempt", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
	}
}

// --- small pure helpers ---------------------------------------------------

func TestSupervisorLogFallbacks(t *testing.T) {
	// No logger at all -> a discard logger, never nil.
	s := &Supervisor[*fakeSess]{}
	if s.log() == nil {
		t.Fatal("log() returned nil with no logger configured")
	}
	explicit := slog.New(slog.NewTextHandler(discard{}, nil))
	s = &Supervisor[*fakeSess]{Logger: explicit}
	if s.log() != explicit {
		t.Fatal("log() ignored the Logger field")
	}
	// LoggerFn wins over Logger when it yields one.
	fn := slog.New(slog.NewTextHandler(discard{}, nil))
	s = &Supervisor[*fakeSess]{Logger: explicit, LoggerFn: func() *slog.Logger { return fn }}
	if s.log() != fn {
		t.Fatal("LoggerFn must take precedence over Logger")
	}
	// A LoggerFn that yields nil falls back to Logger.
	s = &Supervisor[*fakeSess]{Logger: explicit, LoggerFn: func() *slog.Logger { return nil }}
	if s.log() != explicit {
		t.Fatal("a nil from LoggerFn must fall back to Logger")
	}
}

func TestErrText(t *testing.T) {
	if got := errText(nil); got != "unknown" {
		t.Fatalf("errText(nil) = %q, want %q", got, "unknown")
	}
	if got := errText(errors.New("boom")); got != "boom" {
		t.Fatalf("errText = %q, want %q", got, "boom")
	}
}

func TestDiscardWriterAcceptsEverything(t *testing.T) {
	n, err := discard{}.Write([]byte("anything"))
	if err != nil || n != len("anything") {
		t.Fatalf("Write = %d, %v", n, err)
	}
}
