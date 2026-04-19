package main

import (
	"context"
	"flag"
	"fmt"

	"acp/internal/export"
)

func runImport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	cf := addCommonFlags(fs)
	file := fs.String("file", "", "snapshot file (.json, .yaml, .csv)")
	dry := fs.Bool("dry-run", false, "validate and list would-write actions without sending")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: acp import <host> --file SNAPSHOT [--dry-run]")
	}
	_ = fs.Parse(rest)
	if *file == "" {
		return fmt.Errorf("--file is required")
	}

	snap, err := export.LoadSnapshot(*file)
	if err != nil {
		return err
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	rep, err := export.Apply(opCtx, plug, snap, *dry)
	if err != nil {
		return err
	}

	tag := "applied"
	if *dry {
		tag = "would apply"
	}
	fmt.Printf("%s %d, skipped %d, failed %d\n", tag, rep.Applied, rep.Skipped, rep.Failed)
	if len(rep.Failures) > 0 {
		fmt.Println("failures:")
		for _, f := range rep.Failures {
			fmt.Println("  -", f)
		}
	}
	// Dry-run prints the detailed skip report so the operator knows
	// exactly which rows in their CSV/JSON/YAML will not be applied
	// and why. Grouped by reason to keep long lists scannable.
	if *dry && len(rep.Skips) > 0 {
		printSkipReport(rep.Skips)
	}
	return nil
}

// printSkipReport groups skipped rows by reason ("read_only" etc.) and
// prints each group sorted by slot → id so the operator can pin one row
// at a time. Called only from --dry-run to keep non-preview imports
// terse.
func printSkipReport(skips []export.SkipRecord) {
	byReason := map[string][]export.SkipRecord{}
	for _, s := range skips {
		byReason[s.Reason] = append(byReason[s.Reason], s)
	}
	// Stable order for the reason headings so output is diff-friendly.
	reasons := []string{"read_only", "marker", "unknown_kind"}
	printed := map[string]bool{}
	fmt.Println("skipped rows (dry-run detail):")
	for _, r := range reasons {
		group, ok := byReason[r]
		if !ok {
			continue
		}
		printed[r] = true
		fmt.Printf("  %s (%d):\n", r, len(group))
		for _, s := range group {
			fmt.Printf("    slot=%d id=%d kind=%s access=%s path=%q\n",
				s.Slot, s.ID, s.Kind, s.Access, s.Path)
		}
	}
	// Any reason we didn't anticipate goes at the end so nothing is
	// silently dropped from the operator's view.
	for r, group := range byReason {
		if printed[r] {
			continue
		}
		fmt.Printf("  %s (%d):\n", r, len(group))
		for _, s := range group {
			fmt.Printf("    slot=%d id=%d kind=%s access=%s path=%q\n",
				s.Slot, s.ID, s.Kind, s.Access, s.Path)
		}
	}
}
