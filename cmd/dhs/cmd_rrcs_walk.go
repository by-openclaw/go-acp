package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"dhs/internal/rrcs/codec"
	rrcs "dhs/internal/rrcs/consumer"
)

// rrcsWalkLists are the read-only requests of `walk` that take the
// transaction key alone, on top of those of info and discover (§8.8,
// §8.16).
var rrcsWalkLists = []string{
	"GetNodeTypes", "GetClientCardTypes", "GetPortExTypeList", "GetErrorCodeList",
	"GetAllCaps", "GetTrunkPorts",
	"GetTrunklineSetup", "GetTrunklineActivities", "GetTrunkIfbs", "GetTrunkingNetAddr", "GetStageNetAddr",
}

// rrcsObjectTypes are the object types GetObjectList accepts (§8.9.1).
// A real RRCS 9.0 refuses `portex` here with fault 14: that type belongs
// to the configuration changes only.
var rrcsObjectTypes = []string{
	"conference", "group", "port", "ifb", "logic-source", "logic-destination",
	"gp-input", "gp-output", "user", "audiopatch", "client-card", "device",
}

// rrcsWalkCall is one request of a walk and what came back.
type rrcsWalkCall struct {
	Method  string `json:"method"`
	Args    []any  `json:"args,omitempty"`
	Error   string `json:"error,omitempty"`
	Payload any    `json:"payload,omitempty"`
}

// rrcsWalkObject is one entry of an object list with its properties.
type rrcsWalkObject struct {
	ObjectID   int32  `json:"object_id"`
	LongName   string `json:"long_name"`
	Error      string `json:"error,omitempty"`
	Properties any    `json:"properties,omitempty"`
}

// rrcsWalkSnapshot is the document `walk` writes.
type rrcsWalkSnapshot struct {
	SchemaVersion int                         `json:"schema_version"`
	Proto         string                      `json:"proto"`
	Target        string                      `json:"target"`
	StartedUTC    string                      `json:"started_utc"`
	Complete      bool                        `json:"complete"`
	Requests      int                         `json:"requests"`
	Failed        int                         `json:"failed"`
	Status        []rrcsWalkCall              `json:"status"`
	Lists         []rrcsWalkCall              `json:"lists"`
	Licenses      []rrcsWalkCall              `json:"licenses"`
	Objects       map[string][]rrcsWalkObject `json:"objects"`
	ObjectLists   []rrcsWalkCall              `json:"object_lists"`
	Commands      []rrcsWalkCall              `json:"commands"`
	// PortValues holds, per port, what only a request per port gives:
	// label, alias, input gain, output gain (§8.3, §8.4, §8.5).
	PortValues []rrcsWalkCall `json:"port_values"`
	// NotOnline counts the requests a port answered with "not online"
	// (error 24): expected for a port that is unplugged, not a failure.
	NotOnline int `json:"not_online"`
}

// rrcsWalker runs the requests of one walk and keeps the count.
type rrcsWalker struct {
	ctx    context.Context
	client *rrcs.Client
	snap   *rrcsWalkSnapshot
}

// call sends one request and records it. The reply is returned for the
// steps that read addresses or identifiers out of it.
func (w *rrcsWalker) call(method string, params ...codec.Value) (rrcsWalkCall, rrcs.Reply, bool) {
	rec := rrcsWalkCall{Method: method}
	for _, p := range params {
		rec.Args = append(rec.Args, rrcsJSON(p))
	}
	reply, err := w.client.Call(w.ctx, method, params...)
	w.snap.Requests++
	if err != nil {
		if errors.Is(err, &codec.CodeError{Code: codec.CodePortNotOnline}) {
			w.snap.NotOnline++
		} else {
			w.snap.Failed++
		}
		rec.Error = err.Error()
		return rec, reply, false
	}
	rec.Payload = rrcsJSON(reply.Payload())
	return rec, reply, true
}

// rrcsListOf returns the list an answer of the "[key, list]" shape wraps.
func rrcsListOf(reply rrcs.Reply) []codec.Value {
	p := reply.Payload()
	if p.Kind == codec.KindArray && len(p.Items) == 1 && p.Items[0].Kind == codec.KindArray {
		return p.Items[0].Items
	}
	return nil
}

func rrcsFieldInt(v codec.Value, name string) (int32, bool) {
	f, ok := v.Field(name)
	if !ok || f.Kind != codec.KindInt {
		return 0, false
	}
	return f.Int, true
}

func rrcsFieldBool(v codec.Value, name string) bool {
	f, ok := v.Field(name)
	return ok && f.Kind == codec.KindBool && f.Bool
}

