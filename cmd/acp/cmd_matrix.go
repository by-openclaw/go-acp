package main

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"strings"

	emberplus "acp/internal/protocol/emberplus"
)

func runMatrix(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("matrix", flag.ExitOnError)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", 0, "slot number")
	matrixPath := fs.String("path", "", "dot-separated matrix path (e.g. router.oneToN.matrix)")
	target := fs.Int("target", -1, "target number")
	sourcesStr := fs.String("sources", "", "comma-separated source numbers (e.g. 1 or 1,2,3)")
	op := fs.String("op", "absolute", "operation: absolute, connect, disconnect")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: acp matrix <host> --path <matrix.path> --target N --sources N[,N,...] [--op absolute|connect|disconnect]")
	}
	_ = fs.Parse(rest)
	if *matrixPath == "" {
		return fmt.Errorf("--path is required (e.g. router.oneToN.matrix)")
	}
	if *target < 0 {
		return fmt.Errorf("--target is required")
	}

	// Parse sources.
	var sources []int32
	if *sourcesStr != "" {
		for _, s := range strings.Split(*sourcesStr, ",") {
			s = strings.TrimSpace(s)
			n, perr := strconv.Atoi(s)
			if perr != nil {
				return fmt.Errorf("invalid source number %q: %w", s, perr)
			}
			sources = append(sources, int32(n))
		}
	}

	// Parse operation.
	var operation int64
	switch strings.ToLower(*op) {
	case "absolute", "abs", "replace":
		operation = 0
	case "connect", "add":
		operation = 1
	case "disconnect", "remove":
		operation = 2
	default:
		return fmt.Errorf("unknown --op %q (use absolute, connect, disconnect)", *op)
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	// Walk to populate tree (raw ctx — no per-op deadline).
	if _, err := plug.Walk(ctx, *slot); err != nil {
		return fmt.Errorf("walk: %w", err)
	}

	// Cast to Ember+ plugin to access MatrixConnect.
	ep, ok := plug.(*emberplus.Plugin)
	if !ok {
		return fmt.Errorf("matrix command is only supported for Ember+ protocol")
	}

	if err := ep.MatrixConnect(opCtx, *matrixPath, int32(*target), sources, operation); err != nil {
		return err
	}

	fmt.Printf("matrix connect: target %d ← sources %v (op=%s)\n", *target, sources, *op)
	return nil
}
