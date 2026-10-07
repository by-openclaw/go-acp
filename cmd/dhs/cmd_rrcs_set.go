package main

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"dhs/internal/rrcs/codec"
	rrcs "dhs/internal/rrcs/consumer"
)

// rrcsProps collects repeated --prop NAME=VALUE flags.
type rrcsProps []string

func (p *rrcsProps) String() string     { return strings.Join(*p, ",") }
func (p *rrcsProps) Set(v string) error { *p = append(*p, v); return nil }

// rrcsCardAes67 are the client-card blocks the specification puts inside
// the Aes67 struct of a change (§8.10.4.34), while the lists print them
// at the top of the card.
var rrcsCardAes67 = map[string]bool{"Media_1": true, "Media_2": true, "Ptp": true, "Dns": true}

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

// rrcsChange is one edit, ready to send.
type rrcsChange struct {
	objectType string
	// address identifies the object inside SpecificParams.
	address []codec.Member
	// blocks holds the wanted members, by block ("" = top level).
	blocks map[string][]codec.Member
	// order keeps the blocks in the order they were given.
	order []string
}

func (c *rrcsChange) add(name string, value codec.Value) error {
	block, field, nested := strings.Cut(name, ".")
	if !nested {
		block, field = "", name
	}
	if field == "" || strings.Contains(field, ".") {
		return fmt.Errorf("--prop %s: want NAME=VALUE or BLOCK.NAME=VALUE", name)
	}
	for _, m := range c.blocks[block] {
		if m.Name == field {
			return fmt.Errorf("--prop %s given twice", name)
		}
	}
	if _, seen := c.blocks[block]; !seen {
		c.order = append(c.order, block)
	}
	c.blocks[block] = append(c.blocks[block], codec.Member{Name: field, Value: value})
	return nil
}

// params builds SpecificParams.
func (c *rrcsChange) params() codec.Value {
	members := append([]codec.Member{}, c.address...)
	var aes67 []codec.Member
	for _, block := range c.order {
		fields := c.blocks[block]
		switch {
		case block == "":
			members = append(members, fields...)
		case c.objectType == "client-card" && rrcsCardAes67[block]:
			aes67 = append(aes67, codec.Member{Name: block, Value: codec.Struct(fields...)})
		default:
			members = append(members, codec.Member{Name: block, Value: codec.Struct(fields...)})
		}
	}
	if len(aes67) > 0 {
		members = append(members, codec.Member{Name: "Aes67", Value: codec.Struct(aes67...)})
	}
	return codec.Struct(members...)
}

// request is the parameter of ConfigurationChangeEx (§8.10.1): an array
// of changes, here one.
func (c *rrcsChange) request() codec.Value {
	return codec.Array(codec.Struct(
		codec.Member{Name: "ChangeType", Value: codec.String("edit")},
		codec.Member{Name: "ObjectType", Value: codec.String(c.objectType)},
		codec.Member{Name: "SpecificParams", Value: c.params()},
	))
}

// rrcsChangeFor reads a path into the object type and the address of a
// change.
func rrcsChangeFor(path string) (*rrcsChange, error) {
	c := &rrcsChange{blocks: map[string][]codec.Member{}}
	parts := strings.Split(path, ".")
	if len(parts) == 6 && parts[0] == "net" && parts[2] == "node" && parts[4] == "card" {
		node, err1 := strconv.Atoi(parts[3])
		bay, err2 := strconv.Atoi(parts[5])
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("--path %s: not a client card path", path)
		}
		c.objectType = "client-card"
		c.address = []codec.Member{
			{Name: "Node", Value: codec.Int(int32(node))},
			{Name: "Slot", Value: codec.Int(int32(bay))},
		}
		return c, nil
	}
	node, port, isInput, ok := rrcsPortOfPath(path)
	if !ok || len(parts) > 7 || (len(parts) == 7 && parts[6] != "in" && parts[6] != "out") {
		return nil, fmt.Errorf("--path %s: set takes a port path or a client card path", path)
	}
	// §8.10.4.6: PortAddress is a TPortAddress; IsInput is given only
	// where an input and an output share the number.
	addr := []codec.Member{}
	if len(parts) == 7 {
		addr = append(addr, codec.Member{Name: "IsInput", Value: codec.Bool(isInput)})
	}
	addr = append(addr,
		codec.Member{Name: "Node", Value: codec.Int(int32(node))},
		codec.Member{Name: "Port", Value: codec.Int(int32(port))},
	)
	c.objectType = "portex"
	c.address = []codec.Member{{Name: "PortAddress", Value: codec.Struct(addr...)}}
	return c, nil
}

// rrcsCurrent reads what the gateway holds now for the object of a path.
func rrcsCurrent(ctx context.Context, client *rrcs.Client, path string) (map[string]any, error) {
	// For a port, its label, alias and gains are read too: they come
	// from requests of their own and may be what is being set.
	opts := rrcsCollectOpts{}
	if node, port, _, isPort := rrcsPortOfPath(path); isPort {
		opts.values = true
		opts.onlyPort = func(n, p int, _ bool) bool { return n == node && p == port }
	}
	snap, _, err := rrcsCollect(ctx, client, opts)
	if err != nil {
		return nil, err
	}
	m, err := rrcsModelOf(snap)
	if err != nil {
		return nil, err
	}
	props, ok := m.lookup(path)
	if !ok {
		return nil, fmt.Errorf("rrcs set: nothing at %s", path)
	}
	return props, nil
}

// rrcsShow prints the wanted fields with the value the object holds.
func rrcsShow(title string, c *rrcsChange, props map[string]any) {
	fmt.Println(title)
	for _, block := range c.order {
		for _, f := range c.blocks[block] {
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
		if err := change.add(name, rrcsPropValue(value)); err != nil {
			return rrcsValErr("set", err.Error())
		}
	}
	client, _, closeFn, err := cf.open("set", fs.Arg(0))
	if err != nil {
		return err
	}
	defer closeFn()

	before, err := rrcsCurrent(ctx, client, *path)
	if err != nil {
		return err
	}
	request := change.request()
	doc, err := codec.EncodeCall(rrcsChangeMethod, codec.String("C0000000000"), request)
	if err != nil {
		return err
	}
	fmt.Printf("%s  %s  %s\n", *path, change.objectType, jStr(before, "LongName"))
	rrcsShow("now:", change, before)
	if *apply == "no" {
		fmt.Println("request that --apply yes would send (the transaction key is a placeholder):")
		fmt.Println(string(doc))
		fmt.Println("nothing sent")
		return nil
	}

	cf.say("rrcs set: sending %s edit %s %s", rrcsChangeMethod, change.objectType, *path)
	reply, err := client.Call(ctx, rrcsChangeMethod, request)
	if err != nil {
		cf.say("rrcs set: refused: %v", err)
		return fmt.Errorf("rrcs set: %w", err)
	}
	cf.say("rrcs set: answer %s", rrcsCompact(reply.Value))
	after, err := rrcsCurrent(ctx, client, *path)
	if err != nil {
		return fmt.Errorf("rrcs set: sent, but the read back failed: %w", err)
	}
	rrcsShow("read back:", change, after)

	// The change is done only if the object now holds every wanted value.
	var differ []string
	for _, block := range change.order {
		for _, f := range change.blocks[block] {
			holder, name := after, f.Name
			if block != "" {
				holder, name = jMap(after[block]), block+"."+f.Name
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
