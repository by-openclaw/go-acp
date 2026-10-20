package main

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"dhs/internal/rrcs/codec"
)

// rrcsProps collects repeated --prop NAME=VALUE flags.
type rrcsProps []string

func (p *rrcsProps) String() string     { return strings.Join(*p, ",") }
func (p *rrcsProps) Set(v string) error { *p = append(*p, v); return nil }

// rrcsPropValue types a value the way the lists print it: true and false
// are booleans, whole numbers are integers, the rest is text. A value in
// double quotes is always text.
func rrcsPropValue(v string) codec.Value {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		return codec.String(v[1 : len(v)-1])
	}
	switch v {
	case "true":
		return codec.Bool(true)
	case "false":
		return codec.Bool(false)
	}
	if n, err := strconv.ParseInt(v, 10, 32); err == nil {
		return codec.Int(int32(n))
	}
	return codec.String(v)
}

// rrcsShow prints the wanted fields with the value the object holds.
func rrcsShow(title string, c *rrcsChange, props map[string]any) {
	fmt.Println(title)
	for _, block := range c.Order {
		for _, f := range c.Blocks[block] {
			name, holder := f.Name, props
			if block != "" {
				name, holder = block+"."+f.Name, jMap(props[block])
			}
			now := "(not reported)"
			if v, ok := holder[f.Name]; ok {
				now = fmt.Sprint(v)
			}
			fmt.Printf("  %-34s %-22s wanted %s\n", name, now, rrcsCompact(f.Value))
		}
	}
}

