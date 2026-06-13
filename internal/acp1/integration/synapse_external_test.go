//go:build integration

// External-oracle integration for ACP1: drives the dhs consumer against the
// real Axon **Synapse Simulator** (the vendor tool, NOT our own provider), per
// the ADR-0025 deliverable-3 tier-3 oracle ("vendor emulator + real device,
// never our own provider"). The loopback fixture test in manifest_serve_test.go
// is the tier-4 regression net; this is the ground-truth check.
//
// Gated on ACP1_TEST_HOST (per root CLAUDE.md "integration tests gated on
// per-protocol env vars"). When unset the test skips — the vendor oracle is not
// always present. Synapse speaks ACP1 Mode A (UDP) on 2071.
//
//	ACP1_TEST_HOST=10.6.239.113 ACP1_TEST_PORT=2071 \
//	  go test -tags integration ./internal/acp1/integration/ -run Synapse -v
//
// Assertions are STRUCTURAL — they assert what a real Synapse rack exposes
// (a populated rack, a walkable controller slot with the Control group), not
// our committed fixture's specific cards, because the live device's contents
// differ from and are independent of our repo.
package acp1_integration

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// synapseTarget returns the host+port of the live Synapse oracle, or skips.
func synapseTarget(t *testing.T) (host, port string) {
	t.Helper()
	host = os.Getenv("ACP1_TEST_HOST")
	if host == "" {
		t.Skip("ACP1_TEST_HOST not set — skipping live Synapse Simulator oracle")
	}
	port = os.Getenv("ACP1_TEST_PORT")
	if port == "" {
		port = "2071" // Synapse Simulator default (ACP1 Mode A / UDP)
	}
	return host, port
}

// TestSynapseInfo asserts the consumer reports a populated rack from the live
// Synapse Simulator over UDP.
func TestSynapseInfo(t *testing.T) {
	host, port := synapseTarget(t)
	bin := buildDHS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin,
		"consumer", "acp1", "info", host,
		"--transport", "udp",
		"--port", port,
		"--timeout", "15s",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("consumer info vs Synapse %s:%s failed: %v\n%s", host, port, err, out)
	}
	text := string(out)

	// A real Synapse rack reports a slot count and at least one present card.
	if !strings.Contains(text, "slots") {
		t.Fatalf("info missing slot-count header from live Synapse:\n%s", text)
	}
	if !strings.Contains(text, "status=present") {
		t.Fatalf("info reports no present slot from live Synapse:\n%s", text)
	}
	t.Logf("Synapse info: populated rack confirmed\n%s", firstLines(text, 6))
}

// TestSynapseWalk walks the rack-controller slot 0 of the live Synapse and
// asserts a non-trivial object set with the Control group present.
func TestSynapseWalk(t *testing.T) {
	host, port := synapseTarget(t)
	bin := buildDHS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin,
		"consumer", "acp1", "walk", host,
		"--transport", "udp",
		"--port", port,
		"--slot", "0",
		"--timeout", "20s",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("consumer walk slot 0 vs Synapse %s:%s failed: %v\n%s", host, port, err, out)
	}
	text := string(out)

	count := parseObjectCount(t, text)
	if count <= 0 {
		t.Fatalf("walk slot 0 returned no objects from live Synapse:\n%s", text)
	}
	// The rack controller always exposes the Control group (network/broadcast
	// config). Assert the group header so a degenerate single-object reply fails.
	if !strings.Contains(text, "[control]") {
		t.Fatalf("walk slot 0 missing [control] group from live Synapse:\n%s", text)
	}
	t.Logf("Synapse walk slot 0: %d objects, Control group present", count)
}

// firstLines returns up to n lines of s, for compact failure/log output.
func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
