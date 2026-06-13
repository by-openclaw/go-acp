// ACP2 CLI-driving integration test — the ADR-0025 integration deliverable.
//
// This is the tier-4 loopback-regression oracle (per the oracle-per-tier
// taxonomy): it drives the actual `dhs` binary in BOTH roles over real
// AN2/TCP loopback — our acp2 provider as the emulator, our acp2 consumer
// under test. Valid only as a regression net (it does not prove the consumer
// against ground truth — that needs the real Neuron / Lawo VSM), but it is
// CI-repeatable with no external dependencies.
//
// Env-gated external mode: set ACP2_TEST_HOST to skip the local provider and
// run the identical consumer assertions against a real device/emulator from a
// device-reachable host (ACP2_TEST_PORT, default 2072).
//
//go:build integration

package acp2_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// dhsBin is the path to the `dhs` binary built once by TestMain.
var dhsBin string

// fixtureTree is the canonical tree the local provider serves. Resolved
// relative to this test file so `go test` works from any CWD.
var fixtureTree string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "acp2-integration-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "mktemp: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	bin := filepath.Join(tmp, "dhs")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}

	// Build the CLI once. CGO not needed; the caller is expected to set
	// CGO_ENABLED=0 but we don't rely on it — pure-Go build either way.
	buildCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	build := exec.CommandContext(buildCtx, "go", "build", "-o", bin, "dhs/cmd/dhs")
	build.Stderr = os.Stderr
	build.Stdout = os.Stdout
	if err := build.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "build dhs: %v\n", err)
		os.Exit(1)
	}
	dhsBin = bin

	// Resolve the fixture tree relative to this source file:
	//   internal/acp2/integration/  ->  ../testdata/protocol_types/fixture_tree.json
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		fmt.Fprintln(os.Stderr, "cannot resolve test file path")
		os.Exit(1)
	}
	fixtureTree = filepath.Join(filepath.Dir(thisFile), "..", "testdata", "protocol_types", "fixture_tree.json")
	if _, err := os.Stat(fixtureTree); err != nil {
		fmt.Fprintf(os.Stderr, "fixture tree not found at %s: %v\n", fixtureTree, err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// freePort asks the OS for an ephemeral TCP port on loopback, then releases
// it so the provider can bind. There's an inherent race between close and
// re-bind, but on loopback it's reliable enough for a single-process test.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// waitForPort polls until host:port accepts a TCP connection or the deadline
// elapses. No fixed sleep on the assert path — this is the readiness gate.
func waitForPort(t *testing.T, host string, port int, timeout time.Duration) {
	t.Helper()
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("provider did not accept connections on %s within %s", addr, timeout)
}

// startProvider spins up `dhs producer acp2 serve` against the fixture tree on
// host:port and returns a stop function (defer it). It waits for readiness
// before returning.
func startProvider(t *testing.T, host string, port int) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, dhsBin,
		"producer", "acp2", "serve",
		"--tree", fixtureTree,
		"--port", fmt.Sprintf("%d", port),
		"--host", host,
		"--log-level", "error",
	)
	// Surface provider logs only on failure; keep the happy path quiet.
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start provider: %v", err)
	}

	stop := func() {
		cancel()
		_ = cmd.Wait()
	}

	waitForPort(t, host, port, 10*time.Second)
	return stop
}

// runConsumer executes `dhs consumer acp2 <verb> <host> <args...>` with a
// short per-call timeout and returns combined stdout (the assertion surface),
// the error, and the exit code. Operational logs go to stderr and are
// captured separately so they don't pollute stdout assertions.
func runConsumer(t *testing.T, host string, verb string, args ...string) (string, int, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	full := append([]string{"consumer", "acp2", verb, host}, args...)
	cmd := exec.CommandContext(ctx, dhsBin, full...)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()

	exitCode := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			t.Fatalf("consumer %s: launch failed: %v\nstderr: %s", verb, err, errBuf.String())
		}
	}
	if t.Failed() || testing.Verbose() {
		t.Logf("consumer %s args=%v exit=%d\nstdout:\n%s\nstderr:\n%s",
			verb, args, exitCode, outBuf.String(), errBuf.String())
	}
	return outBuf.String(), exitCode, err
}

