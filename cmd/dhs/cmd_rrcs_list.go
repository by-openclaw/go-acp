package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
)

// rrcsListKinds are what `list` prints.
var rrcsListKinds = []string{
	"nodes", "cards", "ports", "panels", "keys", "conferences", "groups", "ifbs", "logic",
}

// rrcsSource is where a verb takes the system from: a gateway, or a
// snapshot `walk` wrote.
type rrcsSource struct {
	cf   *rrcsFlags
	from *string
}

func newRRCSSource(fs *flag.FlagSet) *rrcsSource {
	return &rrcsSource{
		cf:   newRRCSFlags(fs),
		from: fs.String("from", "", "read a snapshot FILE written by walk instead of asking a gateway; no host argument then"),
	}
}

// model builds the tree. commands says whether the keys are needed;
// onlyPort limits them to some ports on a live gateway.
func (s *rrcsSource) model(ctx context.Context, verb string, hosts []string, commands bool,
	onlyPort func(node, port int, isInput bool) bool) (*rrcsModel, error) {
	var doc []byte
	switch {
	case *s.from != "" && len(hosts) > 0:
		return nil, rrcsValErr(verb, "give a host or --from, not both")
	case *s.from != "":
		if s.cf.output != "text" && s.cf.output != "json" {
			return nil, rrcsValErr(verb, "--output must be text or json")
		}
		raw, err := os.ReadFile(*s.from)
		if err != nil {
			return nil, fmt.Errorf("rrcs %s: %w", verb, err)
		}
		doc = raw
	case len(hosts) != 1:
		return nil, rrcsValErr(verb, "want one host[:port] argument, or --from FILE")
	default:
		client, _, closeFn, err := s.cf.open(verb, hosts[0])
		if err != nil {
			return nil, err
		}
		defer closeFn()
		snap, _, err := rrcsCollect(ctx, client, rrcsCollectOpts{commands: commands, onlyPort: onlyPort})
		if err != nil {
			return nil, err
		}
		if !snap.Complete {
			return nil, fmt.Errorf("rrcs %s: interrupted", verb)
		}
		if doc, err = json.Marshal(snap); err != nil {
			return nil, err
		}
	}
	var snap map[string]any
	if err := json.Unmarshal(doc, &snap); err != nil {
		return nil, fmt.Errorf("rrcs %s: snapshot: %w", verb, err)
	}
	if jStr(snap, "proto") != rrcsProto {
		return nil, fmt.Errorf("rrcs %s: %s is not an rrcs snapshot", verb, *s.from)
	}
	return rrcsBuildModel(snap), nil
}

// rrcsFilter is the selection common to list and tree.
type rrcsFilter struct {
	node  *int
	typ   *string
	match *string
}

func newRRCSFilter(fs *flag.FlagSet) *rrcsFilter {
	return &rrcsFilter{
		node:  fs.Int("node", 0, "keep what sits on this node number; 0 = every node"),
		typ:   fs.String("type", "", "keep the ports whose type contains this text, case ignored (e.g. RSP, Bolero, 4-Wire)"),
		match: fs.String("match", "", "keep the rows where a name, a label or the path contains this text, case ignored"),
	}
}

func rrcsHas(text, part string) bool {
	return part == "" || strings.Contains(strings.ToLower(text), strings.ToLower(part))
}

func (f *rrcsFilter) port(p *rrcsPort) bool {
	return (*f.node == 0 || p.Node == *f.node) && rrcsHas(p.Type, *f.typ) &&
		rrcsHas(p.Path+" "+p.Label+" "+p.LongName, *f.match)
}

func rrcsDir(p *rrcsPort) string {
	switch {
	case p.Input && p.Output:
		return "in+out"
	case p.Input:
		return "in"
	case p.Output:
		return "out"
	}
	return "-"
}

func rrcsTarget(k *rrcsKey) string {
	if k.Target == "" {
		return ""
	}
	if k.TargetName == "" {
		return k.Target
	}
	return k.Target + " (" + k.TargetName + ")"
}

