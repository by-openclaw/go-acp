//go:build integration

// Package ccm_integration drives the real dhs CLI against real EVS
// Neurons — ADR-0025 deliverable 3, tier 2/3: the oracle is the device,
// never our own provider (the producer side waits for the final EVS
// release, #1214).
//
// Gated on CCM_TEST_HOST, so CI's unit run never reaches a device (root
// CLAUDE.md: "CI runs unit only — never integration"):
//
//	CCM_TEST_HOST=10.6.255.103 go test -tags integration ./internal/ccm/integration/
//
// CCM_TEST_HOST is a device that serves the CCM §13 event channel (the
// Neuron Shuffle). CCM_TEST_POLLED_HOST, when set, is one that does not
// (CONVERT Hybrid 7.0.3) and exercises the polled path.
//
// What the device says is read here straight from its REST API and
// compared with what the CLI printed: the device is the oracle.
//
// Every write is restored. Every check says why on a non-pass, in the
// vocabulary ADR-0025 fixes: PASS / FAIL-real (a bug in us) /
// FAIL-expected (a reject path that correctly rejected) / TIMEOUT (the
// device did not answer).
package ccm_integration

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// host is the device under test, or "" when this suite should not run.
func host(t *testing.T) string {
	t.Helper()
	h := strings.TrimSpace(os.Getenv("CCM_TEST_HOST"))
	if h == "" {
		t.Skip("CCM_TEST_HOST is not set — this suite needs a real device that serves the event channel")
	}
	return h
}

// repoRoot walks up until it finds the go.mod that declares `module dhs`.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && strings.Contains(string(data), "module dhs") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod for module dhs not found above the test directory")
		}
		dir = parent
	}
}

// binary is the CLI under test: the same binary an operator runs.
// DHS_BIN points at a prebuilt one, for a host that reaches the device
// and has no Go toolchain.
func binary(t *testing.T) string {
	t.Helper()
	if b := strings.TrimSpace(os.Getenv("DHS_BIN")); b != "" {
		if _, err := os.Stat(b); err != nil {
			t.Fatalf("FAIL-real: DHS_BIN=%s: %v", b, err)
		}
		return b
	}
	buildOnce.Do(func() {
		root := repoRoot(t)
		dir, err := os.MkdirTemp("", "dhs-ccm-integration")
		if err != nil {
			buildErr = err
			return
		}
		out := filepath.Join(dir, "dhs")
		cmd := exec.Command("go", "build", "-o", out, "./cmd/dhs")
		cmd.Dir = root
		if b, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("build: %v\n%s", err, b)
			return
		}
		dhsBin = out
	})
	if buildErr != nil {
		t.Fatalf("FAIL-real: %v", buildErr)
	}
	return dhsBin
}

// run drives one CLI invocation in dir and classifies a timeout.
func run(t *testing.T, dir string, timeout time.Duration, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary(t), args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("TIMEOUT: %s did not answer within %s\n%s", strings.Join(args, " "), timeout, tail(string(out)))
	}
	return string(out), err
}

// mustRun is run() for a call that has to succeed.
func mustRun(t *testing.T, timeout time.Duration, args ...string) string {
	t.Helper()
	out, err := run(t, "", timeout, args...)
	if err != nil {
		t.Fatalf("FAIL-real: %s\n%v\n%s", strings.Join(args, " "), err, tail(out))
	}
	return out
}

// tail keeps a transcript readable: a watch of a matrix prints
// thousands of lines.
func tail(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > 40 {
		lines = append([]string{fmt.Sprintf("… %d line(s) before …", len(lines)-40)}, lines[len(lines)-40:]...)
	}
	return strings.Join(lines, "\n")
}

// The device, read directly: the oracle.

var insecure = &http.Client{
	Timeout:   20 * time.Second,
	Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec // lab devices are self-signed by design
}

