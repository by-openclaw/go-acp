package consumer

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"dhs/internal/rrcs/codec"
)

// What reads an RRCS gateway into one document: the requests of a walk,
// how much of them a caller wants, and the document they fill.

// InfoMethods are the status requests of `info` (spec 9.0.1 §8.8).
var InfoMethods = []string{
	"GetVersion", "GetState", "IsConnectedToArtist", "GetConfigurationID",
}

// DiscoverMethods are the read-only list requests of `discover`
// (§8.1, §8.6, §8.7, §8.8). Each takes the transaction key alone.
var DiscoverMethods = []string{
	"GetAllNodes", "GetAllClientCards", "GetAllDevices", "GetAllPorts",
	"GetAllConferences", "GetAllGroups", "GetAllIFBs", "GetAllLogicSources_v2",
	"GetAllGpIns", "GetAllGpOuts", "GetAllActiveXps",
}

// WalkLists are the read-only requests of `walk` that take the
// transaction key alone, on top of those of info and discover (§8.8,
// §8.16).
var WalkLists = []string{
	"GetNodeTypes", "GetClientCardTypes", "GetPortExTypeList", "GetErrorCodeList",
	"GetAllCaps", "GetTrunkPorts",
	"GetTrunklineSetup", "GetTrunklineActivities", "GetTrunkIfbs", "GetTrunkingNetAddr", "GetStageNetAddr",
	"GetNetName", "GetAllLogicSources", "GetAllActivePortClones", "GetLTC", "GetStageRegistryUrl",
}

// ObjectTypes are the object types GetObjectList accepts (§8.9.1).
// A real RRCS 9.0 refuses `portex` here with fault 14: that type belongs
// to the configuration changes only.
var ObjectTypes = []string{
	"conference", "group", "port", "ifb", "logic-source", "logic-destination",
	"gp-input", "gp-output", "user", "audiopatch", "client-card", "device",
}

// WalkCall is one request of a walk and what came back.
type WalkCall struct {
	Method  string `json:"method"`
	Args    []any  `json:"args,omitempty"`
	Error   string `json:"error,omitempty"`
	Payload any    `json:"payload,omitempty"`
}

// WalkObject is one entry of an object list with its properties.
type WalkObject struct {
	ObjectID   int32  `json:"object_id"`
	LongName   string `json:"long_name"`
	Error      string `json:"error,omitempty"`
	Properties any    `json:"properties,omitempty"`
}

// Snapshot is the document `walk` writes.
type Snapshot struct {
	SchemaVersion int                     `json:"schema_version"`
	Proto         string                  `json:"proto"`
	Target        string                  `json:"target"`
	StartedUTC    string                  `json:"started_utc"`
	Complete      bool                    `json:"complete"`
	Requests      int                     `json:"requests"`
	Failed        int                     `json:"failed"`
	Status        []WalkCall              `json:"status"`
	Lists         []WalkCall              `json:"lists"`
	Licenses      []WalkCall              `json:"licenses"`
	Objects       map[string][]WalkObject `json:"objects"`
	ObjectLists   []WalkCall              `json:"object_lists"`
	Commands      []WalkCall              `json:"commands"`
	// PortValues holds, per port, what only a request per port gives:
	// label, alias, input gain, output gain (§8.3, §8.4, §8.5).
	PortValues []WalkCall `json:"port_values"`
	// KeyConfigs holds GetAllKeyConfiguration of each port that has keys
	// (§8.8): the mode and labelling of every key, as another control
	// system polls it on a real RRCS 9.0.
	KeyConfigs []WalkCall `json:"key_configs"`
	// Singles holds the reads that address one thing: GetNode,
	// GetClientCard, GetPort, GetPoolPortInfo, GetLevelMeterValues per
	// port, GetAllRemoteKeys per panel page, GetCommandList and
	// GetRemoteKey for one key, GetXpVolume per active crosspoint,
	// GetIFBVolumeMixMinus per IFB with a mix minus, and the three that
	// stand alone: GetAlive, IsRegisteredForEvents, GetActiveXpsRange.
	Singles []WalkCall `json:"singles"`
	// NotOnline counts the requests a port answered with "not online"
	// (error 24), and those not sent because RRCS had said the port is
	// off line: expected for a port that is unplugged, not a failure.
	NotOnline int `json:"not_online"`
	// NoGain counts the gain requests a port answered with "port address
	// invalid" (error 4). A real RRCS 9.0 answers so for every panel: a
	// panel has no input or output gain. Not a failure either.
	NoGain int `json:"no_gain"`
}

