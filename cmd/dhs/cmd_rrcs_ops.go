package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"dhs/internal/rrcs/codec"
)

// rrcsOpVerb is the verb of a method: SetXpPrio → set-xp-prio.
func rrcsOpVerb(method string) string {
	var b strings.Builder
	for i, r := range method {
		upper := r >= 'A' && r <= 'Z'
		if upper && i > 0 {
			prev := method[i-1]
			nextLower := i+1 < len(method) && method[i+1] >= 'a' && method[i+1] <= 'z'
			if (prev >= 'a' && prev <= 'z') || (prev >= 'A' && prev <= 'Z' && nextLower) {
				b.WriteByte('-')
			}
		}
		b.WriteString(strings.ToLower(string(r)))
	}
	return b.String()
}

func rrcsFindOp(verb string) *rrcsOp {
	for i := range rrcsOps {
		if rrcsOpVerb(rrcsOps[i].Method) == verb {
			return &rrcsOps[i]
		}
	}
	return nil
}

// rrcsRunOp runs one method of the table.
func rrcsRunOp(ctx context.Context, op *rrcsOp, args []string) error {
	verb := rrcsOpVerb(op.Method)
	fs := flag.NewFlagSet("rrcs "+verb, flag.ContinueOnError)
	cf := newRRCSFlags(fs)
	writeTo := fs.String("write-to", "", rrcsWriteToHelp)
	texts := map[string]*string{}
	var xps rrcsProps
	for _, p := range op.Params {
		switch p.Kind {
		case "xp":
			texts["src"] = fs.String("src", "", "source: the path of a port that has an input")
			texts["dst"] = fs.String("dst", "", "destination: the path of a port that has an output")
		case "isinput":
		case "xplist":
			fs.Var(&xps, p.Flag, p.Help)
		default:
			if _, dup := texts[p.Flag]; !dup {
				texts[p.Flag] = fs.String(p.Flag, "", p.Help)
			}
		}
	}
	fs.Usage = func() {
		guard := "It only reads."
		if !op.Read {
			guard = "It WRITES: it needs --write-to HOST, and has not passed on a real RRCS."
		}
		_, _ = fmt.Fprintf(fs.Output(), "Usage: dhs consumer rrcs %s <host>[:port] [flags]\n\n%s: %s. %s\n\n", verb, op.Method, op.About, guard)
		fs.PrintDefaults()
	}
	if err := parseVerbFlags(fs, reorderFlagsFirst(args)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return rrcsValErr(verb, "want exactly one host[:port] argument")
	}
	named := map[string]string{}
	for name, v := range texts {
		named[name] = *v
	}
	for _, p := range op.Params {
		if p.Kind != "changes" || named[p.Flag] == "" {
			continue
		}
		raw, rerr := os.ReadFile(named[p.Flag])
		if rerr != nil {
			return fmt.Errorf("rrcs %s: %w", verb, rerr)
		}
		named[p.Flag] = string(raw)
	}
	values, err := op.Values(named, xps)
	if err != nil {
		return rrcsValErr(verb, err.Error())
	}
	if !op.Read {
		if err := rrcsWriteGuard(verb, fs.Arg(0), *writeTo); err != nil {
			return err
		}
	}
	client, _, closeFn, err := cf.open(verb, fs.Arg(0))
	if err != nil {
		return err
	}
	defer closeFn()
	cf.say("rrcs %s: %s %s", verb, op.Method, rrcsCompact(codec.Array(values...)))
	reply, err := client.Call(ctx, op.Method, values...)
	if err != nil {
		cf.say("rrcs %s: %v", verb, err)
		return fmt.Errorf("rrcs %s: %w", verb, err)
	}
	if cf.output == "json" {
		fmt.Println(rrcsCompact(reply.Value))
		return nil
	}
	if !op.Read && len(reply.Payload().Items) == 0 {
		fmt.Printf("%s: accepted by RRCS (error code 0)\n", op.Method)
		return nil
	}
	fmt.Println(rrcsCompact(reply.Payload()))
	return nil
}
