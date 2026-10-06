package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"dhs/internal/rrcs/codec"
)

// rrcsDesired is the desired state `ensure` converges to (ADR-0007,
// amendment 2026-08-02: declarative data and matrix connections).
//
//	{
//	  "values": {
//	    "net.1.node.60.card.1.Ptp.PTP": 100,
//	    "net.1.node.61.port.7.out.PortAes67Output.Multicast": "239.1.1.1",
//	    "net.1.node.61.port.7.out.PortAes67Output.Protocol": "Manual"
//	  },
//	  "crosspoints": [
//	    {"source": "net.1.node.61.port.7.in", "destination": "net.1.node.61.port.1026", "state": "present"}
//	  ]
//	}
//
// A value is addressed by the path export prints. Only what the file
// names is touched: ensure never removes what it does not mention.
type rrcsDesired struct {
	Values      map[string]json.RawMessage `json:"values"`
	Crosspoints []rrcsDesiredXp            `json:"crosspoints"`
}

type rrcsDesiredXp struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	State       string `json:"state"` // present (default) | absent
}

// rrcsDiffEntry is one entry of the ADR-0007 diff array.
type rrcsDiffEntry struct {
	Field string `json:"field"`
	From  any    `json:"from"`
	To    any    `json:"to"`
}

// rrcsEnsureFailure is one field that could not be brought to its target.
type rrcsEnsureFailure struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// rrcsScalarText prints a JSON scalar the way export prints a value.
func rrcsScalarText(raw json.RawMessage) (string, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", false
	}
	switch t := v.(type) {
	case string:
		return t, true
	case bool:
		return strconv.FormatBool(t), true
	case json.Number:
		return t.String(), true
	}
	return "", false
}

