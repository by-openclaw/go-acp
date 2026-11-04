package main

// `dhs consumer cerebrum-nb rm-add` / `rm-delete` — Northbound v0.17
// §4.2.3-§4.2.5: append sources or destinations to the RouteMaster
// (COUNT, VIRTUAL, FEDERATION) and delete them by id. The reply is the
// §4.2.5 RESULT: ACCEPTED means validated and queued, not applied, so a
// caller that needs the outcome reads the collection back.

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"dhs/internal/cerebrum-nb/codec"
	cerebrumnb "dhs/internal/cerebrum-nb/consumer"
)

func cerebrumRMAdd(_ context.Context, args []string) error {
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb rm-add", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	kind := fs.String("kind", "", "source | destination")
	count := fs.Int("count", 1, "number to create, 1-1000")
	total := fs.Int("total", 0, "ensure this many exist: adds only what is missing, nothing on a second run; instead of --count")
	check := fs.Bool("check", false, "dry-run (ADR-0007): read the live list, report what would be added, send nothing; needs --total")
	wf := addCerebrumWriteFlags(fs)
	virtual := fs.Bool("virtual", false, "create virtual sources / destinations (VIRTUAL=1)")
	federation := fs.Bool("federation", false, "create them in the federation (FEDERATION=1)")
	output := fs.String("output", "text", "output format: text | json (ADR-0002)")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	k, err := rmKind(*kind, "rm-add")
	if err != nil {
		return err
	}
	if *count < 1 || *count > 1000 {
		return cerebrumValErr("rm-add", "--count must be 1-1000")
	}
	if *total < 0 || (*total > 0 && *federation) || (*check && *total == 0) {
		return cerebrumValErr("rm-add", "--total counts the local list, so it needs a positive value and no --federation; --check needs --total")
	}
	jsonOut, oerr := resolveEnsureOutput(*output, false)
	if oerr != nil {
		return oerr
	}
	if *wf.dryRun && *total == 0 {
		// --count adds without reading: the dry run has nothing to read.
		if err := wf.guard("rm-add", fs.Args(), true); err != nil {
			return err
		}
		wf.dryRunNote("rm-add", fmt.Sprintf("%d %s (virtual=%t federation=%t)", *count, k, *virtual, *federation))
		return nil
	}
	*check = *check || *wf.dryRun
	if err := wf.guard("rm-add", fs.Args(), *check); err != nil {
		return err
	}
	p, sess, _, err := dialCerebrumAuth(cf, fs.Args(), "rm-add")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	ctx, cancel := context.WithTimeout(context.Background(), cf.timeout)
	defer cancel()
	extra := map[string]any{"count": *count, "virtual": *virtual, "federation": *federation}
	if *federation {
		// The federation resources are not in the local list: no read-back.
		res, err := sess.RMAdd(ctx, k, *count, *virtual, *federation)
		if err != nil {
			return fmt.Errorf("cerebrum-nb rm-add: %w", err)
		}
		return printRMResult(jsonOut, "rm-add", k, res, extra)
	}
	before, err := cerebrumRMList(ctx, sess, k)
	if err != nil {
		return err
	}
	add := *count
	if *total > 0 {
		add = *total - len(before)
		if add < 0 {
			add = 0
		}
		extra["count"], extra["total"] = add, *total
	}
	if add > 1000 {
		return cerebrumValErr("rm-add", fmt.Sprintf("%d missing: the server takes 1-1000 at a time, run with a lower --total first", add))
	}
	if *check || add == 0 {
		fmt.Fprintf(os.Stderr, "cerebrum-nb rm-add: %d present, would add %d\n", len(before), add)
		if jsonOut {
			changed := add > 0
			if *check {
				return emitEnsure(true, ensureResult{WouldChange: &changed})
			}
			return emitEnsure(true, ensureResult{Changed: &changed})
		}
		return nil
	}
	res, err := sess.RMAdd(ctx, k, add, *virtual, *federation)
	if err != nil {
		return fmt.Errorf("cerebrum-nb rm-add: %w", err)
	}
	if err := printRMResult(jsonOut, "rm-add", k, res, extra); err != nil {
		return err
	}
	ids, err := cerebrumRMAwait(ctx, sess, k, before, add)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[rm-add] created %s: %s\n", k, strings.Join(ids, ","))
	return nil
}

// cerebrumRMList reads the ids of the local sources or destinations.
func cerebrumRMList(ctx context.Context, sess *cerebrumnb.Session, kind cerebrumnb.RMKind) ([]string, error) {
	want := cerebrumStateWant{SrcMne: true, StrictMne: true, Verb: "rm-add"}
	if kind == cerebrumnb.RMDestinations {
		want = cerebrumStateWant{DstMne: true, StrictMne: true, Verb: "rm-add"}
	}
	st, err := cerebrumObtainState(ctx, sess, "0.0.0.0", "ROUTER", 15*time.Second, want)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(st.Src)+len(st.Dst))
	for _, r := range append(st.Src, st.Dst...) {
		ids = append(ids, r.ID)
	}
	return ids, nil
}

