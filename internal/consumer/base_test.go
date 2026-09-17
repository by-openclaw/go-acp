package consumer

import (
	"context"
	"sync"
	"testing"
	"time"

	"dhs/internal/metrics"
	"dhs/internal/plugin"
	"dhs/internal/transport"
)

// The zero value has to work. This repo builds connectors as bare struct
// literals in well over a hundred tests, and an embedded type that needed a
// constructor would be a nil field in every one of them — surfacing as a
// panic at the first accessor rather than at the missing assignment.
func TestZeroBaseIsUsable(t *testing.T) {
	var b Base

	if b.ComplianceProfile() == nil {
		t.Error("ComplianceProfile must never be nil")
	}
	if b.Metrics() == nil {
		t.Error("Metrics must never be nil")
	}
	if b.Recorder() != nil {
		t.Error("Recorder starts absent")
	}
	if got := b.SessionHealth(context.Background()); got.Connected || got.Live {
		t.Errorf("nothing is open on a zero Base, got %+v", got)
	}
}

// Init is the one call a factory makes in place of the four assignments it
// replaces, and the stale window is the only part a connector really decides.
func TestInitWiresTheDependencySet(t *testing.T) {
	met := metrics.NewConnector()
	var b Base
	b.Init(plugin.Deps{Metrics: met}, 5*time.Second)

	if b.Metrics() != met {
		t.Error("Init must keep the supplied Connector, not replace it")
	}
	if got := b.SessionHealth(context.Background()); got.StaleAfter != 5*time.Second {
		t.Errorf("StaleAfter = %v, want the protocol's own 5s", got.StaleAfter)
	}
}

// A connector that names no window takes the shared default rather than
// zero, which would make every session instantly stale.
func TestInitWithoutAWindowTakesTheDefault(t *testing.T) {
	var b Base
	b.Init(plugin.Deps{}, 0)

	if got := b.SessionHealth(context.Background()); got.StaleAfter != DefaultStaleAfter {
		t.Errorf("StaleAfter = %v, want %v", got.StaleAfter, DefaultStaleAfter)
	}
	if b.Metrics() == nil {
		t.Error("WithDefaults must have filled the Connector")
	}
}

// Init is called from a factory, and a factory may run twice in a process.
// The profile is the one thing that must survive, because a deviation
// already recorded is evidence.
func TestInitKeepsAnExistingProfile(t *testing.T) {
	var b Base
	first := b.ComplianceProfile()
	b.Init(plugin.Deps{}, time.Second)

	if b.ComplianceProfile() != first {
		t.Error("Init replaced a profile that already existed")
	}
}

func TestSetRecorderRoundTrips(t *testing.T) {
	var b Base
	rec := &transport.Recorder{}

	b.SetRecorder(rec)
	if b.Recorder() != rec {
		t.Error("Recorder did not return what SetRecorder was given")
	}

	// Clearing it is how a capture is stopped.
	b.SetRecorder(nil)
	if b.Recorder() != nil {
		t.Error("SetRecorder(nil) must clear the recorder")
	}
}

// Base embeds Health, so a connector embedding Base satisfies HealthChecker
// without naming it. That promotion is the whole point of the composition.
func TestBaseSatisfiesHealthCheckerThroughEmbedding(t *testing.T) {
	type connector struct{ Base }
	var _ HealthChecker = (*connector)(nil)

	c := &connector{}
	c.Init(plugin.Deps{}, time.Minute)
	c.Opened("tcp", "10.0.0.1", 2072, nil)
	c.RecordRx()

	got := c.SessionHealth(context.Background())
	if !got.Connected || !got.Live {
		t.Errorf("the embedded Health must work through Base, got %+v", got)
	}
	if c.Metrics() == nil || c.ComplianceProfile() == nil {
		t.Error("the other three concerns must be promoted too")
	}
}

// The accessors are reached from a read loop, a CLI verb and a metrics
// scrape at the same time; none of them may race.
func TestBaseIsConcurrencySafe(t *testing.T) {
	var b Base
	b.Init(plugin.Deps{}, time.Minute)
	rec := &transport.Recorder{}

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(4)
		go func() { defer wg.Done(); _ = b.Metrics() }()
		go func() { defer wg.Done(); _ = b.ComplianceProfile() }()
		go func() { defer wg.Done(); b.SetRecorder(rec) }()
		go func() { defer wg.Done(); _ = b.Recorder() }()
	}
	wg.Wait()
}

// Base's lock is deliberately not the connector's own: a metrics scrape must
// not wait behind whatever protocol state that mutex guards. Reading through
// Base while the connector's lock is held proves they are independent.
func TestBaseLockIsIndependentOfTheConnectorLock(t *testing.T) {
	type connector struct {
		Base
		mu sync.Mutex
	}
	c := &connector{}
	c.Init(plugin.Deps{}, time.Minute)

	c.mu.Lock()
	defer c.mu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Metrics()
		_ = c.ComplianceProfile()
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Base blocked on the connector's mutex — the locks are not independent")
	}
}