// walker runs the requests of one walk and keeps the count.
type walker struct {
	ctx    context.Context
	client *Client
	snap   *Snapshot
}

// call sends one request and records it. The reply is returned for the
// steps that read addresses or identifiers out of it.
func (w *walker) call(method string, params ...codec.Value) (WalkCall, Reply, bool) {
	rec := WalkCall{Method: method}
	for _, p := range params {
		rec.Args = append(rec.Args, JSON(p))
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
	rec.Payload = JSON(reply.Payload())
	return rec, reply, true
}

// ListOf returns the list an answer of the "[key, list]" shape wraps.
func ListOf(reply Reply) []codec.Value {
	p := reply.Payload()
	if p.Kind == codec.KindArray && len(p.Items) == 1 && p.Items[0].Kind == codec.KindArray {
		return p.Items[0].Items
	}
	return nil
}

func FieldInt(v codec.Value, name string) (int32, bool) {
	f, ok := v.Field(name)
	if !ok || f.Kind != codec.KindInt {
		return 0, false
	}
	return f.Int, true
}

func FieldBool(v codec.Value, name string) bool {
	f, ok := v.Field(name)
	return ok && f.Kind == codec.KindBool && f.Bool
}

// CollectOpts says how much of the gateway a collection reads.
type CollectOpts struct {
	// Full adds the type lists, the licences and the object lists.
	Full bool
	// Properties asks every property of every listed object.
	Properties bool
	// Commands asks what is on the keys of the ports.
	Commands bool
	// Values asks label, alias and gains of each port.
	Values bool
	// Singles asks the reads that address one thing at a time.
	Singles bool
	// OnlyPort, when set, limits the commands to the ports it accepts.
	OnlyPort func(node, port int, isInput bool) bool
	// PanelCommands limits the commands to the ports that have keys: one
	// request per panel instead of one per port.
	PanelCommands bool
	// Only, when not nil, names the list requests to send: nothing else
	// of the status and of the lists is asked.
	Only []string
	// KeyConfigOnly, with values, asks the key configuration of a port
	// and not its label, alias and gains.
	KeyConfigOnly bool
	// Online, when not nil, holds the ports RRCS said are on line (node,
	// port). A port that is not in it is not asked for what only a port
	// on line answers: its gains and its level meter. A real RRCS 9.0
	// refuses those with "is not Online" and writes a warning in its log
	// each time (592 per walk on one system).
	Online map[[2]int]bool
	// LocalIP is our own address as RRCS reaches it. Singles asks RRCS
	// whether that address is registered for events.
	LocalIP string
	// Step receives the progress lines.
	Step func(format string, a ...any)
}

// Collect reads the gateway into a snapshot. A request that fails is
// recorded with its error and does not stop the collection.
func Collect(ctx context.Context, client *Client, opts CollectOpts) (*Snapshot, int, error) {
	started := time.Now()
	snap := &Snapshot{
		SchemaVersion: 1, Proto: Name, Target: client.Peer(),
		StartedUTC: started.UTC().Format(time.RFC3339),
		Objects:    map[string][]WalkObject{},
	}
	w := &walker{ctx: ctx, client: client, snap: snap}
	step := opts.Step
	if step == nil {
		step = func(string, ...any) {}
	}

	// 1. Status — unless the caller named the lists it wants: a verb that
	// prints one table asks for that table, nothing around it.
	if opts.Only == nil {
		for _, m := range InfoMethods {
			rec, _, _ := w.call(m)
			snap.Status = append(snap.Status, rec)
		}
		if snap.Failed == len(InfoMethods) {
			return nil, 0, fmt.Errorf("rrcs: no request was answered by %s", client.Peer())
		}
		step("status: %d requests", len(snap.Status))
	}

	// 2. Lists. The ports and the nodes are kept for the later steps.
	var ports, nodes, cards, ifbs []codec.Value
	var activeXps [][]codec.Value
	pools := map[[2]int32]int32{} // node, port → pool port amount
	listMethods := append(append([]string{}, DiscoverMethods...), "GetAllCaps")
	if opts.Full {
		listMethods = append(append([]string{}, DiscoverMethods...), WalkLists...)
	}
	if opts.Only != nil {
		listMethods = append([]string{}, opts.Only...)
		// The pool ports are needed to ask a port for its keys.
		if opts.Commands || opts.Values {
			listMethods = append(listMethods, "GetAllCaps")
		}
	}
	trunked := false
	for _, m := range listMethods {
		rec, reply, ok := w.call(m)
		// A real RRCS 9.0 without trunk ports answers the two trunk line
		// requests with a generic error (99). They are asked anyway, so
		// the answer is on record for every plant; without trunk ports
		// that one refusal is expected and not a failure.
		if !ok && !trunked && (m == "GetTrunklineSetup" || m == "GetTrunklineActivities") &&
			strings.Contains(rec.Error, "(code 99)") {
			snap.Failed--
		}
		snap.Lists = append(snap.Lists, rec)
		if !ok {
			continue
		}
		switch m {
		case "GetAllPorts":
			ports = ListOf(reply)
		case "GetAllNodes":
			nodes = ListOf(reply)
		case "GetTrunkPorts":
			trunked = len(ListOf(reply)) > 0
		case "GetAllClientCards":
			cards = ListOf(reply)
		case "GetAllIFBs":
			ifbs = ListOf(reply)
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
	if opts.Only != nil && snap.Requests > 0 && snap.Failed == snap.Requests {
		return nil, 0, fmt.Errorf("rrcs: no request was answered by %s", client.Peer())
	}
	step("lists: %d requests, %d ports, %d nodes", len(snap.Lists), len(ports), len(nodes))

	// 3. Licence of each node (§8.18).
	for _, n := range nodes {
		if !opts.Full {
			break
		}
		if addr, ok := FieldInt(n, "NodeAddress"); ok {
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
	for _, typ := range ObjectTypes {
		if ctx.Err() != nil || !opts.Full {
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
		objects := make([]WalkObject, 0, len(list.Items))
		for _, it := range list.Items {
			if ctx.Err() != nil {
				break
			}
			id, _ := FieldInt(it, "ObjectID")
			name, _ := it.Field("LongName")
			obj := WalkObject{ObjectID: id, LongName: name.Str}
			if opts.Properties {
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
	if opts.Commands {
		for _, p := range ports {
			if ctx.Err() != nil {
				break
			}
			net, _ := FieldInt(p, "Net")
			node, _ := FieldInt(p, "Node")
			port, _ := FieldInt(p, "Port")
			isInput := !FieldBool(p, "Output")
			if opts.OnlyPort != nil && !opts.OnlyPort(int(node), int(port), isInput) {
				continue
			}
			if keys, _ := FieldInt(p, "KeyCount"); opts.PanelCommands && keys == 0 {
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
	if opts.Values {
		for _, p := range ports {
			if ctx.Err() != nil {
				break
			}
			net, _ := FieldInt(p, "Net")
			node, _ := FieldInt(p, "Node")
			port, _ := FieldInt(p, "Port")
			isInput := !FieldBool(p, "Output")
			if opts.OnlyPort != nil && !opts.OnlyPort(int(node), int(port), isInput) {
				continue
			}
			var rec WalkCall
			if !opts.KeyConfigOnly {
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
			if opts.KeyConfigOnly {
				panel = true // no gain is wanted either
			}
			offline := opts.Online != nil && !opts.Online[[2]int{int(node), int(port)}]
			if FieldBool(p, "Input") && !panel {
				if offline {
					snap.NotOnline++
				} else {
					rec, _, _ = w.call("GetInputGain", codec.Int(net), codec.Int(node), codec.Int(port))
					snap.PortValues = append(snap.PortValues, rec)
				}
			}
			if FieldBool(p, "Output") && !panel {
				if offline {
					snap.NotOnline++
				} else {
					rec, _, _ = w.call("GetOutputGain", codec.Int(net), codec.Int(node), codec.Int(port))
					snap.PortValues = append(snap.PortValues, rec)
				}
			}
			if keys, _ := FieldInt(p, "KeyCount"); keys > 0 {
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
	if opts.Singles && opts.OnlyPort == nil {
		single := func(method string, params ...codec.Value) (Reply, bool) {
			rec, reply, ok := w.call(method, params...)
			snap.Singles = append(snap.Singles, rec)
			return reply, ok
		}
		single("GetAlive")
		single("GetAllRemoteKeys") // every key manipulation made through RRCS: the key alone (§8.11)
		single("IsRegisteredForEvents", codec.String(opts.LocalIP), codec.Int(8195))
		var net, loNode, hiNode, hiPort int32 = 1, 1 << 30, 0, 0
		for _, p := range ports {
			net, _ = FieldInt(p, "Net")
			node, _ := FieldInt(p, "Node")
			port, _ := FieldInt(p, "Port")
			loNode, hiNode, hiPort = min(loNode, node), max(hiNode, node), max(hiPort, port)
		}
		if len(ports) > 0 {
			single("GetActiveXpsRange", codec.Int(net), codec.Int(loNode), codec.Int(0), codec.Int(net), codec.Int(hiNode), codec.Int(hiPort))
		}
		for _, n := range nodes {
			if addr, ok := FieldInt(n, "NodeAddress"); ok {
				single("GetNode", codec.Int(addr))
			}
		}
		for _, c := range cards {
			node, _ := FieldInt(c, "Node")
			bay, _ := FieldInt(c, "Bay")
			single("GetClientCard", codec.Int(node), codec.Int(bay))
		}
		var firstKey *codec.Value
		for _, p := range ports {
			if ctx.Err() != nil {
				break
			}
			node, _ := FieldInt(p, "Node")
			port, _ := FieldInt(p, "Port")
			isInput := !FieldBool(p, "Output")
			pool := int32(-1)
			if amount, known := pools[[2]int32{node, port}]; known && amount > 0 {
				pool = 0
			}
			single("GetPort", codec.Int(net), codec.Int(node), codec.Int(port), codec.Bool(isInput), codec.Int(pool))
			single("GetPoolPortInfo", codec.Int(node), codec.Int(port))
			if FieldBool(p, "Input") {
				if opts.Online != nil && !opts.Online[[2]int{int(node), int(port)}] {
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
			node, _ := FieldInt(mm, "Node")
			port, _ := FieldInt(mm, "Port")
			number, _ := FieldInt(ifb, "Number")
			if node == 0 && port == 0 { // no mix minus assigned
				continue
			}
			single("GetIFBVolumeMixMinus", codec.Int(node), codec.Int(port), codec.Bool(FieldBool(mm, "IsInput")), codec.Int(number))
		}
		step("single reads: %d requests", len(snap.Singles))
	}

	snap.Complete = ctx.Err() == nil
	return snap, len(ports), nil
}

// JSON turns an XML-RPC value into what encoding/json prints.
func JSON(v codec.Value) any {
	switch v.Kind {
	case codec.KindInt:
		return v.Int
	case codec.KindBool:
		return v.Bool
	case codec.KindDouble:
		return v.Double
	case codec.KindBase64:
		return base64.StdEncoding.EncodeToString(v.Bytes)
	case codec.KindStruct:
		m := make(map[string]any, len(v.Members))
		for _, mem := range v.Members {
			m[mem.Name] = JSON(mem.Value)
		}
		return m
	case codec.KindArray:
		a := make([]any, len(v.Items))
		for i, it := range v.Items {
			a[i] = JSON(it)
		}
		return a
	}
	return v.Str
}