// rrcsSet edits properties of a port or of a client card.
func rrcsSet(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("rrcs set", flag.ContinueOnError)
	cf := newRRCSFlags(fs)
	path := fs.String("path", "", "what to change: a port (net.1.node.61.port.1026, …port.7.out) or a client card (net.1.node.60.card.1)")
	var props rrcsProps
	fs.Var(&props, "prop", "NAME=VALUE or BLOCK.NAME=VALUE, with the names get --path prints; repeat for several. Streams: PortAes67Output.Multicast=239.1.1.1 PortAes67Output.MulticastPort=5004 PortAes67Input.SourceIp=… (.Protocol 2 Manual, 3 RTSP, 5 NMOS). Cards: Ptp.PTP=100 Ptp.PtpPriority=128 Nmos.RegistrationIp=… Nmos.RegistrationMode=2 Media_1.DefaultGateway=…")
	writeTo := fs.String("write-to", "", rrcsWriteToHelp)
	apply := fs.String("apply", "no", "no = show the change and what the object holds now, send nothing | yes = send it (ConfigurationChange) and read the object back")
	nmosTest := fs.String("nmos-test", "no", "yes = let one edit through that the tool refuses otherwise: a stream put in NMOS mode with the mode alone (--prop PortAes67Output.Protocol=5 and nothing else). A test, for a system that can be restarted: two edits towards NMOS that also carried an address stopped RRCS (ADR-0035)")
	if err := parseVerbFlags(fs, reorderFlagsFirst(args)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return rrcsValErr("set", "want exactly one host[:port] argument")
	}
	if *apply != "yes" && *apply != "no" {
		return rrcsValErr("set", "--apply must be yes or no")
	}
	if *path == "" || len(props) == 0 {
		return rrcsValErr("set", "want --path and at least one --prop NAME=VALUE")
	}
	if *apply == "yes" {
		if err := rrcsWriteGuard("set", fs.Arg(0), *writeTo); err != nil {
			return err
		}
	}
	change, err := rrcsChangeFor(*path)
	if err != nil {
		return rrcsValErr("set", err.Error())
	}
	for _, p := range props {
		name, value, ok := strings.Cut(p, "=")
		if !ok || name == "" {
			return rrcsValErr("set", "--prop "+p+": want NAME=VALUE")
		}
		if err := change.Add(name, rrcsPropValue(value)); err != nil {
			return rrcsValErr("set", err.Error())
		}
	}
	client, _, closeFn, err := cf.open("set", fs.Arg(0))
	if err != nil {
		return err
	}
	defer closeFn()

	// Top-level properties are where label, alias and gains live.
	_, topLevel := change.Blocks[""]
	before, err := rrcsCurrent(ctx, client, *path, topLevel)
	if err != nil {
		return err
	}
	request := change.Request()
	doc, err := codec.EncodeCall(rrcsChangeMethod, codec.String("C0000000000"), request)
	if err != nil {
		return err
	}
	fmt.Printf("%s  %s  %s\n", *path, change.ObjectType, jStr(before, "LongName"))
	rrcsShow("now:", change, before)
	if block := change.NMOSStream(before); block != "" {
		switch {
		case *nmosTest != "yes":
			fmt.Println(block + ": " + rrcsNMOSRefusal)
			if *apply == "yes" {
				return rrcsValErr("set", block+": "+rrcsNMOSRefusal)
			}
		case len(change.Blocks[block]) != 1 || change.Blocks[block][0].Name != "Protocol":
			// The test is one question: does the mode alone pass. A
			// request that also carries an address is the one that
			// stopped RRCS, and stays refused.
			return rrcsValErr("set", "--nmos-test yes takes one property and no other: "+block+".Protocol=5")
		default:
			fmt.Println(block + ": TEST of an edit towards NMOS mode with the mode alone, no address field. Both edits that stopped RRCS carried address fields; this form has never been sent.")
		}
	}
	if *apply == "no" {
		fmt.Println("request that --apply yes would send (the transaction key is a placeholder):")
		fmt.Println(string(doc))
		fmt.Println("nothing sent")
		return nil
	}

	cf.say("rrcs set: sending %s edit %s %s", rrcsChangeMethod, change.ObjectType, *path)
	reply, err := client.Call(ctx, rrcsChangeMethod, request)
	if err != nil {
		cf.say("rrcs set: refused: %v", err)
		return fmt.Errorf("rrcs set: %w", err)
	}
	cf.say("rrcs set: answer %s", rrcsCompact(reply.Value))
	after, err := rrcsCurrent(ctx, client, *path, topLevel)
	if err != nil {
		return fmt.Errorf("rrcs set: sent, but the read back failed: %w", err)
	}
	rrcsShow("read back:", change, after)

	// The change is done only if the object now holds every wanted value.
	var differ []string
	var logged []rrcsDiffEntry
	var notTaken []rrcsEnsureFailure
	defer func() { cf.logChanges(ctx, "set", fs.Arg(0), false, logged, notTaken) }()
	for _, block := range change.Order {
		for _, f := range change.Blocks[block] {
			holder, name := after, f.Name
			was := before
			if block != "" {
				holder, name = jMap(after[block]), block+"."+f.Name
				was = jMap(before[block])
			}
			if fmt.Sprint(holder[f.Name]) == fmt.Sprint(rrcsJSON(f.Value)) || holder[f.Name] == nil {
				logged = append(logged, rrcsDiffEntry{Field: *path + "." + name, From: fmt.Sprint(was[f.Name]), To: fmt.Sprint(rrcsJSON(f.Value))})
			} else {
				notTaken = append(notTaken, rrcsEnsureFailure{Field: *path + "." + name, Reason: "not_taken: RRCS accepted the request and still reports " + fmt.Sprint(holder[f.Name])})
			}
			// What RRCS never reports (Mode, the port a stream is linked
			// to) cannot be read back; it is said, not counted.
			if _, reported := holder[f.Name]; !reported {
				fmt.Printf("  note: %s was sent and accepted. RRCS never reports this value back, so it is not an error that it shows as not reported\n", name)
				continue
			}
			if fmt.Sprint(holder[f.Name]) != fmt.Sprint(rrcsJSON(f.Value)) {
				differ = append(differ, name)
			}
		}
	}
	if len(differ) > 0 {
		sort.Strings(differ)
		return fmt.Errorf("rrcs set: RRCS accepted the request but these still differ: %s", strings.Join(differ, ", "))
	}
	fmt.Println("done: every wanted value reads back")
	return nil
}

// rrcsWriteToHelp describes the guard every writing verb carries.
const rrcsWriteToHelp = "required to write: repeat the host here, exactly as given. A write is refused without it. On 2026-10-06 a ConfigurationChangeEx edit of an AES67 stream made a production RRCS 9.0 drop the connection and stop answering; configuration writes now use ConfigurationChange and are unproven until one passes on a real RRCS"

// rrcsWriteGuard refuses a write unless the operator named the target a
// second time.
func rrcsWriteGuard(verb, host, writeTo string) error {
	if writeTo == "" {
		return rrcsValErr(verb, "this would WRITE to "+host+": add --write-to "+host+" to confirm the target (see --help), or stay with the dry run")
	}
	if writeTo != host {
		return rrcsValErr(verb, "--write-to "+writeTo+" does not match the host "+host)
	}
	return nil
}
