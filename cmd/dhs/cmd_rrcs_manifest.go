package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"dhs/internal/manifest"
	rrcs "dhs/internal/rrcs/consumer"
)

// rrcsEnsureManifest brings the structure of a system — the type and the
// role of each port — to what a desired manifest says. The actual
// inventory is read as extract reads it; the difference becomes the rows
// import takes (a PortType row creates a port, Channels rows set a main's
// count, Mode and Selection rows link a port) and goes through the same
// convergence, in the proven order. Values stay with import. A port
// present with another type is a retype — RRCS has no edit of a type, so
// the port is deleted and created, and its keys and memberships go with
// it — which needs --allow-retype; its rows are saved first.
func rrcsEnsureManifest(ctx context.Context, cf *rrcsFlags, host, file, writeTo string, check, allowRetype bool) error {
	desired, err := manifest.Load(file)
	if err != nil {
		return rrcsValErr("ensure", "--manifest "+file+": "+err.Error())
	}
	if desired.Device.Protocol != rrcsProto {
		return rrcsValErr("ensure", "--manifest "+file+": protocol "+desired.Device.Protocol+", want rrcs")
	}
	if !check {
		if err := rrcsWriteGuard("ensure", host, writeTo); err != nil {
			return err
		}
	}
	client, _, closeFn, err := cf.open("ensure", host)
	if err != nil {
		return err
	}
	defer closeFn()
	readActual := func() (*rrcsModel, map[int]string, error) {
		snap, _, err := rrcsCollect(ctx, client, rrcsCollectOpts{Only: rrcs.ExtractLists})
		if err != nil {
			return nil, nil, err
		}
		m, err := rrcsModelOf(snap)
		if err != nil {
			return nil, nil, err
		}
		doc, _ := json.Marshal(snap)
		var raw map[string]any
		_ = json.Unmarshal(doc, &raw)
		return m, rrcs.ExTypeNames(raw), nil
	}
	actual, exTypes, err := readActual()
	if err != nil {
		return fmt.Errorf("rrcs ensure: %w", err)
	}
	plan := rrcsPlanManifest(actual, desired, exTypes)

	diff := []rrcsDiffEntry{}
	before, after := map[string]any{}, map[string]any{}
	failures := append([]rrcsEnsureFailure{}, plan.Problems...)
	record := func(applied []*rrcsApplied, skips []rrcsSkip) {
		for _, s := range skips {
			failures = append(failures, rrcsEnsureFailure{Field: s.Path, Reason: strings.TrimSpace(s.Reason + " " + s.Detail)})
		}
		for _, a := range applied {
			switch a.Result {
			case "would_apply", "applied":
				diff = append(diff, rrcsDiffEntry{Field: a.Path, From: a.Live, To: a.Wanted})
				before[a.Path], after[a.Path] = a.Live, a.Wanted
			case "unchanged":
			default:
				failures = append(failures, rrcsEnsureFailure{Field: a.Path, Reason: strings.TrimSpace(a.Result + " " + a.Error)})
			}
		}
	}

	// 1. The retypes: refused without the flag; with it, the port's rows
	// are saved, the port deleted, and its create joins the creates.
	for _, r := range plan.Retypes {
		field := r.Path + ".PortType"
		if !allowRetype {
			reason := fmt.Sprintf("retype %s -> %s: RRCS has no edit of a type; the port would be deleted and created, its keys and memberships gone — add --allow-retype to do it", r.From, r.To)
			if len(r.Inbound) > 0 {
				reason += "; points at it: " + strings.Join(r.Inbound, " ")
			}
			failures = append(failures, rrcsEnsureFailure{Field: field, Reason: reason})
			continue
		}
		if check {
			diff = append(diff, rrcsDiffEntry{Field: field, From: r.From + " (delete)", To: r.To + " (create)"})
			before[field], after[field] = r.From, r.To
			plan.Creates = append(plan.Creates, rrcsRow{Path: field, Value: strconv.Itoa(r.ToNumber)})
			plan.AfterCreate = append(plan.AfterCreate, r.RoleRows...)
			continue
		}
		saved, err := rrcsSaveInstance(host, r)
		if err != nil {
			failures = append(failures, rrcsEnsureFailure{Field: field, Reason: "the port's rows could not be saved, nothing deleted: " + err.Error()})
			continue
		}
		cf.say("rrcs ensure: the rows of %s (%s) saved to %s", r.Path, r.From, saved)
		del, err := rrcsChangeFor(r.Path)
		if err != nil {
			failures = append(failures, rrcsEnsureFailure{Field: field, Reason: err.Error()})
			continue
		}
		del.ChangeType = "delete"
		cf.say("rrcs ensure: delete %s (%s)", r.Path, r.From)
		if _, err := client.Call(ctx, rrcsChangeMethod, del.Request()); err != nil {
			failures = append(failures, rrcsEnsureFailure{Field: field, Reason: "delete: " + err.Error()})
			continue
		}
		diff = append(diff, rrcsDiffEntry{Field: field, From: r.From + " (deleted)", To: r.To})
		before[field], after[field] = r.From, r.To
		plan.Creates = append(plan.Creates, rrcsRow{Path: field, Value: strconv.Itoa(r.ToNumber)})
		plan.AfterCreate = append(plan.AfterCreate, r.RoleRows...)
	}

	// 2. The creates, then the roles of the ports that exist.
	all := func(string) bool { return true }
	if len(plan.Creates) > 0 {
		applied, skips, _, _, err := rrcsConverge(ctx, client, plan.Creates, all, check)
		if err != nil {
			return fmt.Errorf("rrcs ensure: %w", err)
		}
		record(applied, skips)
	}
	if len(plan.Roles) > 0 {
		applied, skips, _, _, err := rrcsConverge(ctx, client, plan.Roles, all, check)
		if err != nil {
			return fmt.Errorf("rrcs ensure: %w", err)
		}
		record(applied, skips)
	}
	// 3. The roles of the ports just created: a second pass, once they
	// exist; the dry run names them as such.
	if len(plan.AfterCreate) > 0 {
		if check {
			for _, row := range plan.AfterCreate {
				diff = append(diff, rrcsDiffEntry{Field: row.Path, From: "(after create)", To: row.Value})
				before[row.Path], after[row.Path] = "(after create)", row.Value
			}
		} else {
			applied, skips, _, _, err := rrcsConverge(ctx, client, plan.AfterCreate, all, false)
			if err != nil {
				return fmt.Errorf("rrcs ensure: %w", err)
			}
			record(applied, skips)
		}
	}
	// 4. The inventory after the run, so the actual manifest stays
	// current (the models are kept).
	if !check && treeStore != nil {
		m, types, err := readActual()
		if err == nil {
			version := ""
			if reply, err := client.Call(ctx, "GetVersion"); err == nil {
				version = rrcs.Version(rrcsFirstString(reply.Payload()))
			}
			if set, err := rrcs.BuildDMs(m, version, types); err == nil {
				port := rrcs.DefaultPort
				if _, p, err := splitHostPort(client.Peer(), rrcs.DefaultPort); err == nil {
					port = p
				}
				if path, err := manifest.Write(treeStore.BaseDir(), rrcs.Manifest(m, hostOnly(client.Peer()), port, set)); err == nil {
					cf.say("rrcs ensure: inventory rewritten: %s", path)
				}
			}
		}
	}
	suffix := fmt.Sprintf("of the manifest's %d port entries: create %d, retype %d, role rows %d, unchanged %d, not named %d",
		plan.Entries, len(plan.Creates), len(plan.Retypes), plan.RoleRows, plan.Unchanged, len(plan.ActualOnly))
	return rrcsEnsureReport(ctx, cf, host, check, diff, failures, before, after, suffix)
}

