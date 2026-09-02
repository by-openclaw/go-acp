package main

// The #855 definition-of-done eviction pair, end to end and
// in-process: a real dhs Registry (GC armed) + a real registration
// client. A cadence LONGER than the registry's heartbeat timeout gets
// the node evicted; a cadence comfortably inside it keeps the node
// registered across several GC windows. cmd/dhs is Layer 4 — the one
// package allowed to import both sides.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	stdhttp "net/http"
	"os"
	"testing"
	"time"

	"dhs/internal/amwa/provider"
	registryslot "dhs/internal/registry"

	_ "dhs/internal/amwa/registry" // registry plugin registration
)

// freeTCPPort asks the OS for a free port and releases it for reuse.
func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// queryNodeCount reads the Query API's /nodes listing.
func queryNodeCount(t *testing.T, base string) (int, error) {
	t.Helper()
	resp, err := stdhttp.Get(base + "/x-nmos/query/v1.3/nodes?paging.limit=100")
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}
	var list []json.RawMessage
	if err := json.Unmarshal(body, &list); err != nil {
		return 0, err
	}
	return len(list), nil
}

func TestHeartbeatCadenceVsRegistryGC(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	bundle, err := provider.LoadNodeConfigFromFile("../../tests/integration/nmos/amwa/amwa-test-node.json")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	run := func(t *testing.T, cadence time.Duration, wantEvicted bool) {
		t.Helper()
		port := freeTCPPort(t)
		base := fmt.Sprintf("http://127.0.0.1:%d", port)

		f, ok := registryslot.Lookup("nmos")
		if !ok {
			t.Fatal("nmos registry plugin not registered")
		}
		regCtx, regCancel := context.WithCancel(context.Background())
		defer regCancel()
		go func() {
			_ = f.New(logger).Serve(regCtx, registryslot.ServeOptions{
				BindAddrs:        []string{fmt.Sprintf("127.0.0.1:%d", port)},
				DiscoveryMode:    "static",
				GCInterval:       100 * time.Millisecond,
				HeartbeatTimeout: 1 * time.Second,
			})
		}()
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := queryNodeCount(t, base); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("registry never came up")
			}
			time.Sleep(50 * time.Millisecond)
		}

		rc := provider.NewRegistrationClient(logger, base, "v1.3", bundle)
		rc.SetDefaultHeartbeatInterval(cadence)
		nodeCtx, nodeCancel := context.WithCancel(context.Background())
		defer nodeCancel()
		go rc.Run(nodeCtx)

		// Wait for the initial registration to land.
		deadline = time.Now().Add(5 * time.Second)
		for {
			if n, err := queryNodeCount(t, base); err == nil && n == 1 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("node never registered")
			}
			time.Sleep(50 * time.Millisecond)
		}

		if wantEvicted {
			// Cadence 3 s vs timeout 1 s: the GC must purge the node
			// well before the second heartbeat is even due.
			deadline = time.Now().Add(2500 * time.Millisecond)
			for {
				n, err := queryNodeCount(t, base)
				if err == nil && n == 0 {
					return // evicted, as the spec demands
				}
				if time.Now().After(deadline) {
					t.Fatalf("node with cadence %v never evicted by a 1s-timeout registry (nodes=%d)", cadence, n)
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
		// Cadence 300 ms vs timeout 1 s: the node must survive several
		// GC windows. (The client re-registers on a heartbeat 404, so
		// an eviction would also show as a count flap — check steadily.)
		for end := time.Now().Add(2500 * time.Millisecond); time.Now().Before(end); {
			n, err := queryNodeCount(t, base)
			if err != nil || n != 1 {
				t.Fatalf("node with cadence %v dropped out of a 1s-timeout registry (nodes=%d, err=%v)", cadence, n, err)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}

	t.Run("cadence beyond the timeout is evicted", func(t *testing.T) {
		run(t, 3*time.Second, true)
	})
	t.Run("cadence inside the timeout survives", func(t *testing.T) {
		run(t, 300*time.Millisecond, false)
	})
}
