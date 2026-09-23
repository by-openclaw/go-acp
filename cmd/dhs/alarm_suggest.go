package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"dhs/internal/consumer"
	"dhs/internal/consumer/alarm"
)

// `alarm suggest` is the answer to "who writes the template for 100
// models?". Nobody does: the device already declares its own states
// and its own ranges, so the draft is read off a walk and a human
// reviews it. What the device did not say stays unjudged and is
// counted out loud, because a generated rule nobody can source is
// exactly what ADR-0033 refuses to ship.
func runAlarmSuggest(ctx context.Context, proto string, args []string) error {
	fs := flag.NewFlagSet("consumer "+proto+" alarm suggest", flag.ContinueOnError)
	cf := addCommonFlags(fs)
	cf.protocol = proto
	slot := fs.Int("slot", -1, "walk this slot (-1 = every present slot)")
	out := fs.String("out", "", "write the draft here (default: stdout)")
	install := fs.Bool("install", false,
		"install the draft into the cache instead of printing it (reports changed=true/false)")
	model := fs.String("model", "",
		"identity to file the draft under (default: whatever the device reports)")

	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer %s alarm suggest <host> [--slot N] [--out FILE | --install]", proto)
	}
	if err := parseVerbFlags(fs, rest); err != nil {
		return err
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	objs, err := walkForSuggest(ctx, plug, *slot)
	if err != nil {
		return err
	}
	id := *model
	if id == "" {
		id = alarmIdentity(ctx, plug, *slot)
	}
	if id == "" {
		id = alarm.DefaultName
	}

	tpl, rep := alarm.Suggest(objs, proto, id)
	fmt.Fprintf(os.Stderr, "%s — %s\n", host, rep)
	if len(tpl.Rows) == 0 {
		return fmt.Errorf("consumer %s alarm suggest: this device declares nothing a rule can be sourced from — write the rules by hand (`alarm set`) with a source naming where the numbers come from", proto)
	}
	// A draft that the engine would refuse is not a draft.
	if err := tpl.Validate(); err != nil {
		return fmt.Errorf("consumer %s alarm suggest: %w", proto, err)
	}

	if *install {
		af := &alarmFlags{model: &id}
		dst, err := af.path(proto)
		if err != nil {
			return err
		}
		changed, err := alarm.Save(dst, tpl)
		if err != nil {
			return err
		}
		fmt.Printf("changed=%t  %d rule(s) → %s\n", changed, len(tpl.Rows), dst)
		return nil
	}

	if *out != "" {
		if _, err := alarm.Save(*out, tpl); err != nil {
			return err
		}
		fmt.Printf("%d rule(s) → %s\n", len(tpl.Rows), *out)
		fmt.Println("read it, keep what your plant means, then: alarm import " + *out)
		return nil
	}
	return tpl.Encode(os.Stdout)
}

// walkForSuggest reads the tree the draft is built from: one slot, or
// every slot the device says is present.
func walkForSuggest(ctx context.Context, plug consumer.Protocol, slot int) ([]consumer.Object, error) {
	if slot >= 0 {
		objs, err := plug.Walk(ctx, slot)
		if err != nil {
			return nil, fmt.Errorf("walk slot %d: %w", slot, err)
		}
		return withSlot(objs, slot), nil
	}
	info, err := plug.GetDeviceInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("device info: %w", err)
	}
	var all []consumer.Object
	for s := 0; s < info.NumSlots; s++ {
		si, err := plug.GetSlotInfo(ctx, s)
		if err != nil || si.Status != consumer.SlotPresent {
			continue
		}
		objs, err := plug.Walk(ctx, s)
		if err != nil {
			// A slot that will not answer is reported, not fatal: the
			// draft from the rest of the device is still worth having.
			fmt.Fprintf(os.Stderr, "slot %d: %v\n", s, err)
			continue
		}
		all = append(all, withSlot(objs, s)...)
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("no slot answered a walk")
	}
	return all, nil
}

func withSlot(objs []consumer.Object, slot int) []consumer.Object {
	for i := range objs {
		objs[i].Slot = slot
	}
	return objs
}
