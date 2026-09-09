// Command gencli regenerates docs/cli.md — the complete `dhs` verb
// reference — by running the REAL binary's help output for every
// entry of the verb matrix below. The document therefore cannot drift
// from the code: what -h prints IS what the page shows.
//
//	go run ./tools/gencli -dhs <path-to-dhs-binary> > docs/cli.md
//
// CI regenerates and diffs it; a verb added without updating the
// matrix here fails the freshness check the moment its help text
// differs from nothing.
//
// # Contract
//
// The document goes to stdout and nothing else does, so the command
// can be redirected straight into the file. Diagnostics go to stderr.
//
//	0  the reference was written
//	2  the command could not run: no -dhs, or a binary that will not start
//
// A verb whose help exits non-zero is NOT an error here: several verbs
// answer -h through flag.ErrHelp and exit 2 while printing exactly the
// text this page exists to show. What is an error is a binary that
// cannot be started at all — that produces a page of empty code blocks
// that CI would then diff against the real one, reporting every verb
// as changed when nothing changed but the invocation.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// helpEntry is one section of the reference: a title and the argv
// whose output (stdout+stderr — help goes to either) fills it.
type helpEntry struct {
	title string
	argv  []string
}

// The matrix. Grouped the way an operator thinks: the roles first,
// then per-protocol surfaces, then the cross-cutting tools.
var matrix = []helpEntry{
	{"dhs (root)", []string{"--help"}},
	{"Consumer verbs", []string{"consumer", "--help"}},
	{"Producer verbs", []string{"producer", "--help"}},

	// AMWA NMOS — the three roles.
	{"NMOS node (producer)", []string{"producer", "nmos", "serve", "--help"}},
	{"NMOS registry", []string{"registry", "nmos", "serve", "--help"}},
	{"NMOS registry mirror", []string{"registry", "nmos", "mirror", "--help"}},
	{"NMOS controller: walk", []string{"consumer", "nmos", "walk", "--help"}},
	{"NMOS controller: watch", []string{"consumer", "nmos", "watch", "--help"}},
	{"NMOS controller: connect (IS-05)", []string{"consumer", "nmos", "connect", "--help"}},
	{"NMOS controller: set", []string{"consumer", "nmos", "set", "--help"}},
	{"NMOS controller: events (IS-07)", []string{"consumer", "nmos", "events", "--help"}},
	{"NMOS plant export", []string{"consumer", "nmos", "export", "--help"}},
	{"NMOS plant audit", []string{"consumer", "nmos", "audit", "--help"}},
	{"NMOS live probe", []string{"consumer", "nmos", "probe", "--help"}},
	{"NMOS parameter registers", []string{"consumer", "nmos", "registers", "--help"}},

	// The generic consumer verb set (shape shared by acp1/acp2/emberplus).
	{"consumer info", []string{"consumer", "acp1", "info", "--help"}},
	{"consumer walk", []string{"consumer", "acp1", "walk", "--help"}},
	{"consumer get", []string{"consumer", "acp1", "get", "--help"}},
	{"consumer set", []string{"consumer", "acp1", "set", "--help"}},
	{"consumer watch", []string{"consumer", "acp1", "watch", "--help"}},
	{"consumer export", []string{"consumer", "acp1", "export", "--help"}},
	{"consumer import", []string{"consumer", "acp1", "import", "--help"}},
	{"consumer matrix", []string{"consumer", "emberplus", "matrix", "--help"}},
	{"consumer invoke", []string{"consumer", "emberplus", "invoke", "--help"}},

	// Per-protocol surfaces with their own verb sets.
	{"probel-sw08p", []string{"consumer", "probel-sw08p", "--help"}},
	{"probel-sw02p", []string{"consumer", "probel-sw02p", "--help"}},
	{"cerebrum-nb", []string{"consumer", "cerebrum-nb", "--help"}},
	{"osc", []string{"consumer", "osc", "--help"}},
	{"tsl", []string{"consumer", "tsl", "--help"}},

	// Cross-cutting tools.
	{"list-commands", []string{"list-commands", "--help"}},
	{"metrics", []string{"metrics", "--help"}},
}

// helpOf runs one matrix entry and returns everything it printed.
//
// Behind a package variable so the generator can be exercised without a
// built binary: what this command does with the output is its own
// business, and it is separable from being able to obtain it.
// Production never reassigns it.
var helpOf = func(bin string, argv []string) ([]byte, error) {
	out, err := exec.Command(bin, argv...).CombinedOutput() //nolint:gosec // argv is the fixed matrix above
	// A non-zero exit is normal: several verbs answer -h through
	// flag.ErrHelp and exit 2 while printing exactly what this page
	// shows. Only a command that could not be STARTED is an error.
	var ee *exec.ExitError
	if err != nil && errors.As(err, &ee) {
		err = nil
	}
	return out, err
}

// osExit is os.Exit behind a package variable, so main itself can be
// exercised rather than only the function under it.
// Production never reassigns it.
var osExit = os.Exit

func main() { osExit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(argv []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gencli", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dhs := fs.String("dhs", "", "path to the dhs binary (required)")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if *dhs == "" {
		_, _ = fmt.Fprintln(stderr, "gencli: -dhs <binary> is required")
		return 2
	}

	var b strings.Builder
	b.WriteString("# dhs CLI reference\n\n")
	b.WriteString("<!-- GENERATED by tools/gencli — do not edit. Regenerate with:\n")
	b.WriteString("     go build -o /tmp/dhs ./cmd/dhs && go run ./tools/gencli -dhs /tmp/dhs > docs/cli.md -->\n\n")
	b.WriteString("Every verb accepts `--settings <file.yaml>` (or `DHS_SETTINGS`): a FLAT\n")
	b.WriteString("YAML of `flag-name: value` defaults, names exactly as `-h` prints them.\n")
	b.WriteString("Precedence: explicit flags > settings file > built-in defaults. Keys that\n")
	b.WriteString("belong to other verbs are ignored, so one file serves a deployment —\n")
	b.WriteString("Ansible templates render the same shape.\n\n")
	b.WriteString("## Contents\n\n")
	for _, e := range matrix {
		fmt.Fprintf(&b, "- [%s](#%s)\n", e.title, anchor(e.title))
	}
	b.WriteString("\n")

	for _, e := range matrix {
		out, err := helpOf(*dhs, e.argv)
		if err != nil {
			// A binary that will not start yields a page of empty code
			// blocks, which CI then diffs against the real one and
			// reports every verb as changed. Refusing here says what
			// actually went wrong.
			_, _ = fmt.Fprintf(stderr, "gencli: %s: %v\n", *dhs, err)
			return 2
		}
		fmt.Fprintf(&b, "## %s\n\n`dhs %s`\n\n```text\n%s\n```\n\n",
			e.title, strings.Join(e.argv, " "), strings.TrimRight(string(out), "\n"))
	}
	_, _ = fmt.Fprint(stdout, b.String())
	return 0
}

// anchor renders a GitHub-style heading anchor.
func anchor(title string) string {
	s := strings.ToLower(title)
	var out []rune
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			out = append(out, r)
		case r == ' ' || r == '-':
			out = append(out, '-')
		}
	}
	return string(out)
}
