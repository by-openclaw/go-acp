package main

// `dhs consumer nmos registers` — walk the AMWA NMOS Parameter
// Registers the way ACP2/Ember+ expose a device tree (#851): typed,
// enumerable, introspectable. No network; the registers are compiled
// in from codec/registers.
//
//   dhs consumer nmos registers list
//   dhs consumer nmos registers show urn:x-nmos:cap:format:component_depth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"dhs/internal/amwa/codec/registers"
)

func runNMOSRegisters(_ context.Context, args []string) error {
	if len(args) == 0 || isHelpToken(args[0]) {
		fmt.Println("usage: dhs consumer nmos registers <list|show> [urn] [--json]")
		fmt.Println("  list          every parameter, URN-sorted")
		fmt.Println("  show <urn>    one parameter's typed constraint")
		fmt.Println("  --json        machine-readable output")
		return nil
	}
	sub := args[0]
	rest := args[1:]
	asJSON := false
	var urn string
	for _, a := range rest {
		switch {
		case a == "--json":
			asJSON = true
		case urn == "":
			urn = a
		default:
			return fmt.Errorf("registers %s: unexpected argument %q", sub, a)
		}
	}

	switch sub {
	case "list":
		all := registers.All()
		if asJSON {
			return json.NewEncoder(os.Stdout).Encode(all)
		}
		fmt.Printf("%-52s %-10s %s\n", "URN", "KIND", "ACCEPTS")
		for _, p := range all {
			fmt.Printf("%-52s %-10s %s\n", p.URN, p.Kind, accepts(p))
		}
		fmt.Printf("\n%d parameter(s)\n", len(all))
		return nil

	case "show":
		if urn == "" {
			return fmt.Errorf("registers show: a parameter URN is required")
		}
		p, ok := registers.Lookup(urn)
		if !ok {
			return fmt.Errorf("registers show: %q is not in any register", urn)
		}
		if asJSON {
			return json.NewEncoder(os.Stdout).Encode(p)
		}
		fmt.Printf("%s\n", p.URN)
		fmt.Printf("  name     %s\n", p.Name)
		if p.Description != "" {
			fmt.Printf("  desc     %s\n", p.Description)
		}
		fmt.Printf("  kind     %s\n", p.Kind)
		fmt.Printf("  accepts  %s\n", accepts(p))
		fmt.Printf("  register %s %s\n", p.Register, p.RegisterVersion)
		return nil
	}
	return fmt.Errorf("registers: unknown subcommand %q (want list or show)", sub)
}

// accepts renders a parameter's constraint as a one-line human answer.
func accepts(p registers.Param) string {
	switch p.Kind {
	case registers.KindEnum:
		return fmt.Sprintf("%v", p.Values)
	case registers.KindInteger, registers.KindNumber:
		lo, hi := "−∞", "+∞"
		if p.Min != nil {
			lo = fmt.Sprintf("%g", *p.Min)
		}
		if p.Max != nil {
			hi = fmt.Sprintf("%g", *p.Max)
		}
		return lo + " .. " + hi
	case registers.KindRational:
		return "rational {numerator, denominator}"
	case registers.KindBoolean:
		return "true | false"
	default:
		return "any string"
	}
}
