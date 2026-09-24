//go:build integration

// Package snmp_integration drives the real dhs CLI against a real SNMP
// agent — ADR-0025 deliverable 3, tier 2/3. Two oracles, because this
// connector has two halves:
//
//   - a real device (ATEME Kyrion DR5000, SNMP v1) for the manager;
//   - our own agent for the round trip a device cannot be asked to do
//     (a write we may repeat, a trap we may provoke).
//
// Gated on SNMP_TEST_HOST, so CI's unit run never reaches a device
// (root CLAUDE.md: "CI runs unit only — never integration"):
//
//	SNMP_TEST_HOST=10.6.255.114 go test -tags integration ./internal/snmp/integration/
//
// DHS_BIN points at a prebuilt CLI for hosts with no Go toolchain —
// the control node is one, and it is the host with a route to the
// fleet.
//
// Every check says why on a non-pass, in ADR-0025's vocabulary:
// PASS / FAIL-real / FAIL-expected / TIMEOUT.
package snmp_integration

import (
	"context"
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

// agent is the device under test, or "" when this suite should not run.
func agent(t *testing.T) string {
	t.Helper()
	h := strings.TrimSpace(os.Getenv("SNMP_TEST_HOST"))
	if h == "" {
		t.Skip("SNMP_TEST_HOST is not set — this suite needs a real agent")
	}
	return h
}

// writeCommunity is the one that may SET. Absent means the write
// checks are skipped rather than guessed at.
func writeCommunity(t *testing.T) string {
	t.Helper()
	c := strings.TrimSpace(os.Getenv("SNMP_WRITE_COMMUNITY"))
	if c == "" {
		t.Skip("SNMP_WRITE_COMMUNITY is not set — a write needs the agent's own password")
	}
	return c
}

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

// binary is the CLI under test. DHS_BIN takes a prebuilt one: the host
// with a route to the fleet has no Go toolchain, and without this the
// only machine that can reach the device could not run the suite.
func binary(t *testing.T) string {
	t.Helper()
	if b := strings.TrimSpace(os.Getenv("DHS_BIN")); b != "" {
		if _, err := os.Stat(b); err != nil {
			t.Fatalf("FAIL-real: DHS_BIN=%s: %v", b, err)
		}
		return b
	}
	buildOnce.Do(func() {
		out := filepath.Join(t.TempDir(), "dhs")
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

func run(t *testing.T, timeout time.Duration, env []string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary(t), args...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("TIMEOUT: %s did not answer within %s\n%s", strings.Join(args, " "), timeout, out)
	}
	return string(out), err
}

func mustRun(t *testing.T, timeout time.Duration, args ...string) string {
	t.Helper()
	out, err := run(t, timeout, nil, args...)
	if err != nil {
		t.Fatalf("FAIL-real: %s\n%v\n%s", strings.Join(args, " "), err, out)
	}
	return out
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

// valueOf reads the value out of a `get` line. The CLI prints an
// enumerated object as its own word plus the ordinal it came from —
//
//	value = "hdsdi"  (enum idx 3)
//
// and a write takes the word, so the annotation goes first and the
// quotes after it. Getting that order wrong sends `hdsdi"` to a device.
func valueOf(out string) string {
	v := strings.TrimSpace(out)
	if i := strings.LastIndex(v, "="); i >= 0 {
		v = strings.TrimSpace(v[i+1:])
	}
	if i := strings.Index(v, "("); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return strings.Trim(v, `"`)
}

func TestTheAgentAnswersItsSystemGroup(t *testing.T) {
	h := agent(t)
	out := mustRun(t, 60*time.Second, "consumer", "snmp", "get", "--version", "1", h)
	for _, want := range []string{"sysDescr.0", "sysObjectID.0", "sysUpTime.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("FAIL-real: the system group has no %s:\n%s", want, out)
		}
	}
}

func TestInfoSeesOneSlotAndTheVendorBranch(t *testing.T) {
	h := agent(t)
	out := mustRun(t, 60*time.Second, "consumer", "snmp", "info", h)
	if !strings.Contains(out, "slots        1") {
		t.Errorf("FAIL-real: an agent is one box, one slot:\n%s", out)
	}
	// dtd_version carries sysObjectID: the branch the model is read from.
	if !strings.Contains(out, "1.3.6.1.4.1.") {
		t.Errorf("FAIL-real: info does not report the enterprise branch:\n%s", out)
	}
}

func TestAVersionTheAgentDoesNotSpeakIsNotSilence(t *testing.T) {
	h := agent(t)
	// FAIL-expected on a v1-only agent: v2c gets no reply, and the
	// connector must say which versions it tried rather than
	// reporting a device that is up as down. The DR5000 is v1-only.
	out, err := run(t, 60*time.Second, nil, "consumer", "snmp", "get", "--version", "2c",
		"--oid", "sysDescr.0", "--timeout", "2s", "--retries", "0", h)
	if err == nil {
		t.Skipf("this agent answers v2c as well — nothing to assert:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "timeout") && !strings.Contains(out, "no reply") {
		t.Errorf("FAIL-real: a version the agent ignores should read as silence, not as %q", strings.TrimSpace(out))
	}
	// The neutral connector tries both and says so.
	out2, err2 := run(t, 90*time.Second, nil, "consumer", "snmp", "info", h)
	if err2 != nil {
		t.Errorf("FAIL-real: the connector must fall back to v1:\n%s", out2)
	}
}

func TestNamesResolveWithoutAWalk(t *testing.T) {
	h := agent(t)
	// PathNative: the compiled MIB is the map, so a name resolves on a
	// cold connection in one GET. A connector that walked 26 000
	// objects to find one leaf would be unusable on this device.
	start := time.Now()
	out := mustRun(t, 60*time.Second, "consumer", "snmp", "get", h, "--path", "sysDescr")
	if d := time.Since(start); d > 30*time.Second {
		t.Errorf("FAIL-real: one named GET took %s — something is walking", d)
	}
	if !strings.Contains(out, "value") {
		t.Errorf("FAIL-real: %s", strings.TrimSpace(out))
	}
}

func TestAnOIDNoAgentHasIsRefused(t *testing.T) {
	h := agent(t)
	// FAIL-expected: the agent answers noSuchName and the connector
	// must surface that rather than an empty value.
	out, err := run(t, 60*time.Second, nil, "consumer", "snmp", "get", "--version", "1",
		"--oid", "1.3.6.1.4.1.27338.9.9.9.9", h)
	if err == nil && !strings.Contains(strings.ToLower(out), "nosuch") {
		t.Errorf("FAIL-real: an OID the agent does not have was accepted:\n%s", out)
	}
}

func TestWalkScopedToABranchIsFast(t *testing.T) {
	h := agent(t)
	// The whole device is ~26 000 objects; a scoped walk must not pay
	// for the ones nobody asked about.
	start := time.Now()
	out := mustRun(t, 3*time.Minute, "consumer", "snmp", "walk", "--version", "1",
		"--oid", "1.3.6.1.2.1.1", h)
	if !strings.Contains(out, "sysDescr") {
		t.Errorf("FAIL-real: the system group walk shows no sysDescr:\n%s", out)
	}
	if d := time.Since(start); d > 2*time.Minute {
		t.Errorf("FAIL-real: walking six objects took %s", d)
	}
}

func TestWriteIsConfirmedByAReadBack(t *testing.T) {
	h := agent(t)
	community := writeCommunity(t)
	path := strings.TrimSpace(os.Getenv("SNMP_WRITABLE_PATH"))
	if path == "" {
		t.Skip("SNMP_WRITABLE_PATH is not set — a write test needs an object this plant agrees may be written")
	}
	// Read it, write back the value it already has, and require the
	// connector to confirm by reading again. Nothing changes on the
	// device: the only safe write against kit in service.
	cur := mustRun(t, 60*time.Second, "consumer", "snmp", "get", h, "--path", path)
	value := valueOf(cur)
	if value == "" {
		t.Skipf("no readable value at %s", path)
	}

	out, err := run(t, 60*time.Second, []string{"SNMP_WRITE_COMMUNITY=" + community},
		"consumer", "snmp", "set", h, "--path", path, "--value", value)
	if err != nil {
		t.Fatalf("FAIL-real: %s = %q refused:\n%s", path, value, out)
	}
	if !strings.Contains(out, "confirmed") {
		t.Errorf("FAIL-real: a write must be confirmed by a read-back, got:\n%s", out)
	}
	if !strings.Contains(out, value) {
		t.Errorf("FAIL-real: the confirm read says %q, not %q", strings.TrimSpace(out), value)
	}
}

func TestAWriteOnTheReadCommunityIsRefused(t *testing.T) {
	h := agent(t)
	path := strings.TrimSpace(os.Getenv("SNMP_WRITABLE_PATH"))
	if path == "" {
		t.Skip("SNMP_WRITABLE_PATH is not set")
	}
	// FAIL-expected: the agent is right to say no, and the connector
	// must pass that on rather than reporting success.
	out, err := run(t, 60*time.Second, []string{"SNMP_WRITE_COMMUNITY="},
		"consumer", "snmp", "set", h, "--path", path, "--value", "1")
	if err == nil {
		t.Errorf("FAIL-real: a write without the write community was accepted:\n%s", out)
	}
	low := strings.ToLower(out)
	if !strings.Contains(low, "noaccess") && !strings.Contains(low, "notwritable") &&
		!strings.Contains(low, "nosuchname") && !strings.Contains(low, "timeout") {
		t.Errorf("FAIL-real: the refusal does not name what the agent said:\n%s", out)
	}
}

func TestWatchPollsTheAgentAtTheOperatorsCadence(t *testing.T) {
	h := agent(t)
	scope := strings.TrimSpace(os.Getenv("SNMP_WATCH_PATH"))
	if scope == "" {
		scope = "system"
	}
	out := runFor(t, 75*time.Second, "consumer", "snmp", "watch", h,
		"--path", scope, "--interval", "5s")
	if !strings.Contains(out, "watching") {
		t.Fatalf("FAIL-real: watch never started:\n%s", out)
	}
	if !strings.Contains(out, "alarms:") {
		t.Errorf("FAIL-real: no alarm rules loaded — not even the built-in default:\n%s", out)
	}
	// SNMP pushes nothing, so every line here is a poll that happened.
	if strings.Count(out, "live") < 2 {
		t.Errorf("FAIL-real: fewer than two polled samples in 75 s:\n%s", out)
	}
}

func TestTheShippedTemplateJudgesTheLiveAgent(t *testing.T) {
	h := agent(t)
	tpl := strings.TrimSpace(os.Getenv("SNMP_ALARM_TEMPLATE"))
	if tpl == "" {
		tpl = filepath.Join(repoRoot(t), "internal", "snmp", "alarm", "DR5000@1.3.1.1.json")
	}
	if _, err := os.Stat(tpl); err != nil {
		t.Skipf("no shipped template at %s", tpl)
	}
	out := mustRun(t, 60*time.Second, "consumer", "snmp", "alarm", "list", "--template", tpl)
	if !strings.Contains(out, "rule(s)") {
		t.Fatalf("FAIL-real: the template did not load:\n%s", out)
	}
	// The lock state the device has right now, judged by the rules.
	live, err := run(t, 60*time.Second, nil, "consumer", "snmp", "get", h,
		"--path", "ateme.dr5000.Status.Input.Sat.Locked")
	if err != nil {
		t.Skipf("this agent is not the DR5000: %s", strings.TrimSpace(live))
	}
	value := valueOf(live)
	if value != "true" && value != "false" {
		t.Skipf("the lock object read %q", value)
	}
	verdict := mustRun(t, 30*time.Second, "consumer", "snmp", "alarm", "test",
		"--template", tpl, "--path", "ateme.dr5000.Status.Input.Sat.Locked", "--value", value)
	if value == "true" && !strings.Contains(verdict, "normal") {
		t.Errorf("FAIL-real: the demod is locked and the template says %q", strings.TrimSpace(verdict))
	}
	if value == "false" && !strings.Contains(verdict, "critical") {
		t.Errorf("FAIL-real: the demod is unlocked and the template says %q", strings.TrimSpace(verdict))
	}
}