// rrcsManifestPlan is what a desired manifest asks of the actual system.
type rrcsManifestPlan struct {
	Entries int
	// Creates are the PortType rows of the absent ports; Roles the rows of
	// the ports that exist; AfterCreate the rows of the ports to create,
	// applied once they do.
	Creates, Roles, AfterCreate []rrcsRow
	RoleRows                    int
	Retypes                     []rrcsRetype
	Unchanged                   int
	ActualOnly                  []string
	Problems                    []rrcsEnsureFailure
}

// rrcsRetype is a port present with another type than wanted.
type rrcsRetype struct {
	Path, From, To string
	ToNumber       int
	// Inbound names the keys and memberships that point at the port.
	Inbound []string
	// Instance is the port's rows and those that point at it, saved
	// before the delete.
	Instance []rrcsRow
	RoleRows []rrcsRow
}

// rrcsPlanManifest compares the port entries of a desired manifest with
// the actual system.
func rrcsPlanManifest(actual *rrcsModel, desired *manifest.Manifest, exTypes map[int]string) *rrcsManifestPlan {
	plan := &rrcsManifestPlan{}
	numberOf := map[string]int{}
	for n, name := range exTypes {
		numberOf[name] = n
	}
	nameOf := func(n int, template string) string {
		if name := exTypes[n]; name != "" {
			return name
		}
		if template != "" {
			return template
		}
		return strconv.Itoa(n)
	}
	type live struct {
		typ   int
		name  string
		split bool
	}
	byAddr := map[[2]int]*live{}
	net := 1
	for _, p := range actual.Ports {
		if p.Net > 0 {
			net = p.Net
		}
		k := [2]int{p.Node, p.Port}
		l := byAddr[k]
		if l == nil {
			l = &live{typ: -1, name: p.Type}
			byAddr[k] = l
		}
		if _, ok := p.Raw["PortExType"]; ok {
			l.typ = jInt(p.Raw, "PortExType")
			l.name = rrcs.TypeName(l.typ, p.Type, exTypes)
		}
		if p.Input != p.Output {
			l.split = true
		}
	}
	// What points at a port: the keys that call it, the objects it is a
	// member of.
	base := func(path string) string { return strings.TrimSuffix(strings.TrimSuffix(path, ".in"), ".out") }
	inbound := map[string][]string{}
	for _, k := range actual.Keys {
		if strings.Contains(k.Target, ".port.") {
			inbound[base(k.Target)] = append(inbound[base(k.Target)], k.Path)
		}
	}
	for _, objs := range actual.Objects {
		for _, o := range objs {
			for _, mem := range o.Members {
				inbound[base(mem.Port)] = append(inbound[base(mem.Port)], o.Path)
			}
		}
	}
	var allRows []rrcsRow
	named := map[[2]int]bool{}
	for _, fr := range desired.Frames {
		for _, s := range fr.Slots {
			port, ok := rrcsAddrInt(s.Addr["port"])
			if !ok {
				continue // the node and the cards
			}
			node, ok := rrcsAddrInt(s.Addr["node"])
			if !ok {
				plan.Problems = append(plan.Problems, rrcsEnsureFailure{Field: "port " + strconv.Itoa(port), Reason: "no node in the entry's addr"})
				continue
			}
			plan.Entries++
			k := [2]int{node, port}
			named[k] = true
			path := fmt.Sprintf("net.%d.node.%d.port.%d", net, node, port)
			template, _ := s.Addr["template"].(string)
			wantType, haveType := rrcsAddrInt(s.Addr["type"])
			if !haveType && template != "" {
				wantType, haveType = numberOf[template]
			}
			l := byAddr[k]
			split := haveType && wantType == 3
			if l != nil {
				split = l.split
			}
			roleRows, problems := rrcsRoleRows(path, split, rrcsRolesOf(s.Addr["role"]))
			for _, p := range problems {
				plan.Problems = append(plan.Problems, rrcsEnsureFailure{Field: path, Reason: p})
			}
			plan.RoleRows += len(roleRows)
			switch {
			case l == nil:
				if !haveType {
					plan.Problems = append(plan.Problems, rrcsEnsureFailure{Field: path, Reason: "absent, and the entry names no type: give type (a number of GetPortExTypeList) or template (its name)"})
					continue
				}
				plan.Creates = append(plan.Creates, rrcsRow{Path: path + ".PortType", Value: strconv.Itoa(wantType)})
				plan.AfterCreate = append(plan.AfterCreate, roleRows...)
			case haveType && l.typ >= 0 && l.typ != wantType:
				if allRows == nil {
					allRows = rrcsRows(actual)
				}
				r := rrcsRetype{Path: path, From: l.name, To: nameOf(wantType, template), ToNumber: wantType, Inbound: inbound[path], RoleRows: roleRows}
				for _, row := range allRows {
					v := row.Value
					if strings.HasPrefix(row.Path, path+".") || v == path || base(v) == path || strings.HasPrefix(v, path+" ") || strings.HasPrefix(base(strings.Fields(v + " x")[0]), path) {
						r.Instance = append(r.Instance, row)
					}
				}
				plan.Retypes = append(plan.Retypes, r)
			default:
				if len(roleRows) == 0 {
					plan.Unchanged++
				}
				plan.Roles = append(plan.Roles, roleRows...)
			}
		}
	}
	for k := range byAddr {
		if !named[k] {
			plan.ActualOnly = append(plan.ActualOnly, fmt.Sprintf("net.%d.node.%d.port.%d", net, k[0], k[1]))
		}
	}
	sort.Strings(plan.ActualOnly)
	return plan
}

