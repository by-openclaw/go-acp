package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"dhs/internal/amwa/session/export"
)

// runNMOSExport implements `dhs consumer nmos export`.
//
// It captures a device — or a registry together with every node it
// lists — into the on-disk layout `dhs consumer nmos audit` reads. The
// two verbs are one workflow: capture on the customer's network, audit
// anywhere.
//
// The capture never repairs what it finds. A 502, a stuck paging
// cursor, a node that answers nowhere: each is recorded and the walk
// continues. Those recordings are what the audit turns into findings,
// and it can only do that if the capture did not quietly paper over
// them first.
func runNMOSExport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("consumer nmos export", flag.ContinueOnError)
	var (
		target   = fs.String("target", "", "device or registry to capture, as host:port (required)")
		out      = fs.String("out", "nmos-export", "output directory")
		https    = fs.Bool("https", false, "use TLS")
		deep     = fs.Bool("deep", false, "also fetch staged / constraints / transporttype per IS-05 endpoint")
		allVers  = fs.Bool("all-versions", false, "walk every minor of every API")
		noSDP    = fs.Bool("no-sdp", false, "skip SDP retrieval")
		maxNodes = fs.Int("max-nodes", 0, "cap how many registered nodes to follow (0 = no cap)")
		timeout  = fs.Duration("timeout", 10*time.Second, "per-request deadline")
		noStamp  = fs.Bool("no-stamp", false, "name folders without a timestamp")
		quiet    = fs.Bool("quiet", false, "suppress progress output")
		jsonOut  = fs.Bool("json", false, "print the capture summary as JSON")
	)

	// Lift a bare positional before parsing: Go's flag package stops at
	// the first non-flag argument, so `export 10.0.0.1:8080 --deep`
	// would otherwise parse no flags at all.
	positional := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		positional, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("consumer nmos export: unexpected argument %q (one target only)", fs.Arg(0))
	}
	if *target == "" {
		*target = positional
	} else if positional != "" {
		return fmt.Errorf("consumer nmos export: target given twice (%q and --target %q)", positional, *target)
	}
	if *target == "" {
		return fmt.Errorf("consumer nmos export: --target is required (host:port of the device or registry)")
	}

	opts := export.Options{
		Target:      *target,
		Out:         *out,
		HTTPS:       *https,
		Deep:        *deep,
		AllVersions: *allVers,
		NoSDP:       *noSDP,
		MaxNodes:    *maxNodes,
		Timeout:     *timeout,
		NoStamp:     *noStamp,
	}
	if !*quiet {
		opts.Log = func(s string) { fmt.Fprintln(os.Stderr, s) }
	}

	res, err := export.Run(ctx, opts)
	if err != nil {
		return err
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}

	total := totalOfCapture(res)
	fmt.Printf("captured %s (%s) -> %s\n", res.Target, res.Role, res.Dir)
	if len(res.Followed) > 0 {
		fmt.Printf("  %d node(s) listed, %d followed\n", res.NodesSeen, len(res.Followed))
	}
	fmt.Printf("  %d request(s), %d failure(s), %d SDP file(s)\n", total.requests, total.failures, total.sdp)
	fmt.Printf("\naudit it with:\n  dhs consumer nmos audit --dir %s\n", *out)
	return nil
}

type captureTotals struct{ requests, failures, sdp int }

// totalOfCapture adds up a capture and everything it followed, so the line
// an operator reads describes the plant rather than just its registry.
func totalOfCapture(r *export.Result) captureTotals {
	t := captureTotals{r.Requests, r.Failures, r.SDPFiles}
	for i := range r.Followed {
		sub := totalOfCapture(&r.Followed[i])
		t.requests += sub.requests
		t.failures += sub.failures
		t.sdp += sub.sdp
	}
	return t
}
