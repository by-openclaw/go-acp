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
	"encoding/json"
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

// ensureResult is the ADR-0007 JSON the matrix verb emits under --output json.
// Only the outcome flags matter here; Changed/WouldChange are pointers so an
// absent field stays nil rather than defaulting to false.
type ensureResult struct {
	Changed     *bool `json:"changed"`
	WouldChange *bool `json:"would_change"`
}

// runMatrixJSON runs `consumer emberplus matrix ... --output json` and decodes
// the ADR-0007 result. Skips the whole test when the provider has no matrix at
// the requested path (a plain provider like TinyEmberPlus :9000), so the test
// asserts only against a matrix-bearing router.
func runMatrixJSON(t *testing.T, bin, host, port, path string, extra ...string) ensureResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := append([]string{
		"consumer", "emberplus", "matrix", host,
		"--port", port, "--path", path, "--output", "json", "--timeout", "20s",
	}, extra...)
	out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	// The JSON line is the last non-log line; slog noise goes to the same pipe.
	line := lastJSONLine(string(out))
	if line == "" {
		if err != nil {
			t.Skipf("matrix %s not usable on %s:%s (no matrix at path?): %v\n%s", path, host, port, err, firstN(string(out), 600))
		}
		t.Fatalf("matrix verb produced no JSON line:\n%s", firstN(string(out), 600))
	}
	var r ensureResult
	if jerr := json.Unmarshal([]byte(line), &r); jerr != nil {
		t.Fatalf("decoding matrix JSON %q: %v", line, jerr)
	}
	return r
}

// lastJSONLine returns the last line that looks like a JSON object, so the
// ADR-0007 result is picked out of interleaved slog output.
func lastJSONLine(out string) string {
	var found string
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "{") && strings.HasSuffix(ln, "}") {
			found = ln
		}
	}
	return found
}

// TestExternalEmberMatrixIdempotent proves the ADR-0007 matrix-ensure contract
// against a real vendor router (TinyEmberPlusRouter): an identical re-apply is a
// no-op (changed:false), and --check reports drift without mutating. Independent
// of the starting crosspoint state — it asserts the invariants, not a fixed
// route. Env-gated (EMBERPLUS_TEST_HOST) and matrix-gated (skips on a provider
// with no matrix at EMBERPLUS_TEST_MATRIX). Requires the router port, e.g.
// EMBERPLUS_TEST_PORT=9092. Leaves target 0 routed to source 0.
func TestExternalEmberMatrixIdempotent(t *testing.T) {
	host, port := emberTarget(t)
	bin := buildDHS(t)

	path := os.Getenv("EMBERPLUS_TEST_MATRIX")
	if path == "" {
		path = "router.dynamic.matrix"
	}
	const target, srcA, srcB = "0", "0", "1"

	// Apply target←srcA. Skips here if the provider has no such matrix.
	runMatrixJSON(t, bin, host, port, path, "--target", target, "--sources", srcA, "--op", "absolute")

	// Re-apply the identical connection: must be a no-op (run-twice = 0 changes).
	again := runMatrixJSON(t, bin, host, port, path, "--target", target, "--sources", srcA, "--op", "absolute")
	if again.Changed == nil || *again.Changed {
		t.Fatalf("idempotency broken: identical matrix re-apply reported changed=%v, want changed:false", again.Changed)
	}

	// --check a different source: reports drift, sends nothing.
	chk := runMatrixJSON(t, bin, host, port, path, "--target", target, "--sources", srcB, "--op", "absolute", "--check")
	if chk.WouldChange == nil || !*chk.WouldChange {
		t.Fatalf("--check to source %s reported would_change=%v, want would_change:true", srcB, chk.WouldChange)
	}

	// Re-apply srcA: still a no-op, proving --check did not mutate the crosspoint.
	after := runMatrixJSON(t, bin, host, port, path, "--target", target, "--sources", srcA, "--op", "absolute")
	if after.Changed == nil || *after.Changed {
		t.Fatalf("--check mutated state: post-check re-apply reported changed=%v, want changed:false", after.Changed)
	}
	t.Logf("matrix-ensure idempotent on %s:%s path %s (re-apply no-op, --check non-mutating)", host, port, path)
}

