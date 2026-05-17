package main

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"strings"

	emberplus "dhs/internal/emberplus/consumer"
)

func runMatrix(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("matrix", flag.ExitOnError)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", 0, "slot number")
	matrixPath := fs.String("path", "", "matrix path: dotted label OR numeric OID (e.g. router.oneToN.matrix or 1.1)")
	target := fs.Int("target", -1, "target number")
	sourcesStr := fs.String("sources", "", "comma-separated source numbers (e.g. 1 or 1,2,3)")
	op := fs.String("op", "absolute", "operation: absolute, connect, disconnect")
	dmIdentity := fs.String("dm", "", `identity-keyed DM hot-load (e.g. "Tiny Ember+ Router@1.6.2"). When set, the tree is seeded from .cache/dm/emberplus/<identity>.json and the per-call walk is skipped — refs #438, ADR-0022.`)
	noWalk := fs.Bool("no-walk", false, "fail fast on cache miss instead of falling back to a wire walk")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer <proto> matrix <host> --path <matrix.path> --target N --sources N[,N,...] [--op absolute|connect|disconnect] [--dm <identity> | --no-walk]")
	}
	_ = fs.Parse(rest)
	if *matrixPath == "" {
		return fmt.Errorf("--path is required (e.g. router.oneToN.matrix)")
	}
	if err := validatePathOrOID(*matrixPath); err != nil {
		return err
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

	// Cast to Ember+ plugin to access MatrixConnect.
	ep, ok := plug.(*emberplus.Plugin)
	if !ok {
		return fmt.Errorf("matrix command is only supported for Ember+ protocol")
	}

	// DM auto-cache: cache hit → no walk; cache miss → walk + auto-extract
	// so the next call hits the cache (refs #438, ADR-0022).
	if err := ensureEmberplusTree(ctx, plug, host, cf.port, *dmIdentity, *slot, *noWalk); err != nil {
		return err
	}

	// Per-op timer started AFTER walk/DM-load so MatrixConnect sees
	// a fresh --timeout budget — same pattern as cmd_invoke / cmd_set.
	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	if err := ep.MatrixConnect(opCtx, *matrixPath, int32(*target), sources, operation); err != nil {
		return err
	}

	fmt.Printf("matrix connect: target %d ← sources %v (op=%s)\n", *target, sources, *op)
	return nil
}
