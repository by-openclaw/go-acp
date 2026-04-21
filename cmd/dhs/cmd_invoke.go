package main

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"strings"

	emberplus "acp/internal/emberplus/consumer"
)

func runInvoke(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("invoke", flag.ExitOnError)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", 0, "slot number")
	funcPath := fs.String("path", "", "dot-separated function path (e.g. router.functions.add)")
	argsStr := fs.String("args", "", "comma-separated arguments (e.g. 3,5)")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: acp invoke <host> --path <func.path> [--args val1,val2,...]")
	}
	_ = fs.Parse(rest)
	if *funcPath == "" {
		return fmt.Errorf("--path is required (e.g. router.functions.add)")
	}

	// Parse arguments — try integer first, then float, then string.
	var funcArgs []interface{}
	if *argsStr != "" {
		for _, a := range strings.Split(*argsStr, ",") {
			a = strings.TrimSpace(a)
			if n, err := strconv.ParseInt(a, 10, 64); err == nil {
				funcArgs = append(funcArgs, n)
			} else if f, err := strconv.ParseFloat(a, 64); err == nil {
				funcArgs = append(funcArgs, f)
			} else if a == "true" {
				funcArgs = append(funcArgs, true)
			} else if a == "false" {
				funcArgs = append(funcArgs, false)
			} else {
				funcArgs = append(funcArgs, a)
			}
		}
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	// Walk to populate tree (raw ctx — no per-op deadline).
	if _, err := plug.Walk(ctx, *slot); err != nil {
		return fmt.Errorf("walk: %w", err)
	}

	// Start the per-op timer AFTER the walk — otherwise the walk burns
	// through --timeout before Invoke even sends its Command frame.
	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	ep, ok := plug.(*emberplus.Plugin)
	if !ok {
		return fmt.Errorf("invoke command is only supported for Ember+ protocol")
	}

	result, err := ep.InvokeFunction(opCtx, *funcPath, funcArgs)
	if err != nil {
		return err
	}

	fmt.Printf("invocation %d: success=%v\n", result.InvocationID, result.Success)
	if len(result.Result) > 0 {
		fmt.Printf("result: %v\n", result.Result)
	}
	return nil
}
