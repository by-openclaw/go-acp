//go:build integration

// Package acp1_integration pins the ACP1 manifest + DM contract by driving the
// real dhs CLI end to end: it builds the binary, starts the producer serving
// the committed integration fixture over TCP (Mode B), then runs the consumer
// info + walk against it and asserts the fixture's slots and objects are
// exposed on the wire.
//
// The fixture is committed but otherwise unexercised:
//
//	internal/acp1/testdata/integration-test/manifest/synapse-test.json
//	internal/acp1/testdata/integration-test/dm/acp1/*.json
//
// The DMs were collected from a real Synapse rack (host .103) in the flat
// {model,sw_rev,protocol,objects[]} shape the producer reads. The manifest
// endpoint is a placeholder (0.0.0.0:2071/udp) — host/port/transport are
// overridden at serve time by the CLI flags below.
//
// Gated behind the `integration` build tag so CI's unit run never picks it up
// (root CLAUDE.md: "CI runs unit only — never integration").
package acp1_integration

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
		out := filepath.Join(os.TempDir(), fmt.Sprintf("dhs-acp1-integration-%d.exe", os.Getpid()))
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

// startProducer builds dhs, starts `producer acp1 serve` on the committed
// fixture over TCP, polls until the port is open, and returns the bound port.
// The producer is killed via t.Cleanup no matter how the test exits.
func startProducer(t *testing.T) (bin string, port int) {
	t.Helper()
	root := repoRoot(t)
	bin = buildDHS(t)

	manifest := filepath.Join(root, "internal", "acp1", "testdata", "integration-test", "manifest", "synapse-test.json")
	cacheDir := filepath.Join(root, "internal", "acp1", "testdata", "integration-test")
	if _, err := os.Stat(manifest); err != nil {
		t.Fatalf("fixture manifest missing: %v", err)
	}

	port = freePort(t)

	prodCtx, prodCancel := context.WithCancel(context.Background())

	prod := exec.CommandContext(prodCtx, bin,
		"producer", "acp1", "serve",
		"--manifest", manifest,
		"--cache-dir", cacheDir,
		"--transport", "tcp",
		"--port", fmt.Sprintf("%d", port),
		"--host", "127.0.0.1",
		"--log-level", "error",
	)
	var prodOut strings.Builder
	prod.Stdout = &prodOut
	prod.Stderr = &prodOut
	if err := prod.Start(); err != nil {
		prodCancel()
		t.Fatalf("start producer: %v", err)
	}
	// defer-kill the producer no matter how the test exits.
	t.Cleanup(func() {
		prodCancel()
		_ = prod.Process.Kill()
		_, _ = prod.Process.Wait()
		if t.Failed() {
			t.Logf("--- producer output ---\n%s", prodOut.String())
		}
	})

	// Poll until the producer accepts connections (no fixed sleep).
	openCtx, openCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer openCancel()
	waitPortOpen(openCtx, t, port)
	return bin, port
}

// TestManifestServeInfo starts the producer on the committed manifest fixture
// and asserts the consumer `info` reports all six populated slots.
func TestManifestServeInfo(t *testing.T) {
	bin, port := startProducer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	info := exec.CommandContext(ctx, bin,
		"consumer", "acp1", "info", "127.0.0.1",
		"--transport", "tcp",
		"--port", fmt.Sprintf("%d", port),
		"--timeout", "10s",
	)
	out, err := info.CombinedOutput()
	if err != nil {
		t.Fatalf("consumer info failed: %v\n--- info output ---\n%s", err, out)
	}
	text := string(out)

	// The manifest's single frame (SFR18) declares six slots (0..5), each
	// bound to a DM, so the producer must report 6 present slots.
	if !strings.Contains(text, "slots        6") {
		t.Fatalf("info did not report 6 slots from the fixture:\n%s", text)
	}
	for s := 0; s <= 5; s++ {
		want := fmt.Sprintf("slot  %d   status=present", s)
		if !strings.Contains(text, want) {
			t.Fatalf("info missing present status for slot %d:\n%s", s, text)
		}
	}
	t.Logf("info reported all 6 fixture slots present")
}

// TestManifestServeWalk walks slot 0 (the RRS18@1601 rack controller card) and
// asserts the exposed objects match the committed DM: a positive object count
// plus known identity labels/values collected from the real device.
func TestManifestServeWalk(t *testing.T) {
	bin, port := startProducer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	walk := exec.CommandContext(ctx, bin,
		"consumer", "acp1", "walk", "127.0.0.1",
		"--transport", "tcp",
		"--port", fmt.Sprintf("%d", port),
		"--slot", "0",
		"--timeout", "15s",
	)
	out, err := walk.CombinedOutput()
	if err != nil {
		t.Fatalf("consumer walk failed: %v\n--- walk output ---\n%s", err, out)
	}
	text := string(out)

	// Assertion 1: a sane number of objects came back. The committed
	// RRS18@1601 DM has 59 objects across identity/control/status/alarm; use a
	// conservative floor.
	if !strings.Contains(text, "objects") {
		t.Fatalf("walk output missing object-count header:\n%s", text)
	}
	count := parseObjectCount(t, text)
	const minObjects = 40
	if count < minObjects {
		t.Fatalf("expected at least %d objects from slot 0, got %d:\n%s", minObjects, count, text)
	}

	// Assertion 2: known identifiers from the committed RRS18 DM appear on the
	// wire — the identity group's "Card name" label plus its device-collected
	// value "RRS18" (the card model).
	for _, want := range []string{"Card name", "RRS18", "Serial number"} {
		if !strings.Contains(text, want) {
			t.Fatalf("walk output missing known fixture identifier %q:\n%s", want, text)
		}
	}

	t.Logf("walk exposed %d objects on slot 0; found Card name=RRS18 + Serial number", count)
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
