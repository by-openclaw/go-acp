package main

import (
	"fmt"
	"sort"
	"strings"

	rrcs "dhs/internal/rrcs/consumer"
)

// rrcsModelDiff says what differs between two readings of a system, as
// the lines watch prints: one per value that changed, was added or is
// gone. It is what a ConfigurationChange notification (§9.5) is turned
// into: RRCS says that the configuration changed, never what.
//
// It compares the keys of the panels (function and target), the
// conferences, groups and IFBs (label, name, members), and every property
// of the ports and of the client cards.
func rrcsModelDiff(before, after *rrcsModel) []rrcsChangeLine {
	if before == nil || after == nil {
		return nil
	}
	var out []rrcsChangeLine
	add := func(path, label, value, name, detail string, oid int) {
		out = append(out, rrcsChangeLine{Event: "ConfigurationChange", OID: oid, Path: path, Label: label, Value: value, Name: name, Detail: detail})
	}

	// Keys: what is on each position.
	was, is := map[string]*rrcsKey{}, map[string]*rrcsKey{}
	for _, k := range before.Keys {
		was[k.Path] = k
	}
	for _, k := range after.Keys {
		is[k.Path] = k
	}
	function := func(k *rrcsKey) string { return strings.TrimSpace(k.CommandType + " " + rrcsTarget(k)) }
	for _, path := range rrcsSortedKeys(was, is) {
		a, b := was[path], is[path]
		switch {
		case a == nil:
			add(path, "Function", function(b), b.PortLabel, "added", 0)
		case b == nil:
			add(path, "Function", "", a.PortLabel, "removed: was "+function(a), 0)
		case function(a) != function(b):
			add(path, "Function", function(b), b.PortLabel, "was "+function(a), 0)
		}
	}

	// Conferences, groups, IFBs.
	for _, kind := range []string{"conference", "group", "ifb"} {
		wasObj, isObj := map[string]*rrcsObject{}, map[string]*rrcsObject{}
		for _, o := range before.Objects[kind] {
			wasObj[o.Path] = o
		}
		for _, o := range after.Objects[kind] {
			isObj[o.Path] = o
		}
		for _, path := range rrcsSortedKeys(wasObj, isObj) {
			a, b := wasObj[path], isObj[path]
			switch {
			case a == nil:
				add(path, "Object", "created", b.LongName, "", b.ObjectID)
			case b == nil:
				add(path, "Object", "deleted", a.LongName, "", a.ObjectID)
			default:
				if a.Label != b.Label {
					add(path, "Label", b.Label, b.LongName, "was "+a.Label, b.ObjectID)
				}
				if a.LongName != b.LongName {
					add(path, "LongName", b.LongName, b.LongName, "was "+a.LongName, b.ObjectID)
				}
				if rrcsMembers(a) != rrcsMembers(b) {
					add(path, "Members", rrcsMembers(b), b.LongName, "was "+rrcsMembers(a), b.ObjectID)
				}
			}
		}
	}

	// Ports and client cards: every property, one level into its blocks.
	props := func(m *rrcsModel) (map[string]map[string]any, map[string]string, map[string]int) {
		raw, names, ids := map[string]map[string]any{}, map[string]string{}, map[string]int{}
		for _, p := range m.Ports {
			raw[p.Path], names[p.Path], ids[p.Path] = p.Raw, p.Label, p.ObjectID
		}
		for _, c := range m.Cards {
			raw[c.Path], names[c.Path], ids[c.Path] = c.Raw, c.LongName, c.ObjectID
		}
		return raw, names, ids
	}
	wasRaw, wasName, wasID := props(before)
	isRaw, isName, isID := props(after)
	for _, path := range rrcsSortedKeys(wasRaw, isRaw) {
		a, hadIt := wasRaw[path]
		b, hasIt := isRaw[path]
		switch {
		case !hadIt:
			add(path, "Object", "created", isName[path], "", isID[path])
			continue
		case !hasIt:
			add(path, "Object", "deleted", wasName[path], "", wasID[path])
			continue
		}
		flatA, flatB := rrcsFlat(a), rrcsFlat(b)
		for _, field := range rrcsSortedKeys(flatA, flatB) {
			if flatA[field] != flatB[field] {
				add(path, field, flatB[field], isName[path], "was "+flatA[field], isID[path])
			}
		}
	}
	return out
}

// rrcsFlat prints the properties of an object as text, one level into
// its blocks: PortAes67Input.Selection, Ptp.PTP.
func rrcsFlat(props map[string]any) map[string]string {
	out := map[string]string{}
	for name, v := range props {
		if block, ok := v.(map[string]any); ok {
			for field, inner := range block {
				out[name+"."+field] = fmt.Sprint(inner)
			}
			continue
		}
		out[name] = fmt.Sprint(v)
	}
	return out
}

// rrcsSortedKeys returns the keys of two maps, once each, in order.
func rrcsSortedKeys[V any](a, b map[string]V) []string {
	seen := map[string]bool{}
	var keys []string
	for _, m := range []map[string]V{a, b} {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	sort.Strings(keys)
	return keys
}

// rrcsWatchKinds are the kinds --kind of watch takes.
var rrcsWatchKinds = map[string]bool{"node": true, "card": true, "stream": true, "panel": true, "port": true,
	"key": true, "conference": true, "group": true, "ifb": true, "logic": true, "xp": true}

// rrcsKindOf says which kind of object a path names, for --kind: a port
// with keys is a panel, one with a stream block a stream.
func rrcsKindOf(m *rrcsModel, path string) string {
	switch {
	case strings.HasPrefix(path, "xp."):
		return "xp"
	case strings.HasPrefix(path, "conference."), strings.HasPrefix(path, "group."), strings.HasPrefix(path, "ifb."), strings.HasPrefix(path, "logic."):
		return path[:strings.Index(path, ".")]
	case strings.Contains(path, ".key."), strings.Contains(path, ".vfunc."):
		return "key"
	case strings.Contains(path, ".card."):
		return "card"
	case strings.Contains(path, ".port."):
		if m != nil {
			if _, node, port, isInput, ok := rrcs.PortOfPath(path); ok {
				if p := m.Find(node, port, isInput); p != nil {
					switch {
					case p.KeyCount > 0:
						return "panel"
					case p.Raw["PortAes67Input"] != nil || p.Raw["PortAes67Output"] != nil:
						return "stream"
					}
				}
			}
		}
		return "port"
	}
	return "node"
}

// rrcsOneObject is a model of one port or client card, for the diff of
// a targeted re-read.
func rrcsOneObject(path string, props map[string]any) *rrcsModel {
	m := &rrcsModel{Objects: map[string][]*rrcsObject{}}
	if strings.Contains(path, ".card.") {
		m.Cards = append(m.Cards, &rrcsCard{Path: path, Raw: props, LongName: jStr(props, "LongName"), ObjectID: jInt(props, "ObjectID")})
	} else {
		m.Ports = append(m.Ports, &rrcsPort{Path: path, Raw: props, Label: jStr(props, "Label"), ObjectID: jInt(props, "ObjectID")})
	}
	return m
}

// rrcsReplaceRaw puts the fresh properties of one object into the model.
func rrcsReplaceRaw(m *rrcsModel, path string, props map[string]any) {
	if m == nil {
		return
	}
	for _, p := range m.Ports {
		if p.Path == path {
			p.Raw = props
			return
		}
	}
	for _, c := range m.Cards {
		if c.Path == path {
			c.Raw = props
			return
		}
	}
}
