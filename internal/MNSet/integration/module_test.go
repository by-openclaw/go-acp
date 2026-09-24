//go:build integration

// Package mnset_integration drives the real dhs CLI against a real
// Riedel module — ADR-0025 deliverable 3, tier 2/3: the oracle is the
// device, never our own provider (this connector has none; the
// producer is parked by scope).
//
// Gated on MNSET_TEST_HOST, so CI's unit run never reaches a device
// (root CLAUDE.md: "CI runs unit only — never integration"):
//
//	MNSET_TEST_HOST=10.6.40.53 go test -tags integration ./internal/MNSet/integration/
//
// The module is on the media VLAN: this runs where a route to it
// exists (the control node), not from a desk.
//
// Every check says why on a non-pass, in the vocabulary ADR-0025
// fixes: PASS / FAIL-real (a bug in us) / FAIL-expected (a reject path
// that correctly rejected) / TIMEOUT (the device did not answer).
package mnset_integration

import (
	"context"
	"encoding/csv"
	"fmt"
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

// host is the module under test, or "" when this suite should not run.
func host(t *testing.T) string {
	t.Helper()
	h := strings.TrimSpace(os.Getenv("MNSET_TEST_HOST"))
	if h == "" {
		t.Skip("MNSET_TEST_HOST is not set — this suite needs the real module")
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

// binary is the CLI under test: the test drives the same binary an
// operator does, not a library call that could diverge from it.
//
// DHS_BIN points at a prebuilt one. That is not a convenience — the
// module is on the media VLAN, the host with a route to it is the
// control node, and the control node has no Go toolchain. Without
// this, the only machine that can reach the device cannot run the
// suite.
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
		out := filepath.Join(t.TempDir(), "dhs")
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

// run drives one CLI invocation and classifies what came back.
func run(t *testing.T, timeout time.Duration, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary(t), args...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("TIMEOUT: %s did not answer within %s\n%s", strings.Join(args, " "), timeout, out)
	}
	return string(out), err
}

// runFor drives a verb that does not end by itself — a watch runs
// until Ctrl-C, so the deadline is how it stops, not a failure.
func runFor(t *testing.T, d time.Duration, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	out, _ := exec.CommandContext(ctx, binary(t), args...).CombinedOutput()
	return string(out)
}

// mustRun is run() for a call that has to succeed.
func mustRun(t *testing.T, timeout time.Duration, args ...string) string {
	t.Helper()
	out, err := run(t, timeout, args...)
	if err != nil {
		t.Fatalf("FAIL-real: %s\n%v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// templatePath finds the shipped alarm template: in the repo when the
// test runs from it, or beside the binary when it was deployed.
func templatePath(t *testing.T) string {
	t.Helper()
	if p := strings.TrimSpace(os.Getenv("MNSET_ALARM_TEMPLATE")); p != "" {
		return p
	}
	dir, err := os.Getwd()
	if err == nil {
		for {
			cand := filepath.Join(dir, "internal", "MNSet", "alarm", "fusion6.alarm.json")
			if _, err := os.Stat(cand); err == nil {
				return cand
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	t.Skip("the shipped template is not beside this test — set MNSET_ALARM_TEMPLATE")
	return ""
}

func TestInfoNamesTheModule(t *testing.T) {
	h := host(t)
	out := mustRun(t, 60*time.Second, "consumer", "mnset", "info", h)
	for _, want := range []string{"slots", "slot  0"} {
		if !strings.Contains(out, want) {
			t.Errorf("FAIL-real: info does not report %q:\n%s", want, out)
		}
	}
}

func TestWalkReadsTheWholeModule(t *testing.T) {
	h := host(t)
	out := mustRun(t, 5*time.Minute, "consumer", "mnset", "walk", h, "--slot", "0")
	// The FusioN6 publishes ~7 200 leaves; a walk that comes back with
	// a few hundred means it stopped early and said nothing about it.
	if n := strings.Count(out, "\n"); n < 5000 {
		t.Errorf("FAIL-real: walk returned %d lines — the module's tree is far larger", n)
	}
	for _, want := range []string{"self", "port", "flows", "refclk"} {
		if !strings.Contains(out, want) {
			t.Errorf("FAIL-real: walk shows no %q branch", want)
		}
	}
}

func TestExportIsReadableAndCarriesUnits(t *testing.T) {
	h := host(t)
	out := filepath.Join(t.TempDir(), "module.csv")
	mustRun(t, 5*time.Minute, "consumer", "mnset", "export", h, "--format", "csv", "--out", out)

	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("FAIL-real: %v", err)
	}
	defer func() { _ = f.Close() }()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("FAIL-real: the export is not readable CSV: %v", err)
	}
	if len(rows) < 5000 {
		t.Errorf("FAIL-real: export has %d rows", len(rows))
	}
	head := rows[0]
	idx := map[string]int{}
	for i, c := range head {
		idx[c] = i
	}
	for _, col := range []string{"path", "value", "unit", "access"} {
		if _, ok := idx[col]; !ok {
			t.Fatalf("FAIL-real: export has no %q column: %v", col, head)
		}
	}
	// The dictionary's whole job: units and access on the leaves an
	// operator reads. An export with neither walks fine and tells
	// nobody anything.
	var withUnit, readOnly int
	for _, r := range rows[1:] {
		if r[idx["unit"]] != "" {
			withUnit++
		}
		if !strings.Contains(r[idx["access"]], "W") {
			readOnly++
		}
	}
	if withUnit == 0 || readOnly == 0 {
		t.Errorf("FAIL-real: export carries %d units and %d read-only leaves", withUnit, readOnly)
	}
}

func TestGetByPathReadsOneLeaf(t *testing.T) {
	h := host(t)
	out := mustRun(t, 60*time.Second, "consumer", "mnset", "get", h,
		"--path", "self.information.base_type")
	if !strings.Contains(out, "FusioN") && !strings.Contains(out, "MuoN") {
		t.Errorf("FAIL-real: base_type = %q", strings.TrimSpace(out))
	}
}

func TestAPathTheModuleDoesNotHaveIsRefused(t *testing.T) {
	h := host(t)
	// FAIL-expected: the connector must reject this, and say which
	// path it could not find rather than returning an empty value.
	out, err := run(t, 60*time.Second, "consumer", "mnset", "get", h,
		"--path", "self.information.no_such_leaf")
	if err == nil {
		t.Errorf("FAIL-real: a leaf that does not exist was accepted:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "no_such_leaf") {
		t.Errorf("FAIL-real: the refusal does not name the path:\n%s", out)
	}
}

func TestEnsureIsIdempotentOnTheDevice(t *testing.T) {
	h := host(t)
	// Read a writable leaf, write back the value it already has, and
	// require the connector to report no change. This is ADR-0007 on a
	// real device: converge, do not disturb.
	// The module's own hostname: writable on every eMSFP, and writing
	// back the value it already has changes nothing on air — which is
	// the only kind of write a test may do to a device carrying
	// programme.
	const path = "self.ipconfig.hostname"
	cur, err := run(t, 60*time.Second, "consumer", "mnset", "get", h, "--path", path)
	if err != nil {
		t.Skipf("the module does not expose %s: %s", path, strings.TrimSpace(cur))
	}
	value := strings.TrimSpace(cur)
	if i := strings.LastIndex(value, "="); i >= 0 {
		value = strings.TrimSpace(value[i+1:])
	}
	value = strings.Trim(value, `"`)
	if value == "" {
		t.Skipf("no readable value at %s", path)
	}

	first := mustRun(t, 60*time.Second, "consumer", "mnset", "ensure", h,
		"--path", path, "--value", value)
	if !strings.Contains(first, "changed=false") && !strings.Contains(first, "already") {
		t.Errorf("FAIL-real: ensure to the current value reported a change:\n%s", first)
	}
	second := mustRun(t, 60*time.Second, "consumer", "mnset", "ensure", h,
		"--path", path, "--value", value)
	if !strings.Contains(second, "changed=false") && !strings.Contains(second, "already") {
		t.Errorf("FAIL-real: a second ensure reported a change:\n%s", second)
	}
}

func TestWatchPollsAndJudges(t *testing.T) {
	h := host(t)
	// A short watch over the branch the alarm template covers: it must
	// poll (the module pushes nothing), print values, and load the
	// rules rather than running blind. The cadence comes from the
	// dictionary's poll plan — this connector takes it from the model,
	// not from a flag.
	out := runFor(t, 90*time.Second, "consumer", "mnset", "watch", h, "--path", "port")
	if !strings.Contains(out, "watching") {
		t.Fatalf("FAIL-real: watch never started:\n%s", out)
	}
	if !strings.Contains(out, "alarms:") {
		t.Errorf("FAIL-real: watch loaded no alarm rules — not even the built-in default:\n%s", out)
	}
	if !strings.Contains(out, "port.") {
		t.Errorf("FAIL-real: watch printed no polled value:\n%s", out)
	}
}

func TestAlarmTemplateJudgesTheLiveModule(t *testing.T) {
	h := host(t)
	tpl := templatePath(t)

	// The shipped rules, against the values the module has right now.
	// Not "does it alarm" — whether it can read the module and judge the
	// same objects the template names.
	out := mustRun(t, 60*time.Second, "consumer", "mnset", "alarm", "list", "--template", tpl)
	if !strings.Contains(out, "rule(s)") {
		t.Fatalf("FAIL-real: the shipped template did not load:\n%s", out)
	}
	live := mustRun(t, 60*time.Second, "consumer", "mnset", "get", h,
		"--path", "telemetry.node.interfaces.e1.link_status")
	value := strings.TrimSpace(live)
	if i := strings.LastIndex(value, "="); i >= 0 {
		value = strings.TrimSpace(strings.Trim(value[i+1:], ` "`))
	}
	verdict := mustRun(t, 30*time.Second, "consumer", "mnset", "alarm", "test",
		"--template", tpl, "--path", "telemetry.node.interfaces.e1.link_status", "--value", value)
	if value == "up" && !strings.Contains(verdict, "normal") {
		t.Errorf("FAIL-real: e1 is up and the template says %q", strings.TrimSpace(verdict))
	}
	if value != "up" && !strings.Contains(verdict, "critical") {
		t.Errorf("FAIL-real: e1 is %q and the template says %q", value, strings.TrimSpace(verdict))
	}
}
