package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"dhs/internal/amwa/audit"
)

// runNMOSAudit implements `dhs consumer nmos audit`.
//
// The verb is offline by design. An operator exports a customer's plant
// once — on their site, on their network, with their credentials — and
// the audit then runs anywhere, any number of times, over those bytes.
// That separation is what makes the report something you can attach to
// a ticket and re-run six months later against the same export to prove
// what changed.
func runNMOSAudit(_ context.Context, args []string) (err error) {
	fs := flag.NewFlagSet("consumer nmos audit", flag.ContinueOnError)
	var (
		dir    = fs.String("dir", "", "export directory to audit (required)")
		format = fs.String("format", "text", "output format: text | json | jsonl")
		minSev = fs.String("min-severity", "info", "drop findings below this severity: info | warn | error | critical")
		outPh  = fs.String("out", "", "write the report to this file instead of stdout")
		failOn = fs.String("fail-on", "", "exit non-zero when a finding at or above this severity is present")
		policy = fs.String("policy", "", "site policy JSON (#852): multicast bandwidth classes, expected PTP grandmaster/domain, private/public media plane. Without it, the policy-specific checks report SKIP")
	)

	// A bare positional is the friendlier spelling of --dir, and the one
	// an operator reaches for first. It has to be lifted out BEFORE
	// parsing: Go's flag package stops at the first non-flag argument,
	// so `audit ./export --format jsonl` would otherwise parse zero
	// flags and silently render the wrong format.
	positional := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		positional, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("consumer nmos audit: unexpected argument %q (one directory only)", fs.Arg(0))
	}
	if *dir == "" {
		*dir = positional
	} else if positional != "" {
		return fmt.Errorf("consumer nmos audit: directory given twice (%q and --dir %q)", positional, *dir)
	}
	if *dir == "" {
		return fmt.Errorf("consumer nmos audit: --dir is required (the export directory to audit)")
	}

	sev, err := audit.ParseSeverity(*minSev)
	if err != nil {
		return err
	}
	var gate audit.Severity
	gated := *failOn != ""
	if gated {
		if gate, err = audit.ParseSeverity(*failOn); err != nil {
			return err
		}
	}

	harvests, err := audit.Load(*dir)
	if err != nil {
		return err
	}
	var pol *audit.Policy
	if *policy != "" {
		pol, err = audit.LoadPolicy(*policy)
		if err != nil {
			return fmt.Errorf("consumer nmos audit: %w", err)
		}
	}
	res := audit.Run(harvests, audit.Options{MinSeverity: sev, Policy: pol})

	var w io.Writer = os.Stdout
	if *outPh != "" {
		f, err := os.Create(*outPh)
		if err != nil {
			return fmt.Errorf("consumer nmos audit: %w", err)
		}
		// A failed Close on a report file means the report may be
		// truncated, and a truncated audit reads as a cleaner plant
		// than the capture actually showed. Surface it.
		defer func() {
			if cerr := f.Close(); cerr != nil && err == nil {
				err = fmt.Errorf("consumer nmos audit: writing %s: %w", *outPh, cerr)
			}
		}()
		w = f
	}

	switch *format {
	case "text":
		err = audit.RenderText(w, res)
	case "json":
		err = audit.RenderJSON(w, res)
	case "jsonl":
		err = audit.RenderJSONL(w, res)
	default:
		return fmt.Errorf("consumer nmos audit: unknown --format %q (want text, json or jsonl)", *format)
	}
	if err != nil {
		return err
	}

	// The gate is opt-in. Without --fail-on, an audit that finds
	// problems still exits 0: reporting is not the same as failing, and
	// a CI job decides for itself which severity is a build break.
	if worst, any := res.Worst(); gated && any && worst >= gate {
		return fmt.Errorf("consumer nmos audit: %d finding(s), worst %s (--fail-on %s)", len(res.Findings), worst, gate)
	}
	return nil
}
