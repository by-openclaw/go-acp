package main

import (
	"context"
	"flag"
	"fmt"

	"dhs/internal/consumer"
)

// cerebrumGet is the CLI face of the canonical read from D2 unit (a):
// ONE dotted path — DEVICE.SUB.OBJECT… — through Plugin.GetValue,
// which issues the §5.4.3 VALUE obtain under the hood. Same verb name
// and same output shape (formatValue) as every other connector's
// `get`; the wire addressing (DEVICE_NAME / SUB_DEVICE / OBJECT)
// never surfaces (ADR-0002). The §5.4-native flag form stays
// available as `device-value` for operators who think in wire terms.
func cerebrumGet(ctx context.Context, args []string) error {
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb get", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	pathFlag := fs.String("path", "", `canonical object path DEVICE.SUB.OBJECT… — DEVICE_NAME verbatim incl. whitespace (e.g. "bm-n-nncvt-001 .1.PROCESSING AUDIO.AUDIO DELAY.BANK 1.Delay")`)
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	if *pathFlag == "" {
		return cerebrumValErr("get", "--path is required (canonical DEVICE.SUB.OBJECT… form)")
	}

	p, _, _, err := dialCerebrum(cf, fs.Args(), "get")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()

	opCtx, cancel := context.WithTimeout(ctx, cf.timeout)
	defer cancel()
	val, err := p.GetValue(opCtx, consumer.ValueRequest{Path: *pathFlag})
	if err != nil {
		return err
	}
	fmt.Println(formatValue(val, nil))
	return nil
}
