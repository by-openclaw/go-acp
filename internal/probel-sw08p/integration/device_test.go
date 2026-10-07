//go:build integration

// Device test: the released `dhs` CLI against a REAL SW-P-08 matrix (the
// lab's EVS Neuron), per ADR-0025 #3 — the oracle is a device we did not
// write, and what is driven is the binary an operator runs, not the
// library behind it.
//
// It writes ONE crosspoint and puts it back. Everything about that route
// is given by the caller, never assumed — the loopback tests of this
// package address destinations 0..15, which on a real matrix are live
// outputs, and that is why this test has its own switch instead of
// PROBEL_SW08P_TEST_HOST:
//
//	PROBEL_SW08P_DEVICE      host:port of the matrix          (required; unset = skipped)
//	DHS_BIN                  the CLI under test               (required)
//	PROBEL_SW08P_DEVICE_DST  the one destination written      (required)
//	PROBEL_SW08P_DEVICE_SRC  the source it is moved to        (required; must be carried by no destination)
//	PROBEL_SW08P_DEVICE_SRCS the matrix's source count        (required; above 1 024 the consumer asks in the extended form)
//
//	GOOS=linux GOARCH=amd64 go test -c -tags integration \
//	    -o probel-sw08p-integration.test ./internal/probel-sw08p/integration/
//	PROBEL_SW08P_DEVICE=10.6.255.102:7800 DHS_BIN=/usr/local/bin/dhs \
//	PROBEL_SW08P_DEVICE_DST=4175 PROBEL_SW08P_DEVICE_SRC=256 PROBEL_SW08P_DEVICE_SRCS=4176 \
//	    ./probel-sw08p-integration.test -test.run TestDeviceCLIRoundTrip -test.v
package probelsw08p_integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	deviceEnv     = "PROBEL_SW08P_DEVICE"
	deviceDstEnv  = "PROBEL_SW08P_DEVICE_DST"
	deviceSrcEnv  = "PROBEL_SW08P_DEVICE_SRC"
	deviceSrcsEnv = "PROBEL_SW08P_DEVICE_SRCS"
)

var tallyLine = regexp.MustCompile(`dst=(\d+) <- src=(\d+)`)

func TestDeviceCLIRoundTrip(t *testing.T) {
	target := os.Getenv(deviceEnv)
	if target == "" {
		t.Skipf("%s not set — no real matrix to test against", deviceEnv)
	}
	bin := os.Getenv("DHS_BIN")
	if bin == "" {
		t.Fatal("DHS_BIN is required: the device test drives the released CLI, not a build of this tree")
	}
	dst := mustEnvInt(t, deviceDstEnv)
	src := mustEnvInt(t, deviceSrcEnv)
	srcs := mustEnvInt(t, deviceSrcsEnv)

	cli := func(args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		full := append([]string{"consumer", "probel-sw08p", "--srcs", strconv.Itoa(srcs)}, args...)
		cmd := exec.CommandContext(ctx, bin, full...)
		cmd.Dir = t.TempDir()
		out, err := cmd.Output() // stdout only: the wire log goes to stderr
		return string(out), err
	}
	must := func(args ...string) string {
		t.Helper()
		out, err := cli(args...)
		if err != nil {
			t.Fatalf("dhs %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return out
	}
	route := func() int {
		t.Helper()
		out := must("interrogate", target, "--matrix", "0", "--level", "0", "--dst", strconv.Itoa(dst))
		m := tallyLine.FindStringSubmatch(out)
		if m == nil || m[1] != strconv.Itoa(dst) {
			t.Fatalf("interrogate dst %d answered %q", dst, out)
		}
		n, _ := strconv.Atoi(m[2])
		return n
	}
	// The matrix confirms a connect before its own tally table shows it.
	routeBecomes := func(want int) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		got := route()
		for got != want && time.Now().Before(deadline) {
			time.Sleep(500 * time.Millisecond)
			got = route()
		}
		if got != want {
			t.Fatalf("dst %d reads src %d ten seconds after the write, want %d", dst, got, want)
		}
	}

	// The session comes up.
	if out := must("status", target); !strings.Contains(out, "reachable=true") || !strings.Contains(out, "connected=true") {
		t.Fatalf("status: %q", out)
	}

	// The whole table, and the guard: the test source is free, so the one
	// route this test moves is the only one that can carry it.
	dump := must("tally-dump", target, "--matrix", "0", "--level", "0")
	rows := tallyLine.FindAllStringSubmatch(dump, -1)
	if len(rows) <= dst {
		t.Fatalf("tally-dump answered %d destination(s); destination %d is not in it", len(rows), dst)
	}
	for _, r := range rows {
		if r[2] == strconv.Itoa(src) {
			t.Fatalf("source %d is carried by destination %s — choose a free source for %s", src, r[1], deviceSrcEnv)
		}
	}

	home := route()
	if home == src {
		t.Fatalf("dst %d is already on the test source %d", dst, src)
	}
	connect := func(to int) {
		t.Helper()
		out := must("connect", target, "--matrix", "0", "--level", "0",
			"--dst", strconv.Itoa(dst), "--src", strconv.Itoa(to))
		if want := fmt.Sprintf("dst=%d src=%d", dst, to); !strings.Contains(out, want) {
			t.Fatalf("connect answered %q, want %q in it", out, want)
		}
	}
	t.Cleanup(func() {
		// Whatever happened above, the route goes back where it was.
		if _, err := cli("connect", target, "--matrix", "0", "--level", "0",
			"--dst", strconv.Itoa(dst), "--src", strconv.Itoa(home)); err != nil {
			t.Errorf("RESTORE FAILED: dst %d was on src %d before the test: %v", dst, home, err)
		}
	})

	connect(src)
	routeBecomes(src)

	// The reverse tally sees it: the test source is on that destination, and nowhere else.
	usage := must("usage", target, "--matrix", "0", "--level", "0", "--srce", strconv.Itoa(src), "--format", "ascii")
	if n := strings.Count(usage, "dst "); n != 1 || !strings.Contains(usage, fmt.Sprintf("dst %d ", dst)) {
		t.Errorf("usage of src %d: %q — want destination %d alone", src, usage, dst)
	}

	connect(home)
	routeBecomes(home)

	// Names of that one route, as the matrix holds them.
	if out := must("single-dest-name", target, "--matrix", "0", "--dst", strconv.Itoa(dst)); !strings.Contains(out, fmt.Sprintf("dst=%d  \"", dst)) {
		t.Errorf("single-dest-name: %q", out)
	}
	if out := must("single-source-name", target, "--matrix", "0", "--level", "0", "--src", strconv.Itoa(src)); !strings.Contains(out, fmt.Sprintf("src=%d  \"", src)) {
		t.Errorf("single-source-name: %q", out)
	}

	t.Logf("PASS: %d destination(s) dumped; dst %d moved from src %d to %d and back by the released CLI, each read back from %s",
		len(rows), dst, home, src, target)
}

func mustEnvInt(t *testing.T, name string) int {
	t.Helper()
	n, err := strconv.Atoi(os.Getenv(name))
	if err != nil || n < 0 {
		t.Fatalf("%s must be a number (got %q)", name, os.Getenv(name))
	}
	return n
}
