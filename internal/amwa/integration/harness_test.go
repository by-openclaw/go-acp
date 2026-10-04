//go:build integration

// Package amwa_integration drives the real dhs CLI against real NMOS
// peers — ADR-0025 deliverable 3. The oracle is never our own registry
// or node (ADR-0034): it is the nmos-cpp reference registry and node,
// and the real devices (EVS Neuron, Riedel FusioN).
//
// Gated on environment, so CI's unit run never reaches a peer (root
// CLAUDE.md: "CI runs unit only — never integration"):
//
//	NMOS_TEST_REGISTRY=http://10.6.250.104:8110 \
//	NMOS_TEST_NODE=http://10.6.255.102:3000 \
//	NMOS_TEST_MUTATE_NODE=http://10.6.250.104:8120 \
//	DHS_BIN=/usr/local/bin/dhs \
//	go test -tags integration ./internal/amwa/integration/
//
// NMOS_TEST_REGISTRY is an oracle Registry (its Query API), NMOS_TEST_NODE
// a device's own Node API. NMOS_TEST_MUTATE_NODE, when set, is a Node on
// which a route may be made and unmade — the reference node, never a
// device carrying programme.
//
// What the peer says is read here straight from its own API and
// compared with what the CLI printed: the peer is the oracle. Every
// change is undone. Every check says why on a non-pass, in the
// vocabulary ADR-0025 fixes: PASS / FAIL-real (a bug in us) /
// FAIL-expected (a reject path that correctly rejected) / TIMEOUT (the
// peer did not answer).
package amwa_integration

import (
	"bytes"
	"context"
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

// env returns a peer address from the environment, or skips the test.
func env(t *testing.T, name, what string) string {
	t.Helper()
	v := strings.TrimRight(strings.TrimSpace(os.Getenv(name)), "/")
	if v == "" {
		t.Skipf("%s is not set — this test needs %s", name, what)
	}
	return v
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
// DHS_BIN points at a prebuilt one — the released binary on a host that
// reaches the peers and has no Go toolchain.
func binary(t *testing.T) string {
	t.Helper()
	if b := strings.TrimSpace(os.Getenv("DHS_BIN")); b != "" {
		if _, err := os.Stat(b); err != nil {
			t.Fatalf("FAIL-real: DHS_BIN=%s: %v", b, err)
		}
		return b
	}
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "dhs-amwa-integration")
		if err != nil {
			buildErr = err
			return
		}
		out := filepath.Join(dir, "dhs")
		cmd := exec.Command("go", "build", "-o", out, "./cmd/dhs")
		cmd.Dir = repoRoot(t)
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

// run drives one CLI invocation and classifies a timeout. stdout and
// stderr come back separately: the catalogue is on stdout, the
// compliance summary on stderr.
func run(t *testing.T, timeout time.Duration, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary(t), args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("TIMEOUT: dhs %s did not finish within %s\n%s", strings.Join(args, " "), timeout, tail(out.String()+errb.String()))
	}
	return out.String(), errb.String(), err
}

// mustRun is run() for a call that has to succeed.
func mustRun(t *testing.T, timeout time.Duration, args ...string) string {
	t.Helper()
	out, errOut, err := run(t, timeout, args...)
	if err != nil {
		t.Fatalf("FAIL-real: dhs %s\n%v\n%s", strings.Join(args, " "), err, tail(out+errOut))
	}
	return out
}

// tail keeps a transcript readable.
func tail(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > 40 {
		lines = append([]string{fmt.Sprintf("… %d line(s) before …", len(lines)-40)}, lines[len(lines)-40:]...)
	}
	return strings.Join(lines, "\n")
}

// The peer, read directly: the oracle.

var peer = &http.Client{Timeout: 20 * time.Second}

// oracle reads one URL from the peer and decodes the JSON into v.
func oracle(t *testing.T, url string, v any) {
	t.Helper()
	resp, err := peer.Get(url)
	if err != nil {
		t.Fatalf("TIMEOUT: the peer did not answer GET %s: %v", url, err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("FAIL-real: the peer answered GET %s with %s", url, resp.Status)
	}
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("FAIL-real: GET %s is not the JSON this test expects: %v", url, err)
	}
}

// resource is the part of an IS-04 resource these tests read.
type resource struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Format    string `json:"format"`
	Transport string `json:"transport"`
	DeviceID  string `json:"device_id"`
	Controls  []struct {
		Href string `json:"href"`
		Type string `json:"type"`
	} `json:"controls"`
}

// collections are the six IS-04 collections, in the CLI's JSON spelling
// and the API's.
var collections = []struct{ key, path string }{
	{"Nodes", "nodes"}, {"Devices", "devices"}, {"Sources", "sources"},
	{"Flows", "flows"}, {"Senders", "senders"}, {"Receivers", "receivers"},
}

// walked is the CLI's `walk --json` catalogue.
type walked map[string][]resource

// walk runs `dhs consumer nmos walk … --json` and decodes its stdout.
func walk(t *testing.T, flag, target string) walked {
	t.Helper()
	out := mustRun(t, 2*time.Minute, "consumer", "nmos", "walk", flag, target, "--json")
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("FAIL-real: `walk --json` did not print JSON: %v\n%s", err, tail(out))
	}
	w := walked{}
	for _, c := range collections {
		var list []resource
		if err := json.Unmarshal(raw[c.key], &list); err != nil {
			t.Fatalf("FAIL-real: `walk --json` %s is not a list of resources: %v", c.key, err)
		}
		w[c.key] = list
	}
	return w
}

// ids is the id set of a list of resources.
func ids(list []resource) map[string]bool {
	set := make(map[string]bool, len(list))
	for _, r := range list {
		set[r.ID] = true
	}
	return set
}

// sameIDs reports what one side holds and the other does not.
func sameIDs(t *testing.T, what string, cli, peerSide []resource) {
	t.Helper()
	a, b := ids(cli), ids(peerSide)
	var onlyCLI, onlyPeer []string
	for id := range a {
		if !b[id] {
			onlyCLI = append(onlyCLI, id)
		}
	}
	for id := range b {
		if !a[id] {
			onlyPeer = append(onlyPeer, id)
		}
	}
	if len(onlyCLI) > 0 || len(onlyPeer) > 0 {
		t.Errorf("FAIL-real: %s — the CLI lists %d, the peer %d; only in the CLI %v, only at the peer %v",
			what, len(cli), len(peerSide), head(onlyCLI), head(onlyPeer))
	}
}

func head(s []string) []string {
	if len(s) > 5 {
		return append(s[:5:5], fmt.Sprintf("… %d more", len(s)-5))
	}
	return s
}
