package provider

// A Registry that does not answer is given up within a heartbeat
// period — when the next beat is due, and what the failover to the
// next advertised Registry is timed against (IS-04-01 test_16_01).

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
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
