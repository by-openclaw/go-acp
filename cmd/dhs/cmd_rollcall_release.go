package main

import (
	"context"
	"flag"
	"fmt"
)

func helpRollcallRelease() {
	fmt.Println(`dhs consumer rollcall release <host> --slot N

Terminate our sessions to ONE node without leaving the frame.

Before a card firmware upgrade the precondition is that no connection is still
established on that card. This sends SP_TERM to the card's control and file
sessions so it reclaims them at once, and leaves every other node and the link
itself untouched. Releasing a card we hold nothing on is not an error.

Inside a held connection use "release <slot>" in 'dhs consumer rollcall
session' instead, which keeps the link up across the release.

Example:
  dhs consumer rollcall release 10.6.255.113 --slot 3`)
}

func runRollcallRelease(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("release", flag.ExitOnError)
	fs.Usage = verbUsageFn(fs, helpRollcallRelease)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", -1, "the node to release; required")

	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer rollcall release <host> --slot N")
	}
	_ = parseVerbFlags(fs, rest)
	if *slot < 0 {
		return fmt.Errorf("--slot is required: which node to release")
	}

	p, cleanup, err := rollcallPlugin(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := p.Release(ctx, *slot); err != nil {
		return err
	}
	fmt.Printf("released slot %d — its sessions are terminated; the link and other nodes are untouched\n", *slot)
	return nil
}
