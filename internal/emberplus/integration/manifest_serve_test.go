//go:build integration

// Package emberplus_integration pins the Ember+ manifest + DM contract by
// driving the real dhs CLI end to end: it builds the binary, starts the
// producer serving the committed integration fixture, then runs the consumer
// walk against it and asserts the fixture's objects are exposed on the wire.
//
// The fixture is committed but otherwise unexercised:
//
//	internal/emberplus/testdata/integration-test/manifest/emberplus-integration.json
//	internal/emberplus/testdata/integration-test/dm/emberplus/*.json
//
// This test is gated behind the `integration` build tag so CI's unit run never
// picks it up (root CLAUDE.md: "CI runs unit only — never integration").
package emberplus_integration

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	buildOnce sync.Once
	dhsBin    string
	buildErr  error
)

// repoRoot walks up from the test's working directory (the package dir) until
// it finds the go.mod that declares `module dhs`, returning the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		gomod := filepath.Join(dir, "go.mod")
		if data, err := os.ReadFile(gomod); err == nil {
			if strings.Contains(string(data), "module dhs") {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root (module dhs go.mod) from %s", dir)
		}
		dir = parent
	}
}

// buildDHS compiles cmd/dhs once into a temp binary and returns its path.
func buildDHS(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		root := repoRoot(t)
		// Use a process-lived temp path (not t.TempDir(), which is removed at
		// the end of the first test) so the once-built binary survives for
		// every test in the package.
		out := filepath.Join(os.TempDir(), fmt.Sprintf("dhs-emberplus-integration-%d.exe", os.Getpid()))
		cmd := exec.Command("go", "build", "-o", out, "./cmd/dhs")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if b, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("go build ./cmd/dhs failed: %v\n%s", err, b)
			return
		}
		dhsBin = out
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return dhsBin
}

// freePort asks the OS for an ephemeral TCP port, then releases it so the
// producer can bind it. Small race window, acceptable for a local test.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// waitPortOpen polls until 127.0.0.1:port accepts a TCP connection or ctx ends.
func waitPortOpen(ctx context.Context, t *testing.T, port int) {
	t.Helper()
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("producer port %d never opened: %v", port, ctx.Err())
		default:
		}
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestManifestServeWalk starts the producer on the committed manifest fixture
// and asserts the consumer walk exposes the fixture's objects.
func TestManifestServeWalk(t *testing.T) {
	root := repoRoot(t)
	bin := buildDHS(t)

	manifest := filepath.Join(root, "internal", "emberplus", "testdata", "integration-test", "manifest", "emberplus-integration.json")
	cacheDir := filepath.Join(root, "internal", "emberplus", "testdata", "integration-test")
	if _, err := os.Stat(manifest); err != nil {
		t.Fatalf("fixture manifest missing: %v", err)
	}

	port := freePort(t)

	prodCtx, prodCancel := context.WithCancel(context.Background())
	defer prodCancel()

	prod := exec.CommandContext(prodCtx, bin,
		"producer", "emberplus", "serve",
		"--manifest", manifest,
		"--cache-dir", cacheDir,
		"--port", fmt.Sprintf("%d", port),
		"--host", "127.0.0.1",
		"--log-level", "error",
	)
	var prodOut strings.Builder
	prod.Stdout = &prodOut
	prod.Stderr = &prodOut
	if err := prod.Start(); err != nil {
		t.Fatalf("start producer: %v", err)
	}
	// defer-kill the producer no matter how the test exits.
	defer func() {
		prodCancel()
		_ = prod.Process.Kill()
		_, _ = prod.Process.Wait()
	}()

	// Poll until the producer accepts connections (no fixed sleep).
	openCtx, openCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer openCancel()
	waitPortOpen(openCtx, t, port)

	// Drive the consumer walk against the producer.
	walkCtx, walkCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer walkCancel()
	walk := exec.CommandContext(walkCtx, bin,
		"consumer", "emberplus", "walk", "127.0.0.1",
		"--port", fmt.Sprintf("%d", port),
		"--timeout", "10s",
	)
	out, err := walk.CombinedOutput()
	if err != nil {
		t.Fatalf("consumer walk failed: %v\n--- walk output ---\n%s\n--- producer output ---\n%s",
			err, out, prodOut.String())
	}
	text := string(out)

	// Assertion 1: a sane number of objects came back (> 0; the fixture frame
	// has ~1.3k objects, so use a conservative floor well below that). The walk
	// prints a "slot N — <count> objects" header.
	if !strings.Contains(text, "objects") {
		t.Fatalf("walk output missing object-count header:\n%s", text)
	}
	count := parseObjectCount(t, text)
	if count <= 0 {
		t.Fatalf("expected a positive object count, got %d:\n%s", count, text)
	}
	const minObjects = 100
	if count < minObjects {
		t.Fatalf("expected at least %d objects from the fixture, got %d", minObjects, count)
	}

	// Assertion 2: the manifest attaches all 7 DMs, so the walk MUST expose the
	// full Ember+ type surface — every matrix behaviour, functions, and every
	// glow value type — not just one label. Identifiers come from the committed
	// DMs: oneToN/oneToOne/nToN/dynamic matrices, functions-strict builtins,
	// glow-types-strict typed params, identity-strict.
	wantIDs := []string{
		"dhs-emberplus-integration",              // manifest device.name
		"oneToN", "oneToOne", "nToN", "dynamic",  // all matrix behaviours
		"functions", "getSalvo", "recallSalvo",   // function elements
		"vInteger", "vReal", "vString", "vBoolean", "vEnum", "vOctets", "vTrigger", // every glow value type
		"identity", "product",                    // identity tree
	}
	for _, want := range wantIDs {
		if !strings.Contains(text, want) {
			t.Fatalf("walk output missing expected fixture identifier %q — the emulator must expose the full type surface (all matrix types + functions + every glow type):\n%s", want, text)
		}
	}

	t.Logf("walk exposed %d objects across the full surface: matrices(oneToN/oneToOne/nToN/dynamic) + functions + all glow types(vInteger/vReal/vString/vBoolean/vEnum/vOctets/vTrigger) + identity", count)
}

// parseObjectCount extracts N from a "slot 0 — N objects" header line.
func parseObjectCount(t *testing.T, text string) int {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "objects") {
			continue
		}
		// Split on whitespace and find the integer immediately before "objects".
		fields := strings.Fields(line)
		for i, f := range fields {
			if f == "objects" && i > 0 {
				var n int
				if _, err := fmt.Sscanf(fields[i-1], "%d", &n); err == nil {
					return n
				}
			}
		}
	}
	return -1
}
