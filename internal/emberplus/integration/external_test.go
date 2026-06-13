//go:build integration

// External-oracle integration for Ember+: drives the dhs consumer against a
// real vendor Ember+ provider (TinyEmberPlus / TinyEmberPlusRouter, the Lawo
// reference tools — NOT our own provider), per ADR-0025 deliverable-3's tier-3
// oracle. The manifest_serve_test.go loopback test is the tier-4 regression net.
//
// Gated on EMBERPLUS_TEST_HOST (per-protocol env var, root CLAUDE.md). Skips
// when unset — the vendor oracle is not always present. TinyEmber+ speaks
// S101/TCP (9000 plain provider, 9092 router/matrix).
//
//	EMBERPLUS_TEST_HOST=10.6.239.113 EMBERPLUS_TEST_PORT=9000 \
//	  go test -tags integration ./internal/emberplus/integration/ -run External -v
//	# matrix surface:
//	EMBERPLUS_TEST_HOST=10.6.239.113 EMBERPLUS_TEST_PORT=9092 go test ... -run External
//
// Assertions are STRUCTURAL — they prove our S101 + BER + glow decoder reads a
// real vendor tree (a populated tree with at least one scalar-typed parameter),
// independent of which tree the tool happens to host.
package emberplus_integration

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// emberTarget returns the host+port of the live vendor oracle, or skips.
func emberTarget(t *testing.T) (host, port string) {
	t.Helper()
	host = os.Getenv("EMBERPLUS_TEST_HOST")
	if host == "" {
		t.Skip("EMBERPLUS_TEST_HOST not set — skipping live TinyEmber+ oracle")
	}
	port = os.Getenv("EMBERPLUS_TEST_PORT")
	if port == "" {
		port = "9000" // TinyEmberPlus default provider port
	}
	return host, port
}

// hasScalarParam reports whether the walk output decoded at least one
// scalar-typed Ember+ parameter (proving glow value decoding, not just the
// node skeleton). Node containers render as "raw"; real parameters carry a
// glow value type.
func hasScalarParam(text string) bool {
	for _, ty := range []string{" string ", " bool ", " int ", " real ", " enum "} {
		if strings.Contains(text, ty) {
			return true
		}
	}
	return false
}

// TestExternalEmberInfo asserts the consumer reports a present, online device
// from the live vendor provider over S101/TCP.
func TestExternalEmberInfo(t *testing.T) {
	host, port := emberTarget(t)
	bin := buildDHS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin,
		"consumer", "emberplus", "info", host,
		"--port", port,
		"--timeout", "15s",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("consumer info vs vendor %s:%s failed: %v\n%s", host, port, err, out)
	}
	text := string(out)
	if !strings.Contains(text, "status=present") {
		t.Fatalf("info reports no present slot from live vendor provider:\n%s", text)
	}
	t.Logf("vendor Ember+ info: present\n%s", text)
}

// TestExternalEmberWalk asserts the consumer walks a substantial real tree and
// decodes at least one scalar parameter.
func TestExternalEmberWalk(t *testing.T) {
	host, port := emberTarget(t)
	bin := buildDHS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin,
		"consumer", "emberplus", "walk", host,
		"--port", port,
		"--timeout", "45s",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("consumer walk vs vendor %s:%s failed: %v\n%s", host, port, err, out)
	}
	text := string(out)

	count := parseObjectCount(t, text)
	if count <= 0 {
		t.Fatalf("walk returned no objects from live vendor provider:\n%s", firstN(text, 1200))
	}
	if !hasScalarParam(text) {
		t.Fatalf("walk decoded no scalar-typed parameter from live vendor provider "+
			"(only node skeleton?) — glow value decoding suspect:\n%s", firstN(text, 1200))
	}
	t.Logf("vendor Ember+ walk on %s:%s: %d objects, scalar parameters decoded", host, port, count)
}

// firstN returns up to n bytes of s for compact failure output.
func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