// rrcsCollectOpts says how much of the gateway a collection reads.
type rrcsCollectOpts struct {
	// full adds the type lists, the licences and the object lists.
	full bool
	// properties asks every property of every listed object.
	properties bool
	// commands asks what is on the keys of the ports.
	commands bool
	// values asks label, alias and gains of each port.
	values bool
	// onlyPort, when set, limits the commands to the ports it accepts.
	onlyPort func(node, port int, isInput bool) bool
	// step receives the progress lines.
	step func(format string, a ...any)
}

// rrcsCollect reads the gateway into a snapshot. A request that fails is
// recorded with its error and does not stop the collection.
func rrcsCollect(ctx context.Context, client *rrcs.Client, opts rrcsCollectOpts) (*rrcsWalkSnapshot, int, error) {
	started := time.Now()
	snap := &rrcsWalkSnapshot{
		SchemaVersion: 1, Proto: rrcsProto, Target: client.Peer(),
		StartedUTC: started.UTC().Format(time.RFC3339),
		Objects:    map[string][]rrcsWalkObject{},
	}
	w := &rrcsWalker{ctx: ctx, client: client, snap: snap}
	step := opts.step
	if step == nil {
		step = func(string, ...any) {}
	}

	// 1. Status.
	for _, m := range rrcsInfoMethods {
		rec, _, _ := w.call(m)
		snap.Status = append(snap.Status, rec)
	}
	if snap.Failed == len(rrcsInfoMethods) {
		return nil, 0, fmt.Errorf("rrcs: no request was answered by %s", client.Peer())
	}
	step("status: %d requests", len(snap.Status))

	// 2. Lists. The ports and the nodes are kept for the later steps.
	var ports, nodes []codec.Value
	pools := map[[2]int32]int32{} // node, port → pool port amount
	listMethods := append(append([]string{}, rrcsDiscoverMethods...), "GetAllCaps")
	if opts.full {
		listMethods = append(append([]string{}, rrcsDiscoverMethods...), rrcsWalkLists...)
	}
	for _, m := range listMethods {
		rec, reply, ok := w.call(m)
		snap.Lists = append(snap.Lists, rec)
		if !ok {
			continue
		}
		switch m {
		case "GetAllPorts":
			ports = rrcsListOf(reply)
		case "GetAllNodes":
			nodes = rrcsListOf(reply)
		case "GetAllCaps":
			// "port#N": [net, node, port, pool port amount] (§8.8).
			for _, m := range reply.Value.Members {
				if it := m.Value.Items; m.Value.Kind == codec.KindArray && len(it) == 4 {
					pools[[2]int32{it[1].Int, it[2].Int}] = it[3].Int
				}
			}
		}
	}
	step("lists: %d requests, %d ports, %d nodes", len(snap.Lists), len(ports), len(nodes))

	// 3. Licence of each node (§8.18).
	for _, n := range nodes {
		if !opts.full {
			break
		}
		if addr, ok := rrcsFieldInt(n, "NodeAddress"); ok {
			rec, _, _ := w.call("GetLicenseInfo", codec.Int(addr))
			snap.Licenses = append(snap.Licenses, rec)
		}
	}

	// 4. Object lists, then every property of every object (§8.9.1: an
	// empty property name "returns all available object properties").
	// An object listed under two types is asked once.
	type props struct {
		value any
		err   string
	}
	seen := map[int32]props{}
	for _, typ := range rrcsObjectTypes {
		if ctx.Err() != nil || !opts.full {
			break
		}
		rec, reply, ok := w.call("GetObjectList", codec.String(typ))
		if !ok {
			snap.ObjectLists = append(snap.ObjectLists, rec)
			continue
		}
		list, _ := reply.Value.Field("ObjectList")
		rec.Payload = strconv.Itoa(len(list.Items)) + " objects"
		snap.ObjectLists = append(snap.ObjectLists, rec)
		objects := make([]rrcsWalkObject, 0, len(list.Items))
		for _, it := range list.Items {
			if ctx.Err() != nil {
				break
			}
			id, _ := rrcsFieldInt(it, "ObjectID")
			name, _ := it.Field("LongName")
			obj := rrcsWalkObject{ObjectID: id, LongName: name.Str}
			if opts.properties {
				p, done := seen[id]
				if !done {
					prec, _, _ := w.call("GetObjectProperty", codec.Int(id), codec.String(""))
					p = props{value: prec.Payload, err: prec.Error}
					seen[id] = p
				}
				obj.Properties, obj.Error = p.value, p.err
			}
			objects = append(objects, obj)
		}
		snap.Objects[typ] = objects
		step("objects %-18s %d", typ, len(objects))
	}

	// 5. What is configured on the keys and virtual functions of each
	// port (§8.9.3). One request per port: a real RRCS 9.0 answers the
	// same list for both directions of a port (381 of 381).
	if opts.commands {
		for _, p := range ports {
			if ctx.Err() != nil {
				break
			}
			net, _ := rrcsFieldInt(p, "Net")
			node, _ := rrcsFieldInt(p, "Node")
			port, _ := rrcsFieldInt(p, "Port")
			isInput := !rrcsFieldBool(p, "Output")
			if opts.onlyPort != nil && !opts.onlyPort(int(node), int(port), isInput) {
				continue
			}
			// A real RRCS 9.0 answers "does not exist" to pool port 0
			// on a port without pool ports; GetAllCaps gives -1 for
			// those, and an amount for the others.
			amount, known := pools[[2]int32{node, port}]
			if !known || amount <= 0 {
				rec, _, _ := w.call("GetPortsCommandLists",
					codec.Int(net), codec.Int(node), codec.Int(port), codec.Bool(isInput), codec.Int(-1))
				snap.Commands = append(snap.Commands, rec)
				continue
			}
			for pool := int32(0); pool < amount; pool++ {
				rec, _, _ := w.call("GetPortsCommandLists",
					codec.Int(net), codec.Int(node), codec.Int(port), codec.Bool(isInput), codec.Int(pool))
				snap.Commands = append(snap.Commands, rec)
			}
		}
		step("commands: %d requests", len(snap.Commands))
	}

	// 6. Label, alias and gains of each port. A real RRCS 9.0 logs these
	// forms for another control system that polls them all day:
	// GetPortLabel(node, port, input), GetPortAlias(net, node, port,
	// input), GetInputGain / GetOutputGain(net, node, port).
	if opts.values {
		for _, p := range ports {
			if ctx.Err() != nil {
				break
			}
			net, _ := rrcsFieldInt(p, "Net")
			node, _ := rrcsFieldInt(p, "Node")
			port, _ := rrcsFieldInt(p, "Port")
			isInput := !rrcsFieldBool(p, "Output")
			if opts.onlyPort != nil && !opts.onlyPort(int(node), int(port), isInput) {
				continue
			}
			rec, _, _ := w.call("GetPortLabel", codec.Int(node), codec.Int(port), codec.Bool(isInput))
			snap.PortValues = append(snap.PortValues, rec)
			rec, _, _ = w.call("GetPortAlias", codec.Int(net), codec.Int(node), codec.Int(port), codec.Bool(isInput))
			snap.PortValues = append(snap.PortValues, rec)
			if rrcsFieldBool(p, "Input") {
				rec, _, _ = w.call("GetInputGain", codec.Int(net), codec.Int(node), codec.Int(port))
				snap.PortValues = append(snap.PortValues, rec)
			}
			if rrcsFieldBool(p, "Output") {
				rec, _, _ = w.call("GetOutputGain", codec.Int(net), codec.Int(node), codec.Int(port))
				snap.PortValues = append(snap.PortValues, rec)
			}
		}
		step("port values: %d requests, %d on ports not online", len(snap.PortValues), snap.NotOnline)
	}

	snap.Complete = ctx.Err() == nil
	return snap, len(ports), nil
}

