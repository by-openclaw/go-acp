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

	"dhs/internal/cerebrum-nb/codec"
	cerebrumnb "dhs/internal/cerebrum-nb/consumer"
)

func cerebrumRMAdd(_ context.Context, args []string) error {
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb rm-add", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	kind := fs.String("kind", "", "source | destination")
	count := fs.Int("count", 1, "number to create, 1-1000")
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
	jsonOut, oerr := resolveEnsureOutput(*output, false)
	if oerr != nil {
		return oerr
	}
	p, sess, _, err := dialCerebrumAuth(cf, fs.Args(), "rm-add")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	ctx, cancel := context.WithTimeout(context.Background(), cf.timeout)
	defer cancel()
	res, err := sess.RMAdd(ctx, k, *count, *virtual, *federation)
	if err != nil {
		return fmt.Errorf("cerebrum-nb rm-add: %w", err)
	}
	return printRMResult(jsonOut, "rm-add", k, res, map[string]any{"count": *count, "virtual": *virtual, "federation": *federation})
}

func cerebrumRMDelete(_ context.Context, args []string) error {
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb rm-delete", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	kind := fs.String("kind", "", "source | destination")
	id := fs.String("id", "", "comma-separated existing numeric ids, e.g. 21,22,23")
	federation := fs.Bool("federation", false, "delete them in the federation (FEDERATION=1)")
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
