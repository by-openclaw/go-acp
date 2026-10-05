package provider

// A Registry that does not answer is given up within a heartbeat
// period — when the next beat is due, and what the failover to the
// next advertised Registry is timed against (IS-04-01 test_16_01).

import (
	"context"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRegistryWaitsFollowTheHeartbeatCadence(t *testing.T) {
	cases := []struct {
		name            string
		cadence         time.Duration
		answer, connect time.Duration
	}{
		{"the IS-04 default", 0, 5 * time.Second, 2500 * time.Millisecond},
		{"a slower plant", 10 * time.Second, 10 * time.Second, 5 * time.Second},
		{"a two-second cadence", 2 * time.Second, 2 * time.Second, time.Second},
		{"a sub-second cadence is floored", 200 * time.Millisecond, time.Second, time.Second},
	}
	for _, tc := range cases {
		c := NewRegistrationClient(nil, "http://reg.invalid:8235", "v1.3", validBundle())
		c.SetDefaultHeartbeatInterval(tc.cadence)
		if got := c.answerWait(); got != tc.answer {
			t.Errorf("%s: a heartbeat may go unanswered for %v, want %v", tc.name, got, tc.answer)
		}
		if got := c.connectWait(); got != tc.connect {
			t.Errorf("%s: a connection may take %v, want %v", tc.name, got, tc.connect)
		}
	}

	// The System API's cadence outranks the local default.
	c := NewRegistrationClient(nil, "http://reg.invalid:8235", "v1.3", validBundle())
	c.SetHeartbeatIntervalFn(func() time.Duration { return 3 * time.Second })
	if got := c.answerWait(); got != 3*time.Second {
		t.Errorf("with a System API cadence of 3 s the wait is %v", got)
	}
}

// The Registry takes the heartbeat and says nothing. The Node gives up
// after one heartbeat period — it used to wait ten seconds, by which
// time the next Registry in the chain had been given up on.
func TestHeartbeatGivesUpWithinAHeartbeatPeriod(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	letGo := func() { once.Do(func() { close(release) }) }
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(stdhttp.ResponseWriter, *stdhttp.Request) {
		<-release
	}))
	defer srv.Close()
	defer letGo() // the held request must not keep Close waiting

	c := NewRegistrationClient(nil, srv.URL, "v1.3", validBundle())
	c.SetDefaultHeartbeatInterval(1200 * time.Millisecond)

	started := time.Now()
	err := c.sendHeartbeat(context.Background())
	took := time.Since(started)
	if err == nil {
		t.Fatal("a heartbeat that is never answered must fail")
	}
	if took < time.Second || took > 4*time.Second {
		t.Errorf("gave up after %v, want about the 1.2 s heartbeat period (and nowhere near the old 10 s)", took)
	}
}

// A Node owes its heartbeats from the moment its node resource is
// registered. With a Registry that takes its time over each resource,
// the heartbeats start while the rest is still being registered; and a
// Registry that has lost the Node meanwhile ends the registration.
func TestHeartbeatsStartWhileTheRestIsStillBeingRegistered(t *testing.T) {
	var mu sync.Mutex
	var calls []string // "POST resource" / "POST health"
	lose := false
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		mu.Lock()
		health := strings.Contains(r.URL.Path, "/health/nodes/")
		if health {
			calls = append(calls, "health")
		} else {
			calls = append(calls, "resource")
		}
		gone := lose && health
		mu.Unlock()
		switch {
		case gone:
			w.WriteHeader(stdhttp.StatusNotFound)
		case health:
			_, _ = w.Write([]byte(`{"health":"1"}`))
		default:
			time.Sleep(40 * time.Millisecond) // a Registry that takes its time
			w.WriteHeader(stdhttp.StatusCreated)
		}
	}))
	defer srv.Close()

	// The committed fixture: a node with some thirty resources.
	bundle, err := LoadNodeConfigFromFile(filepath.Join("..", "..", "..", "tests", "integration", "nmos", "amwa", "amwa-test-node.json"))
	if err != nil {
		t.Fatal(err)
	}
	c := NewRegistrationClient(nil, srv.URL, "v1.3", bundle)
	c.SetDefaultHeartbeatInterval(100 * time.Millisecond)
	c.base = srv.URL + "/x-nmos/registration/v1.3"
	if err := c.registerAll(context.Background()); err != nil {
		t.Fatalf("registerAll: %v", err)
	}
	mu.Lock()
	first, last := -1, -1
	for i, call := range calls {
		if call == "health" && first < 0 {
			first = i
		}
		if call == "resource" {
			last = i
		}
	}
	total := len(calls)
	mu.Unlock()
	if first < 0 || first > last {
		t.Fatalf("of %d requests the first heartbeat is number %d and the last resource number %d — no heartbeat went out during the registration", total, first, last)
	}

	// The Registry forgets the Node mid-registration: the heartbeat's
	// 404 ends it, and the caller registers afresh.
	mu.Lock()
	lose = true
	mu.Unlock()
	if err := c.registerAll(context.Background()); !errors.Is(err, ErrRegistryNotFound) {
		t.Errorf("a Registry that lost the Node mid-registration: err = %v, want ErrRegistryNotFound", err)
	}
}