// rrcsWalk reads everything the gateway offers for reading and writes it
// to one JSON document.
func rrcsWalk(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("rrcs walk", flag.ContinueOnError)
	cf := newRRCSFlags(fs)
	out := fs.String("out", "auto", "snapshot FILE the walk is written to. Literal \"auto\" = snapshots/rrcs/<host>/walk-<utcstamp>.json (ADR-0028)")
	skip := fs.String("skip", "", "leave out parts, comma-separated: properties (one request per object) | commands (one request per port) | values (label, alias and gains: up to four requests per port)")
	if err := parseVerbFlags(fs, reorderFlagsFirst(args)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return rrcsValErr("walk", "want exactly one host[:port] argument")
	}
	skips := map[string]bool{}
	for _, part := range strings.Split(*skip, ",") {
		switch part = strings.TrimSpace(part); part {
		case "":
		case "both": // kept from the first release
			skips["properties"], skips["commands"] = true, true
		case "properties", "commands", "values":
			skips[part] = true
		default:
			return rrcsValErr("walk", "--skip takes properties, commands, values, comma-separated")
		}
	}
	client, _, closeFn, err := cf.open("walk", fs.Arg(0))
	if err != nil {
		return err
	}
	defer closeFn()
	started := time.Now()
	if *out == "auto" {
		*out = filepath.Join(snapshotDir(rrcsProto, hostOnly(fs.Arg(0))),
			"walk-"+started.UTC().Format("20060102T1504Z")+".json")
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return fmt.Errorf("rrcs walk: %w", err)
	}

	snap, portCount, err := rrcsCollect(ctx, client, rrcsCollectOpts{
		full:       true,
		properties: !skips["properties"],
		commands:   !skips["commands"],
		values:     !skips["values"],
		step:       func(format string, a ...any) { cf.say("rrcs walk: "+format, a...) },
	})
	if err != nil {
		return err
	}
	step := func(format string, a ...any) { cf.say("rrcs walk: "+format, a...) }

	doc, err := json.MarshalIndent(snap, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, doc, 0o644); err != nil {
		return fmt.Errorf("rrcs walk: %w", err)
	}
	step("%d requests, %d failed, %s — written to %s", snap.Requests, snap.Failed,
		time.Since(started).Round(time.Millisecond), *out)
	if !snap.Complete {
		return fmt.Errorf("rrcs walk: interrupted, the snapshot is partial")
	}
	if cf.output == "json" {
		fmt.Println(string(doc))
		return nil
	}
	fmt.Printf("%-20s %s\n", "target", snap.Target)
	for _, s := range snap.Status {
		if s.Error == "" {
			b, _ := json.Marshal(s.Payload)
			fmt.Printf("%-20s %s\n", s.Method, b)
		}
	}
	for _, typ := range rrcsObjectTypes {
		if objs, ok := snap.Objects[typ]; ok {
			fmt.Printf("%-20s %d\n", typ, len(objs))
		}
	}
	fmt.Printf("%-20s %d\n", "ports", portCount)
	fmt.Printf("%-20s %d\n", "command lists", len(snap.Commands))
	fmt.Printf("%-20s %d\n", "port values", len(snap.PortValues))
	fmt.Printf("%-20s %d\n", "port not online", snap.NotOnline)
	fmt.Printf("%-20s %d of %d\n", "failed requests", snap.Failed, snap.Requests)
	fmt.Printf("%-20s %s\n", "snapshot", *out)
	return nil
}

