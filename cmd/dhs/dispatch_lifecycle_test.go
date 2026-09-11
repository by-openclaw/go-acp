package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// The canonical lifecycle verbs (ensure / stop / status) are answered by
// dispatchProducer before any per-protocol verb table, so every producer
// — including the ones with their own dispatcher (osc, tsl, nmos) — exposes
// the same ADR-0007 contract the Ansible role drives. "ensure --state
// absent" on a pidfile that does not exist is already converged (no error,
// changed:false); "stop" on it is a pidfile error, never "unknown verb".
func TestProducerLifecycleVerbsReachEveryProtocol(t *testing.T) {
	for _, proto := range []string{"osc-v10", "osc-v11", "tsl-v31", "tsl-v50", "nmos", "acp1", "probel-sw08p"} {
		t.Run(proto, func(t *testing.T) {
			missing := filepath.Join(t.TempDir(), "absent.pid")
			if err := dispatchProducer(context.Background(), []string{proto, "ensure", "--state", "absent", "--pidfile", missing, "--output", "json"}); err != nil {
				t.Fatalf("%s ensure --state absent on a missing pidfile = %v, want converged (nil)", proto, err)
			}
			err := dispatchProducer(context.Background(), []string{proto, "stop", "--pidfile", missing})
			if err == nil || strings.Contains(err.Error(), "unknown verb") {
				t.Fatalf("%s stop on a missing pidfile = %v, want a pidfile error from the shared stop verb", proto, err)
			}
			if err := dispatchProducer(context.Background(), []string{proto, "ensure", "--state", "absent"}); err == nil || !strings.Contains(err.Error(), "--pidfile") {
				t.Fatalf("%s ensure without --pidfile = %v, want the shared ensure's usage error", proto, err)
			}
		})
	}
}
