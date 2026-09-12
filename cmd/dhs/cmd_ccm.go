package main

// `dhs consumer ccm <verb>` — the EVS Neuron REST API connector
// (issue #975), UUID-addressed. Distinct from acp2 (AN2/binary): this
// is the HTTPS/JSON control surface, and it lines its streams up with
// the plant's NMOS registry by the same UUIDs.
//
//   dhs consumer ccm walk <host>            list streams by UUID
//   dhs consumer ccm walk <host> --json     the whole device as JSON

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	ccmc "dhs/internal/ccm/consumer"
)

func runCCM(ctx context.Context, args []string) error {
	if len(args) == 0 || isHelpToken(args[0]) {
		fmt.Println("usage: dhs consumer ccm <verb> <host> [flags]")
		fmt.Println("  walk <host>            list io/ip streams by UUID")
		fmt.Println("  walk <host> --tree     walk the FULL recursive DM (every node/resource)")
		fmt.Println("       [--start p1,p2]   seed --tree from explicit node paths (default: from the API root)")
		fmt.Println("  export <host>  store api.yml (schema) + tree (DM) + dm-tree.json (full DM) + extract, versioned for firmware diff")
		fmt.Println("  flags: --json  emit the whole device as JSON")
		fmt.Println("         --verify-tls  verify the device certificate (default: skip, lab self-signed)")
		fmt.Println("         --timeout D   per-request timeout (default 8s)")
		return nil
	}
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "walk":
		return runCCMWalk(ctx, rest)
	case "export":
		return runCCMExport(ctx, rest)
	}
	return fmt.Errorf("consumer ccm: unknown verb %q (expected: walk, export)", verb)
}

func runCCMWalk(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("consumer ccm walk", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the whole device as JSON")
	tree := fs.Bool("tree", false, "walk the FULL recursive DM (every node/resource), not just io/ip streams")
	verifyTLS := fs.Bool("verify-tls", false, "verify the device certificate (default: skip)")
	timeout := fs.Duration("timeout", 0, "per-request timeout (default 8s)")
	start := fs.String("start", "", "with --tree: comma-separated node paths to seed the walk (default: discover from the API root)")

	host := ""
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		host, args = args[0], args[1:]
	}
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	if host == "" {
		return fmt.Errorf("consumer ccm walk: a host is required (e.g. 10.6.255.102)")
	}

	c := ccmc.New(ccmc.Options{Host: host, VerifyTLS: *verifyTLS, Timeout: *timeout})

	if *tree {
		return runCCMWalkTree(ctx, c, *asJSON, *start)
	}

	dev, deviations, err := c.Walk(ctx)
	if err != nil {
		return fmt.Errorf("consumer ccm walk: %w", err)
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

// runCCMWalkTree drives the full recursive DM walk (dhs consumer ccm walk
// <host> --tree). It follows the device's own node/resource shape instead
// of the hardcoded io/ip slice, so it captures the whole model — the fix
// for "we forgot to get the DM": no root is assumed (seed with --start),
// and no wildcard is used (each node is GET and only its listed children
// are recursed).
func runCCMWalkTree(ctx context.Context, c *ccmc.Client, asJSON bool, start string) error {
	var starts []string
	if s := strings.TrimSpace(start); s != "" {
		for _, p := range strings.Split(s, ",") {
			if p = strings.TrimSpace(p); p != "" {
				starts = append(starts, p)
			}
		}
	}

	tree, deviations, err := c.WalkTree(ctx, starts...)
	if err != nil {
		return fmt.Errorf("consumer ccm walk --tree: %w", err)
	}

	if asJSON {
		out := make(map[string]json.RawMessage, tree.Len())
		for p, raw := range tree.Resources {
			out[p] = raw
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			return err
		}
	} else {
		fmt.Printf("CCM DM: %d resource(s) across %d node(s)\n", tree.Len(), len(tree.Branches))
		for _, p := range tree.SortedPaths() {
			fmt.Println("  " + p)
		}
	}
	for _, d := range deviations {
		fmt.Fprintf(os.Stderr, "  deviation: %s\n", d)
	}
	return nil
}