// rrcsRolesOf reads the role of an entry: one per direction ("in",
// "out"), or one for the whole port under "".
func rrcsRolesOf(v any) map[string]map[string]any {
	role, _ := v.(map[string]any)
	out := map[string]map[string]any{}
	if role == nil {
		return out
	}
	if _, whole := role["kind"]; whole {
		out[""] = role
		return out
	}
	for _, dir := range []string{"in", "out"} {
		if r, ok := role[dir].(map[string]any); ok {
			out[dir] = r
		}
	}
	return out
}

// rrcsRoleRows turns the roles of a port into the rows import takes.
func rrcsRoleRows(path string, split bool, roles map[string]map[string]any) (rows []rrcsRow, problems []string) {
	if whole := roles[""]; whole != nil {
		switch kind, _ := whole["kind"].(string); kind {
		case "", "panel", "port", "stream":
		case "stream-block", "stream-linked":
			problems = append(problems, "a stream role is given per direction: role.in and role.out")
		default:
			problems = append(problems, "role "+kind+" is not stream-block, stream-linked, stream, panel or port")
		}
		return rows, problems
	}
	for _, dir := range []string{"in", "out"} {
		role := roles[dir]
		if role == nil {
			continue
		}
		object, block := path, "PortAes67Input"
		if split {
			object = path + "." + dir
		}
		if dir == "out" {
			block = "PortAes67Output"
		}
		switch kind, _ := role["kind"].(string); kind {
		case "stream-block":
			n, ok := rrcsAddrInt(role["channels"])
			if !ok || n < 1 {
				problems = append(problems, dir+": stream-block needs channels")
				continue
			}
			rows = append(rows, rrcsRow{Path: object + "." + block + ".Channels", Value: strconv.Itoa(n)})
		case "stream-linked":
			main, ok1 := rrcsAddrInt(role["main"])
			ch, ok2 := rrcsAddrInt(role["channel"])
			if !ok1 || !ok2 {
				problems = append(problems, dir+": stream-linked needs main (the number of the main port) and channel")
				continue
			}
			rows = append(rows,
				rrcsRow{Path: object + "." + block + ".Mode", Value: strconv.Itoa(main)},
				rrcsRow{Path: object + "." + block + ".Selection", Value: strconv.Itoa(ch)})
		case "", "stream", "panel", "port":
		default:
			problems = append(problems, dir+": role "+kind+" is not stream-block, stream-linked, stream, panel or port")
		}
	}
	return rows, problems
}

