package export

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"acp/internal/protocol"
)

// WriteJSON emits a Snapshot as pretty-printed JSON. Uses stdlib
// encoding/json — no external dependencies.
//
// For ACP2 (hierarchical path depth > 2), slots are rendered as nested
// object trees matching the device structure. For ACP1 (flat), the
// original grouped format is preserved.
func WriteJSON(w io.Writer, s *Snapshot) error {
	// Always use hierarchical JSON — ACP1 groups (identity, control,
	// status, alarm) and ACP2 tree (BOARD, PSU, etc.) both render as
	// nested object trees.
	out := jsonHierarchicalSnapshot(s)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("json encode: %w", err)
	}
	return nil
}

// jsonHierarchicalSnapshot builds a map-based representation where each
// slot's objects are nested by their Path, matching the device tree.
func jsonHierarchicalSnapshot(s *Snapshot) map[string]any {
	out := map[string]any{
		"device":     s.Device,
		"generator":  s.Generator,
		"created_at": s.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}

	slots := make([]map[string]any, 0, len(s.Slots))
	for _, slot := range s.Slots {
		sd := map[string]any{
			"slot":      slot.Slot,
			"walked_at": slot.WalkedAt.UTC().Format("2006-01-02T15:04:05Z"),
		}
		if slot.Status != "" {
			sd["status"] = slot.Status
		}

		tree := buildJSONTree(slot.Objects)
		sd["objects"] = tree

		slots = append(slots, sd)
	}
	out["slots"] = slots
	return out
}

// buildJSONTree creates nested maps from flat objects using their Path.
//
// Unified logic across all protocols:
//
//	ACP1    Path = ["identity"]                    depth 1 → group key
//	        Label = "Card name"                    leaf key at depth 2
//	ACP2    Path = ["ROOT_NODE_V2","BOARD","Card"] depth 3 → strip ROOT, nest
//	Ember+  Path = ["router","oneToN","labels",    depth N → nest at every
//	               "targets","t-1"]                          level
//
// The ROOT_NODE_V2 sentinel is dropped. Container nodes (Kind=Raw,
// typical for Ember+ internal nodes) are materialised as empty maps
// so their children have a place to nest, but they do not produce a
// leaf entry of their own.
func buildJSONTree(objs []protocol.Object) map[string]any {
	root := make(map[string]any)
	for _, o := range objs {
		path := stripRootNodeSentinel(o.Path)

		// Container nodes (Kind=Raw): create the sub-map so
		// children have a place to nest, and record the
		// container's own metadata under a reserved "_meta" key.
		// We cannot flatten container props alongside children —
		// they'd clash with child identifiers (e.g. a matrix's
		// "labels" metadata field vs its "labels" child node).
		if o.Kind == protocol.KindRaw {
			sub := ensureMapChain(root, path)
			if meta := containerMeta(o); meta != nil {
				sub["_meta"] = meta
			}
			continue
		}

		// Build the keying path: when Path has > 1 element the last
		// element is already the object's own key; for ACP1 (Path=[group])
		// we append Label so objects group under their section.
		keyPath := path
		if len(keyPath) == 1 {
			keyPath = append([]string(nil), path...)
			keyPath = append(keyPath, o.Label)
		}
		if len(keyPath) == 0 {
			// No path info at all — fall back to Group+Label.
			group := o.Group
			if group == "" {
				group = "other"
			}
			keyPath = []string{group, o.Label}
		}

		// Walk every segment except the last, creating maps as needed.
		cur := ensureMapChain(root, keyPath[:len(keyPath)-1])
		cur[keyPath[len(keyPath)-1]] = jsonLeaf(o)
	}
	return root
}

// stripRootNodeSentinel removes the ACP2 synthetic root prefix if present.
// No-op for ACP1 / Ember+.
func stripRootNodeSentinel(path []string) []string {
	if len(path) > 0 && strings.EqualFold(path[0], "ROOT_NODE_V2") {
		return path[1:]
	}
	return path
}

