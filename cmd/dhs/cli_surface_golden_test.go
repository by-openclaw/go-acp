package main

// CLI-surface freeze — the no-regression oracle for the per-verb refactor.
//
// The refactor moves every verb into its own module and replaces the two
// parallel dispatch worlds (the generic commands[] table and the per-protocol
// switches) with one self-registering verb registry. That is a large,
// mechanical restructuring, and the ONLY thing that makes it safe is a
// byte-exact record of what the CLI promised BEFORE it started.
//
// This test drives the real binary across every documented entry point —
// top-level help, the protocol list, every consumer verb's --help, every
// producer serve --help — concatenates the results, and compares them to
// testdata/golden/cli-surface.txt. Any change to a verb name, a flag name, a
// default value, or a usage line fails the test.
//
// Regenerate deliberately (and review the diff) with:
//
//	go test ./cmd/dhs -run TestCLISurfaceGolden -update-cli-surface
//
// A diff here is not automatically a bug — but it is always a decision.

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

var updateCLISurface = flag.Bool("update-cli-surface", false,
	"regenerate cmd/dhs/testdata/golden/cli-surface.txt from the current binary")

// goldenCLISurfacePath is the frozen CLI contract.
var goldenCLISurfacePath = filepath.Join("testdata", "golden", "cli-surface.txt")

// perInvocationTimeout caps a single `dhs ... --help` run. Help must never
// touch the network; a verb that blocks here is itself the finding.
const perInvocationTimeout = 20 * time.Second

// genericVerbs is the commands[] dispatch table (main.go). These route through
// the shared addCommonFlags/connect() path for acp1 / acp2 / emberplus.
var genericVerbs = []string{
	"info", "walk", "tree", "get", "set", "inc", "dec", "reset", "ensure",
	"watch", "export", "import", "extract", "diff", "convert", "discover",
	"matrix", "usage", "replace", "invoke", "stream", "profile", "diag",
	"validate", "health", "status", "bench",
}

// emberplusOnlyVerbs / acp2OnlyVerbs are captured under their owning protocol
// too, because their flag sets differ from the acp1 rendering.
var emberplusOnlyVerbs = []string{"matrix", "invoke", "stream", "profile", "bench", "usage", "replace"}

var acp2OnlyVerbs = []string{"diag"}

// perProtocolVerbs are the connectors that own a private dispatch switch
// instead of the generic table. Killing this split is the point of the
// refactor, so their surface is frozen verb by verb.
var perProtocolVerbs = map[string][]string{
	"cerebrum-nb": {
		"connect", "validate", "listen", "list-devices", "device-details",
		"device-value", "list-categories", "category-details",
		"list-salvo-groups", "list-salvo-instances", "salvo-instance-details",
		"keepalive-probe", "tree", "get", "extract", "watch", "route", "usage",
		"replace", "export", "list-sources", "list-dests", "list-levels",
		"import", "lock", "unlock", "device-config", "set-mnemonic", "set-tags",
		"salvo", "category", "set-value", "obtain-datastore",
	},
	"probel-sw08p": {
		"interrogate", "connect", "watch", "maintenance", "dual-status",
		"tally-dump", "protect-interrogate", "protect-connect",
		"protect-disconnect", "protect-name", "protect-dump", "master-protect",
		"discover", "all-source-names", "single-source-name", "all-dest-names",
		"single-dest-name", "all-source-assoc-names", "single-source-assoc-name",
		// hard-reset / soft-reset / clear-protects / database-transfer are
		// ARGUMENTS to `maintenance`, not verbs of their own.
		"update-name", "bench", "export", "import", "salvo-connect", "usage",
		"replace",
	},
	"probel-sw02p": {
		"interrogate", "connect", "connect-on-go", "go", "salvo-connect",
		"protect-connect", "protect-disconnect", "protect-interrogate",
		"protect-dump", "protect-name", "dual-status", "lock-status", "status",
		"router-config", "watch", "usage", "replace", "export", "import",
		"discover",
	},
	// osc consumer is watch (alias: listen) + validate. send / fader / serve
	// are PRODUCER verbs and are captured under producerVerbs.
	"osc-v10": {"watch", "listen", "validate"},
	"osc-v11": {"watch", "listen", "validate"},
	// tsl consumer is listen (alias: watch) + validate, one entry per wire
	// version. These were missing from the first freeze, so a whole
	// connector's consumer surface was unprotected.
	"tsl-v31": {"listen", "watch", "validate"},
	"tsl-v40": {"listen", "watch", "validate"},
	"tsl-v50": {"listen", "watch", "validate"},
	"nmos": {
		"discover", "system", "walk", "watch", "connect", "set", "facade",
		"events", "export", "audit", "probe", "registers",
	},
	"ccm": {"walk", "export"},
}

