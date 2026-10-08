package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"dhs/internal/rrcs/codec"
	rrcs "dhs/internal/rrcs/consumer"
)

// rrcsChangeMethod is the method every configuration write of this
// connector uses (§8.10.1): all the changes of a request are applied, or
// none. Keys, conferences and stream edits passed with it on a real RRCS
// 9.0 on 2026-10-08. The form of the method is not what stops RRCS on an
// NMOS stream edit: both forms did (ADR-0035).
const rrcsChangeMethod = "ConfigurationChange"

// The configuration part of the desired state: what is on the keys, and
// the conferences and groups themselves.
//
//	"keys": [
//	  {"panel": "net.1.node.61.port.1024", "key": "0.1.14",
//	   "function": "call-to-port", "target": "net.1.node.63.port.1043",
//	   "label": "HYP1", "mode": "latching"},
//	  {"panel": "net.1.node.61.port.1024", "key": "0.1.15", "state": "absent"}
//	],
//	"conferences": [{"name": "Conference 040", "label": "CONF 40"}],
//	"groups": [{"name": "Group NOC", "label": "NOC",
//	            "members": ["net.1.node.61.port.1024", "net.1.node.61.port.1026"]}]
//
// A key is named by expansion panel, page and key, as `list keys` prints
// it. A key the file names carries exactly the function the file gives.
type rrcsDesiredKey struct {
	Panel    string  `json:"panel"`
	Key      string  `json:"key"`                // E.G.K, or vfunc.TYPE for a virtual function
	Function string  `json:"function,omitempty"` // call-to-port | call-to-conference | call-to-group | call-to-ifb | reply
	Target   string  `json:"target,omitempty"`   // a port path, conference.ID, group.ID or ifb.ID
	Label    *string `json:"label,omitempty"`
	Mode     string  `json:"mode,omitempty"` // auto | momentary | latching
	State    string  `json:"state,omitempty"`
}

// rrcsDesiredObject is a conference or a group, named by its object ID or,
// where the ID is not known yet, by its long name.
type rrcsDesiredObject struct {
	ID      int      `json:"id,omitempty"`
	Name    string   `json:"name,omitempty"`
	Label   *string  `json:"label,omitempty"`
	Members []string `json:"members,omitempty"` // groups only: the whole wanted list
	State   string   `json:"state,omitempty"`
}

// rrcsDesiredLevel is the level of a crosspoint, in dB (0 = unity) or
// "mute".
type rrcsDesiredLevel struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Single      string `json:"single"`
}

// rrcsCfgStep is one request that brings one field to its target.
type rrcsCfgStep struct {
	field      string
	from, to   string
	changeType string
	objectType string
	params     []codec.Member
}

func (s rrcsCfgStep) request() codec.Value {
	return codec.Array(codec.Struct(
		codec.Member{Name: "ChangeType", Value: codec.String(s.changeType)},
		codec.Member{Name: "ObjectType", Value: codec.String(s.objectType)},
		codec.Member{Name: "SpecificParams", Value: codec.Struct(s.params...)},
	))
}

// rrcsKeyFunctions maps the word of the file to the object type of the
// command (§8.10.1).
var rrcsKeyFunctions = map[string]string{
	"call-to-port":       "call-to-port-cmd",
	"call-to-conference": "call-to-conference",
	"call-to-group":      "call-to-group",
	"call-to-ifb":        "call-to-ifb",
	"reply":              "reply-cmd",
}

// rrcsKeyModes are the values an edit takes (§8.10.4.33). A read gives
// them plus one.
var rrcsKeyModes = map[string]int{"auto": 0, "momentary": 1, "latching": 2}

