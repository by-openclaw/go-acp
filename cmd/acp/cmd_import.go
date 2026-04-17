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
	return nil
}