// producerVerbs freezes the producer role. The producer is selectable at build
// time after the refactor, so its surface matters as much as the consumer's.
var producerVerbs = map[string][]string{
	"acp1":         {"serve"},
	"acp2":         {"serve"},
	"emberplus":    {"serve"},
	"probel-sw08p": {"serve"},
	"probel-sw02p": {"serve"},
	"tsl-v31":      {"send", "serve"},
	"tsl-v40":      {"send", "serve"},
	"tsl-v50":      {"send", "serve"},
	"osc-v10":      {"send", "fader", "serve"},
	"osc-v11":      {"send", "fader", "serve"},
	"nmos":         {"serve", "events"},
}

// registryVerbs freezes the THIRD role — `dhs registry nmos <verb>`. NMOS
// splits into node / registry / controller responsibilities, and the registry
// already has its own dispatcher, so it is frozen as its own role rather than
// being folded into producer.
var registryVerbs = map[string][]string{
	"nmos": {"serve", "mirror"},
}

func TestCLISurfaceGolden(t *testing.T) {
	bin := buildDHSBinary(t)

	type invocation struct {
		label string
		args  []string
	}
	var invs []invocation
	add := func(label string, args ...string) {
		invs = append(invs, invocation{label: label, args: args})
	}

	// 1. Top-level surface.
	add("top-help", "help")
	add("list-protocols", "list-protocols")

	// 2. Generic verb table, rendered under acp1 (the gold-template connector).
	for _, v := range genericVerbs {
		add("consumer/acp1/"+v, "consumer", "acp1", v, "--help")
	}
	for _, v := range emberplusOnlyVerbs {
		add("consumer/emberplus/"+v, "consumer", "emberplus", v, "--help")
	}
	for _, v := range acp2OnlyVerbs {
		add("consumer/acp2/"+v, "consumer", "acp2", v, "--help")
	}

	// 3. Every connector that owns a private dispatch switch.
	for _, proto := range sortedKeys(perProtocolVerbs) {
		add("consumer/"+proto+"/<dispatcher-help>", "consumer", proto, "--help")
		for _, v := range perProtocolVerbs[proto] {
			add("consumer/"+proto+"/"+v, "consumer", proto, v, "--help")
		}
	}

	// 4. Producer role.
	for _, proto := range sortedKeys(producerVerbs) {
		add("producer/"+proto+"/<dispatcher-help>", "producer", proto, "--help")
		for _, v := range producerVerbs[proto] {
			add("producer/"+proto+"/"+v, "producer", proto, v, "--help")
		}
	}

	// 5. Registry role (NMOS node / registry / controller split).
	for _, proto := range sortedKeys(registryVerbs) {
		add("registry/"+proto+"/<dispatcher-help>", "registry", proto, "--help")
		for _, v := range registryVerbs[proto] {
			add("registry/"+proto+"/"+v, "registry", proto, v, "--help")
		}
	}

	var out bytes.Buffer
	for _, in := range invs {
		out.WriteString("========== " + in.label + " ==========\n")
		out.WriteString("$ dhs " + strings.Join(in.args, " ") + "\n")
		out.WriteString(runCLI(t, bin, in.args))
		out.WriteString("\n")
	}
	got := normalizeCLISurface(out.String(), filepath.Dir(bin))

	if *updateCLISurface {
		if err := os.MkdirAll(filepath.Dir(goldenCLISurfacePath), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(goldenCLISurfacePath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s (%d invocations, %d bytes)", goldenCLISurfacePath, len(invs), len(got))
		return
	}

	wantBytes, err := os.ReadFile(goldenCLISurfacePath)
	if err != nil {
		t.Fatalf("read golden %s: %v\ngenerate it with: go test ./cmd/dhs -run TestCLISurfaceGolden -update-cli-surface",
			goldenCLISurfacePath, err)
	}
	want := string(wantBytes)
	if got == want {
		return
	}
	t.Errorf("CLI surface changed — the frozen contract in %s no longer matches the binary.\n%s",
		goldenCLISurfacePath, firstDiff(want, got))
}

// buildDHSBinary compiles the CLI under test once into the test's temp dir.
func buildDHSBinary(t *testing.T) string {
	t.Helper()
	name := "dhs-surface"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// runCLI executes one invocation and returns its combined output. A non-zero
// exit is part of the frozen surface (several verbs answer --help via
// flag.ErrHelp), so the exit status is recorded rather than failed on.
func runCLI(t *testing.T, bin string, args []string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), perInvocationTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	// Deterministic environment: a stray DHS_SETTINGS on the developer's
	// machine must not leak flag defaults into the frozen contract.
	cmd.Env = append(os.Environ(), "DHS_SETTINGS=", "NO_COLOR=1")
	out, err := cmd.CombinedOutput()

	body := string(out)
	switch {
	case ctx.Err() != nil:
		body += "\n[TIMED OUT after " + perInvocationTimeout.String() + "]\n"
	case err != nil:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			body += "\n[exit status " + strconv.Itoa(ee.ExitCode()) + "]\n"
		} else {
			body += "\n[run error: " + err.Error() + "]\n"
		}
	}
	return body
}

// normalizeCLISurface removes the volatile parts of the captured output so the
// golden is stable across machines, temp dirs and builds.
func normalizeCLISurface(s, binDir string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if binDir != "" {
		s = strings.ReplaceAll(s, binDir, "<BINDIR>")
		s = strings.ReplaceAll(s, filepath.ToSlash(binDir), "<BINDIR>")
	}
	for _, re := range volatilePatterns {
		s = re.ReplaceAllString(s, "<REDACTED>")
	}
	return s
}

var volatilePatterns = []*regexp.Regexp{
	// Build metadata: version / commit / date lines vary per build.
	regexp.MustCompile(`(?m)^(version|commit|built|go)\s+.*$`),
	// Absolute temp paths that survive the binDir replacement.
	regexp.MustCompile(`(?i)[A-Z]:\\Users\\[^\s"]*\\Temp\\[^\s"]*`),
	regexp.MustCompile(`/tmp/[A-Za-z0-9_.-]+`),
}

// firstDiff renders the first differing line with a little context so the
// failure message points straight at the changed verb.
func firstDiff(want, got string) string {
	wl := strings.Split(want, "\n")
	gl := strings.Split(got, "\n")
	n := len(wl)
	if len(gl) < n {
		n = len(gl)
	}
	for i := 0; i < n; i++ {
		if wl[i] != gl[i] {
			var b strings.Builder
			b.WriteString("first difference at line ")
			b.WriteString(strconv.Itoa(i + 1))
			b.WriteString(":\n")
			for j := i - 3; j <= i+3; j++ {
				if j < 0 || j >= n {
					continue
				}
				marker := "  "
				if j == i {
					marker = "! "
				}
				b.WriteString(marker + "want: " + wl[j] + "\n")
				b.WriteString(marker + "got : " + gl[j] + "\n")
			}
			return b.String()
		}
	}
	return "outputs share a common prefix but differ in length: want " +
		strconv.Itoa(len(wl)) + " lines, got " + strconv.Itoa(len(gl)) + " lines"
}

func sortedKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
