package main

import (
	"context"
	"encoding/json"
	"errors"
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
	// RRCS deletes what a client buffered when that client's connection
	// closes (RRCS 9.0.RR1 log, oracle 2026-11-07: "Deleting 1 buffered
	// configuration changes: No connection to client"). A buffer and its
	// apply therefore go in one run, on one connection.
	buffered := strings.HasPrefix(op.Method, "BufferConfigurationChange")
	apply := new(string)
	if buffered {
		apply = fs.String("apply", "yes", "yes = send the matching Apply… on the same connection before closing it; no = buffer only (RRCS drops it when this run ends)")
	}
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
		if buffered {
			if err := rrcsBufferable(op.Method, raw); err != nil {
				return rrcsValErr(verb, err.Error())
			}
		}
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
		return fmt.Errorf("rrcs %s: %w%s", verb, err, rrcsCodeHint(op.Method, err))
	}
	if cf.output == "json" {
		fmt.Println(rrcsCompact(reply.Value))
		return nil
	}
	if buffered && strings.EqualFold(*apply, "yes") {
		applyMethod := strings.Replace(op.Method, "Buffer", "Apply", 1)
		cf.say("rrcs %s: %s on the same connection", verb, applyMethod)
		if _, aerr := client.Call(ctx, applyMethod); aerr != nil {
			return fmt.Errorf("rrcs %s: %s: %w", verb, applyMethod, aerr)
		}
		fmt.Printf("%s + %s: accepted by RRCS (error code 0)\n", op.Method, applyMethod)
		return nil
	}
	if !op.Read && len(reply.Payload().Items) == 0 {
		fmt.Printf("%s: accepted by RRCS (error code 0)\n", op.Method)
		return nil
	}
	fmt.Println(rrcsCompact(reply.Payload()))
	return nil
}

// rrcsBufferTypes are the object types §8.10.2 lists for
// BufferConfigurationChange; the Ex form adds portex, monitor-port-cmd
// and device (§ change history, 8.5 and later). Any other type is
// accepted by RRCS and then dropped without a word: a conference edit
// buffered and applied on the oracle (2026-11-07) answered success and
// changed nothing.
var rrcsBufferTypes = map[string]bool{
	"port": true, "group": true, "call-to-port-cmd": true, "route-cmd": true,
	"remote-key-cmd": true, "reply-cmd": true, "edit-ifb-cmd": true,
	"dim-panel-speaker-cmd": true, "auto-listen-off-cmd": true,
	"set-input-output-gain-cmd": true, "sidetone-cmd": true, "send-string-cmd": true,
	"command-container": true, "client-card": true, "panel-key": true,
	"virtual-key": true, "device": true,
}

var rrcsBufferTypesEx = map[string]bool{"portex": true, "monitor-port-cmd": true}

// rrcsBufferable refuses a buffered change of a type the buffer does not
// take, before anything is sent.
func rrcsBufferable(method string, raw []byte) error {
	var changes []struct {
		ObjectType string `json:"ObjectType"`
	}
	if err := json.Unmarshal(raw, &changes); err != nil {
		return fmt.Errorf("--file: want one JSON array of changes: %v", err)
	}
	ex := strings.HasSuffix(method, "Ex")
	for i, c := range changes {
		t := strings.ToLower(c.ObjectType)
		if rrcsBufferTypes[t] || (ex && rrcsBufferTypesEx[t]) {
			continue
		}
		return fmt.Errorf("change %d: %s cannot be buffered (§8.10.2); RRCS would accept it and drop it — send it with configuration-change instead", i+1, c.ObjectType)
	}
	return nil
}

// rrcsCodeHint names the cause behind an error code where the oracle
// showed one the code alone does not say.
func rrcsCodeHint(method string, err error) string {
	var ce *codec.CodeError
	if !errors.As(err, &ce) {
		return ""
	}
	switch {
	case ce.Code == codec.CodeNoPermission && (method == "SetInputGain" || method == "SetOutputGain"):
		// Oracle, 2026-11-07: the port's Director setting "Gain values set
		// by" was "the configuration (with Director)"; RRCS carries no
		// property for it.
		return " — the port is set to take its gains from the Director configuration; in Director, port → Gain → \"Gain values set by: the Set Input/Output Gain function from a panel or by remote software (RRCS)\", then this works"
	}
	return ""
}
