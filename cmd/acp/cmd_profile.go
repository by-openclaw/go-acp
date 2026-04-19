// acp profile — run a walk against an Ember+ provider and print the
// compliance classification (strict / partial) plus every tolerance
// event that fired. Lets the user build a compatibility matrix by
// running the same command against every provider in a fleet.
//
// See docs/protocols/emberplus/consumer.md §A9 for the list of event labels
// and what each one means in spec terms.
package main

import (
	"context"
	"flag"
	"fmt"
	"sort"

	emberplus "acp/internal/protocol/emberplus"
)

func runProfile(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("profile", flag.ExitOnError)
	cf := addCommonFlags(fs)
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: acp profile <host> [--port 9000] [--timeout DUR]")
	}
	_ = fs.Parse(rest)

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	objs, err := plug.Walk(opCtx, 0)
	if err != nil {
		return fmt.Errorf("walk: %w", err)
	}

	ep, ok := plug.(*emberplus.Plugin)
	if !ok {
		return fmt.Errorf("profile command is only supported for Ember+ protocol")
	}

	profile := ep.ComplianceProfile()
	classification := profile.Classification()
	snap := profile.Snapshot()

	fmt.Printf("host             %s:%d\n", host, cf.port)
	fmt.Printf("objects walked   %d\n", len(objs))
	fmt.Printf("classification   %s\n", classification)

	if len(snap) == 0 {
		fmt.Println("\nno tolerance events observed — provider is fully spec-compliant")
		return nil
	}

	fmt.Println("\ntolerance events")
	keys := make([]string, 0, len(snap))
	for k := range snap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %-32s %d\n", k, snap[k])
	}
	return nil
}

func helpProfile() {
	fmt.Println(`acp profile — Ember+ compliance classification

USAGE
  acp profile <host> [--port 9000] [--timeout DUR]

FLAGS
  [global flags accepted: --port, --timeout, --verbose]

BEHAVIOUR
  Runs a full walk, then prints:
    - object count
    - classification: strict (zero events) | partial (>=1 event)
    - every tolerance event that fired with its hit count

  Tolerance events catalogue documented in docs/protocols/emberplus/consumer.md
  §A9. Each event names one spec deviation the decoder absorbed:

    non_qualified_element           Node/Parameter sent without RelOID path
    multi_frame_reassembly          S101 FlagFirst/FlagLast chain
    invocation_success_default      success field omitted on InvocationResult
    connection_operation_default    Connection operation field omitted
    connection_disposition_default  Connection disposition field omitted
    contents_set_omitted            contents without UNIVERSAL SET envelope
    tuple_direct_ctx                Tuple without SEQUENCE wrapper
    element_collection_bare         CTX[0] children without APP[4] wrapper
    unknown_tag_skipped             vendor-private tag observed

EXAMPLES
  acp profile 127.0.0.1 --port 9000
  acp profile 10.41.40.195 --port 9092

References: Ember+ Documentation v2.50 §A9 (compliance audit).`)
}
