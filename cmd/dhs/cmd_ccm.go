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

// runCCM dispatches `dhs consumer ccm <verb>`.
//
// It answers CCM's own verbs — walk (streams by UUID) and export (the
// versioned firmware artefacts) — and hands everything else to the
// neutral dispatcher, which now has this connector in its registry.
// `handled` is false for those, exactly as the SNMP dispatcher does
// it: two faces, one session, and whichever shape the operator typed
// is the one they meant.
func runCCM(ctx context.Context, args []string) (handled bool, remaining []string, err error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		fmt.Println("usage: dhs consumer ccm <verb> <host> [flags]")
		fmt.Println("  walk <host>            list io/ip streams by UUID")
		fmt.Println("  walk <host> --tree     walk the FULL recursive DM (every node/resource)")
		fmt.Println("       [--start p1,p2]   seed --tree from explicit node paths (default: from the API root)")
		fmt.Println("  export <host>  store api.yml (schema) + tree (DM) + dm-tree.json (full DM) + extract, versioned for firmware diff")
		fmt.Println("  flags: --json  emit the whole device as JSON")
		fmt.Println("         --verify-tls  verify the device certificate (default: skip, lab self-signed)")
		fmt.Println("         --timeout D   per-request timeout (default 8s)")
		fmt.Println("  every neutral verb also works here: info, tree, get, watch, alarm, …")
		return true, nil, nil
	}
	verb := args[0]
	rest := args[1:]

	// This connector's own settings — where the API hangs off, whether
	// to verify the certificate — apply to BOTH shapes, but the neutral
	// verbs are protocol-agnostic and have no flags for them. So they
	// are taken here, before the split, and passed on the way every
	// other per-connector setting reaches a neutral verb.
	//
	// Without this, `export --api-base /api --format csv` fails with
	// "flag provided but not defined: -api-base" and prints ACP1's
	// help — the two flags an operator needs at a customer site, in
	// the one combination that did not work.
	rest = takeCCMSettings(rest)

	switch verb {
	case "walk":
		// Both shapes exist. CCM's own walk lists streams by UUID; the
		// neutral one walks a SLOT and writes the device model to the
		// DM cache, which is what every other connector's walk does and
		// what an alarm template and a fixture are keyed by. Whichever
		// the operator named is the one they meant.
		if namesASlot(rest) {
			return false, append([]string{verb}, rest...), nil
		}
		return true, nil, runCCMWalk(ctx, rest)
	case "export":
		// Two shapes again. CCM's own export is the firmware-diff
		// artefact set — api.yml, the DM tree, the extract, versioned
		// per identity — and it keeps the bare verb. `--format` asks
		// for the neutral one: json / yaml / CSV of the walked model,
		// which is what an operator hands to somebody who does not run
		// this tool.
		if namesAFormat(rest) {
			return false, append([]string{verb}, rest...), nil
		}
		return true, nil, runCCMExport(ctx, rest)
	}
	// info, tree, get, set, watch, alarm, ensure, validate…: the neutral
	// dispatcher owns them now.
	return false, append([]string{verb}, rest...), nil
}

func runCCMWalk(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("consumer ccm walk", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the whole device as JSON")
	tree := fs.Bool("tree", false, "walk the FULL recursive DM (every node/resource), not just io/ip streams")
	verifyTLS := fs.Bool("verify-tls", false, "verify the device certificate (default: skip)")
	apiBase := fs.String("api-base", "", "the path the API hangs off ('/api/v1' on BRIDGE 7.0.3, '/api' on the newer firmware). Empty asks the device.")
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

	c := ccmc.New(ccmc.Options{Host: host, VerifyTLS: ccmVerifyTLS(*verifyTLS),
		Timeout: *timeout, APIBase: ccmAPIBase(*apiBase)})
	if err := c.Resolve(ctx); err != nil {
		return err
	}

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

// namesAFormat reports whether the operator asked for the neutral
// export — a file in a named format — rather than this connector's own
// artefact set.
func namesAFormat(args []string) bool {
	for _, a := range args {
		if a == "--format" || a == "-format" ||
			strings.HasPrefix(a, "--format=") || strings.HasPrefix(a, "-format=") {
			return true
		}
	}
	return false
}

// takeCCMSettings pulls this connector's own settings out of the
// argument list and applies them, returning what is left.
//
// They are applied through the environment because that is the channel
// a registered connector already reads (the plugin's dialClient), and
// because it is the same mechanism every other per-connector secret
// and setting uses here. Flags still win over an environment the
// operator exported earlier — they are the more specific statement.
//
// CCM's own verbs read the same settings back out of the environment
// when their flag was not given, so removing them here takes nothing
// away from those either — see ccmAPIBase.
func takeCCMSettings(args []string) []string {
	settings := []struct {
		flag string
		env  string
		// bare is true for a flag with no value ("--verify-tls").
		bare bool
	}{
		{"api-base", "CCM_API_BASE", false},
		{"verify-tls", "CCM_VERIFY_TLS", true},
	}

	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		matched := false
		for _, s := range settings {
			long, short := "--"+s.flag, "-"+s.flag
			switch {
			case strings.HasPrefix(a, long+"="), strings.HasPrefix(a, short+"="):
				_, v, _ := strings.Cut(a, "=")
				_ = os.Setenv(s.env, v)
				matched = true
			case a == long || a == short:
				if s.bare {
					_ = os.Setenv(s.env, "1")
					matched = true
					break
				}
				if i+1 < len(args) {
					_ = os.Setenv(s.env, args[i+1])
					i++
				}
				matched = true
			}
			if matched {
				break
			}
		}
		if !matched {
			out = append(out, a)
		}
	}
	return out
}

// ccmAPIBase is the base a CCM verb should use: its own flag when
// given, otherwise whatever the dispatcher took off the command line
// or the operator exported. Empty means ask the device.
func ccmAPIBase(flagValue string) string {
	if v := strings.TrimSpace(flagValue); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("CCM_API_BASE"))
}

// ccmVerifyTLS is the same for certificate verification. These devices
// are self-signed by design, so verification is off unless somebody
// asks for it — and asking once, anywhere, is enough.
func ccmVerifyTLS(flagValue bool) bool {
	return flagValue || strings.TrimSpace(os.Getenv("CCM_VERIFY_TLS")) != ""
}
