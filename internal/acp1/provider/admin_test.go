package acp1

import (
	"context"
	"testing"
	"time"
)

// startAdminServer launches s.ServeAdmin under a unique name and
// returns the client + cancel func.
func startAdminServer(t *testing.T, s *server, name string) *AdminClient {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- s.ServeAdmin(ctx, name)
	}()

	deadline := time.Now().Add(2 * time.Second)
	var c *AdminClient
	for time.Now().Before(deadline) {
		client, err := NewAdminClient(name)
		if err == nil {
			c = client
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if c == nil {
		cancel()
		t.Fatalf("admin server did not become reachable")
	}
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Errorf("admin server did not return within 2s of cancel")
		}
	})
	return c
}

func TestAdminPing(t *testing.T) {
	s := newTestServer(t)
	c := startAdminServer(t, s, "test-ping")

	resp, err := c.Call(context.Background(), &AdminRequest{Verb: "ping"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("status = %q, want ok (msg=%s)", resp.Status, resp.Message)
	}
	if resp.Result == nil {
		t.Fatalf("result missing")
	}
	slots, ok := resp.Result["slots"].(float64)
	if !ok || slots <= 0 {
		t.Fatalf("slots = %v, want positive number", resp.Result["slots"])
	}
}

func TestAdminUnknownVerb(t *testing.T) {
	s := newTestServer(t)
	c := startAdminServer(t, s, "test-unknown")

	resp, err := c.Call(context.Background(), &AdminRequest{Verb: "wat"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Status != "error" {
		t.Fatalf("status = %q, want error", resp.Status)
	}
}

func TestAdminValueGet(t *testing.T) {
	s := newTestServer(t)
	c := startAdminServer(t, s, "test-value-get")

	// slot=1 group=1 (identity) id=0 returns the Card Label string.
	resp, err := c.Call(context.Background(), &AdminRequest{
		Verb: "value.get",
		Args: map[string]any{"slot": 1, "group": 1, "id": 0},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("status = %q (msg=%s)", resp.Status, resp.Message)
	}
}

func TestAdminValueSet(t *testing.T) {
	s := newTestServer(t)
	c := startAdminServer(t, s, "test-value-set")

	resp, err := c.Call(context.Background(), &AdminRequest{
		Verb: "value.set",
		Args: map[string]any{"path": "1.2.2.0", "value": int64(7)},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("status = %q (msg=%s)", resp.Status, resp.Message)
	}
}

func TestAdminSlotState(t *testing.T) {
	s := newTestServer(t)
	c := startAdminServer(t, s, "test-slot-state")

	resp, err := c.Call(context.Background(), &AdminRequest{
		Verb: "slot.state",
		Args: map[string]any{"slot": 1, "state": "error"},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("status = %q (msg=%s)", resp.Status, resp.Message)
	}
}

func TestAdminSlotState_UnknownState(t *testing.T) {
	s := newTestServer(t)
	c := startAdminServer(t, s, "test-slot-bad")

	resp, err := c.Call(context.Background(), &AdminRequest{
		Verb: "slot.state",
		Args: map[string]any{"slot": 1, "state": "wat"},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Status != "error" {
		t.Fatalf("status = %q, want error for unknown state", resp.Status)
	}
}

func TestAdminSlotLoad_NotImplemented(t *testing.T) {
	s := newTestServer(t)
	c := startAdminServer(t, s, "test-slot-load")

	resp, err := c.Call(context.Background(), &AdminRequest{
		Verb: "slot.load",
		Args: map[string]any{"slot": 1, "card": "axon/synapse/RRS18-1601/acp1"},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Status != "error" {
		t.Fatalf("status = %q, want error (#260 not yet wired)", resp.Status)
	}
}

func TestAdminMultiInstance_DistinctDiscoveryFiles(t *testing.T) {
	s1 := newTestServer(t)
	s2 := newTestServer(t)
	c1 := startAdminServer(t, s1, "rackA")
	c2 := startAdminServer(t, s2, "rackB")

	if c1.addr == c2.addr {
		t.Fatalf("two instances share addr %q — discovery file collision", c1.addr)
	}

	r1, err := c1.Call(context.Background(), &AdminRequest{Verb: "ping"})
	if err != nil || r1.Status != "ok" {
		t.Fatalf("rackA ping: %v / %+v", err, r1)
	}
	r2, err := c2.Call(context.Background(), &AdminRequest{Verb: "ping"})
	if err != nil || r2.Status != "ok" {
		t.Fatalf("rackB ping: %v / %+v", err, r2)
	}
}

func TestSanitiseName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"dhs-acp1", "dhs-acp1"},
		{"rack A", "rackA"},
		{"slash/danger", "slashdanger"},
		{"empty:::", "empty"},
		{":::", "dhs-acp1"},
		{"valid.name_v2", "valid.name_v2"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := sanitiseName(tc.in); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
