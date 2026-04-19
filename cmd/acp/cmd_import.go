package main

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"acp/internal/export"
)

// multiInt and multiString let --id / --label / --path repeat on the
// command line, building up a slice for the ImportFilter.
type multiInt []int

func (m *multiInt) String() string {
	parts := make([]string, len(*m))
	for i, v := range *m {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ",")
}

func (m *multiInt) Set(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("--id value %q is not an integer", s)
	}
	*m = append(*m, n)
	return nil
}

type multiString []string

func (m *multiString) String() string { return strings.Join(*m, ",") }
func (m *multiString) Set(s string) error {
	*m = append(*m, s)
	return nil
}

func runImport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	cf := addCommonFlags(fs)
	file := fs.String("file", "", "snapshot file (.json, .yaml, .csv)")
	dry := fs.Bool("dry-run", false, "validate and list would-write actions without sending")
	slot := fs.Int("slot", -1, "apply only this slot (-1 = all slots in snapshot)")

	// Selective-import filters (issue #45). --id and --path are
	// mutually exclusive — pick one addressing scheme per invocation.
	// Multiple values of the same flag are fine: --id 1 --id 2 targets
	// both objects; --path "A.B" --path "C.D" targets both paths.
	//
	// --label is deliberately NOT offered. Labels collide across
	// sub-trees thousands of times in Ember+ ("gain" per channel) and
	// in ACP2 ("Present" per PSU). The only unambiguous keys are the
	// per-protocol ID and the dotted path — --label would be a
	// footgun.
	var filterIDs multiInt
	var filterPaths multiString
	fs.Var(&filterIDs, "id",
		"apply only this object ID. Repeat for multiple IDs. Mutually exclusive with --path.")
	fs.Var(&filterPaths, "path",
		"apply only objects with this dotted path (e.g. \"BOARD.Gain A\"). Repeat for multiple. Mutually exclusive with --id.")

	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: acp import <host> --file SNAPSHOT [--slot N] [--id N ...| --path P ...] [--dry-run]")
	}
	_ = fs.Parse(rest)
	if *file == "" {
		return fmt.Errorf("--file is required")
	}
	if len(filterIDs) > 0 && len(filterPaths) > 0 {
		return fmt.Errorf("--id and --path are mutually exclusive; pick one addressing scheme")
	}

	snap, err := export.LoadSnapshot(*file)
	if err != nil {
		return err
	}

	if *slot >= 0 {
		filtered := snap.Slots[:0]
		for _, sd := range snap.Slots {
			if sd.Slot == *slot {
				filtered = append(filtered, sd)
			}
		}
		snap.Slots = filtered
		if len(snap.Slots) == 0 {
			return fmt.Errorf("snapshot does not contain slot %d", *slot)
		}
	}

	// Apply --id / --path filtering in place. Count of removed objects
	// surfaces in the report so the operator sees exactly how many
	// snapshot rows were excluded before Apply ran.
	filter := &export.ImportFilter{
		IDs:   []int(filterIDs),
		Paths: []string(filterPaths),
	}
	filteredOut := export.ApplyFilter(snap, filter)

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	// Raw ctx: Apply's internal walk per slot has no deadline (44k
	// objects on ACP2 takes minutes); individual SetValue calls inside
	// Apply still follow their own per-plugin timeouts.
	rep, err := export.Apply(ctx, plug, snap, *dry)
	if err != nil {
		return err
	}
	rep.Filtered = filteredOut

	tag := "applied"
	if *dry {
		tag = "would apply"
	}
	if rep.Filtered > 0 {
		fmt.Printf("%s %d, skipped %d, failed %d, filtered %d\n",
			tag, rep.Applied, rep.Skipped, rep.Failed, rep.Filtered)
	} else {
		fmt.Printf("%s %d, skipped %d, failed %d\n", tag, rep.Applied, rep.Skipped, rep.Failed)
	}
	if len(rep.Failures) > 0 {
		fmt.Println("failures:")
		for _, f := range rep.Failures {
			fmt.Println("  -", f)
		}
	}
	// Dry-run prints the detailed skip report so the operator knows
	// exactly which rows in their CSV/JSON/YAML will not be applied
	// and why. Grouped by reason to keep long lists scannable.
	if *dry && len(rep.Skips) > 0 {
		printSkipReport(rep.Skips)
	}
	return nil
}

// printSkipReport groups skipped rows by reason ("read_only" etc.) and
// prints each group sorted by slot → id so the operator can pin one row
// at a time. Called only from --dry-run to keep non-preview imports
// terse.
func printSkipReport(skips []export.SkipRecord) {
	byReason := map[string][]export.SkipRecord{}
	for _, s := range skips {
		byReason[s.Reason] = append(byReason[s.Reason], s)
	}
	// Stable order for the reason headings so output is diff-friendly.
	reasons := []string{"read_only", "marker", "unknown_kind"}
	printed := map[string]bool{}
	fmt.Println("skipped rows (dry-run detail):")
	for _, r := range reasons {
		group, ok := byReason[r]
		if !ok {
			continue
		}
		printed[r] = true
		fmt.Printf("  %s (%d):\n", r, len(group))
		for _, s := range group {
			fmt.Printf("    slot=%d id=%d kind=%s access=%s path=%q\n",
				s.Slot, s.ID, s.Kind, s.Access, s.Path)
		}
	}
	// Any reason we didn't anticipate goes at the end so nothing is
	// silently dropped from the operator's view.
	for r, group := range byReason {
		if printed[r] {
			continue
		}
		fmt.Printf("  %s (%d):\n", r, len(group))
		for _, s := range group {
			fmt.Printf("    slot=%d id=%d kind=%s access=%s path=%q\n",
				s.Slot, s.ID, s.Kind, s.Access, s.Path)
		}
	}
}
