package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	"GetNetName", "GetAllLogicSources", "GetAllActivePortClones", "GetLTC", "GetStageRegistryUrl",
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
	// KeyConfigs holds GetAllKeyConfiguration of each port that has keys
	// (§8.8): the mode and labelling of every key, as another control
	// system polls it on a real RRCS 9.0.
	KeyConfigs []rrcsWalkCall `json:"key_configs"`
	// Singles holds the reads that address one thing: GetNode,
	// GetClientCard, GetPort, GetPoolPortInfo, GetLevelMeterValues per
	// port, GetAllRemoteKeys per panel page, GetCommandList and
	// GetRemoteKey for one key, GetXpVolume per active crosspoint,
	// GetIFBVolumeMixMinus per IFB with a mix minus, and the three that
	// stand alone: GetAlive, IsRegisteredForEvents, GetActiveXpsRange.
	Singles []rrcsWalkCall `json:"singles"`
	// NotOnline counts the requests a port answered with "not online"
	// (error 24), and those not sent because RRCS had said the port is
	// off line: expected for a port that is unplugged, not a failure.
	NotOnline int `json:"not_online"`
	// NoGain counts the gain requests a port answered with "port address
	// invalid" (error 4). A real RRCS 9.0 answers so for every panel: a
	// panel has no input or output gain. Not a failure either.
	NoGain int `json:"no_gain"`
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
		gain := method == "GetInputGain" || method == "GetOutputGain"
		switch {
		case errors.Is(err, &codec.CodeError{Code: codec.CodePortNotOnline}):
			w.snap.NotOnline++
		case gain && errors.Is(err, &codec.CodeError{Code: codec.ErrorCode(4)}):
			w.snap.NoGain++
		default:
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
	// singles asks the reads that address one thing at a time.
	singles bool
	// onlyPort, when set, limits the commands to the ports it accepts.
	onlyPort func(node, port int, isInput bool) bool
	// panelCommands limits the commands to the ports that have keys: one
	// request per panel instead of one per port.
	panelCommands bool
	// only, when not nil, names the list requests to send: nothing else
	// of the status and of the lists is asked.
	only []string
	// keyConfigOnly, with values, asks the key configuration of a port
	// and not its label, alias and gains.
	keyConfigOnly bool
	// online, when not nil, holds the ports RRCS said are on line (node,
	// port). A port that is not in it is not asked for what only a port
	// on line answers: its gains and its level meter. A real RRCS 9.0
	// refuses those with "is not online" and writes a warning in its log
	// each time (592 per walk on one system).
	online map[[2]int]bool
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

	// 1. Status — unless the caller named the lists it wants: a verb that
	// prints one table asks for that table, nothing around it.
	if opts.only == nil {
		for _, m := range rrcsInfoMethods {
			rec, _, _ := w.call(m)
			snap.Status = append(snap.Status, rec)
		}
		if snap.Failed == len(rrcsInfoMethods) {
			return nil, 0, fmt.Errorf("rrcs: no request was answered by %s", client.Peer())
		}
		step("status: %d requests", len(snap.Status))
	}

	// 2. Lists. The ports and the nodes are kept for the later steps.
	var ports, nodes, cards, ifbs []codec.Value
	var activeXps [][]codec.Value
	pools := map[[2]int32]int32{} // node, port → pool port amount
	listMethods := append(append([]string{}, rrcsDiscoverMethods...), "GetAllCaps")
	if opts.full {
		listMethods = append(append([]string{}, rrcsDiscoverMethods...), rrcsWalkLists...)
	}
	if opts.only != nil {
		listMethods = append([]string{}, opts.only...)
		// The pool ports are needed to ask a port for its keys.
		if opts.commands || opts.values {
			listMethods = append(listMethods, "GetAllCaps")
		}
	}
	trunked := false
	for _, m := range listMethods {
		// A real RRCS 9.0 without trunk ports answers the two trunk line
		// requests with a generic error (99): they are asked only where
		// GetTrunkPorts listed something.
		if (m == "GetTrunklineSetup" || m == "GetTrunklineActivities") && !trunked {
			continue
		}
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
		case "GetTrunkPorts":
			trunked = len(rrcsListOf(reply)) > 0
		case "GetAllClientCards":
			cards = rrcsListOf(reply)
		case "GetAllIFBs":
			ifbs = rrcsListOf(reply)
		case "GetAllActiveXps":
			// "XP#N": [source net, node, port, destination net, node, port].
			for _, m := range reply.Value.Members {
				if strings.HasPrefix(m.Name, "XP#") && m.Value.Kind == codec.KindArray && len(m.Value.Items) >= 6 {
					activeXps = append(activeXps, m.Value.Items[:6])
				}
			}
		case "GetAllCaps":
			// "port#N": [net, node, port, pool port amount] (§8.8).
			for _, m := range reply.Value.Members {
				if it := m.Value.Items; m.Value.Kind == codec.KindArray && len(it) == 4 {
					pools[[2]int32{it[1].Int, it[2].Int}] = it[3].Int
				}
			}
		}
	}
	if opts.only != nil && snap.Requests > 0 && snap.Failed == snap.Requests {
		return nil, 0, fmt.Errorf("rrcs: no request was answered by %s", client.Peer())
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
			if keys, _ := rrcsFieldInt(p, "KeyCount"); opts.panelCommands && keys == 0 {
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
			var rec rrcsWalkCall
			if !opts.keyConfigOnly {
				rec, _, _ = w.call("GetPortLabel", codec.Int(node), codec.Int(port), codec.Bool(isInput))
				snap.PortValues = append(snap.PortValues, rec)
				rec, _, _ = w.call("GetPortAlias", codec.Int(net), codec.Int(node), codec.Int(port), codec.Bool(isInput))
				snap.PortValues = append(snap.PortValues, rec)
			}
			// A smart panel has no input or output gain: a real RRCS 9.0
			// answers "Invalid port address" for every RSP-12xx and
			// writes a warning in its own log each time (94 per walk on
			// one system). They are not asked.
			panel := false
			if typ, ok := p.Field("PortType"); ok && strings.HasPrefix(typ.Str, "RSP-") {
				panel = true
			}
			if opts.keyConfigOnly {
				panel = true // no gain is wanted either
			}
			offline := opts.online != nil && !opts.online[[2]int{int(node), int(port)}]
			if rrcsFieldBool(p, "Input") && !panel {
				if offline {
					snap.NotOnline++
				} else {
					rec, _, _ = w.call("GetInputGain", codec.Int(net), codec.Int(node), codec.Int(port))
					snap.PortValues = append(snap.PortValues, rec)
				}
			}
			if rrcsFieldBool(p, "Output") && !panel {
				if offline {
					snap.NotOnline++
				} else {
					rec, _, _ = w.call("GetOutputGain", codec.Int(net), codec.Int(node), codec.Int(port))
					snap.PortValues = append(snap.PortValues, rec)
				}
			}
			if keys, _ := rrcsFieldInt(p, "KeyCount"); keys > 0 {
				// §8.8: Node, Port, IsInput, PoolPort — no net. A real RRCS
				// 9.0 refuses the net in front ("expects an input parameter
				// 4 ('IsInput') of type 'boolean'"), although its own log
				// prints the request with one.
				pool := int32(-1)
				if amount, known := pools[[2]int32{node, port}]; known && amount > 0 {
					pool = 0
				}
				rec, _, _ = w.call("GetAllKeyConfiguration", codec.Int(node), codec.Int(port), codec.Bool(isInput), codec.Int(pool))
				snap.KeyConfigs = append(snap.KeyConfigs, rec)
			}
		}
		step("port values: %d requests, key configurations: %d, ports not online: %d, ports without gain: %d",
			len(snap.PortValues), len(snap.KeyConfigs), snap.NotOnline, snap.NoGain)
	}

	// 7. The reads that address one thing at a time (specification
	// §8.1, §8.2, §8.5, §8.8, §8.9.2, §8.11, §8.14, §8.19).
	if opts.singles && opts.onlyPort == nil {
		single := func(method string, params ...codec.Value) (rrcs.Reply, bool) {
			rec, reply, ok := w.call(method, params...)
			snap.Singles = append(snap.Singles, rec)
			return reply, ok
		}
		single("GetAlive")
		single("GetAllRemoteKeys") // every key manipulation made through RRCS: the key alone (§8.11)
		single("IsRegisteredForEvents", codec.String(rrcsLocalIP("", client.Peer())), codec.Int(8195))
		var net, loNode, hiNode, hiPort int32 = 1, 1 << 30, 0, 0
		for _, p := range ports {
			net, _ = rrcsFieldInt(p, "Net")
			node, _ := rrcsFieldInt(p, "Node")
			port, _ := rrcsFieldInt(p, "Port")
			loNode, hiNode, hiPort = min(loNode, node), max(hiNode, node), max(hiPort, port)
		}
		if len(ports) > 0 {
			single("GetActiveXpsRange", codec.Int(net), codec.Int(loNode), codec.Int(0), codec.Int(net), codec.Int(hiNode), codec.Int(hiPort))
		}
		for _, n := range nodes {
			if addr, ok := rrcsFieldInt(n, "NodeAddress"); ok {
				single("GetNode", codec.Int(addr))
			}
		}
		for _, c := range cards {
			node, _ := rrcsFieldInt(c, "Node")
			bay, _ := rrcsFieldInt(c, "Bay")
			single("GetClientCard", codec.Int(node), codec.Int(bay))
		}
		var firstKey *codec.Value
		for _, p := range ports {
			if ctx.Err() != nil {
				break
			}
			node, _ := rrcsFieldInt(p, "Node")
			port, _ := rrcsFieldInt(p, "Port")
			isInput := !rrcsFieldBool(p, "Output")
			pool := int32(-1)
			if amount, known := pools[[2]int32{node, port}]; known && amount > 0 {
				pool = 0
			}
			single("GetPort", codec.Int(net), codec.Int(node), codec.Int(port), codec.Bool(isInput), codec.Int(pool))
			single("GetPoolPortInfo", codec.Int(node), codec.Int(port))
			if rrcsFieldBool(p, "Input") {
				if opts.online != nil && !opts.online[[2]int{int(node), int(port)}] {
					snap.NotOnline++
				} else {
					single("GetLevelMeterValues", codec.Int(node), codec.Int(port), codec.Int(pool))
				}
			}
		}
		// One key, read the two ways a single key can be: the position
		// RRCS itself returned is handed back as it came.
		for _, c := range snap.Commands {
			if firstKey != nil {
				break
			}
			lists, _ := c.Payload.(map[string]any)["CommandLists"].([]any)
			for _, e := range lists {
				pos, _ := e.(map[string]any)["CommandPosition"].(map[string]any)
				if pos == nil || pos["PositionType"] != "key" {
					continue
				}
				num := func(name string) int32 {
					switch v := pos[name].(type) {
					case int32:
						return v
					case float64:
						return int32(v)
					}
					return 0
				}
				isInput, _ := pos["IsInput"].(bool)
				v := codec.Struct(
					codec.Member{Name: "ExpansionPanel", Value: codec.Int(num("ExpansionPanel"))},
					codec.Member{Name: "IsInput", Value: codec.Bool(isInput)},
					codec.Member{Name: "KeyNumber", Value: codec.Int(num("KeyNumber"))},
					codec.Member{Name: "Net", Value: codec.Int(num("Net"))},
					codec.Member{Name: "Node", Value: codec.Int(num("Node"))},
					codec.Member{Name: "Page", Value: codec.Int(num("Page"))},
					codec.Member{Name: "Port", Value: codec.Int(num("Port"))},
					codec.Member{Name: "PositionType", Value: codec.String("key")},
				)
				firstKey = &v
				single("GetCommandList", v)
				single("GetRemoteKey", codec.Int(num("Node")), codec.Int(num("Port")), codec.Bool(isInput),
					codec.Int(num("Page")), codec.Int(num("ExpansionPanel")), codec.Int(num("KeyNumber")), codec.Bool(false))
				break
			}
		}
		for _, x := range activeXps {
			single("GetXpVolume", x...)
		}
		for _, ifb := range ifbs {
			mm, _ := ifb.Field("MixMinus")
			node, _ := rrcsFieldInt(mm, "Node")
			port, _ := rrcsFieldInt(mm, "Port")
			number, _ := rrcsFieldInt(ifb, "Number")
			if node == 0 && port == 0 { // no mix minus assigned
				continue
			}
			single("GetIFBVolumeMixMinus", codec.Int(node), codec.Int(port), codec.Bool(rrcsFieldBool(mm, "IsInput")), codec.Int(number))
		}
		step("single reads: %d requests", len(snap.Singles))
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
	skip := fs.String("skip", "", "leave out parts, comma-separated: properties (one request per object) | commands (one request per port) | values (label, alias and gains: up to four requests per port) | singles (the reads that address one node, card, port, key or crosspoint)")
	online := fs.String("online", "yes", "yes = first learn which ports are on line, by a registration of a second or two that is removed at once, and do not ask the others for gain and level (RRCS refuses those and logs a warning each time) | no = ask every port")
	listen := fs.String("listen", ":8196", "with --online yes: local [ip]:port RRCS sends the port states to. Not the port of watch, so a walk can run beside it")
	if err := parseVerbFlags(fs, reorderFlagsFirst(args)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return rrcsValErr("walk", "want exactly one host[:port] argument")
	}
	if *online != "yes" && *online != "no" {
		return rrcsValErr("walk", "--online must be yes or no")
	}
	skips := map[string]bool{}
	for _, part := range strings.Split(*skip, ",") {
		switch part = strings.TrimSpace(part); part {
		case "":
		case "both": // kept from the first release
			skips["properties"], skips["commands"] = true, true
		case "properties", "commands", "values", "singles":
			skips[part] = true
		default:
			return rrcsValErr("walk", "--skip takes properties, commands, values, singles, comma-separated")
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

	// Which ports are on line, when something that depends on it is asked.
	var onLine map[[2]int]bool
	if *online == "yes" && (!skips["values"] || !skips["singles"]) {
		ports, err := rrcsOnlinePorts(ctx, cf, client, *listen)
		if err != nil {
			cf.say("rrcs walk: the ports on line could not be learnt, every port is asked: %v", err)
		} else {
			onLine = ports
			cf.say("rrcs walk: %d ports on line; the others are not asked for gain and level", len(ports))
		}
	}

	snap, portCount, err := rrcsCollect(ctx, client, rrcsCollectOpts{
		online:     onLine,
		full:       true,
		properties: !skips["properties"],
		commands:   !skips["commands"],
		values:     !skips["values"],
		singles:    !skips["singles"],
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
	fmt.Printf("%-20s %d\n", "key configurations", len(snap.KeyConfigs))
	fmt.Printf("%-20s %d\n", "port not online", snap.NotOnline)
	fmt.Printf("%-20s %d\n", "port without gain", snap.NoGain)
	fmt.Printf("%-20s %d\n", "single reads", len(snap.Singles))
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

// rrcsOnlinePorts learns which ports are on line the only way RRCS tells
// it: a registration is answered with one PortActive per port on line
// (§8.15.1, §9.7). It registers, takes what comes until RRCS has been
// silent for half a second, and unregisters.
func rrcsOnlinePorts(ctx context.Context, cf *rrcsFlags, client *rrcs.Client, listen string) (map[[2]int]bool, error) {
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w (is another dhs command listening there? --listen takes another port)", listen, err)
	}
	var mu sync.Mutex
	ports := map[[2]int]bool{}
	last := time.Now()
	listener := &rrcs.Listener{OnEvent: func(e rrcs.Event) {
		if e.Method != "PortActive" && e.Method != "PortInactive" {
			return
		}
		p := e.Params
		if e.TransKey != "" {
			p = p[1:]
		}
		if len(p) < 3 {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		ports[[2]int{rrcsParamInt(p, 1), rrcsParamInt(p, 2)}] = e.Method == "PortActive"
		last = time.Now()
	}}
	srv := &http.Server{Handler: listener, ReadHeaderTimeout: rrcsListenerWait}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	reg := rrcs.Registration{Port: ln.Addr().(*net.TCPAddr).Port}
	if _, err := client.Register(ctx, reg); err != nil {
		return nil, err
	}
	defer func() {
		bye, cancel := context.WithTimeout(context.Background(), cf.timeout)
		defer cancel()
		if _, err := client.Unregister(bye, reg); err != nil {
			cf.say("rrcs: ports on line: unregister: %v", err)
		}
	}()
	mu.Lock()
	last = time.Now()
	mu.Unlock()
	deadline := time.After(cf.timeout)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline:
		case <-tick.C:
			mu.Lock()
			quiet := time.Since(last) > 500*time.Millisecond
			mu.Unlock()
			if !quiet {
				continue
			}
		}
		break
	}
	mu.Lock()
	defer mu.Unlock()
	out := map[[2]int]bool{}
	for k, on := range ports {
		if on {
			out[k] = true
		}
	}
	return out, nil
}
