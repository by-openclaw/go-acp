// ACP2 CLI-driving integration test — the ADR-0025 integration deliverable.
//
// This is the tier-4 loopback-regression oracle (per the oracle-per-tier
// taxonomy): it drives the actual `dhs` binary in BOTH roles over real
// AN2/TCP loopback — our acp2 provider as the emulator, our acp2 consumer
// under test. Valid only as a regression net (it does not prove the consumer
// against ground truth — that needs the real Neuron / Lawo VSM), but it is
// CI-repeatable with no external dependencies.
//
// The provider is built from the COMMITTED manifest + DM fixture under
// internal/acp2/testdata/integration-test/ (emberplus pattern, ADR-0022 /
// internal/manifest/TEMPLATE.md): a neuron-test controller exposing two
// slots — slot 0 = SHPRM1@5.3.5 (full converted DM), slot 1 =
// CONVERT Hybrid@6.7.4 (representative trimmed DM). Both DMs were collected
// from the real EVS Neuron on the DMZ fleet (.103) and flattened from the
// walk-snapshot export shape into the producer's flat DM shape.
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

// manifestPath is the committed manifest the local provider serves.
// cacheDir is its cache root (resolves dm/acp2/<Model@SwRev>.json refs).
// Both are resolved relative to this test file so `go test` works from any CWD.
var (
	manifestPath string
	cacheDir     string
)

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

	// Resolve the committed manifest + cache dir relative to this source file:
	//   internal/acp2/integration/  ->  ../testdata/integration-test/
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		fmt.Fprintln(os.Stderr, "cannot resolve test file path")
		os.Exit(1)
	}
	cacheDir = filepath.Join(filepath.Dir(thisFile), "..", "testdata", "integration-test")
	manifestPath = filepath.Join(cacheDir, "manifest", "neuron-test.json")
	if _, err := os.Stat(manifestPath); err != nil {
		fmt.Fprintf(os.Stderr, "manifest not found at %s: %v\n", manifestPath, err)
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

// startProvider spins up `dhs producer acp2 serve` from the COMMITTED
// manifest + cache dir on host:port and returns a stop function (defer it).
// It waits for readiness before returning. This is the whole point of the
// fixture: the provider builds its frame from repo files alone (BuildExport
// over the manifest's slot DMs), with no host dependency.
func startProvider(t *testing.T, host string, port int) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, dhsBin,
		"producer", "acp2", "serve",
		"--manifest", manifestPath,
		"--cache-dir", cacheDir,
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
		if t.Failed() {
			t.Logf("provider stderr:\n%s", errBuf.String())
		}
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
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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

// TestACP2CLIRoundTrip drives info + walk end-to-end against either a
// locally-spun provider built from the committed manifest (default) or an
// external device/emulator (ACP2_TEST_HOST set). The assertions are identical
// in both modes — they assert the REAL objects collected from the Neuron.
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
		// Loopback mode: our provider serves the committed manifest+DM
		// fixture (slot 0 = SHPRM1@5.3.5, slot 1 = CONVERT Hybrid@6.7.4).
		port = freePort(t)
		stop := startProvider(t, host, port)
		defer stop()
	}

	portArg := fmt.Sprintf("%d", port)
	common := func(extra ...string) []string {
		return append([]string{"--port", portArg, "--timeout", "30s"}, extra...)
	}

	// --- info: the manifest exposes a 2-slot chassis; both slots present ----
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
		// The committed fixture wires exactly two slots (0 + 1). Assert both
		// are reported present so a missing/empty slot DM regresses loudly.
		if c := strings.Count(out, "present"); c < 2 {
			t.Errorf("info: want >=2 present slots from the 2-slot manifest, saw %d\n%s", c, out)
		}
	})

	// --- walk slot 0 = SHPRM1: real rack-controller objects ----------------
	// The flat DM was converted from the Neuron walk snapshot; "Card Name"
	// with value "SHPRM1" is the canonical identity leaf.
	t.Run("walk_slot0_SHPRM1", func(t *testing.T) {
		out, code, _ := runConsumer(t, host, "walk", common("--slot", "0")...)
		if code != 0 {
			t.Fatalf("walk slot 0 exit=%d, want 0\n%s", code, out)
		}
		if !strings.Contains(out, "Card Name") {
			t.Errorf("walk slot 0 missing object %q\n%s", "Card Name", out)
		}
		if os.Getenv("ACP2_TEST_HOST") == "" && !strings.Contains(out, "SHPRM1") {
			t.Errorf("walk slot 0: want SHPRM1 Card Name value\n%s", out)
		}
		// Cross-kind coverage: the SHPRM1 DM carries string/int/enum/float
		// leaves; a real walk surfaces the BOARD group's Card ID and a PSU
		// numeric. Assert a non-identity leaf so a degenerate single-object
		// DM regresses.
		if !strings.Contains(out, "Card ID") {
			t.Errorf("walk slot 0 missing object %q\n%s", "Card ID", out)
		}
	})

	// --- walk slot 1 = CONVERT Hybrid: real processing-card objects --------
	// Trimmed DM keeps ROOT-NODE-V2 + IDENTITY + a GENERAL sampling covering
	// every object kind present (node/string/enum/int/float).
	t.Run("walk_slot1_CONVERTHybrid", func(t *testing.T) {
		out, code, _ := runConsumer(t, host, "walk", common("--slot", "1")...)
		if code != 0 {
			t.Fatalf("walk slot 1 exit=%d, want 0\n%s", code, out)
		}
		if !strings.Contains(out, "Card Name") {
			t.Errorf("walk slot 1 missing object %q\n%s", "Card Name", out)
		}
		if os.Getenv("ACP2_TEST_HOST") == "" {
			if !strings.Contains(out, "CONVERT Hybrid") {
				t.Errorf("walk slot 1: want CONVERT Hybrid Card Name value\n%s", out)
			}
			// Card Description is a CONVERT-Hybrid-specific IDENTITY leaf;
			// proves slot 1 served the CONVERT DM, not a copy of slot 0.
			if !strings.Contains(out, "Card Description") {
				t.Errorf("walk slot 1 missing object %q\n%s", "Card Description", out)
			}
		}
	})
}
