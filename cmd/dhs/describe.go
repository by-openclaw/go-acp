package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"dhs/internal/consumer"
	"dhs/internal/datastore"
	"dhs/internal/manifest"
)

// pointMetaJSON is the schema of a point as the JSON of get and set carries
// it beside the value (ADR-0037 P4).
type pointMetaJSON struct {
	Kind      string   `json:"kind"`
	Access    string   `json:"access"`
	Min       any      `json:"min,omitempty"`
	Max       any      `json:"max,omitempty"`
	Step      any      `json:"step,omitempty"`
	Default   any      `json:"default,omitempty"`
	Unit      string   `json:"unit,omitempty"`
	EnumItems []string `json:"enum_items,omitempty"`
	MaxLen    int      `json:"max_len,omitempty"`
}

// metaOf is the JSON shape of an object's schema; nil when none is known.
func metaOf(o *consumer.Object) *pointMetaJSON {
	if o == nil {
		return nil
	}
	return &pointMetaJSON{Kind: kindName(o.Kind), Access: accessStr(o.Access), Min: o.Min, Max: o.Max, Step: o.Step,
		Default: o.Def, Unit: o.Unit, EnumItems: o.EnumItems, MaxLen: o.MaxLen}
}

// describePoint finds the schema of one point and the device model it
// belongs to (ADR-0037 P4): the plugin's Describe when it has one; the
// walked tree and the identity probe otherwise. The manifest extract
// wrote completes the model reference. Either result may be nil when
// nothing is known; a get or set never fails for want of a description.
func describePoint(ctx context.Context, plug consumer.Protocol, proto, host string, req consumer.ValueRequest) (*consumer.Object, *consumer.DMRef) {
	var meta *consumer.Object
	var ref *consumer.DMRef
	if d, ok := plug.(consumer.Describer); ok {
		if o, r, err := d.Describe(ctx, req); err == nil {
			meta, ref = &o, &r
		}
	}
	if meta == nil {
		switch {
		case req.Label != "":
			meta = findObjectByLabel(plug, req.Slot, req.Group, req.Label)
		case req.Path != "" && !pathNative(plug, req.Path, ""):
			meta = findObjectByPath(plug, req.Slot, req.Path)
		case req.Path == "" && req.ID >= 0:
			meta = findObjectMeta(plug, req.Slot, req.Group, "", req.ID)
		}
	}
	if ref == nil {
		ref = &consumer.DMRef{}
		if meta != nil {
			ref.Path = strings.Join(meta.Path, ".")
		}
		if probe, ok := plug.(interface {
			IdentityProbe(context.Context, int) (string, error)
		}); ok {
			if identity, err := probe.IdentityProbe(ctx, req.Slot); err == nil && identity != "" {
				ref.Model, ref.SwRev, _ = strings.Cut(identity, "@")
			}
		}
	}
	dmFromManifest(proto, host, ref)
	if ref.IsZero() {
		ref = nil
	}
	return meta, ref
}

// dmFromManifest completes a model reference from the manifest extract
// wrote (.cache/manifest/<proto>/<host>.json, ADR-0022): the entry whose
// address carries every key of ref.Addr gives the model, its software
// revision and the template.
func dmFromManifest(proto, host string, ref *consumer.DMRef) {
	if treeStore == nil || ref == nil || len(ref.Addr) == 0 {
		return
	}
	path := filepath.Join(treeStore.BaseDir(), "manifest", datastore.SanitizePathSeg(proto), datastore.SanitizePathSeg(deviceKeyFor(proto, host))+".json")
	m, err := manifest.Load(path)
	if err != nil {
		return
	}
	for _, fr := range m.Frames {
		for _, sl := range fr.Slots {
			if !addrMatches(sl.Addr, ref.Addr) {
				continue
			}
			model, rev, _ := strings.Cut(sl.DM, "@")
			if ref.Model == "" {
				ref.Model = model
			}
			if ref.SwRev == "" {
				ref.SwRev = rev
			}
			if t, ok := sl.Addr["template"].(string); ok && ref.Template == "" {
				ref.Template = t
			}
			return
		}
	}
}

// addrMatches says whether a manifest address carries every wanted key
// with the same printed value (numbers arrive as float64 from JSON).
func addrMatches(addr, want map[string]any) bool {
	for k, v := range want {
		got, ok := addr[k]
		if !ok || fmt.Sprint(got) != fmt.Sprint(v) {
			return false
		}
	}
	return true
}

// findObjectByPath finds the object of a walked slot whose path is the
// one given, the way findObjectByLabel finds a label.
func findObjectByPath(plug consumer.Protocol, slot int, path string) *consumer.Object {
	objs, err := plug.Walk(context.Background(), slot)
	if err != nil {
		return nil
	}
	want := strings.Split(path, ".")
	for i := range objs {
		if strings.Join(objs[i].Path, ".") == path {
			return &objs[i]
		}
	}
	for i := range objs {
		p := objs[i].Path
		if len(p) > 1 && strings.EqualFold(p[0], "ROOT_NODE_V2") {
			p = p[1:]
		}
		if len(p) == len(want) && matchPathPrefix(objs[i].Path, want) {
			return &objs[i]
		}
	}
	return nil
}

// printDMRef prints the model reference of a point in text mode.
func printDMRef(ref *consumer.DMRef) {
	if ref == nil {
		return
	}
	line := "dm = "
	switch {
	case ref.Model != "" && ref.SwRev != "":
		line += ref.Model + "@" + ref.SwRev
	case ref.Model != "":
		line += ref.Model
	default:
		line += "(run extract once: the manifest names the model)"
	}
	if ref.Template != "" {
		line += "  template = " + ref.Template
	}
	if ref.Path != "" {
		line += "  path = " + ref.Path
	}
	fmt.Println(line)
}