// rrcsKeyPosition builds the TCmdPosition of a key or virtual function
// (§6.6). ExpansionPanel exists only where there is one.
func rrcsKeyPosition(path string) (codec.Value, bool) {
	node, port, isInput, ok := rrcsPortOfPath(path)
	if !ok {
		return codec.Value{}, false
	}
	if _, _, _, e, g, k, isKey := rrcsKeyOfPath(path); isKey {
		m := []codec.Member{
			{Name: "PositionType", Value: codec.String("key")},
			{Name: "Node", Value: codec.Int(int32(node))},
			{Name: "Port", Value: codec.Int(int32(port))},
		}
		if e > 0 {
			m = append(m, codec.Member{Name: "ExpansionPanel", Value: codec.Int(int32(e))})
		}
		return codec.Struct(append(m,
			codec.Member{Name: "Page", Value: codec.Int(int32(g))},
			codec.Member{Name: "KeyNumber", Value: codec.Int(int32(k))},
			codec.Member{Name: "IsInput", Value: codec.Bool(isInput)})...), true
	}
	parts := strings.Split(path, ".")
	for i, p := range parts {
		if p == "vfunc" && i+1 < len(parts) {
			return codec.Struct(
				codec.Member{Name: "PositionType", Value: codec.String("virtual-function")},
				codec.Member{Name: "Node", Value: codec.Int(int32(node))},
				codec.Member{Name: "Port", Value: codec.Int(int32(port))},
				codec.Member{Name: "VirtualFunctionType", Value: codec.String(parts[i+1])},
				codec.Member{Name: "IsInput", Value: codec.Bool(isInput)}), true
		}
	}
	return codec.Value{}, false
}

// rrcsCommandParams are the parameters that name one command at a
// position: what a create and a delete both carry.
func rrcsCommandParams(objectType, position, target string) ([]codec.Member, error) {
	pos, ok := rrcsKeyPosition(position)
	if !ok {
		return nil, fmt.Errorf("%s is not a key or a virtual function", position)
	}
	at := codec.Member{Name: "CommandPosition", Value: pos}
	id := func(prefix string) (int32, error) {
		n, err := strconv.Atoi(strings.TrimPrefix(target, prefix+"."))
		if err != nil || !strings.HasPrefix(target, prefix+".") {
			return 0, fmt.Errorf("target %q: want %s.<object ID>", target, prefix)
		}
		return int32(n), nil
	}
	switch objectType {
	case "call-to-port-cmd":
		node, port, _, ok := rrcsPortOfPath(target)
		if !ok {
			return nil, fmt.Errorf("target %q: want a port path", target)
		}
		return []codec.Member{at, {Name: "DestinationPortAddress", Value: codec.Struct(
			codec.Member{Name: "Node", Value: codec.Int(int32(node))},
			codec.Member{Name: "Port", Value: codec.Int(int32(port))})}}, nil
	case "call-to-conference":
		n, err := id("conference")
		return []codec.Member{{Name: "Conference", Value: codec.Int(n)}, at}, err
	case "call-to-group":
		n, err := id("group")
		return []codec.Member{{Name: "Group", Value: codec.Int(n)}, at}, err
	case "call-to-ifb":
		n, err := id("ifb")
		return []codec.Member{{Name: "IFBObjectID", Value: codec.Int(n)}, at}, err
	case "reply-cmd":
		return []codec.Member{at}, nil
	}
	return nil, fmt.Errorf("a %s cannot be placed or removed by ensure yet", objectType)
}

