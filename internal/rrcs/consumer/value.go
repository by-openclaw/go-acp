package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	dhsc "dhs/internal/consumer"
	"dhs/internal/rrcs/codec"
)

// Reading and writing one property of one object: what the shared
// contract calls GetValue and SetValue.

// Current reads what the gateway holds now for the object of a path.
func Current(ctx context.Context, client *Client, path string, values bool) (map[string]any, error) {
	// The label, alias and gains of a port come from requests of their
	// own. They are asked only when one of them is what is being set: on
	// a port that is off line RRCS answers the gain with a warning in its
	// log, four lines per set on a real RRCS 9.0.
	//
	// Without them, one request reads the object: GetPort or
	// GetClientCard give what the lists give for it (527 of 527 ports
	// identical on a real RRCS 9.0), where the lists cost sixteen requests
	// before a set and sixteen after. The lists stay the fallback.
	if !values {
		if props, ok := CurrentOne(ctx, client, path); ok {
			return props, nil
		}
	}
	opts := CollectOpts{}
	if node, port, _, isPort := portOfPath(path); isPort && values {
		opts.Values = true
		opts.OnlyPort = func(n, p int, _ bool) bool { return n == node && p == port }
	}
	snap, _, err := Collect(ctx, client, opts)
	if err != nil {
		return nil, err
	}
	m, err := modelOf(snap)
	if err != nil {
		return nil, err
	}
	props, ok := m.Lookup(path)
	if !ok {
		return nil, fmt.Errorf("rrcs: nothing at %s", path)
	}
	return props, nil
}

// CurrentOne reads one port or one client card with a single request
// (§8.9 GetPort, §8.10.4.34 GetClientCard). ok is false when the path is
// neither, or when RRCS does not answer it as expected.
func CurrentOne(ctx context.Context, client *Client, path string) (map[string]any, bool) {
	parts := strings.Split(path, ".")
	var reply Reply
	var err error
	switch {
	case len(parts) == 6 && parts[4] == "card":
		node, err1 := strconv.Atoi(parts[3])
		bay, err2 := strconv.Atoi(parts[5])
		if err1 != nil || err2 != nil {
			return nil, false
		}
		reply, err = client.Call(ctx, "GetClientCard", codec.Int(int32(node)), codec.Int(int32(bay)))
	case len(parts) == 6 || len(parts) == 7:
		node, port, isInput, ok := portOfPath(path)
		if !ok {
			return nil, false
		}
		reply, err = client.Call(ctx, "GetPort", codec.Int(NetOfPath(path)), codec.Int(int32(node)), codec.Int(int32(port)),
			codec.Bool(isInput), codec.Int(-1))
	default:
		return nil, false
	}
	if err != nil || len(reply.Payload().Items) == 0 {
		return nil, false
	}
	// Through JSON, so the values have the types the rest of the code
	// reads from a snapshot.
	raw, err := json.Marshal(JSON(reply.Payload().Items[0]))
	if err != nil {
		return nil, false
	}
	var props map[string]any
	if json.Unmarshal(raw, &props) != nil || len(props) == 0 {
		return nil, false
	}
	return props, true
}

// SplitPath cuts a row path into the object and the property: the
// property starts at the first element that begins with a capital.
func SplitPath(path string) (object, block, field string, ok bool) {
	parts := strings.Split(path, ".")
	for i, p := range parts {
		if p != "" && p[0] >= 'A' && p[0] <= 'Z' {
			object = strings.Join(parts[:i], ".")
			switch len(parts) - i {
			case 1:
				return object, "", parts[i], object != ""
			case 2:
				return object, parts[i], parts[i+1], object != ""
			}
			return "", "", "", false
		}
	}
	return "", "", "", false
}

// listOfKind is the one request that lists the objects of a kind.
var listOfKind = map[string]string{
	"conference": "GetAllConferences", "group": "GetAllGroups",
	"ifb": "GetAllIFBs", "logic": "GetAllLogicSources_v2",
}

// propsOf reads the properties of the object a path names: one request
// for a port, a card or a listed kind.
func (p *Plugin) propsOf(ctx context.Context, client *Client, object string) (map[string]any, error) {
	if props, ok := CurrentOne(ctx, client, object); ok {
		return props, nil
	}
	kind, _, _ := strings.Cut(object, ".")
	list, listed := listOfKind[kind]
	only := []string{list}
	if !listed {
		// A node, or a kind without a list of its own: the discover lists.
		only = nil
	}
	snap, _, err := Collect(ctx, client, CollectOpts{Only: only})
	if err != nil {
		return nil, err
	}
	m, err := modelOf(snap)
	if err != nil {
		return nil, err
	}
	props, ok := m.Lookup(object)
	if !ok {
		return nil, fmt.Errorf("rrcs: nothing at %s", object)
	}
	return props, nil
}

// valueOf is the shared value of what RRCS reports for one property.
func valueOf(field string, v any) (dhsc.Value, error) {
	// Scalar reads what came through encoding/json; an integer straight
	// off the wire arrives as int32.
	if n, isInt := v.(int32); isInt {
		v = float64(n)
	}
	value, kind, _, _, ok := Scalar(field, v)
	if !ok {
		return dhsc.Value{}, fmt.Errorf("rrcs: %s is not a value RRCS reports", field)
	}
	switch kind {
	case "bool":
		return dhsc.Value{Kind: dhsc.KindBool, Bool: value == "true"}, nil
	case "int", "enum":
		n, _ := strconv.ParseInt(value, 10, 64)
		return dhsc.Value{Kind: dhsc.KindInt, Int: n}, nil
	}
	return dhsc.Value{Kind: dhsc.KindString, Str: value}, nil
}

