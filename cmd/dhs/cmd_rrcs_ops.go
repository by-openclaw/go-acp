package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"dhs/internal/rrcs/codec"
)

// An rrcsOp is one method of the specification given a verb of its own:
// named, typed flags in the place of raw parameters, the write guard
// where the method changes something, and the answer printed as
// received. The parameters are sent in the order the specification
// prints them.
//
// Every method here is read from the 9.0.1 text. None of the writes has
// passed on a real RRCS: they are for a test system first.
type rrcsOp struct {
	Method string
	// Read is true for the few methods here that only read.
	Read bool
	// What the method does, in one line.
	About string
	// Params, in the order of the specification.
	Params []rrcsParam
}

// rrcsParam is one or more parameters of a method, taken from one flag.
type rrcsParam struct {
	Flag string
	// Kind says how the flag becomes parameters:
	//
	//	int, bool, text   one parameter
	//	netnodeport       a port path → Net, Node, Port
	//	nodeport          a port path → Node, Port
	//	isinput           no flag: IsInput of the port path given before
	//	xp                --src and --dst port paths → six integers
	//	key               a key path → Node, Port, IsInput, page,
	//	                  ExpansionPanel, KeyNumber
	//	portaddr          a port path → one TPortAddress struct (§6.1)
	//	cmdpos            a key, virtual function or port path → one
	//	                  TCmdPosition struct (§6.6)
	//	changes           a JSON file → the array of configuration changes
	//	xplist            repeated SRC>DST → array of {Destination, Source}
	Kind string
	Help string
}

func pInt(flag, help string) rrcsParam  { return rrcsParam{flag, "int", help} }
func pBool(flag, help string) rrcsParam { return rrcsParam{flag, "bool", help} }
func pText(flag, help string) rrcsParam { return rrcsParam{flag, "text", help} }

var (
	pXp       = rrcsParam{"", "xp", ""}
	pKey      = rrcsParam{"key", "key", "key path, as list keys prints it: …port.P.key.EXPANSION.PAGE.KEY"}
	pVirtual  = pBool("virtual", "the key is a virtual key")
	pPort3    = rrcsParam{"port-path", "netnodeport", "port path, as list ports prints it"}
	pPort2    = rrcsParam{"port-path", "nodeport", "port path, as list ports prints it"}
	pIsInput  = rrcsParam{"", "isinput", ""}
	pPoolPort = pInt("pool-port", "pool port; -1 for a port without pool ports")
	pReceiver = []rrcsParam{pText("ip", "address of the receiver of the notifications"), pInt("tcp-port", "TCP port of the receiver")}
	pKey8     = []rrcsParam{pKey, pVirtual}
)

func params(groups ...any) []rrcsParam {
	var out []rrcsParam
	for _, g := range groups {
		switch v := g.(type) {
		case rrcsParam:
			out = append(out, v)
		case []rrcsParam:
			out = append(out, v...)
		}
	}
	return out
}