func rrcsMembers(o *rrcsObject) string {
	parts := make([]string, 0, len(o.Members))
	for _, m := range o.Members {
		s := m.Port
		if m.Label != "" {
			s += " (" + m.Label + ")"
		}
		if m.Flags != "" {
			s += " " + m.Flags
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}

// rrcsList prints one kind of thing as a table.
func rrcsList(ctx context.Context, args []string) error {
	if len(args) == 0 || isHelpToken(args[0]) {
		args = append([]string{""}, args...)
	}
	kind, rest := args[0], args[1:]
	fs := flag.NewFlagSet("rrcs list", flag.ContinueOnError)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "Usage: dhs consumer rrcs list <%s> <host>[:port] | --from FILE [flags]\n", strings.Join(rrcsListKinds, "|"))
		fs.PrintDefaults()
	}
	src := newRRCSSource(fs)
	flt := newRRCSFilter(fs)
	if err := parseVerbFlags(fs, reorderFlagsFirst(rest)); err != nil {
		return err
	}
	known := false
	for _, k := range rrcsListKinds {
		known = known || k == kind
	}
	if !known {
		return rrcsValErr("list", "want one of: "+strings.Join(rrcsListKinds, ", "))
	}
	m, err := src.model(ctx, "list", fs.Args(), kind == "keys", nil)
	if err != nil {
		return err
	}

	var rows any
	var header []string
	var cells [][]string
	switch kind {
	case "nodes":
		header = []string{"PATH", "TYPE", "PORTS", "LONG NAME"}
		var out []*rrcsNode
		for _, n := range m.Nodes {
			if (*flt.node == 0 || n.Node == *flt.node) && rrcsHas(n.Path+" "+n.LongName, *flt.match) {
				out = append(out, n)
				cells = append(cells, []string{n.Path, n.Type, strconv.Itoa(n.Ports), n.LongName})
			}
		}
		rows = out
	case "cards":
		header = []string{"PATH", "TYPE", "OBJECT ID", "LONG NAME"}
		var out []*rrcsCard
		for _, c := range m.Cards {
			if (*flt.node == 0 || c.Node == *flt.node) && rrcsHas(c.Path+" "+c.LongName, *flt.match) {
				out = append(out, c)
				cells = append(cells, []string{c.Path, c.Type, strconv.Itoa(c.ObjectID), c.LongName})
			}
		}
		rows = out
	case "ports", "panels":
		header = []string{"PATH", "DIR", "TYPE", "LABEL", "KEYS", "PAGES", "OBJECT ID", "LONG NAME"}
		var out []*rrcsPort
		for _, p := range m.Ports {
			if flt.port(p) && (kind == "ports" || p.KeyCount > 0) {
				out = append(out, p)
				cells = append(cells, []string{p.Path, rrcsDir(p), p.Type, p.Label, strconv.Itoa(p.KeyCount),
					strconv.Itoa(p.Pages), strconv.Itoa(p.ObjectID), p.LongName})
			}
		}
		rows = out
	case "keys":
		header = []string{"PATH", "PANEL", "COMMAND", "TARGET"}
		var out []*rrcsKey
		for _, p := range m.Ports {
			if (*flt.node != 0 && p.Node != *flt.node) || !rrcsHas(p.Type, *flt.typ) {
				continue
			}
			for _, k := range p.Keys {
				if rrcsHas(k.Path+" "+k.PortLabel+" "+k.CommandType+" "+rrcsTarget(k)+" "+k.Description, *flt.match) {
					out = append(out, k)
					cells = append(cells, []string{k.Path, k.PortLabel, k.CommandType, rrcsTarget(k)})
				}
			}
		}
		rows = out
	default:
		objKind := map[string]string{"conferences": "conference", "groups": "group", "ifbs": "ifb", "logic": "logic"}[kind]
		header = []string{"PATH", "LABEL", "LONG NAME", "MEMBERS"}
		if objKind == "logic" {
			header[3] = "STATE"
		}
		var out []*rrcsObject
		for _, o := range m.Objects[objKind] {
			if rrcsHas(o.Path+" "+o.Label+" "+o.LongName+" "+rrcsMembers(o), *flt.match) {
				out = append(out, o)
				last := rrcsMembers(o)
				if objKind == "logic" {
					last = o.State
				}
				cells = append(cells, []string{o.Path, o.Label, o.LongName, last})
			}
		}
		rows = out
	}

	if src.cf.output == "json" {
		b, err := json.Marshal(rows)
		if err != nil {
			return err
		}
		if string(b) == "null" {
			b = []byte("[]")
		}
		fmt.Println(string(b))
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, strings.Join(header, "\t"))
	for _, c := range cells {
		_, _ = fmt.Fprintln(tw, strings.Join(c, "\t"))
	}
	_ = tw.Flush()
	fmt.Printf("%d %s\n", len(cells), kind)
	return nil
}