// rrcsPlanConfig compares the wanted keys, conferences and groups with
// the model and returns the requests that close the gap, in the order
// they must be sent. problems are wishes that cannot be turned into a
// request.
func rrcsPlanConfig(m *rrcsModel, want *rrcsDesired) (steps []rrcsCfgStep, problems []rrcsEnsureFailure) {
	bad := func(field, format string, a ...any) {
		problems = append(problems, rrcsEnsureFailure{Field: field, Reason: fmt.Sprintf(format, a...)})
	}
	for _, k := range want.Keys {
		pos := k.Panel + ".key." + k.Key
		if strings.HasPrefix(k.Key, "vfunc.") {
			pos = k.Panel + "." + k.Key
		}
		field := pos + ".function"
		node, port, isInput, ok := rrcsPortOfPath(k.Panel)
		if !ok || m.find(node, port, isInput) == nil {
			bad(field, "no port at %s", k.Panel)
			continue
		}
		if _, ok := rrcsKeyPosition(pos); !ok {
			bad(field, "key %q: want EXPANSION.PAGE.KEY (0.1.14) or vfunc.TYPE", k.Key)
			continue
		}
		wantType := rrcsKeyFunctions[k.Function]
		absent := k.State == "absent"
		if !absent && k.Function != "" && wantType == "" {
			bad(field, "function %q: want call-to-port, call-to-conference, call-to-group, call-to-ifb or reply", k.Function)
			continue
		}
		// What sits at the position now: the first command at the path,
		// further ones at <path>.cmd.N; a virtual function at <path>.N.
		var current []*rrcsKey
		for _, c := range m.Keys {
			if c.Path == pos || strings.HasPrefix(c.Path, pos+".") {
				current = append(current, c)
			}
		}
		if absent || wantType != "" {
			kept := false
			for _, c := range current {
				if !absent && !kept && c.CommandType == wantType && (wantType == "reply-cmd" || c.Target == k.Target) {
					kept = true
					continue
				}
				params, err := rrcsCommandParams(c.CommandType, pos, c.Target)
				if err != nil {
					bad(field, "what is on the key cannot be removed: %v", err)
					kept = true // nothing is added next to what could not be removed
					continue
				}
				steps = append(steps, rrcsCfgStep{field: field, from: strings.TrimSpace(c.CommandType + " " + c.Target), to: "",
					changeType: "delete", objectType: c.CommandType, params: params})
			}
			if !absent && !kept {
				params, err := rrcsCommandParams(wantType, pos, k.Target)
				if err != nil {
					bad(field, "%v", err)
					continue
				}
				steps = append(steps, rrcsCfgStep{field: field, from: "", to: strings.TrimSpace(wantType + " " + k.Target),
					changeType: "create", objectType: wantType, params: params})
			}
		}
		// Label and mode are properties of the key itself (§8.10.4.33),
		// and they outlive the function: a key emptied on a real RRCS 9.0
		// kept the label it had been given. An absent key goes back to
		// what an untouched key holds: automatic label, momentary.
		if !absent && k.Label == nil && k.Mode == "" {
			continue
		}
		_, _, _, e, g, n, isKey := rrcsKeyOfPath(pos)
		if !isKey {
			if !absent {
				bad(pos+".LabelValue", "a virtual function has no label or mode")
			}
			continue
		}
		var props map[string]any
		for _, p := range m.Ports {
			if p.Node != node || p.Port != port {
				continue
			}
			for _, kc := range p.KeyConfigs {
				if kc.Path == pos {
					props = kc.Props
				}
			}
		}
		edit := []codec.Member{}
		var from, to []string
		if absent && props != nil {
			// The flag alone: a real RRCS 9.0 refuses an empty LabelValue
			// ("LabelValue should contain at least 1 character(s)!").
			if !jBool(props, "AutoLabelFlag") {
				edit = append(edit, codec.Member{Name: "AutoLabelFlag", Value: codec.Bool(true)})
				from, to = append(from, "label "+jStr(props, "LabelValue")), append(to, "label automatic")
			}
			if jInt(props, "KeyMode") != rrcsKeyModes["momentary"]+1 {
				edit = append(edit, codec.Member{Name: "KeyMode", Value: codec.Int(int32(rrcsKeyModes["momentary"]))})
				from, to = append(from, "mode "+strconv.Itoa(jInt(props, "KeyMode")-1)), append(to, "mode 1 momentary")
			}
		}
		if k.Label != nil && !absent && (props == nil || jStr(props, "LabelValue") != *k.Label || jBool(props, "AutoLabelFlag")) {
			edit = append(edit,
				codec.Member{Name: "AutoLabelFlag", Value: codec.Bool(false)},
				codec.Member{Name: "LabelValue", Value: codec.String(*k.Label)})
			from, to = append(from, "label "+jStr(props, "LabelValue")), append(to, "label "+*k.Label)
		}
		if k.Mode != "" && !absent {
			mode, known := rrcsKeyModes[k.Mode]
			if !known {
				bad(pos+".KeyMode", "mode %q: want auto, momentary or latching", k.Mode)
				continue
			}
			if props == nil || jInt(props, "KeyMode") != mode+1 {
				edit = append(edit, codec.Member{Name: "KeyMode", Value: codec.Int(int32(mode))})
				from, to = append(from, "mode "+strconv.Itoa(jInt(props, "KeyMode")-1)), append(to, "mode "+strconv.Itoa(mode)+" "+k.Mode)
			}
		}
		if len(edit) > 0 {
			steps = append(steps, rrcsCfgStep{field: pos, from: strings.Join(from, ", "), to: strings.Join(to, ", "),
				changeType: "edit", objectType: "panel-key", params: []codec.Member{
					{Name: "Node", Value: codec.Int(int32(node))},
					{Name: "Port", Value: codec.Int(int32(port))},
					{Name: "ExpansionPanel", Value: codec.Int(int32(e))},
					{Name: "Page", Value: codec.Int(int32(g))},
					{Name: "KeyNumber", Value: codec.Int(int32(n))},
					{Name: "IsInput", Value: codec.Bool(isInput)},
					{Name: "NewProperties", Value: codec.Struct(edit...)},
				}})
		}
	}

	object := func(kind string, list []rrcsDesiredObject) {
		for _, o := range list {
			field := kind + "." + strconv.Itoa(o.ID)
			if o.ID == 0 {
				field = kind + "." + o.Name
			}
			if o.ID == 0 && o.Name == "" {
				bad(kind, "an entry needs id or name")
				continue
			}
			var have *rrcsObject
			for _, c := range m.Objects[kind] {
				if (o.ID != 0 && c.ObjectID == o.ID) || (o.ID == 0 && c.LongName == o.Name) {
					have = c
				}
			}
			switch {
			case o.State == "absent":
				if have != nil {
					steps = append(steps, rrcsCfgStep{field: field, from: have.LongName, to: "", changeType: "delete", objectType: kind,
						params: []codec.Member{{Name: "ObjectID", Value: codec.Int(int32(have.ObjectID))}}})
				}
				continue
			case have == nil && o.ID != 0:
				bad(field, "no %s with this object ID; give a name to create one", kind)
				continue
			case have == nil:
				// §8.10.4.3: a group is created by its name alone; a
				// conference takes its properties at once (§8.10.4.2).
				params := []codec.Member{{Name: "Name", Value: codec.String(o.Name)}}
				if kind == "conference" {
					params = []codec.Member{{Name: "LongName", Value: codec.String(o.Name)}}
					if o.Label != nil {
						params = append(params, codec.Member{Name: "Label", Value: codec.String(*o.Label)})
					}
				}
				steps = append(steps, rrcsCfgStep{field: field, from: "", to: o.Name, changeType: "create", objectType: kind, params: params})
				continue // its label and members follow once it has an ID
			}
			edit := []codec.Member{}
			var from, to []string
			if o.Label != nil && have.Label != *o.Label {
				edit = append(edit, codec.Member{Name: "Label", Value: codec.String(*o.Label)})
				from, to = append(from, "label "+have.Label), append(to, "label "+*o.Label)
			}
			if o.ID != 0 && o.Name != "" && have.LongName != o.Name {
				edit = append(edit, codec.Member{Name: "LongName", Value: codec.String(o.Name)})
				from, to = append(from, "name "+have.LongName), append(to, "name "+o.Name)
			}
			if kind == "group" && o.Members != nil {
				now := map[string]bool{}
				for _, mb := range have.Members {
					now[mb.Port] = true
				}
				wanted := map[string]bool{}
				var list []codec.Value
				change := func(path string, add bool) bool {
					n, p, in, ok := rrcsPortOfPath(path)
					if !ok {
						bad(field, "member %q is not a port path", path)
						return false
					}
					list = append(list, codec.Struct(
						codec.Member{Name: "AddToGroup", Value: codec.Bool(add)},
						codec.Member{Name: "PortAddress", Value: codec.Struct(
							codec.Member{Name: "IsInput", Value: codec.Bool(in)},
							codec.Member{Name: "Node", Value: codec.Int(int32(n))},
							codec.Member{Name: "Port", Value: codec.Int(int32(p))})},
						codec.Member{Name: "UseSecondChannel", Value: codec.Bool(false)}))
					return true
				}
				for _, path := range o.Members {
					wanted[path] = true
					if !now[path] && change(path, true) {
						from, to = append(from, ""), append(to, "member "+path)
					}
				}
				gone := []string{}
				for path := range now {
					if !wanted[path] {
						gone = append(gone, path)
					}
				}
				sort.Strings(gone)
				for _, path := range gone {
					if change(path, false) {
						from, to = append(from, "member "+path), append(to, "")
					}
				}
				if len(list) > 0 {
					edit = append(edit, codec.Member{Name: "MemberList", Value: codec.Array(list...)})
				}
			}
			if len(edit) > 0 {
				params := append(edit, codec.Member{Name: "ObjectID", Value: codec.Int(int32(have.ObjectID))})
				steps = append(steps, rrcsCfgStep{field: field, from: strings.TrimSpace(strings.Join(from, ", ")), to: strings.TrimSpace(strings.Join(to, ", ")),
					changeType: "edit", objectType: kind, params: params})
			}
		}
	}
	object("conference", want.Conferences)
	object("group", want.Groups)
	ifbSteps, ifbProblems := rrcsPlanIFBs(m, want)
	steps, problems = append(steps, ifbSteps...), append(problems, ifbProblems...)
	return steps, problems
}

