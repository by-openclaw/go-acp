package main

import (
	"context"
	"flag"
	"fmt"
)

func runInfo(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("info", flag.ExitOnError)
	cf := addCommonFlags(fs)
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: acp info <host>")
	}
	_ = fs.Parse(rest)

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	info, err := plug.GetDeviceInfo(opCtx)
	if err != nil {
		return err
	}
	fmt.Printf("device       %s:%d\n", info.IP, info.Port)
	fmt.Printf("protocol     %s v%d\n", cf.protocol, info.ProtocolVersion)
	fmt.Printf("slots        %d\n", info.NumSlots)
	fmt.Println()
	fmt.Println("per-slot status:")
	for slot := 0; slot < info.NumSlots; slot++ {
		si, err := plug.GetSlotInfo(opCtx, slot)
		if err != nil {
			fmt.Printf("  slot %2d   <error: %v>\n", slot, err)
			continue
		}
		fmt.Printf("  slot %2d   %s\n", slot, si.Status)
	}
	return nil
}
