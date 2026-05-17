package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sort"

	"dhs/internal/consumer"
	"dhs/internal/wiretrace"
)

// runValidate drives `dhs consumer <proto> validate <frames.jsonl> [flags]`.
//
// Decodes every Trame in the JSONL through the connector's codec
// (per ADR-0021) and reports per-direction counts plus invariant
// violations. Optional flags drive offline canonical-tree / params
// emission in the same pass.
//
// `replay` (peer simulation per ADR-0021) is a separate, deferred verb.
// See ADR-0002 canonical-verb table for the full role split.
func runValidate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	cf := addCommonFlags(fs)

	outTree := fs.String("out-tree", "", "optional path: write canonical tree.json (per-protocol support)")
	outParams := fs.String("out-params", "", "optional path: write canonical params dump (csv/json by extension)")
	stopAt := fs.String("stop-at", "", "optional Trame.Note marker to halt decoding at")

	tramesPath, rest, err := popHost(args)
	if err != nil {
		return errors.New("usage: dhs consumer <proto> validate <frames.jsonl> [flags]")
	}
	_ = fs.Parse(rest)

	factory, err := consumer.Get(cf.protocol)
	if err != nil {
		return err
	}

	f, err := os.Open(tramesPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", tramesPath, err)
	}
	defer func() { _ = f.Close() }()

	trames, err := wiretrace.ReadTrames(f)
	if err != nil {
		return err
	}

	plug := factory.New(slog.Default())

	validator, ok := plug.(consumer.Validator)
	if !ok {
		return fmt.Errorf("validate: protocol %q does not implement consumer.Validator yet (per-protocol migration tracker: issue #212)", cf.protocol)
	}

	report, err := validator.Validate(ctx, trames, consumer.ValidateOpts{
		OutTree:   *outTree,
		OutParams: *outParams,
		StopAt:    *stopAt,
	})
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	fmt.Printf("validate: %d trames decoded\n", report.TramesProcessed)
	dirs := make([]string, 0, len(report.PerDirection))
	for d := range report.PerDirection {
		dirs = append(dirs, string(d))
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		fmt.Printf("  %s: %d\n", d, report.PerDirection[wiretrace.Direction(d)])
	}
	if report.StoppedAt != "" {
		fmt.Printf("  stopped-at: %s\n", report.StoppedAt)
	}
	if len(report.Errors) > 0 {
		fmt.Fprintf(os.Stderr, "errors: %d\n", len(report.Errors))
		for _, e := range report.Errors {
			fmt.Fprintf(os.Stderr, "  trame %d (%s): %s\n", e.TrameIndex, e.Direction, e.Err)
		}
	}
	if len(report.Invariants) > 0 {
		fmt.Fprintf(os.Stderr, "invariant violations: %d\n", len(report.Invariants))
		for _, v := range report.Invariants {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
	}

	if len(report.Errors) > 0 || len(report.Invariants) > 0 {
		return errors.New("validate: failures detected")
	}
	return nil
}

func helpValidate() {
	fmt.Print(`dhs consumer <proto> validate <frames.jsonl> [flags]

Decode every Trame in <frames.jsonl> (per ADR-0021) through the
protocol's codec offline, with no live peer. Reports per-direction
counts and any decode failures or invariant violations. Exits 0 on
a clean capture, 1 on any failure.

Naming: "Trame" is the wire-trace record type — see ADR-0021. The
file naming convention (frames.jsonl) is kept for compatibility
with existing capture tooling.

Flags:
  --out-tree <path>     also write a canonical tree.json
  --out-params <path>   also write a canonical params dump (csv/json by ext)
  --stop-at <note>      halt at the first trame whose .note matches

IN   dhs consumer acp1 validate captures/acp1/slot0_walk/frames.jsonl
OUT  validate: 642 trames decoded
       rx: 321
       tx: 321

Captures live under captures/<proto>/<scenario>/frames.jsonl by
convention (per ADR-0020 Bucket 4); the captures/ tree is gitignored
in its entirety, no LFS, no committed blobs.

`)
}
