package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"dhs/internal/datastore"
	"dhs/internal/manifest"
	"dhs/internal/rrcs/codec"
	rrcs "dhs/internal/rrcs/consumer"
)

// rrcsExtract drives `dhs consumer rrcs extract`: the device models of
// the system, one per type as templates (ADR-0022), and the inventory
// that says where each instance is (ADR-0028):
//
//	.cache/dm/rrcs/<Type@version>.json      the node's model, one per card type
//	.cache/manifest/rrcs/<ip>.json           node, cards, ports: address, type, role
//
// The specification gives the shape of every type; the walk of this run
// says which types the system has, which blocks each carries, and what
// it exposes beyond the printed tables. Eight list requests, plus the
// keys and the commands of one port per type.
func rrcsExtract(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("rrcs extract", flag.ContinueOnError)
	src := newRRCSSource(fs)
	cf := src.cf
	refresh := fs.Bool("refresh", false, "rewrite a device model that exists (default: a model that exists is kept, the inventory is rewritten)")
	if err := parseVerbFlags(fs, reorderFlagsFirst(args)); err != nil {
		return err
	}
	hosts := fs.Args()
	if treeStore == nil {
		return rrcsValErr("extract", "the cache is not available (.cache next to the binary)")
	}
	var (
		model    *rrcsModel
		exTypes  map[int]string
		version  string
		host     string
		port     = rrcs.DefaultPort
		requests int
	)
	switch {
	case *src.from != "" && len(hosts) > 0:
		return rrcsValErr("extract", "give a host or --from, not both")
	case *src.from != "":
		// From a walk: the snapshot holds the version, the lists, the
		// keys and the commands of every port.
		raw, err := os.ReadFile(*src.from)
		if err != nil {
			return fmt.Errorf("rrcs extract: %w", err)
		}
		var snap map[string]any
		if err := json.Unmarshal(raw, &snap); err != nil {
			return fmt.Errorf("rrcs extract: snapshot: %w", err)
		}
		if jStr(snap, "proto") != rrcsProto {
			return fmt.Errorf("rrcs extract: %s is not an rrcs snapshot", *src.from)
		}
		model = rrcsBuildModel(snap)
		exTypes = rrcs.ExTypeNames(snap)
		version = rrcs.Version(model.Status["GetVersion"])
		host = hostOnly(jStr(snap, "target"))
		if _, p, err := splitHostPort(jStr(snap, "target"), rrcs.DefaultPort); err == nil {
			port = p
		}
	case len(hosts) != 1:
		return rrcsValErr("extract", "want one host[:port] argument, or --from FILE")
	default:
		client, _, closeFn, err := cf.open("extract", hosts[0])
		if err != nil {
			return err
		}
		defer closeFn()
		host = hostOnly(client.Peer())
		if _, p, err := splitHostPort(client.Peer(), rrcs.DefaultPort); err == nil {
			port = p
		}

		// 1. The version names the surface.
		reply, err := client.Call(ctx, "GetVersion")
		if err != nil {
			return fmt.Errorf("rrcs extract: GetVersion: %w", err)
		}
		version = rrcs.Version(rrcsFirstString(reply.Payload()))

		// 2. The lists: every instance with every property.
		snap, _, err := rrcsCollect(ctx, client, rrcsCollectOpts{Only: rrcs.ExtractLists})
		if err != nil {
			return err
		}
		if model, err = rrcsModelOf(snap); err != nil {
			return err
		}

		// 3. The keys and the commands of one port per type: the shape
		// of a key, and the command types this system uses.
		doc, _ := json.Marshal(snap)
		var raw map[string]any
		_ = json.Unmarshal(doc, &raw)
		exTypes = rrcs.ExTypeNames(raw)
		requests = len(snap.Lists) + 1
		seen := map[string]bool{}
		for _, p := range model.Ports {
			name := rrcs.TypeName(-1, p.Type, nil)
			if _, ok := p.Raw["PortExType"]; ok {
				name = rrcs.TypeName(jInt(p.Raw, "PortExType"), p.Type, exTypes)
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			isInput := p.Input && !p.Output
			args := []codec.Value{codec.Int(int32(p.Net)), codec.Int(int32(p.Node)), codec.Int(int32(p.Port)), codec.Bool(isInput), codec.Int(-1)}
			if rec, ok := rrcsRecordedCall(ctx, client, "GetPortsCommandLists", args...); ok {
				snap.Commands = append(snap.Commands, rec)
			}
			requests++
			if p.KeyCount > 0 {
				args := []codec.Value{codec.Int(int32(p.Node)), codec.Int(int32(p.Port)), codec.Bool(isInput), codec.Int(-1)}
				if rec, ok := rrcsRecordedCall(ctx, client, "GetAllKeyConfiguration", args...); ok {
					snap.KeyConfigs = append(snap.KeyConfigs, rec)
				}
				requests++
			}
		}
		if model, err = rrcsModelOf(snap); err != nil {
			return err
		}
	}
	if version == "" {
		return fmt.Errorf("rrcs extract: the version of the gateway is not known (GetVersion)")
	}

	// 4. The models and the inventory.
	set, err := rrcs.BuildDMs(model, version, exTypes)
	if err != nil {
		return err
	}
	type written struct {
		Identity string `json:"identity"`
		Path     string `json:"path"`
		State    string `json:"state"`
		Sha256   string `json:"sha256,omitempty"`
	}
	var files []written
	write := func(f *rrcs.DMFile) error {
		path := treeStore.IdentityPath(rrcsProto, f.Identity)
		if _, statErr := os.Stat(path); statErr == nil && !*refresh {
			files = append(files, written{Identity: f.Identity, Path: path, State: "kept"})
			return nil
		}
		dm := datastore.DM{Model: f.Model, SwRev: f.SwRev, Protocol: rrcsProto, Root: f.Export.Root, Templates: f.Export.Templates}
		if err := treeStore.WriteDM(rrcsProto, f.Identity, dm); err != nil {
			return fmt.Errorf("rrcs extract: %w", err)
		}
		sum, _ := sha256File(path)
		files = append(files, written{Identity: f.Identity, Path: path, State: "written", Sha256: sum})
		return nil
	}
	if err := write(set.Node); err != nil {
		return err
	}
	for _, c := range set.Cards {
		if err := write(c); err != nil {
			return err
		}
	}
	mf := rrcs.Manifest(model, host, port, set)
	mfPath, err := manifest.Write(treeStore.BaseDir(), mf)
	if err != nil {
		return fmt.Errorf("rrcs extract: %w", err)
	}
	entries := 0
	for _, fr := range mf.Frames {
		entries += len(fr.Slots)
	}

	// 5. The account.
	if cf.output == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", " ")
		return enc.Encode(map[string]any{
			"version": version, "requests": requests, "device": mf.Device.Name,
			"models": files, "manifest": mfPath, "entries": entries, "types": set.Types,
		})
	}
	fmt.Printf("version:  %s (%s)\n", version, mf.Device.Name)
	if *src.from != "" {
		fmt.Printf("source:   %s\n", *src.from)
	} else {
		fmt.Printf("requests: %d\n", requests)
	}
	for _, f := range files {
		if f.State == "written" {
			fmt.Printf("model %s: %s (%s)\n", f.State, f.Path, f.Sha256)
		} else {
			fmt.Printf("model %s:    %s (--refresh rewrites it)\n", f.State, f.Path)
		}
	}
	fmt.Printf("manifest: %s (%d entries)\n", mfPath, entries)
	fmt.Println()
	wName, wObject := len("TYPE"), len("OBJECT")
	for _, t := range set.Types {
		wName, wObject = max(wName, len(t.Name)), max(wObject, len(t.Object))
	}
	fmt.Printf("%-*s  %-*s  COUNT  PROPS  OBSERVED  NOT-EXPOSED  ROLES\n", wName, "TYPE", wObject, "OBJECT")
	for _, t := range set.Types {
		roles := make([]string, 0, len(t.Roles))
		for k, n := range t.Roles {
			roles = append(roles, fmt.Sprintf("%s %d", k, n))
		}
		sort.Strings(roles)
		fmt.Printf("%-*s  %-*s  %5d  %5d  %8d  %11d  %s\n", wName, t.Name, wObject, t.Object, t.Count, t.Props, len(t.Observed), len(t.Hidden), strings.Join(roles, ", "))
	}
	for _, t := range set.Types {
		if len(t.Observed) > 0 {
			fmt.Printf("\n%s — observed, not in the specification's tables: %s\n", t.Name, strings.Join(t.Observed, " "))
		}
	}
	return nil
}

// rrcsRecordedCall sends one request and records it as a walk does, so
// the model reads it as it reads a snapshot. A refused request is
// recorded with its error and skipped.
func rrcsRecordedCall(ctx context.Context, client *rrcs.Client, method string, params ...codec.Value) (rrcs.WalkCall, bool) {
	rec := rrcs.WalkCall{Method: method}
	for _, p := range params {
		rec.Args = append(rec.Args, rrcsJSON(p))
	}
	reply, err := client.Call(ctx, method, params...)
	if err != nil {
		rec.Error = err.Error()
		return rec, false
	}
	rec.Payload = rrcsJSON(reply.Payload())
	return rec, true
}

// rrcsFirstString is the first string an answer holds, at any depth.
func rrcsFirstString(v codec.Value) string {
	switch v.Kind {
	case codec.KindString:
		return v.Str
	case codec.KindArray:
		for _, it := range v.Items {
			if s := rrcsFirstString(it); s != "" {
				return s
			}
		}
	case codec.KindStruct:
		for _, m := range v.Members {
			if s := rrcsFirstString(m.Value); s != "" {
				return s
			}
		}
	}
	return ""
}