// containerMeta builds the `_meta` payload attached to every container
// (Kind=Raw) node: numeric OID, local element number, access bits, and
// every protocol-specific property the plugin placed in Object.Meta
// (matrix type/connections/labels, function arguments/result, node
// description/isOnline, etc). Returns nil when there is nothing
// informative to emit.
func containerMeta(o protocol.Object) map[string]any {
	m := map[string]any{}
	if o.OID != "" {
		m["oid"] = o.OID
	}
	m["number"] = o.ID
	if o.Label != "" {
		m["identifier"] = o.Label
	}
	m["access"] = accessStr(o.Access)
	for k, v := range o.Meta {
		m[k] = v
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// ensureMapChain walks segs from root, creating empty sub-maps where
// segments are missing, and returns the deepest map reached. When a
// non-map value already occupies a segment name we skip past it —
// this happens when a leaf and a container share a name; the leaf
// wins (unlikely in practice, but a defensive choice).
func ensureMapChain(root map[string]any, segs []string) map[string]any {
	cur := root
	for _, seg := range segs {
		if _, exists := cur[seg]; !exists {
			cur[seg] = make(map[string]any)
		}
		if sub, ok := cur[seg].(map[string]any); ok {
			cur = sub
		}
	}
	return cur
}

// jsonLeaf builds the property map for a single leaf object. For
// Ember+ this also merges the plugin-supplied Meta (parameter
// description/format/formula/factor/streamDescriptor/enumMap/...)
// flat so the exported JSON can be fed back to a provider.
func jsonLeaf(o protocol.Object) map[string]any {
	m := map[string]any{
		"id":     o.ID,
		"kind":   kindName(o.Kind),
		"access": accessStr(o.Access),
	}
	if o.OID != "" {
		m["oid"] = o.OID
	}
	if o.Unit != "" {
		m["unit"] = o.Unit
	}
	switch o.Kind {
	case protocol.KindInt, protocol.KindUint, protocol.KindFloat:
		m["min"] = o.Min
		m["max"] = o.Max
		m["step"] = o.Step
		m["default"] = o.Def
	case protocol.KindEnum:
		m["enum_items"] = o.EnumItems
		m["default"] = o.Def
	case protocol.KindString:
		if o.MaxLen > 0 {
			m["max_len"] = o.MaxLen
		}
	}
	// Value
	v := o.Value
	switch v.Kind {
	case protocol.KindInt:
		m["value"] = v.Int
	case protocol.KindUint:
		m["value"] = v.Uint
	case protocol.KindFloat:
		m["value"] = v.Float
	case protocol.KindEnum:
		m["value"] = v.Enum
		if v.Str != "" {
			m["value_name"] = v.Str
		}
	case protocol.KindString:
		m["value"] = v.Str
	case protocol.KindIPAddr:
		m["value"] = fmt.Sprintf("%d.%d.%d.%d",
			v.IPAddr[0], v.IPAddr[1], v.IPAddr[2], v.IPAddr[3])
	}
	// Merge plugin-supplied metadata last so the leaf carries
	// description/format/formula/streamDescriptor/etc. Existing
	// standard keys win on collision.
	for k, val := range o.Meta {
		if _, exists := m[k]; !exists {
			m[k] = val
		}
	}
	return m
}

// ReadJSON parses a Snapshot from JSON bytes. Handles both the flat
// format (objects as array) and the hierarchical format (objects as
// nested map) for full round-trip compatibility.
func ReadJSON(r io.Reader) (*Snapshot, error) {
	// First try the flat format (original Snapshot struct).
	var raw json.RawMessage
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, fmt.Errorf("json decode: %w", err)
	}

	// Try flat format first.
	var s Snapshot
	if err := json.Unmarshal(raw, &s); err == nil && len(s.Slots) > 0 && len(s.Slots[0].Objects) > 0 {
		return &s, nil
	}

	// Hierarchical format — parse manually and flatten.
	return readHierarchicalJSON(raw)
}

// readHierarchicalJSON parses the nested JSON tree format back into a
// flat Snapshot. The "objects" field in each slot is a nested map
// instead of an array.
func readHierarchicalJSON(raw json.RawMessage) (*Snapshot, error) {
	var doc struct {
		Device    DeviceInfo `json:"device"`
		Generator string     `json:"generator"`
		CreatedAt string     `json:"created_at"`
		Slots     []struct {
			Slot     int             `json:"slot"`
			Status   string          `json:"status"`
			WalkedAt string          `json:"walked_at"`
			Objects  json.RawMessage `json:"objects"`
		} `json:"slots"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("json decode hierarchical: %w", err)
	}

	snap := &Snapshot{
		Device:    doc.Device,
		Generator: doc.Generator,
	}
	snap.CreatedAt, _ = parseTime(doc.CreatedAt)

	for _, sd := range doc.Slots {
		dump := SlotDump{
			Slot:   sd.Slot,
			Status: sd.Status,
		}
		dump.WalkedAt, _ = parseTime(sd.WalkedAt)

		// Try array first (flat), then map (hierarchical).
		var flatObjs []protocol.Object
		if err := json.Unmarshal(sd.Objects, &flatObjs); err == nil {
			dump.Objects = flatObjs
		} else {
			var tree map[string]json.RawMessage
			if err := json.Unmarshal(sd.Objects, &tree); err == nil {
				flattenJSONTree(tree, sd.Slot, nil, &dump.Objects)
			}
		}

		snap.Slots = append(snap.Slots, dump)
	}
	return snap, nil
}

// flattenJSONTree recursively walks a nested JSON map and produces flat
// protocol.Object entries. Each leaf has "id", "kind", "value" etc.
// Each branch is a container node with sub-keys.
func flattenJSONTree(tree map[string]json.RawMessage, slot int, path []string, out *[]protocol.Object) {
	for name, raw := range tree {
		curPath := append(append([]string{}, path...), name)

		// Try to parse as leaf (has "id" and "kind" fields).
		var leaf map[string]json.RawMessage
		if err := json.Unmarshal(raw, &leaf); err != nil {
			continue
		}

		if _, hasID := leaf["id"]; hasID {
			// Leaf object.
			obj := protocol.Object{
				Slot:  slot,
				Label: name,
				Path:  curPath,
			}
			if len(curPath) > 0 {
				obj.Group = curPath[0]
			}
			// Re-unmarshal the leaf into a temporary struct to get all fields.
			var lf struct {
				ID        int              `json:"id"`
				Kind      string           `json:"kind"`
				Access    string           `json:"access"`
				Unit      string           `json:"unit"`
				Min       any              `json:"min"`
				Max       any              `json:"max"`
				Step      any              `json:"step"`
				Default   any              `json:"default"`
				EnumItems []string         `json:"enum_items"`
				MaxLen    int              `json:"max_len"`
				Value     json.RawMessage  `json:"value"`
				ValueName string           `json:"value_name"`
			}
			_ = json.Unmarshal(raw, &lf)
			obj.ID = lf.ID
			obj.Kind = parseValueKind(lf.Kind)
			obj.Access = parseAccess(lf.Access)
			obj.Unit = lf.Unit
			obj.EnumItems = lf.EnumItems
			obj.MaxLen = lf.MaxLen
			if lf.Value != nil {
				valStr := strings.Trim(string(lf.Value), "\"")
				obj.Value = parseCSVValue(obj.Kind, valStr, lf.ValueName, lf.EnumItems)
			}
			*out = append(*out, obj)
		} else {
			// Container — recurse.
			flattenJSONTree(leaf, slot, curPath, out)
		}
	}
}

// parseTime tries common time formats.
func parseTime(s string) (t time.Time, err error) {
	for _, layout := range []string{
		"2006-01-02T15:04:05Z",
		time.RFC3339,
	} {
		if t, err = time.Parse(layout, s); err == nil {
			return
		}
	}
	return
}