// getScalarValue reads a scalar parameter via `consumer emberplus get --path`
// and returns the current value, unquoted. ok=false when the path is not a
// readable scalar (so the caller can skip). The verb prints `value = "..."`
// for strings and `value = <v>` for numerics/bools.
func getScalarValue(t *testing.T, bin, host, port, path string) (string, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin,
		"consumer", "emberplus", "get", host,
		"--port", port, "--path", path, "--timeout", "20s",
	).CombinedOutput()
	if err != nil {
		return "", false
	}
	for _, ln := range strings.Split(string(out), "\n") {
		ln = strings.TrimSpace(ln)
		if v, found := strings.CutPrefix(ln, "value = "); found {
			return strings.Trim(strings.TrimSpace(v), `"`), true
		}
	}
	return "", false
}

// runEnsureJSON runs `consumer emberplus ensure ... --output json` and decodes
// the ADR-0007 result. Reuses ensureResult / lastJSONLine from the matrix test.
func runEnsureJSON(t *testing.T, bin, host, port, path, value string, extra ...string) ensureResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := append([]string{
		"consumer", "emberplus", "ensure", host,
		"--port", port, "--path", path, "--value", value,
		"--output", "json", "--timeout", "20s",
	}, extra...)
	out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	line := lastJSONLine(string(out))
	if line == "" {
		t.Fatalf("ensure produced no JSON line (err=%v):\n%s", err, firstN(string(out), 600))
	}
	var r ensureResult
	if jerr := json.Unmarshal([]byte(line), &r); jerr != nil {
		t.Fatalf("decoding ensure JSON %q: %v", line, jerr)
	}
	return r
}

// TestExternalEmberScalarEnsureIdempotent proves the ADR-0007 scalar-ensure
// contract against a real vendor provider: converging a parameter to its
// CURRENT value is a no-op (changed:false), and --check to a different value
// reports drift without mutating. Entirely non-mutating — it never leaves the
// provider changed. Env-gated (EMBERPLUS_TEST_HOST); the writable scalar path
// is EMBERPLUS_TEST_PARAM (default a router source label on :9092). Skips when
// that path is not a readable scalar (e.g. wrong port / different tree).
func TestExternalEmberScalarEnsureIdempotent(t *testing.T) {
	host, port := emberTarget(t)
	bin := buildDHS(t)

	path := os.Getenv("EMBERPLUS_TEST_PARAM")
	if path == "" {
		path = "router.dynamic.labels.sources.s-0"
	}

	cur, ok := getScalarValue(t, bin, host, port, path)
	if !ok || cur == "" {
		t.Skipf("no readable scalar at %s on %s:%s — skipping", path, host, port)
	}

	// Converge to the current value: must be a no-op (run-twice = 0 changes).
	noop := runEnsureJSON(t, bin, host, port, path, cur)
	if noop.Changed == nil || *noop.Changed {
		t.Fatalf("idempotency broken: ensure to current value %q reported changed=%v, want changed:false", cur, noop.Changed)
	}

	// --check to a guaranteed-different value: reports drift, sends nothing.
	other := cur + "_dhs_probe"
	chk := runEnsureJSON(t, bin, host, port, path, other, "--check")
	if chk.WouldChange == nil || !*chk.WouldChange {
		t.Fatalf("--check to %q reported would_change=%v, want would_change:true", other, chk.WouldChange)
	}

	// Converge to current again: still a no-op, proving --check did not mutate.
	after := runEnsureJSON(t, bin, host, port, path, cur)
	if after.Changed == nil || *after.Changed {
		t.Fatalf("--check mutated state: post-check ensure reported changed=%v, want changed:false", after.Changed)
	}
	t.Logf("scalar-ensure idempotent on %s:%s path %s (current=%q; converge no-op, --check non-mutating)", host, port, path, cur)
}