// rrcsAddrInt reads a number of an addr, whatever JSON made of it.
func rrcsAddrInt(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case int:
		return t, true
	case int64:
		return int(t), true
	case json.Number:
		n, err := t.Int64()
		return int(n), err == nil
	case string:
		n, err := strconv.Atoi(t)
		return n, err == nil
	}
	return 0, false
}

// rrcsSaveInstance writes the rows of a port about to be retyped, and
// those that point at it, to the snapshots home of the host (ADR-0028):
// what import takes to bring the type back.
func rrcsSaveInstance(host string, r rrcsRetype) (string, error) {
	dir := snapshotDir(rrcsProto, hostOnly(host))
	if treeStore != nil {
		dir = filepath.Join(filepath.Dir(treeStore.BaseDir()), "snapshots", rrcsProto, hostOnly(host))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	_, node, port, _, _ := rrcs.PortOfPath(r.Path)
	path := filepath.Join(dir, fmt.Sprintf("retype-%d-%d-%s.csv", node, port, time.Now().UTC().Format("20060102T1504Z")))
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	w := csv.NewWriter(f)
	_ = w.Write(rrcsCSVHeader)
	for _, row := range r.Instance {
		_ = w.Write(row.Record())
	}
	w.Flush()
	if err := w.Error(); err != nil {
		_ = f.Close()
		return "", err
	}
	return path, f.Close()
}