// rrcsOps is every method of the specification that the other verbs do
// not already cover.
var rrcsOps = []rrcsOp{
	// §8.1 – §8.2 Crosspoints and their level
	{"SetXpPrio", false, "make a crosspoint with a priority", params(pXp, pInt("priority", "priority"))},
	{"SetXpDestructive", false, "make a crosspoint and remove the routes it displaces", params(pXp, pInt("priority", "audio priority"))},
	{"SetXpVolume", false, "set the level of a crosspoint", params(pXp, pBool("single", "set the single volume"), pBool("conference", "set the conference volume"),
		pInt("volume", "0 mute, 1..255 = (volume - 230) / 2 dB"))},
	// §8.3 – §8.5 Alias, label, gain
	{"SetPortAlias", false, "set the alias of a port", params(pPort3, pText("alias", "alias"), pIsInput)},
	{"SetPortLabel", false, "set the 8-character label of a port", params(pPort2, pText("label", "label, 8 characters at most"), pIsInput)},
	{"SetInputGain", false, "set the input gain of a port", params(pPort3, pInt("gain", "-36..36 in half dB, -128 mute"))},
	{"SetOutputGain", false, "set the output gain of a port", params(pPort3, pInt("gain", "-36..36 in half dB, -128 mute"))},
	// §8.6 GPIOs
	{"SetGpOutput", false, "switch a GP output", params(pPort3, pInt("slot", "slot"), pInt("gpio", "GPIO number"), pBool("state", "on or off"))},
	{"GetGpInputState", true, "read a GP input", params(pPort3, pInt("slot", "slot"), pInt("gpio", "GPIO number"))},
	{"GetGpOutputState", true, "read a GP output", params(pPort3, pInt("slot", "slot"), pInt("gpio", "GPIO number"))},
	// §8.7 Logic sources
	{"SetLogicSourceState", false, "switch a logic source", params(pInt("id", "object ID of the logic source, as list logic prints it"), pBool("state", "on or off"))},
	// §8.8 Gateway and net
	{"SetStateWorking", false, "make this gateway the working one", nil},
	{"SetStateStandby", false, "make this gateway the standby one", nil},
	{"SetNetName", false, "set the long name of the net", params(pText("name", "long name"))},
	// §8.10 Configuration changes
	{"ConfigurationChange", false, "apply configuration changes, all or nothing", params(rrcsParam{"file", "changes", "JSON file: the array of changes"})},
	{"BufferConfigurationChange", false, "buffer configuration changes for a later apply", params(rrcsParam{"file", "changes", "JSON file: the array of changes"})},
	{"BufferConfigurationChangeEx", false, "buffer configuration changes, each handled on its own", params(rrcsParam{"file", "changes", "JSON file: the array of changes"})},
	{"ApplyConfigurationChange", false, "send the buffered changes, all or nothing", nil},
	{"ApplyConfigurationChangeEx", false, "send the buffered changes, each handled on its own", nil},
	// §8.11 Key and marker manipulation
	{"ClearKeyLabel", false, "clear a key label set through RRCS", params(pKey8)},
	{"ClearKeyLabelAndMarker", false, "clear a key label and marker set through RRCS", params(pKey8)},
	{"ClearKeyMarker", false, "clear a key marker set through RRCS", params(pKey8)},
	{"LockKey", false, "lock or unlock a key", params(pKey8, pBool("lock", "lock or unlock"), pPoolPort)},
	{"PressKey", false, "press or release a key", params(pKey8, pBool("press", "press or release"), pPoolPort)},
	{"PressKeyEx", false, "press or release a key with a trigger", params(pKey8, pBool("press", "press or release"), pInt("trigger", "1 primary, 2 secondary"), pPoolPort)},
	{"SetKeyLabel", false, "set a key label", params(pKey8, pText("label", "label, 8 characters"))},
	{"SetKeyLabelAndMarker", false, "set a key label and marker", params(pKey8, pText("label", "label, 8 characters"), pInt("marker", "index of the marker in the configuration"))},
	{"SetKeyMarker", false, "set a key marker", params(pKey8, pInt("marker", "index of the marker in the configuration"))},
	// §8.13 Port cloning
	{"StartPortCloning", false, "clone the output of a port to a monitoring port", params(rrcsParam{"monitor", "portaddr", "port to monitor"}, rrcsParam{"clone", "portaddr", "port that receives the clone"})},
	{"StopPortCloning", false, "stop a port clone", params(rrcsParam{"monitor", "portaddr", "port to monitor"}, rrcsParam{"clone", "portaddr", "port that receives the clone"})},
	// §8.14 IFB volume
	{"SetIFBVolumeMixMinus", false, "set the mix-minus level of an IFB", params(pPort2, pIsInput, pInt("ifb", "IFB number"), pInt("volume", "0 mute, 1..255 = (volume - 230) / 2 dB"))},
	{"RemoveIFBVolumeMixMinus", false, "return the mix-minus level of an IFB to unity", params(pPort2, pIsInput, pInt("ifb", "IFB number"))},
	// §8.15 Registrations of the older kind
	{"XpVolumeChangeRegistryReset", false, "replace the crosspoints whose level is followed", params(pReceiver, rrcsParam{"xp", "xplist", "SRC>DST as two port paths; repeat"})},
	{"XpVolumeChangeRegistryRemove", false, "stop following the level of crosspoints", params(pReceiver, rrcsParam{"xp", "xplist", "SRC>DST as two port paths; repeat"})},
	{"RegisterForEvents", false, "register a receiver for the general events", params(pReceiver)},
	{"UnregisterForEvents", false, "remove a receiver of the general events", params(pReceiver)},
	{"RegisterForGpInputChange", false, "register a receiver for GP input changes", params(pReceiver)},
	{"UnregisterForGpInputChange", false, "remove a receiver of GP input changes", params(pReceiver)},
	{"RegisterForGpOutputChange", false, "register a receiver for GP output changes", params(pReceiver)},
	{"UnregisterForGpOutputChange", false, "remove a receiver of GP output changes", params(pReceiver)},
	// §8.16 Trunking
	{"SetTrunkingNetAddr", false, "set the trunking net address of the ring", params(pInt("addr", "trunking net address"))},
	{"SetLTC", false, "move the local trunk controller to a node", params(pInt("node", "node"))},
	// §8.17 Telephone
	{"DialNumber", false, "dial a number on a pool port", params(pPort2, pText("number", "number to dial"))},
	{"HangUpCall", false, "hang up the call of a port", params(pPort2)},
	{"LineStatus", true, "read the line status of a port", params(pPort2)},
	// §8.20 – §8.24
	{"SetSystemTimeOnAllNodes", false, "set the time of the gateway PC on every node", nil},
	{"ConnectToArtist", false, "connect the gateway to an Artist system", params(pText("ip", "address of the Artist node"), pBool("redundant", "the connection is redundant"))},
	{"DisconnectFromArtist", false, "disconnect the gateway from the Artist system", nil},
	{"ResetAllNodes", false, "RESET every node that is on line", nil},
	{"DeletePortCommands", false, "delete the commands of a command position", params(rrcsParam{"position", "cmdpos", "a key path, a virtual function path, or a port path for every position of the port"})},
	{"SetStageNetAddr", false, "set the stage net address", params(pInt("addr", "stage net address"))},
	{"SetStageRegistryUrl", false, "set the stage registry URL", params(pText("url", "URL"))},
}

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