// TestACP2CLIRoundTrip drives info/walk/get/set end-to-end against either a
// locally-spun provider (default) or an external device/emulator
// (ACP2_TEST_HOST set). The assertions are identical in both modes.
func TestACP2CLIRoundTrip(t *testing.T) {
	host := "127.0.0.1"
	var port int

	if ext := os.Getenv("ACP2_TEST_HOST"); ext != "" {
		// External mode: do NOT spin the local provider. Target the real
		// fleet emulator / Neuron from a device-reachable host.
		host = ext
		port = 2072
		if ps := os.Getenv("ACP2_TEST_PORT"); ps != "" {
			var p int
			if _, err := fmt.Sscanf(ps, "%d", &p); err == nil && p > 0 {
				port = p
			}
		}
		t.Logf("external mode: targeting %s:%d (ACP2_TEST_HOST set)", host, port)
	} else {
		// Loopback mode: our provider serves the fixture tree.
		port = freePort(t)
		stop := startProvider(t, host, port)
		defer stop()
	}

	portArg := fmt.Sprintf("%d", port)
	common := func(extra ...string) []string {
		return append([]string{"--port", portArg, "--timeout", "5s"}, extra...)
	}

	// --- info: prints "slots" and at least one present slot -----------------
	t.Run("info", func(t *testing.T) {
		out, code, _ := runConsumer(t, host, "info", common()...)
		if code != 0 {
			t.Fatalf("info exit=%d, want 0\n%s", code, out)
		}
		if !strings.Contains(out, "slots") {
			t.Errorf("info output missing %q\n%s", "slots", out)
		}
		if !strings.Contains(out, "present") {
			t.Errorf("info output has no present slot\n%s", out)
		}
	})

	// --- walk: the fixture card (slot 1) lists multiple objects -------------
	t.Run("walk", func(t *testing.T) {
		out, code, _ := runConsumer(t, host, "walk", common("--slot", "1")...)
		if code != 0 {
			t.Fatalf("walk slot 1 exit=%d, want 0\n%s", code, out)
		}
		// slot 1 = "one leaf per ACP2 object type" — expect named leaves.
		for _, want := range []string{"UserLabel", "GainS32", "Mode"} {
			if !strings.Contains(out, want) {
				t.Errorf("walk slot 1 missing object %q\n%s", want, out)
			}
		}
	})

	// --- get + set round-trip on slot 0's writable UserLabel leaf ----------
	// Fixture leaf: device.slot-0.ROOT_NODE_V2.IDENTITY.UserLabel
	//   obj-id 3, readWrite string, initial value "ACP2-Fixture".
	// ACP2 addressing is by obj-id (--id); dotted-path resolution is not
	// wired for this protocol in the CLI, so we address by id.
	t.Run("get_set_roundtrip", func(t *testing.T) {
		const (
			slot     = "0"
			objID    = "3"
			original = "ACP2-Fixture"
			updated  = "ACP2-RT"
		)

		// Read the known initial value.
		out, code, _ := runConsumer(t, host, "get", common("--slot", slot, "--id", objID)...)
		if code != 0 {
			t.Fatalf("get exit=%d, want 0\n%s", code, out)
		}
		// In loopback mode the value is exactly the fixture's; assert it.
		// In external mode the device may already hold a different value,
		// so we only require a non-empty quoted value there.
		if os.Getenv("ACP2_TEST_HOST") == "" {
			if !strings.Contains(out, original) {
				t.Errorf("get: want initial value %q\n%s", original, out)
			}
		}
		if !strings.Contains(out, "value") {
			t.Errorf("get: output missing %q line\n%s", "value", out)
		}

		// Write a new value; the producer echoes the confirmed value.
		out, code, _ = runConsumer(t, host, "set", common("--slot", slot, "--id", objID, "--value", updated)...)
		if code != 0 {
			t.Fatalf("set exit=%d, want 0\n%s", code, out)
		}
		if !strings.Contains(out, "confirmed") || !strings.Contains(out, updated) {
			t.Errorf("set: want confirmed %q\n%s", updated, out)
		}

		// Read back: the round-trip must reflect the new value.
		out, code, _ = runConsumer(t, host, "get", common("--slot", slot, "--id", objID)...)
		if code != 0 {
			t.Fatalf("get-after-set exit=%d, want 0\n%s", code, out)
		}
		if !strings.Contains(out, updated) {
			t.Errorf("get-after-set: want round-tripped value %q\n%s", updated, out)
		}

		// Restore the original so re-runs are deterministic (loopback mode
		// uses a fresh provider each run, but external mode persists).
		_, _, _ = runConsumer(t, host, "set", common("--slot", slot, "--id", objID, "--value", original)...)
	})
}
