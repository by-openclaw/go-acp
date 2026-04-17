package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"acp/internal/protocol/acp2"
)

func runDiag(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("diag", flag.ExitOnError)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", 0, "target slot")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: acp diag <host> [--slot N]")
	}
	_ = fs.Parse(rest)
	_ = cf

	lvl := slog.LevelDebug
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))

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