// rrcsEnsure converges the gateway to a desired-state file.
func rrcsEnsure(ctx context.Context, args []string) error {
	// --check and --diff are flags without a value (ADR-0007); they are
	// taken out before the flags are reordered around the host.
	check := false
	rest := make([]string, 0, len(args))
	for _, a := range args {
		switch a {
		case "--check", "-check", "--check=true":
			check = true
		case "--diff", "-diff", "--diff=true": // the diff is always emitted
		default:
			rest = append(rest, a)
		}
	}
	fs := flag.NewFlagSet("rrcs ensure", flag.ContinueOnError)
	fs.Usage = func() {
		_, _ = fmt.Fprint(fs.Output(), `Usage: dhs consumer rrcs ensure <host>[:port] --file DESIRED.json [--check] [flags]

Brings the gateway to the state a file describes and reports what changed,
in the contract of every dhs ensure (ADR-0007): run it twice and the second
run changes nothing.

  --check   dry-run: compare and report would_change, send nothing
  --diff    accepted; the diff is always part of the output

The file names values by the path export prints, and crosspoints by their
two ports:
  {"values": {"net.1.node.60.card.1.Ptp.PTP": 100},
   "crosspoints": [{"source": "…port.7.in", "destination": "…port.1026", "state": "present"}]}
Only what the file names is touched. Without --check this WRITES and needs
--write-to HOST.

`)
		fs.PrintDefaults()
	}
	cf := newRRCSFlags(fs)
	file := fs.String("file", "", "desired-state file, JSON (required)")
	writeTo := fs.String("write-to", "", rrcsWriteToHelp)
	if err := parseVerbFlags(fs, reorderFlagsFirst(rest)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return rrcsValErr("ensure", "want exactly one host[:port] argument")
	}
	if *file == "" {
		return rrcsValErr("ensure", "want --file DESIRED.json")
	}
	raw, err := os.ReadFile(*file)
	if err != nil {
		return fmt.Errorf("rrcs ensure: %w", err)
	}
	var want rrcsDesired
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&want); err != nil {
		return rrcsValErr("ensure", "--file "+*file+": "+err.Error())
	}

	// The values, as the rows import takes.
	paths := make([]string, 0, len(want.Values))
	for p := range want.Values {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	rows := make([]rrcsRow, 0, len(paths))
	for _, p := range paths {
		text, ok := rrcsScalarText(want.Values[p])
		if !ok {
			return rrcsValErr("ensure", "values: "+p+" must be a text, a number or true/false")
		}
		row := rrcsRow{Path: p, Value: text}
		// An enum may be given by its word ("Manual").
		if _, _, field, ok := rrcsSplitPath(p); ok {
			if _, isEnum := rrcsEnums[field]; isEnum {
				if _, err := strconv.Atoi(text); err != nil {
					row.Value, row.ValueName = "", text
				}
			}
		}
		rows = append(rows, row)
	}
	type xp struct {
		field   string
		address []codec.Value
		want    bool
	}
	xps := make([]xp, 0, len(want.Crosspoints))
	for i, c := range want.Crosspoints {
		sNode, sPort, _, ok1 := rrcsPortOfPath(c.Source)
		dNode, dPort, _, ok2 := rrcsPortOfPath(c.Destination)
		if !ok1 || !ok2 {
			return rrcsValErr("ensure", fmt.Sprintf("crosspoints[%d]: source and destination must be port paths", i))
		}
		if c.State != "" && c.State != "present" && c.State != "absent" {
			return rrcsValErr("ensure", fmt.Sprintf("crosspoints[%d]: state must be present or absent", i))
		}
		xps = append(xps, xp{
			field: "xp." + c.Source + ">" + c.Destination,
			address: []codec.Value{codec.Int(1), codec.Int(int32(sNode)), codec.Int(int32(sPort)),
				codec.Int(1), codec.Int(int32(dNode)), codec.Int(int32(dPort))},
			want: c.State != "absent",
		})
	}
	if !check {
		if err := rrcsWriteGuard("ensure", fs.Arg(0), *writeTo); err != nil {
			return err
		}
	}
	client, _, closeFn, err := cf.open("ensure", fs.Arg(0))
	if err != nil {
		return err
	}
	defer closeFn()

	diff := []rrcsDiffEntry{}
	before, after := map[string]any{}, map[string]any{}
	failures := []rrcsEnsureFailure{}
	applied, skips, _, _, err := rrcsConverge(ctx, client, rows, func(string) bool { return true }, check)
	if err != nil {
		return fmt.Errorf("rrcs ensure: %w", err)
	}
	for _, s := range skips {
		// A value the file asks for and the gateway cannot take is a
		// failure of the request, not something to pass over.
		failures = append(failures, rrcsEnsureFailure{Field: s.Path, Reason: strings.TrimSpace(s.Reason + " " + s.Detail)})
	}
	for _, a := range applied {
		switch a.Result {
		case "would_apply", "applied":
			diff = append(diff, rrcsDiffEntry{Field: a.Path, From: a.Live, To: a.Wanted})
			before[a.Path], after[a.Path] = a.Live, a.Wanted
		default:
			failures = append(failures, rrcsEnsureFailure{Field: a.Path, Reason: strings.TrimSpace(a.Result + " " + a.Error)})
		}
	}
	onOff := map[bool]string{true: "present", false: "absent"}
	for _, x := range xps {
		reply, err := client.Call(ctx, "GetXpStatus", x.address...)
		if err != nil {
			failures = append(failures, rrcsEnsureFailure{Field: x.field, Reason: err.Error()})
			continue
		}
		p := reply.Payload()
		now := len(p.Items) > 0 && p.Items[0].Kind == codec.KindBool && p.Items[0].Bool
		if now == x.want {
			continue
		}
		if !check {
			method := map[bool]string{true: "SetXp", false: "KillXp"}[x.want]
			cf.say("rrcs ensure: %s %s", method, x.field)
			if _, err := client.Call(ctx, method, x.address...); err != nil {
				failures = append(failures, rrcsEnsureFailure{Field: x.field, Reason: err.Error()})
				continue
			}
			reply, err := client.Call(ctx, "GetXpStatus", x.address...)
			p := reply.Payload()
			if err != nil || len(p.Items) == 0 || p.Items[0].Bool != x.want {
				failures = append(failures, rrcsEnsureFailure{Field: x.field, Reason: "not_taken: RRCS accepted the request and the crosspoint did not change"})
				continue
			}
		}
		diff = append(diff, rrcsDiffEntry{Field: x.field, From: onOff[now], To: onOff[x.want]})
		before[x.field], after[x.field] = onOff[now], onOff[x.want]
	}

	// The ADR-0007 shapes. The failures ride along; the exit code says
	// whether the run reached its target.
	var doc any
	if check {
		doc = struct {
			WouldChange bool                `json:"would_change"`
			Current     map[string]any      `json:"current"`
			Target      map[string]any      `json:"target"`
			Diff        []rrcsDiffEntry     `json:"diff"`
			Failed      []rrcsEnsureFailure `json:"failed"`
		}{len(diff) > 0, before, after, diff, failures}
	} else {
		doc = struct {
			Changed  bool                `json:"changed"`
			Previous map[string]any      `json:"previous"`
			Current  map[string]any      `json:"current"`
			Diff     []rrcsDiffEntry     `json:"diff"`
			Failed   []rrcsEnsureFailure `json:"failed"`
		}{len(diff) > 0, before, after, diff, failures}
	}
	if cf.output == "json" {
		b, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
	} else {
		verb := map[bool]string{true: "would change", false: "changed"}[check]
		for _, d := range diff {
			fmt.Printf("%-13s %-64s %v -> %v\n", verb, d.Field, d.From, d.To)
		}
		for _, f := range failures {
			fmt.Printf("%-13s %-64s %s\n", "failed", f.Field, f.Reason)
		}
		fmt.Printf("%s %d, failed %d, of %d values and %d crosspoints\n", verb, len(diff), len(failures), len(rows), len(xps))
	}
	if len(failures) > 0 {
		return fmt.Errorf("rrcs ensure: %d field(s) could not be brought to their target", len(failures))
	}
	return nil
}