// rrcsKeyOfPath reads a key path.
func rrcsKeyOfPath(path string) (node, port int, isInput bool, expansion, page, key int, ok bool) {
	node, port, isInput, ok = rrcsPortOfPath(path)
	if !ok {
		return
	}
	parts := strings.Split(path, ".")
	for i, p := range parts {
		if p == "key" && len(parts) == i+4 {
			e, err1 := strconv.Atoi(parts[i+1])
			g, err2 := strconv.Atoi(parts[i+2])
			k, err3 := strconv.Atoi(parts[i+3])
			return node, port, isInput, e, g, k, err1 == nil && err2 == nil && err3 == nil
		}
	}
	return node, port, isInput, 0, 0, 0, false
}

func rrcsNetOfPath(path string) int32 {
	var n int32 = 1
	_, _ = fmt.Sscanf(path, "net.%d.", &n)
	return n
}

// rrcsCmdPos builds a TCmdPosition (§6.6) from a key path, a virtual
// function path, or a port path (no PositionType: every position).
func rrcsCmdPos(path string) (codec.Value, error) {
	node, port, isInput, ok := rrcsPortOfPath(path)
	if !ok {
		return codec.Value{}, fmt.Errorf("%q is not a port, key or virtual function path", path)
	}
	base := []codec.Member{
		{Name: "Node", Value: codec.Int(int32(node))},
		{Name: "Port", Value: codec.Int(int32(port))},
		{Name: "IsInput", Value: codec.Bool(isInput)},
	}
	parts := strings.Split(path, ".")
	if _, _, _, e, g, k, isKey := rrcsKeyOfPath(path); isKey {
		return codec.Struct(append([]codec.Member{{Name: "PositionType", Value: codec.String("key")}}, append(base,
			codec.Member{Name: "ExpansionPanel", Value: codec.Int(int32(e))},
			codec.Member{Name: "Page", Value: codec.Int(int32(g))},
			codec.Member{Name: "KeyNumber", Value: codec.Int(int32(k))})...)...), nil
	}
	for i, p := range parts {
		if p == "vfunc" && i+1 < len(parts) {
			return codec.Struct(append([]codec.Member{{Name: "PositionType", Value: codec.String("virtual-function")}}, append(base,
				codec.Member{Name: "VirtualFunctionType", Value: codec.String(parts[i+1])})...)...), nil
		}
	}
	if len(parts) > 7 {
		return codec.Value{}, fmt.Errorf("%q is not a port, key or virtual function path", path)
	}
	return codec.Struct(base...), nil
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
	bad := func(format string, a ...any) error { return rrcsValErr(verb, fmt.Sprintf(format, a...)) }
	need := func(flagName string) (string, error) {
		v := *texts[flagName]
		if v == "" {
			return "", bad("want --%s", flagName)
		}
		return v, nil
	}
	lastPort := ""
	var values []codec.Value
	for _, p := range op.Params {
		switch p.Kind {
		case "int":
			v, err := need(p.Flag)
			if err != nil {
				return err
			}
			n, perr := strconv.ParseInt(v, 10, 32)
			if perr != nil {
				return bad("--%s %q is not a whole number", p.Flag, v)
			}
			values = append(values, codec.Int(int32(n)))
		case "bool":
			v, err := need(p.Flag)
			if err != nil {
				return err
			}
			on, known := map[string]bool{"true": true, "yes": true, "on": true, "1": true, "false": false, "no": false, "off": false, "0": false}[strings.ToLower(v)]
			if !known {
				return bad("--%s %q: want yes or no", p.Flag, v)
			}
			values = append(values, codec.Bool(on))
		case "text":
			// A text may be empty on purpose (an alias cleared).
			values = append(values, codec.String(*texts[p.Flag]))
		case "netnodeport", "nodeport":
			v, err := need(p.Flag)
			if err != nil {
				return err
			}
			node, port, _, ok := rrcsPortOfPath(v)
			if !ok {
				return bad("--%s %q is not a port path", p.Flag, v)
			}
			lastPort = v
			if p.Kind == "netnodeport" {
				values = append(values, codec.Int(rrcsNetOfPath(v)))
			}
			values = append(values, codec.Int(int32(node)), codec.Int(int32(port)))
		case "isinput":
			_, _, isInput, _ := rrcsPortOfPath(lastPort)
			values = append(values, codec.Bool(isInput))
		case "xp":
			src, err := need("src")
			if err != nil {
				return err
			}
			dst, err := need("dst")
			if err != nil {
				return err
			}
			sn, sp, _, ok1 := rrcsPortOfPath(src)
			dn, dp, _, ok2 := rrcsPortOfPath(dst)
			if !ok1 || !ok2 {
				return bad("--src and --dst must be port paths")
			}
			values = append(values, codec.Int(rrcsNetOfPath(src)), codec.Int(int32(sn)), codec.Int(int32(sp)),
				codec.Int(rrcsNetOfPath(dst)), codec.Int(int32(dn)), codec.Int(int32(dp)))
		case "key":
			v, err := need(p.Flag)
			if err != nil {
				return err
			}
			node, port, isInput, e, g, k, ok := rrcsKeyOfPath(v)
			if !ok {
				return bad("--%s %q is not a key path (…port.P.key.EXPANSION.PAGE.KEY)", p.Flag, v)
			}
			// §8.11: Node, Port, IsInput, page, ExpansionPanel, KeyNumber.
			values = append(values, codec.Int(int32(node)), codec.Int(int32(port)), codec.Bool(isInput),
				codec.Int(int32(g)), codec.Int(int32(e)), codec.Int(int32(k)))
		case "portaddr":
			v, err := need(p.Flag)
			if err != nil {
				return err
			}
			node, port, isInput, ok := rrcsPortOfPath(v)
			if !ok {
				return bad("--%s %q is not a port path", p.Flag, v)
			}
			values = append(values, codec.PortAddress{IsInput: isInput, Node: int32(node), Port: int32(port)}.Value())
		case "cmdpos":
			v, err := need(p.Flag)
			if err != nil {
				return err
			}
			pos, perr := rrcsCmdPos(v)
			if perr != nil {
				return bad("--%s: %v", p.Flag, perr)
			}
			values = append(values, pos)
		case "changes":
			v, err := need(p.Flag)
			if err != nil {
				return err
			}
			raw, rerr := os.ReadFile(v)
			if rerr != nil {
				return fmt.Errorf("rrcs %s: %w", verb, rerr)
			}
			list, perr := rrcsValueOfJSON(strings.TrimPrefix(string(raw), "\xef\xbb\xbf"))
			if perr != nil || list.Kind != codec.KindArray {
				return bad("--%s %s: want one JSON array of changes", p.Flag, v)
			}
			values = append(values, list)
		case "xplist":
			if len(xps) == 0 {
				return bad("want at least one --%s SRC>DST", p.Flag)
			}
			list := make([]codec.Value, 0, len(xps))
			for _, x := range xps {
				src, dst, cut := strings.Cut(x, ">")
				sn, sp, _, ok1 := rrcsPortOfPath(src)
				dn, dp, _, ok2 := rrcsPortOfPath(dst)
				if !cut || !ok1 || !ok2 {
					return bad("--%s %q: want SRC>DST as two port paths", p.Flag, x)
				}
				list = append(list, codec.Struct(
					codec.Member{Name: "Destination", Value: codec.PortAddress{Node: int32(dn), Port: int32(dp)}.Value()},
					codec.Member{Name: "Source", Value: codec.PortAddress{IsInput: true, Node: int32(sn), Port: int32(sp)}.Value()},
				))
			}
			values = append(values, codec.Array(list...))
		}
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
	fmt.Println(rrcsCompact(reply.Payload()))
	return nil
}