// GetValue reads one property: the path of the object with the property
// behind it (net.1.node.61.port.1024.Label,
// net.1.node.63.port.1064.PortAes67Input.Protocol, conference.555.Label).
// One request for a port or a card, one for a listed kind.
func (p *Plugin) GetValue(ctx context.Context, req dhsc.ValueRequest) (dhsc.Value, error) {
	p.mu.Lock()
	client := p.client
	p.mu.Unlock()
	if client == nil {
		return dhsc.Value{}, dhsc.ErrNotConnected
	}
	object, block, field, ok := SplitPath(req.Path)
	if !ok {
		return dhsc.Value{}, fmt.Errorf("rrcs: %q does not name an object and a property", req.Path)
	}
	props, err := p.propsOf(ctx, client, object)
	if err != nil {
		return dhsc.Value{}, err
	}
	holder := props
	if block != "" {
		holder = jMap(props[block])
	}
	v, found := holder[field]
	if !found {
		return dhsc.Value{}, fmt.Errorf("rrcs: %s reports no %s", object, strings.TrimPrefix(block+"."+field, "."))
	}
	return valueOf(field, v)
}

// setDirect are the properties §8.3 to §8.7 write with a method of their
// own: one write request, one read back. Everything else is a
// configuration change (§8.10) and stays with the set and ensure verbs
// until that guarded write has proven equal on a real RRCS (ADR-0035;
// docs/mapping.md §8).
func (p *Plugin) setDirect(ctx context.Context, client *Client, object, block, field string, val dhsc.Value) (bool, error) {
	text := val.Str
	switch val.Kind {
	case dhsc.KindInt:
		text = strconv.FormatInt(val.Int, 10)
	case dhsc.KindBool:
		text = map[bool]string{true: "yes", false: "no"}[val.Bool]
	}
	kind, id, _ := strings.Cut(object, ".")
	if kind == "logic" && block == "" && field == "State" {
		_, err := client.Do(ctx, "SetLogicSourceState", map[string]string{"id": id, "state": text}, nil)
		return true, err
	}
	if _, _, _, _, isPort := PortOfPath(object); !isPort || block != "" {
		return false, nil
	}
	var method, arg string
	switch field {
	case "Label":
		method, arg = "SetPortLabel", "label"
	case "Alias":
		method, arg = "SetPortAlias", "alias"
	case "InputGain":
		method, arg = "SetInputGain", "gain"
	case "OutputGain":
		method, arg = "SetOutputGain", "gain"
	default:
		return false, nil
	}
	_, err := client.Do(ctx, method, map[string]string{"port-path": object, arg: text}, nil)
	return true, err
}

// SetValue writes one property and reads it back. The gains are in the
// protocol's own unit: half decibels, -128 mute.
func (p *Plugin) SetValue(ctx context.Context, req dhsc.ValueRequest, val dhsc.Value) (dhsc.Value, error) {
	p.mu.Lock()
	client := p.client
	p.mu.Unlock()
	if client == nil {
		return dhsc.Value{}, dhsc.ErrNotConnected
	}
	object, block, field, ok := SplitPath(req.Path)
	if !ok {
		return dhsc.Value{}, fmt.Errorf("rrcs: %q does not name an object and a property", req.Path)
	}
	written, err := p.setDirect(ctx, client, object, block, field, val)
	if err != nil {
		return dhsc.Value{}, fmt.Errorf("rrcs: set %s: %w", req.Path, err)
	}
	if !written {
		return dhsc.Value{}, fmt.Errorf("rrcs: %s is written by a configuration change: the set and ensure verbs carry it, with the guard of ADR-0035, until that write has proven equal through the plugin", req.Path)
	}
	// Read back through the requests of §8.3 to §8.5 where the property
	// has one; GetPort does not report the gains, the label or the alias.
	if _, _, _, _, isPort := PortOfPath(object); isPort {
		node, port, isInput, _ := portOfPath(object)
		read := func(method string, params ...codec.Value) (dhsc.Value, error) {
			reply, err := client.Call(ctx, method, params...)
			if err != nil {
				return dhsc.Value{}, fmt.Errorf("rrcs: %s after the write: %w", method, err)
			}
			items := reply.Payload().Items
			if len(items) == 0 {
				return dhsc.Value{}, fmt.Errorf("rrcs: %s answered nothing", method)
			}
			return valueOf(field, JSON(items[len(items)-1]))
		}
		switch field {
		case "Label":
			return read("GetPortLabel", codec.Int(int32(node)), codec.Int(int32(port)), codec.Bool(isInput))
		case "Alias":
			return read("GetPortAlias", codec.Int(NetOfPath(object)), codec.Int(int32(node)), codec.Int(int32(port)), codec.Bool(isInput))
		case "InputGain":
			return read("GetInputGain", codec.Int(NetOfPath(object)), codec.Int(int32(node)), codec.Int(int32(port)))
		case "OutputGain":
			return read("GetOutputGain", codec.Int(NetOfPath(object)), codec.Int(int32(node)), codec.Int(int32(port)))
		}
	}
	return p.GetValue(ctx, req)
}
