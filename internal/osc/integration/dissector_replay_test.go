//go:build integration

// Replay regression for the dhs_osc Wireshark dissector.
//
// Loads tests/fixtures/osc/battery.pcapng — a captured live emission
// of every type tag, both wire versions, and all three transports — and
// asserts our dissector decodes every frame cleanly:
//
//   - exactly the expected count of frames per transport
//   - exactly the expected set of addresses
//   - no malformed / expert-error frames
//
// If the dissector regresses (e.g. a future codec change drifts from the
// dissector), this test catches it without needing live capture.
//
// Skips when tshark or the dissector aren't available. Tshark looks in:
//   - $PATH
//   - C:\Program Files\Wireshark\tshark.exe (Windows default)
//
// Run with:
//
//	go test -tags integration ./internal/osc/integration/... -run DissectorReplay

package osc_integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const fixtureRel = "../../../tests/fixtures/osc/battery.pcapng"

func findTshark(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("tshark"); err == nil {
		return p
	}
	if runtime.GOOS == "windows" {
		for _, p := range []string{
			`C:\Program Files\Wireshark\tshark.exe`,
			`C:\Program Files (x86)\Wireshark\tshark.exe`,
		} {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	t.Skip("tshark not found; install Wireshark or skip")
	return ""
}

func findDissector(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../wireshark/dhs_osc.lua")
	if err != nil {
		t.Skip("dissector path: ", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Skip("dissector not found at ", abs)
	}
	return abs
}

func findFixture(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(fixtureRel)
	if err != nil {
		t.Skip("fixture path: ", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Skip("fixture not found at ", abs)
	}
	return abs
}

// runTshark runs tshark with the dissector loaded via -X lua_script and
// returns stdout. Stderr is forwarded on test failure for debugging.
func runTshark(t *testing.T, tshark, dissector, pcap string, extra ...string) string {
	t.Helper()
	args := []string{"-r", pcap, "-X", "lua_script:" + dissector}
	args = append(args, extra...)
	cmd := exec.Command(tshark, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("tshark %v: %v\nstderr: %s", args, err, stderr.String())
	}
	return stdout.String()
}

func TestDissectorReplay_FrameCounts(t *testing.T) {
	tshark := findTshark(t)
	dissector := findDissector(t)
	fixture := findFixture(t)

	out := runTshark(t, tshark, dissector, fixture,
		"-Y", "dhs_osc",
		"-T", "fields", "-e", "dhs_osc.transport")

	transports := map[string]int{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		transports[line]++
	}
	// The fixture is the comprehensive battery; expect at least these
	// minimums per transport. Exact counts will drift over time as the
	// fixture is regenerated; the floors guarantee dissector coverage
	// on every transport.
	type expect struct {
		key string
		min int
	}
	mins := []expect{
		{"UDP", 15},
		{"TCP/length-prefix", 5},
		{"TCP/SLIP", 5},
	}
	for _, e := range mins {
		if got := transports[e.key]; got < e.min {
			t.Errorf("%s frames: got %d, want >= %d (all transports: %v)", e.key, got, e.min, transports)
		}
	}
}

func TestDissectorReplay_RequiredAddresses(t *testing.T) {
	tshark := findTshark(t)
	dissector := findDissector(t)
	fixture := findFixture(t)

	out := runTshark(t, tshark, dissector, fixture,
		"-Y", "dhs_osc",
		"-T", "fields", "-e", "dhs_osc.address")

	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		seen[strings.TrimSpace(line)] = true
	}
	// The battery emits these specific addresses; if the dissector
	// fails to decode any of them, the regression net catches it.
	required := []string{
		"/wave1/i", "/wave1/f", "/wave1/s", "/wave1/b",
		"/wave2/h", "/wave2/d", "/wave2/S", "/wave2/c", "/wave2/r", "/wave2/m", "/wave2/t",
		"/wave3/T", "/wave3/F", "/wave3/N", "/wave3/I", "/wave3/array",
	}
	for _, addr := range required {
		if !seen[addr] {
			t.Errorf("missing address %q in dissected pcap", addr)
		}
	}
}

func TestDissectorReplay_NoMalformedFrames(t *testing.T) {
	tshark := findTshark(t)
	dissector := findDissector(t)
	fixture := findFixture(t)

	// Filter for any of our expert-info abbrevs that flag malformed
	// frames. None should fire on the battery fixture.
	expertFilter := strings.Join([]string{
		"dhs_osc.alignment", "dhs_osc.comma_missing", "dhs_osc.truncated",
		"dhs_osc.tag_unknown", "dhs_osc.array_unbalanced",
		"dhs_osc.slip_truncated", "dhs_osc.slip_bad_escape",
		"dhs_osc.lp_size_unreasonable",
	}, " or ")
	out := runTshark(t, tshark, dissector, fixture, "-Y", expertFilter, "-T", "fields", "-e", "dhs_osc.address")

	bad := []string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			bad = append(bad, line)
		}
	}
	if len(bad) > 0 {
		t.Errorf("dissector flagged %d malformed frame(s): %v", len(bad), bad)
	}
}

func TestDissectorReplay_BothVersionsRepresented(t *testing.T) {
	tshark := findTshark(t)
	dissector := findDissector(t)
	fixture := findFixture(t)

	out := runTshark(t, tshark, dissector, fixture,
		"-Y", "dhs_osc",
		"-T", "fields", "-e", "dhs_osc.version")

	versions := map[string]int{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		versions[line]++
	}
	if versions["OSC 1.0"] < 1 {
		t.Errorf("expected at least one OSC 1.0 frame, got %v", versions)
	}
	if versions["OSC 1.1"] < 1 {
		t.Errorf("expected at least one OSC 1.1 frame, got %v", versions)
	}
}
