package main

import (
	"sort"
	"strconv"
	"strings"
)

// The members section is a matrix: one column per conference or group,
// one row per port, and in each cell what the port does there.
//
//	"members": {
//	  "conference.DHS CONF 1": {"net.1.node.93.port.8": "talk+listen",
//	                            "net.1.node.93.port.20": "listen",
//	                            "net.1.node.93.port.21": "off"},
//	  "group.DHS GROUP 1":      {"net.1.node.93.port.17": "on",
//	                            "net.1.node.93.port.18": "off"}
//	}
//
// A column is conference.<object ID or long name> or group.<object ID or
// long name>. Only the cells written are touched: a port the matrix does
// not name keeps what it has.
//
// How RRCS 9.0.RR1 takes it (oracle, 2026-11-07):
//
//   - A conference has no member list that an edit changes: MemberList
//     edits are accepted and ignored. A port is a member through a
//     call-to-conference command; on a port without keys that command
//     sits on its "always" virtual function (TCmdPosition §6.6). On a
//     FOUR_WIRE_SPLIT port the input half gives the talk right and the
//     output half the listen right, so talk and listen are set apart; a
//     port that is one object for both directions has both or neither.
//   - A group's members are its MemberList (TMemberChangeList §6.4): the
//     cells become the whole wanted list of the group.
type rrcsMemberMatrix map[string]map[string]string

// rrcsMemberPorts is every port the matrix names, for the read.
func rrcsMemberPorts(mx rrcsMemberMatrix) []string {
	var out []string
	for _, rows := range mx {
		for port := range rows {
			out = append(out, port)
		}
	}
	return out
}

// rrcsMemberRights reads a cell.
func rrcsMemberRights(cell string) (talk, listen, ok bool) {
	switch strings.ToLower(strings.TrimSpace(cell)) {
	case "talk+listen", "listen+talk", "on", "both":
		return true, true, true
	case "talk":
		return true, false, true
	case "listen":
		return false, true, true
	case "off", "absent", "":
		return false, false, true
	}
	return false, false, false
}

// rrcsFindObject names a conference or group of the model by object ID or
// by long name; 0 when it is not there.
func rrcsFindObject(m *rrcsModel, kind, ref string) int {
	n, isID := strconv.Atoi(ref)
	for _, o := range m.Objects[kind] {
		if (isID == nil && o.ObjectID == n) || (isID != nil && (o.LongName == ref || o.Label == ref)) {
			return o.ObjectID
		}
	}
	return 0
}

// rrcsWithMembers returns the file with the matrix turned into what the
// planner already knows: additive keys on the "always" virtual function
// for conferences, the whole member list for groups. A column whose
// object the file creates in the same run waits for the next pass.
func rrcsWithMembers(m *rrcsModel, want *rrcsDesired, bad func(field, format string, a ...any)) *rrcsDesired {
	if len(want.Members) == 0 {
		return want
	}
	out := *want
	out.Keys = append([]rrcsDesiredKey(nil), want.Keys...)
	out.Groups = append([]rrcsDesiredObject(nil), want.Groups...)
	created := func(kind, ref string) bool {
		list := want.Conferences
		if kind == "group" {
			list = want.Groups
		}
		for _, o := range list {
			if o.ID == 0 && o.Name == ref {
				return true
			}
		}
		return false
	}
	columns := make([]string, 0, len(want.Members))
	for c := range want.Members {
		columns = append(columns, c)
	}
	sort.Strings(columns)
	for _, column := range columns {
		rows := want.Members[column]
		kind, ref, _ := strings.Cut(column, ".")
		if kind != "conference" && kind != "group" {
			bad("members."+column, "a column is conference.<ID or name> or group.<ID or name>")
			continue
		}
		id := rrcsFindObject(m, kind, ref)
		if id == 0 {
			if !created(kind, ref) {
				bad("members."+column, "no %s %q; add it to %ss to create it", kind, ref, kind)
			}
			continue
		}
		ports := make([]string, 0, len(rows))
		for p := range rows {
			ports = append(ports, p)
		}
		sort.Strings(ports)
		if kind == "group" {
			rrcsGroupCells(m, &out, id, rows, ports, bad)
			continue
		}
		target := "conference." + strconv.Itoa(id)
		for _, port := range ports {
			field := "members." + column + "." + port
			talk, listen, ok := rrcsMemberRights(rows[port])
			if !ok {
				bad(field, "%q: want talk+listen, talk, listen or off", rows[port])
				continue
			}
			node, num, _, ok := rrcsPortOfPath(port)
			if !ok {
				bad(field, "not a port path")
				continue
			}
			base := strings.TrimSuffix(strings.TrimSuffix(port, ".in"), ".out")
			in, out2 := m.Find(node, num, true), m.Find(node, num, false)
			add := func(panel string, on bool) {
				k := rrcsDesiredKey{Panel: panel, Key: "vfunc.always", Function: "call-to-conference", Target: target, Additive: true}
				if !on {
					k.State = "absent"
				}
				out.Keys = append(out.Keys, k)
			}
			switch {
			case in != nil && out2 != nil && in != out2:
				// Two objects: talk on the input half, listen on the output.
				add(base+".in", talk)
				add(base+".out", listen)
			case in != nil:
				if talk != listen {
					bad(field, "%s is one object for both directions: talk+listen or off", port)
					continue
				}
				add(in.Path, talk)
			default:
				bad(field, "no port at %s", port)
			}
		}
	}
	return &out
}

// rrcsGroupCells turns the cells of one group column into its whole
// wanted member list: the members it has, plus the cells on, minus the
// cells off.
func rrcsGroupCells(m *rrcsModel, out *rrcsDesired, id int, rows map[string]string, ports []string, bad func(field, format string, a ...any)) {
	var have []string
	for _, o := range m.Objects["group"] {
		if o.ObjectID == id {
			for _, mb := range o.Members {
				have = append(have, mb.Port)
			}
		}
	}
	keep := map[string]bool{}
	for _, p := range have {
		keep[p] = true
	}
	for _, port := range ports {
		on, _, ok := rrcsMemberRights(rows[port])
		if !ok {
			bad("members.group."+strconv.Itoa(id)+"."+port, "%q: want on or off", rows[port])
			continue
		}
		keep[rrcsTalkPath(m, port)] = on
	}
	list := []string{}
	for p, on := range keep {
		if on {
			list = append(list, p)
		}
	}
	sort.Strings(list)
	for i := range out.Groups {
		if out.Groups[i].ID == id {
			out.Groups[i].Members = list
			return
		}
	}
	out.Groups = append(out.Groups, rrcsDesiredObject{ID: id, Members: list})
}