// cerebrumRMAwait reads the list until the new ids are there: ACCEPTED is
// queued, as on a device configuration.
func cerebrumRMAwait(ctx context.Context, sess *cerebrumnb.Session, kind cerebrumnb.RMKind, before []string, add int) ([]string, error) {
	known := make(map[string]bool, len(before))
	for _, id := range before {
		known[id] = true
	}
	deadline := time.Now().Add(cerebrumDeviceSettle)
	for {
		now, err := cerebrumRMList(ctx, sess, kind)
		if err != nil {
			return nil, err
		}
		var fresh []string
		for _, id := range now {
			if !known[id] {
				fresh = append(fresh, id)
			}
		}
		if len(fresh) >= add {
			sortCerebrumIDs(fresh)
			return fresh, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("cerebrum-nb rm-add: accepted, but the list shows %d new of %d after %s", len(fresh), add, cerebrumDeviceSettle)
		}
		time.Sleep(time.Second)
	}
}

func cerebrumRMDelete(_ context.Context, args []string) error {
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb rm-delete", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	kind := fs.String("kind", "", "source | destination")
	id := fs.String("id", "", "comma-separated existing numeric ids, e.g. 21,22,23")
	federation := fs.Bool("federation", false, "delete them in the federation (FEDERATION=1)")
	wf := addCerebrumWriteFlags(fs)
	output := fs.String("output", "text", "output format: text | json (ADR-0002)")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	k, err := rmKind(*kind, "rm-delete")
	if err != nil {
		return err
	}
	ids, err := rmIDs(*id)
	if err != nil {
		return err
	}
	jsonOut, oerr := resolveEnsureOutput(*output, false)
	if oerr != nil {
		return oerr
	}
	if err := wf.guard("rm-delete", fs.Args(), *wf.dryRun); err != nil {
		return err
	}
	if *wf.dryRun {
		wf.dryRunNote("rm-delete", fmt.Sprintf("%s %s (federation=%t)", k, strings.Join(ids, ","), *federation))
		return nil
	}
	p, sess, _, err := dialCerebrumAuth(cf, fs.Args(), "rm-delete")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	ctx, cancel := context.WithTimeout(context.Background(), cf.timeout)
	defer cancel()
	res, err := sess.RMDelete(ctx, k, ids, *federation)
	if err != nil {
		return fmt.Errorf("cerebrum-nb rm-delete: %w", err)
	}
	return printRMResult(jsonOut, "rm-delete", k, res, map[string]any{"ids": ids, "federation": *federation})
}

// rmKind maps the operator's word to the RM_* collection.
func rmKind(s, verb string) (cerebrumnb.RMKind, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "source", "sources", "srce":
		return cerebrumnb.RMSources, nil
	case "destination", "destinations", "dest":
		return cerebrumnb.RMDestinations, nil
	}
	return "", cerebrumValErr(verb, "--kind must be source or destination")
}

// rmIDs parses the comma list of §4.2.4: existing numeric ids, no empty
// element, no 0 — the server rejects those, so the verb does first.
func rmIDs(s string) ([]string, error) {
	if strings.TrimSpace(s) == "" {
		return nil, cerebrumValErr("rm-delete", "--id is required (comma-separated numeric ids)")
	}
	var ids []string
	for _, part := range strings.Split(s, ",") {
		p := strings.TrimSpace(part)
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 {
			return nil, cerebrumValErr("rm-delete", fmt.Sprintf("--id element %q is not a numeric id >= 1", part))
		}
		ids = append(ids, strconv.Itoa(n))
	}
	return ids, nil
}

// printRMResult reports the §4.2.5 result; a FAILED result is an error
// after it is printed, so a play sees a non-zero exit.
func printRMResult(jsonOut bool, verb string, kind cerebrumnb.RMKind, res *codec.RoutingResult, extra map[string]any) error {
	if jsonOut {
		out := map[string]any{"verb": verb, "kind": string(kind), "result": res.Value, "accepted": res.Accepted}
		for k, v := range extra {
			out[k] = v
		}
		if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
			return err
		}
	} else {
		fmt.Printf("[%s] RESULT=%s %s %s\n", verb, res.Value, kind, rmExtraText(extra))
	}
	if !res.Accepted {
		return fmt.Errorf("cerebrum-nb %s: the server answered %q", verb, res.Value)
	}
	return nil
}

// rmExtraText words the request parameters for the terminal, in a fixed
// key order so the line is stable.
func rmExtraText(extra map[string]any) string {
	var parts []string
	for _, k := range []string{"count", "ids", "virtual", "federation"} {
		if v, ok := extra[k]; ok {
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
		}
	}
	return strings.Join(parts, " ")
}
