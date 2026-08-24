package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"dhs/internal/amwa/profile"
)

// runNMOSProfile implements `dhs consumer nmos profile`.
//
// Where `export` + `audit` answer "what does this plant look like",
// probe answers "does this device behave". The difference is the live
// exchange: whether an unknown version is rejected, whether CORS is
// present, whether a paging limit is honoured and reported back. None
// of that survives into a capture, because a capture records answers
// rather than the conversation.
//
// The verb is read-only by construction. It never PATCHes, never
// activates, never registers — a conformance tool that stages a
// connection is one that can take a live source off air, and this one
// is meant to be safe to point at a plant that is on.
func runNMOSProfile(ctx context.Context, args []string) (err error) {
	fs := flag.NewFlagSet("consumer nmos profile", flag.ContinueOnError)
	var (
		target  = fs.String("target", "", "device to probe, as host:port (required)")
		https   = fs.Bool("https", false, "use TLS")
		deep    = fs.Bool("deep", false, "assert every IS-05 endpoint, not just the first")
		format  = fs.String("format", "text", "output format: text | json | jsonl")
		outPath = fs.String("out", "", "write the report to this file instead of stdout")
		failOn  = fs.String("fail-on", "", "exit non-zero when a result at or above this status is present: warn | fail")
		timeout = fs.Duration("timeout", 10*time.Second, "per-request deadline")
	)

	// Lift a bare positional before parsing — Go's flag package stops
	// at the first non-flag argument.
	positional := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		positional, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("consumer nmos profile: unexpected argument %q (one target only)", fs.Arg(0))
	}
	if *target == "" {
		*target = positional
	} else if positional != "" {
		return fmt.Errorf("consumer nmos profile: target given twice (%q and --target %q)", positional, *target)
	}
	if *target == "" {
		return fmt.Errorf("consumer nmos profile: --target is required (host:port of the device to probe)")
	}

	var gate profile.Status
	switch strings.ToLower(*failOn) {
	case "":
	case "warn":
		gate = profile.StatusWarn
	case "fail":
		gate = profile.StatusFail
	default:
		return fmt.Errorf("consumer nmos profile: unknown --fail-on %q (want warn or fail)", *failOn)
	}

	rep, err := profile.Run(ctx, profile.Options{
		Target:  *target,
		HTTPS:   *https,
		Deep:    *deep,
		Timeout: *timeout,
	})
	if err != nil {
		return err
	}

	var w io.Writer = os.Stdout
	if *outPath != "" {
		f, ferr := os.Create(*outPath)
		if ferr != nil {
			return fmt.Errorf("consumer nmos profile: %w", ferr)
		}
		// A truncated report reads as a healthier device than the probe
		// actually found, so a failed Close is surfaced.
		defer func() {
			if cerr := f.Close(); cerr != nil && err == nil {
				err = fmt.Errorf("consumer nmos profile: writing %s: %w", *outPath, cerr)
			}
		}()
		w = f
	}

	switch *format {
	case "text":
		err = profile.RenderText(w, rep)
	case "json":
		err = profile.RenderJSON(w, rep)
	case "jsonl":
		err = profile.RenderJSONL(w, rep)
	default:
		return fmt.Errorf("consumer nmos profile: unknown --format %q (want text, json or jsonl)", *format)
	}
	if err != nil {
		return err
	}

	// Opt-in, like the audit's gate. Reporting is not failing, and each
	// CI job picks its own threshold.
	if gate == "" {
		return nil
	}
	rank := map[profile.Status]int{profile.StatusPass: 0, profile.StatusSkip: 1, profile.StatusWarn: 2, profile.StatusFail: 3}
	if worst, any := rep.Worst(); any && rank[worst] >= rank[gate] {
		return fmt.Errorf("consumer nmos profile: worst result %s (--fail-on %s)", worst, gate)
	}
	return nil
}