// rrcsReadForConfig reads what the plan compares with: the objects, and
// the keys of the panels the file names.
func rrcsReadForConfig(ctx context.Context, client *rrcs.Client, want *rrcsDesired) (*rrcsModel, error) {
	panels := map[[2]int]bool{}
	labels := false
	for _, k := range want.Keys {
		if n, p, _, ok := rrcsPortOfPath(k.Panel); ok {
			panels[[2]int{n, p}] = true
		}
		labels = labels || k.Label != nil || k.Mode != "" || k.State == "absent"
	}
	snap, _, err := rrcsCollect(ctx, client, rrcsCollectOpts{
		commands: len(panels) > 0, values: labels,
		onlyPort: func(n, p int, _ bool) bool { return panels[[2]int{n, p}] },
	})
	if err != nil {
		return nil, err
	}
	return rrcsModelOf(snap)
}

// rrcsEnsureConfig brings keys, conferences and groups to the file. It
// plans, sends one ConfigurationChange per step, reads again and plans
// again: what a second pass still wants (the label of a group it has just
// created) is sent then; what is left after that was accepted and not
// taken.
func rrcsEnsureConfig(ctx context.Context, cf *rrcsFlags, client *rrcs.Client, want *rrcsDesired, check bool) (diff []rrcsDiffEntry, failures []rrcsEnsureFailure, err error) {
	if len(want.Keys)+len(want.Conferences)+len(want.Groups)+len(want.IFBs) == 0 {
		return nil, nil, nil
	}
	seen := map[string]bool{}
	for pass := 1; pass <= 3; pass++ {
		m, err := rrcsReadForConfig(ctx, client, want)
		if err != nil {
			return diff, failures, err
		}
		steps, problems := rrcsPlanConfig(m, want)
		if pass == 1 {
			failures = append(failures, problems...)
		}
		if len(steps) == 0 {
			return diff, failures, nil
		}
		if pass == 3 {
			for _, s := range steps {
				failures = append(failures, rrcsEnsureFailure{Field: s.field, Reason: "not_taken: RRCS accepted the request and the configuration still differs"})
			}
			return diff, failures, nil
		}
		refused := false
		for _, s := range steps {
			// A change counts once RRCS has accepted it; a refused one is
			// a failure only.
			count := func() {
				key := s.field + "|" + s.changeType + "|" + s.to
				if !seen[key] {
					seen[key] = true
					diff = append(diff, rrcsDiffEntry{Field: s.field, From: s.from, To: s.to})
				}
			}
			if check {
				count()
				cf.say("rrcs ensure: would send %s %s", rrcsChangeMethod, rrcsCompact(s.request()))
				continue
			}
			cf.say("rrcs ensure: %s %s %s %s", rrcsChangeMethod, s.changeType, s.objectType, s.field)
			if _, err := client.Call(ctx, rrcsChangeMethod, s.request()); err != nil {
				failures = append(failures, rrcsEnsureFailure{Field: s.field, Reason: err.Error()})
				refused = true
				continue
			}
			count()
		}
		if check || refused {
			return diff, failures, nil
		}
	}
	return diff, failures, nil
}

