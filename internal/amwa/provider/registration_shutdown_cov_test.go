package provider

// Shutdown is the one thing the registration loop must never get
// wrong: a Node that goes away without deregistering leaves the Query
// API advertising senders nobody can route, until the Registry's own
// heartbeat timeout eventually notices.

import (
	"context"
	stdhttp "net/http"
	"testing"
	"time"
)

// A tick and a cancellation can be ready in the same iteration, and
// Go's select picks between ready cases at random — so the loop can
// enter the tick branch with the context already dead. Proceeding
// there would run the heartbeat against a cancelled context, read the
// resulting error as a Registry failure, clear `registered`, and make
// the shutdown deregistration early-return: every DELETE skipped, and
// the Node left in the Registry.
//
// Which branch wins is a coin flip per shutdown, so this runs the
// whole lifecycle enough times that both are exercised, and asserts
// the invariant that must hold either way.
func TestEveryShutdownDeregisters(t *testing.T) {
	for i := 0; i < 40; i++ {
		reg := newTypedRegistry(t)
		c := NewRegistrationClient(newLogTap().logger(), reg.ts.URL, "v1.3", validBundle())
		// A tick far shorter than the loop's work, so the ticker is
		// almost always already ready when the select is re-entered.
		c.SetHeartbeatIntervalFn(func() time.Duration { return time.Millisecond })

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); c.Run(ctx) }()

		waitUntil(t, "the registration", c.registered.Load)
		cancel()
		<-done

		if c.registered.Load() {
			t.Fatalf("run %d ended still registered", i)
		}
		if reg.seen("node") == 0 {
			t.Fatalf("run %d never registered at all", i)
		}
		if reg.deregistrations() == 0 {
			t.Fatalf("run %d went away without deregistering", i)
		}
	}
}

// A Registry that answers the shutdown DELETEs with an error does not
// hang the shutdown: the Node is going away either way, and the
// Registry's own heartbeat timeout will finish the job.
func TestShutdownSurvivesARegistryThatRefusesTheDeletes(t *testing.T) {
	reg := newTypedRegistry(t)
	c := NewRegistrationClient(newLogTap().logger(), reg.ts.URL, "v1.3", fullBundle(t))
	cancel := runClient(t, c)
	waitUntil(t, "the registration", c.registered.Load)

	reg.set(func(r *typedRegistry) { r.health = stdhttp.StatusInternalServerError })
	cancel()

	waitUntil(t, "the shutdown", func() bool { return !c.registered.Load() })
}