// rrcsTree prints the system as an indented tree.
func rrcsTree(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("rrcs tree", flag.ContinueOnError)
	src := newRRCSSource(fs)
	flt := newRRCSFilter(fs)
	keys := fs.String("keys", "yes", "yes = show what is on the keys of each panel | no = ports only (one request per port less)")
	if err := parseVerbFlags(fs, reorderFlagsFirst(args)); err != nil {
		return err
	}
	if *keys != "yes" && *keys != "no" {
		return rrcsValErr("tree", "--keys must be yes or no")
	}
	m, err := src.model(ctx, "tree", fs.Args(), *keys == "yes", nil)
	if err != nil {
		return err
	}
	if src.cf.output == "json" {
		type portJSON struct {
			*rrcsPort
			Keys []*rrcsKey `json:"keys,omitempty"`
		}
		type nodeJSON struct {
			*rrcsNode
			Cards []*rrcsCard `json:"cards,omitempty"`
			Ports []portJSON  `json:"ports"`
		}
		doc := struct {
			Target  string                   `json:"target"`
			Status  map[string]string        `json:"status"`
			Nodes   []nodeJSON               `json:"nodes"`
			Objects map[string][]*rrcsObject `json:"objects"`
		}{Target: m.Target, Status: m.Status, Objects: m.Objects}
		for _, n := range m.Nodes {
			if *flt.node != 0 && n.Node != *flt.node {
				continue
			}
			nj := nodeJSON{rrcsNode: n, Ports: []portJSON{}}
			for _, c := range m.Cards {
				if c.Node == n.Node {
					nj.Cards = append(nj.Cards, c)
				}
			}
			for _, p := range m.Ports {
				if p.Node == n.Node && flt.port(p) {
					nj.Ports = append(nj.Ports, portJSON{rrcsPort: p, Keys: p.Keys})
				}
			}
			doc.Nodes = append(doc.Nodes, nj)
		}
		b, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}

	status := make([]string, 0, len(m.Status))
	for _, k := range []string{"GetVersion", "GetState", "IsConnected", "GetConfigurationID"} {
		if v, ok := m.Status[k]; ok {
			status = append(status, strings.TrimPrefix(k, "Get")+"="+v)
		}
	}
	fmt.Printf("rrcs %s  %s\n", m.Target, strings.Join(status, "  "))
	for _, n := range m.Nodes {
		if *flt.node != 0 && n.Node != *flt.node {
			continue
		}
		head := n.Path
		for _, part := range []string{n.Type, n.LongName} {
			if part != "" {
				head += "  " + part
			}
		}
		fmt.Printf("%s  (%d ports)\n", head, n.Ports)
		for _, c := range m.Cards {
			if c.Node == n.Node {
				fmt.Printf("  card.%d  %s  %s\n", c.Bay, c.Type, c.LongName)
			}
		}
		for _, p := range m.Ports {
			if p.Node != n.Node || !flt.port(p) {
				continue
			}
			leaf := strings.TrimPrefix(p.Path, n.Path+".")
			fmt.Printf("  %-14s %-7s %-26s %-9s %s\n", leaf, rrcsDir(p), p.Type, p.Label, p.LongName)
			for _, k := range p.Keys {
				fmt.Printf("    %-16s %-20s %s\n", strings.TrimPrefix(k.Path, p.Path+"."), k.CommandType, rrcsTarget(k))
			}
		}
	}
	if *flt.node != 0 || *flt.typ != "" {
		return nil
	}
	kinds := make([]string, 0, len(m.Objects))
	for k := range m.Objects {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		fmt.Printf("%s  (%d)\n", kind, len(m.Objects[kind]))
		for _, o := range m.Objects[kind] {
			if !rrcsHas(o.Path+" "+o.Label+" "+o.LongName, *flt.match) {
				continue
			}
			fmt.Printf("  %-24s %-9s %s %s\n", o.Path, o.Label, o.LongName, o.State)
			for _, mem := range o.Members {
				fmt.Printf("    %-34s %-9s %s\n", mem.Port, mem.Label, mem.Flags)
			}
		}
	}
	return nil
}

// rrcsPortOfPath reads the node, the port number and the direction out of
// a port path or of a path below a port.
func rrcsPortOfPath(path string) (node, port int, isInput, ok bool) {
	parts := strings.Split(path, ".")
	if len(parts) < 6 || parts[0] != "net" || parts[2] != "node" || parts[4] != "port" {
		return 0, 0, false, false
	}
	node, err1 := strconv.Atoi(parts[3])
	port, err2 := strconv.Atoi(parts[5])
	if err1 != nil || err2 != nil {
		return 0, 0, false, false
	}
	return node, port, len(parts) > 6 && parts[6] == "in", true
}

// rrcsGetPath prints the properties of what a path names.
func rrcsGetPath(ctx context.Context, src *rrcsSource, hosts []string, path, prop string) error {
	node, port, _, isPort := rrcsPortOfPath(path)
	var only func(n, p int, in bool) bool
	if isPort {
		only = func(n, p int, _ bool) bool { return n == node && p == port }
	}
	m, err := src.model(ctx, "get", hosts, isPort && (strings.Contains(path, ".key.") || strings.Contains(path, ".vfunc.")), only)
	if err != nil {
		return err
	}
	props, ok := m.lookup(path)
	if !ok {
		return fmt.Errorf("rrcs get: nothing at %s", path)
	}
	if prop != "" {
		v, ok := props[prop]
		if !ok {
			return fmt.Errorf("rrcs get: %s has no property %q", path, prop)
		}
		props = map[string]any{prop: v}
	}
	if src.cf.output == "json" {
		b, err := json.Marshal(props)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	names := make([]string, 0, len(props))
	for k := range props {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		b, _ := json.Marshal(props[k])
		fmt.Printf("%-28s %s\n", k, b)
	}
	return nil
}