// rrcsReadLevels reads the single level of crosspoints the way watch
// gets them (§8.15): GetXpVolume is refused on an Artist-1024, and RRCS
// sends the current level of a crosspoint as soon as it is added to the
// volume registry. Everything registered is removed before it returns.
// The values are the raw ones: 0 mute, 1..255 = (x - 230) / 2 dB.
func rrcsReadLevels(ctx context.Context, cf *rrcsFlags, client *rrcs.Client, listen string, xs []rrcs.Crosspoint) (map[rrcs.Crosspoint]int, error) {
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, fmt.Errorf("levels: listen %s: %w (is another dhs command listening there? --listen takes another port)", listen, err)
	}
	var mu sync.Mutex
	got := map[rrcs.Crosspoint]int{}
	done := make(chan struct{}, 1)
	listener := &rrcs.Listener{AnyPath: true, OnEvent: func(e rrcs.Event) {
		if e.Method != rrcs.MethodXpVolumeChange || len(e.Params) == 0 {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		for _, ch := range e.Params[len(e.Params)-1].Items {
			s, _ := ch.Field("Source")
			d, _ := ch.Field("Destination")
			v, ok := ch.Field("SingleVolume")
			if !ok {
				continue
			}
			got[rrcs.Crosspoint{SrcNode: rrcsMemberInt(s, "Node"), SrcPort: rrcsMemberInt(s, "Port"),
				DstNode: rrcsMemberInt(d, "Node"), DstPort: rrcsMemberInt(d, "Port")}] = int(v.Int)
		}
		all := true
		for _, x := range xs {
			if _, ok := got[x]; !ok {
				all = false
			}
		}
		if all {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	}}
	srv := &http.Server{Handler: listener, ReadHeaderTimeout: rrcsListenerWait}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	reg := rrcs.Registration{Port: ln.Addr().(*net.TCPAddr).Port}
	ip := rrcsLocalIP(listen, client.Peer())
	if _, err := client.Register(ctx, reg); err != nil {
		return nil, fmt.Errorf("levels: %w", err)
	}
	defer func() {
		bye, cancel := context.WithTimeout(context.Background(), cf.timeout)
		defer cancel()
		if _, err := client.UnregisterVolumeEvents(bye, ip); err != nil {
			cf.say("rrcs: levels: unregister: %v", err)
		}
		if _, err := client.Unregister(bye, reg); err != nil {
			cf.say("rrcs: levels: unregister: %v", err)
		}
	}()
	if _, err := client.RegisterVolumeEvents(ctx, ip, reg.Port); err != nil {
		return nil, fmt.Errorf("levels: %w", err)
	}
	if _, err := client.FollowVolumes(ctx, ip, reg.Port, xs...); err != nil {
		return nil, fmt.Errorf("levels: %w", err)
	}
	select {
	case <-done:
	case <-time.After(cf.timeout):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	mu.Lock()
	defer mu.Unlock()
	out := map[rrcs.Crosspoint]int{}
	for k, v := range got {
		out[k] = v
	}
	return out, nil
}

// rrcsLevelRaw reads a level of the file: "mute", or dB in half steps.
func rrcsLevelRaw(text string) (int, error) {
	text = strings.TrimSpace(text)
	if strings.EqualFold(text, "mute") {
		return 0, nil
	}
	db, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(text, "dB")), 64)
	raw := int(db*2 + 230)
	if err != nil || float64(raw) != db*2+230 || raw < 1 || raw > 255 {
		return 0, fmt.Errorf("level %q: want mute or -114.5 to 12.5 dB in steps of 0.5", text)
	}
	return raw, nil
}