// device reads one REST path from the device, under whichever base it
// serves, and decodes the JSON into v.
func device(t *testing.T, h, path string, v any) {
	t.Helper()
	var last error
	for _, base := range []string{"/api", "/api/v1"} {
		resp, err := insecure.Get("https://" + h + base + path)
		if err != nil {
			t.Fatalf("TIMEOUT: the device did not answer GET %s%s: %v", base, path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			last = fmt.Errorf("GET %s%s: %s", base, path, resp.Status)
			continue
		}
		if err := json.Unmarshal(body, v); err != nil {
			t.Fatalf("FAIL-real: GET %s%s is not the JSON this test expects: %v", base, path, err)
		}
		return
	}
	t.Fatalf("FAIL-real: %v", last)
}

// A watch runs until it is stopped, so it is a process with a
// transcript that can be searched while it runs.

type watch struct {
	t      *testing.T
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
	out    bytes.Buffer
}

func (w *watch) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.out.Write(p)
}

func (w *watch) text() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.out.String()
}

// startWatch runs `dhs consumer ccm watch` with args until the test
// ends. bin is the binary to run, "" for the one under test — a test
// that needs a cache of its own runs a copy, because the cache lives
// beside the binary.
func startWatch(t *testing.T, bin string, args ...string) *watch {
	t.Helper()
	if bin == "" {
		bin = binary(t)
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &watch{t: t, cancel: cancel, done: make(chan struct{})}
	cmd := exec.CommandContext(ctx, bin, append([]string{"consumer", "ccm", "watch"}, args...)...)
	cmd.Stdout, cmd.Stderr = w, w
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("FAIL-real: start watch: %v", err)
	}
	go func() { _ = cmd.Wait(); close(w.done) }()
	t.Cleanup(w.stop)
	return w
}

func (w *watch) stop() {
	w.cancel()
	<-w.done
}

// until waits for the transcript to satisfy ok.
func (w *watch) until(what string, within time.Duration, ok func(transcript string) bool) {
	w.t.Helper()
	deadline := time.Now().Add(within)
	for {
		text := w.text()
		if ok(text) {
			return
		}
		select {
		case <-w.done:
			w.t.Fatalf("FAIL-real: the watch ended before %s:\n%s", what, tail(text))
		default:
		}
		if time.Now().After(deadline) {
			w.t.Fatalf("TIMEOUT: %s did not happen within %s:\n%s", what, within, tail(text))
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// has reports a transcript line carrying every one of parts.
func has(transcript string, parts ...string) bool {
	for _, line := range strings.Split(transcript, "\n") {
		all := true
		for _, p := range parts {
			all = all && strings.Contains(line, p)
		}
		if all {
			return true
		}
	}
	return false
}

// pushed is the line the connector logs when a watch is fed by the
// device's event channel, and polled the one when it is not.
const (
	pushed  = "ccm: watching over the event channel"
	polled  = "ccm: no event channel on this device, watch polls"
	members = "ccm: members watched"
)

// setAndRestore writes value to path through the CLI and puts back
// `was` when the test ends.
func setAndRestore(t *testing.T, h, path, value, was string) {
	t.Helper()
	t.Cleanup(func() {
		if out, err := run(t, "", 60*time.Second, "consumer", "ccm", "set", h, "--path", path, "--value", was); err != nil {
			t.Errorf("FAIL-real: restoring %s to %s: %v\n%s", path, was, err, out)
		}
	})
	mustRun(t, 60*time.Second, "consumer", "ccm", "set", h, "--path", path, "--value", value)
}

// ownCopy puts a copy of the binary under test in a directory of the
// test's own, so what it caches there is this test's and nobody else's.
func ownCopy(t *testing.T) (bin, dir string) {
	t.Helper()
	dir = t.TempDir()
	src, err := os.ReadFile(binary(t))
	if err != nil {
		t.Fatalf("FAIL-real: read the binary under test: %v", err)
	}
	bin = filepath.Join(dir, filepath.Base(binary(t)))
	if err := os.WriteFile(bin, src, 0o755); err != nil {
		t.Fatalf("FAIL-real: copy the binary under test: %v", err)
	}
	return bin, dir
}
