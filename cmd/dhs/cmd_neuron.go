package main

// `dhs consumer neuron <verb>` — the EVS Neuron REST API connector
// (issue #975), UUID-addressed. Distinct from acp2 (AN2/binary): this
// is the HTTPS/JSON control surface, and it lines its streams up with
// the plant's NMOS registry by the same UUIDs.
//
//   dhs consumer neuron walk <host>            list streams by UUID
//   dhs consumer neuron walk <host> --json     the whole device as JSON

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"

	neuron "dhs/internal/neuron/consumer"
)

func runNeuron(ctx context.Context, args []string) error {
	if len(args) == 0 || isHelpToken(args[0]) {
		fmt.Println("usage: dhs consumer neuron <verb> <host> [flags]")
		fmt.Println("  walk <host>    connect to the Neuron REST API and list its streams by UUID")
		fmt.Println("  flags: --json  emit the whole device as JSON")
		fmt.Println("         --verify-tls  verify the device certificate (default: skip, lab self-signed)")
		fmt.Println("         --timeout D   per-request timeout (default 8s)")
		return nil
	}
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "walk":
		return runNeuronWalk(ctx, rest)
	}
	return fmt.Errorf("consumer neuron: unknown verb %q (expected: walk)", verb)
}

func runNeuronWalk(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("consumer neuron walk", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the whole device as JSON")
	verifyTLS := fs.Bool("verify-tls", false, "verify the device certificate (default: skip)")
	timeout := fs.Duration("timeout", 0, "per-request timeout (default 8s)")

	host := ""
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		host, args = args[0], args[1:]
	}
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	if host == "" {
		return fmt.Errorf("consumer neuron walk: a host is required (e.g. 10.6.255.102)")
	}

	c := neuron.New(neuron.Options{Host: host, VerifyTLS: *verifyTLS, Timeout: *timeout})
	dev, deviations, err := c.Walk(ctx)
	if err != nil {
		return fmt.Errorf("consumer neuron walk: %w", err)
	}

	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(dev)
	}

	fmt.Printf("Neuron %s %s (model %d) — %d stream(s)\n",
		dev.ProductName, dev.ProductVersion, dev.ModelVersion, len(dev.Streams))
	uuids := make([]string, 0, len(dev.Streams))
	for u := range dev.Streams {
		uuids = append(uuids, u)
	}
	sort.Strings(uuids)
	fmt.Printf("%-38s %-9s %-6s %-6s %s\n", "UUID", "KIND", "ESSENCE", "ON", "NAME")
	for _, u := range uuids {
		s := dev.Streams[u]
		on := "no"
		if s.Enable {
			on = "yes"
		}
		fmt.Printf("%-38s %-9s %-6s %-6s %s\n", s.UUID, s.Kind, s.Essence, on, s.Name)
	}
	for _, d := range deviations {
		fmt.Fprintf(os.Stderr, "  deviation: %s\n", d)
	}
	return nil
}