func rrcsLevelText(raw int) string {
	value, unit := rrcsVolume(raw)
	return strings.TrimSpace(value + " " + unit)
}

// rrcsEnsureLevels brings crosspoint levels to the file (§8.2
// SetXpVolume) and reads them back.
func rrcsEnsureLevels(ctx context.Context, cf *rrcsFlags, client *rrcs.Client, listen string, levels []rrcsDesiredLevel, check bool) (diff []rrcsDiffEntry, failures []rrcsEnsureFailure, err error) {
	if len(levels) == 0 {
		return nil, nil, nil
	}
	type item struct {
		field string
		x     rrcs.Crosspoint
		args  []codec.Value
		raw   int
	}
	var items []item
	var xs []rrcs.Crosspoint
	for _, l := range levels {
		field := "xp." + l.Source + ">" + l.Destination + ".SingleVolume"
		sn, sp, _, ok1 := rrcsPortOfPath(l.Source)
		dn, dp, _, ok2 := rrcsPortOfPath(l.Destination)
		raw, lerr := rrcsLevelRaw(l.Single)
		switch {
		case !ok1 || !ok2:
			failures = append(failures, rrcsEnsureFailure{Field: field, Reason: "source and destination must be port paths"})
			continue
		case lerr != nil:
			failures = append(failures, rrcsEnsureFailure{Field: field, Reason: lerr.Error()})
			continue
		}
		x := rrcs.Crosspoint{SrcNode: sn, SrcPort: sp, DstNode: dn, DstPort: dp}
		items = append(items, item{field, x, []codec.Value{
			codec.Int(rrcsNetOfPath(l.Source)), codec.Int(int32(sn)), codec.Int(int32(sp)),
			codec.Int(rrcsNetOfPath(l.Destination)), codec.Int(int32(dn)), codec.Int(int32(dp)),
			codec.Bool(true), codec.Bool(false), codec.Int(int32(raw))}, raw})
		xs = append(xs, x)
	}
	if len(items) == 0 {
		return diff, failures, nil
	}
	now, err := rrcsReadLevels(ctx, cf, client, listen, xs)
	if err != nil {
		return diff, failures, err
	}
	sent := false
	for _, it := range items {
		have, known := now[it.x]
		if !known {
			failures = append(failures, rrcsEnsureFailure{Field: it.field, Reason: "RRCS sent no level for this crosspoint"})
			continue
		}
		if have == it.raw {
			continue
		}
		diff = append(diff, rrcsDiffEntry{Field: it.field, From: rrcsLevelText(have), To: rrcsLevelText(it.raw)})
		if check {
			continue
		}
		cf.say("rrcs ensure: SetXpVolume %s %s", it.field, rrcsLevelText(it.raw))
		if _, err := client.Call(ctx, "SetXpVolume", it.args...); err != nil {
			failures = append(failures, rrcsEnsureFailure{Field: it.field, Reason: err.Error()})
			continue
		}
		sent = true
	}
	if !sent {
		return diff, failures, nil
	}
	after, err := rrcsReadLevels(ctx, cf, client, listen, xs)
	if err != nil {
		return diff, failures, err
	}
	for _, it := range items {
		if have, known := after[it.x]; known && have != it.raw && now[it.x] != it.raw {
			failures = append(failures, rrcsEnsureFailure{Field: it.field, Reason: "not_taken: RRCS accepted the level and it reads " + rrcsLevelText(have)})
		}
	}
	return diff, failures, nil
}