// rrcsGet reads the properties of one thing: an object by its ID, asked
// to the gateway, or anything by its path, read from the lists.
func rrcsGet(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("rrcs get", flag.ContinueOnError)
	src := newRRCSSource(fs)
	cf := src.cf
	id := fs.Int64("id", 0, "object ID, as the lists print it: GetObjectProperty")
	path := fs.String("path", "", "path, as list and tree print it (e.g. net.1.node.61.port.1026, group.109136507): the properties the lists hold")
	prop := fs.String("prop", "", "property name; empty = every property")
	names := fs.String("names", "no", "with --id: yes = list the property names the object supports instead of the values")
	if err := parseVerbFlags(fs, reorderFlagsFirst(args)); err != nil {
		return err
	}
	if *names != "yes" && *names != "no" {
		return rrcsValErr("get", "--names must be yes or no")
	}
	if (*id == 0) == (*path == "") {
		return rrcsValErr("get", "want --id or --path, one of them")
	}
	if *path != "" {
		return rrcsGetPath(ctx, src, fs.Args(), *path, *prop)
	}
	if fs.NArg() != 1 || *src.from != "" {
		return rrcsValErr("get", "--id asks a gateway: want exactly one host[:port] argument and no --from")
	}
	if *id < -1<<31 || *id > 1<<31-1 {
		return rrcsValErr("get", "--id must be a 32-bit object ID")
	}
	client, _, closeFn, err := cf.open("get", fs.Arg(0))
	if err != nil {
		return err
	}
	defer closeFn()

	var reply rrcs.Reply
	if *names == "yes" {
		reply, err = client.Call(ctx, "GetObjectPropertyNames", codec.Int(int32(*id)))
	} else {
		reply, err = client.Call(ctx, "GetObjectProperty", codec.Int(int32(*id)), codec.String(*prop))
	}
	if err != nil {
		return fmt.Errorf("rrcs get: %w", err)
	}
	payload := reply.Payload()
	if cf.output == "json" || payload.Kind != codec.KindStruct {
		fmt.Println(rrcsCompact(payload))
		return nil
	}
	for _, m := range payload.Members {
		fmt.Printf("%-28s %s\n", m.Name, rrcsCompact(m.Value))
	}
	return nil
}
