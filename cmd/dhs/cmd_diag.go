package main

import (
	"context"
	"flag"
	"fmt"
	"strings"

	acp2 "dhs/internal/acp2/consumer"
)

func runDiag(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("diag", flag.ExitOnError)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", 0, "target slot")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer <proto> diag <host> [--slot N]")
	}
	_ = parseVerbFlags(fs, rest)
	_ = cf

	// The shared consumer logger: honours --log-format / --syslog-addr /
	// --debug like every other verb instead of a private stderr handler.
	logger, _, logClean, _ := consumerLogger(ctx, "acp2", host, "diag")
	defer logClean()

	port := 2072
	results, err := acp2.RunDiagnostics(ctx, host, port, uint8(*slot), logger)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("%-45s  %-30s  %s\n", "PROBE", "STATUS", "REPLY")
	fmt.Println(strings.Repeat("-", 100))
	for _, r := range results {
		fmt.Printf("%-45s  %-30s  %s\n", r.Name, r.Status, r.Reply)
	}
	fmt.Printf("\nSent payloads:\n")
	for _, r := range results {
		fmt.Printf("  %-45s  %s\n", r.Name, r.Sent)
	}
	return nil
}
