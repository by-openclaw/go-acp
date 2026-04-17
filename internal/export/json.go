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
	// Check if any slot has hierarchical objects.
	hierarchical := false
	for _, slot := range s.Slots {
		for _, o := range slot.Objects {
			if len(o.Path) > 2 {
				hierarchical = true
				break
			}
		}
		if hierarchical {
			break
		}
	}

	if !hierarchical {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(s); err != nil {
			return fmt.Errorf("json encode: %w", err)
		}
		return nil
	}

	// ACP2: build hierarchical JSON.
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
func buildJSONTree(objs []protocol.Object) map[string]any {
	root := make(map[string]any)
	for _, o := range objs {
		if len(o.Path) <= 1 {
			continue // skip ROOT_NODE_V2 itself
		}
		if o.Kind == protocol.KindRaw {
			// Container node — ensure map exists but don't add leaf data.
			cur := root
			for _, seg := range o.Path[1:] {
				if _, exists := cur[seg]; !exists {
					cur[seg] = make(map[string]any)
				}
				if sub, ok := cur[seg].(map[string]any); ok {
					cur = sub
				}
			}
			continue
		}
		// Leaf node — walk path and insert object properties.
		cur := root
		for _, seg := range o.Path[1 : len(o.Path)-1] {
			if _, exists := cur[seg]; !exists {
				cur[seg] = make(map[string]any)
			}
			if sub, ok := cur[seg].(map[string]any); ok {
				cur = sub
			}
		}
		// Last path element is the leaf label.
		leaf := jsonLeaf(o)
		cur[o.Path[len(o.Path)-1]] = leaf
	}
	return root
}

// jsonLeaf builds the property map for a single leaf object.
func jsonLeaf(o protocol.Object) map[string]any {
	m := map[string]any{
		"id":     o.ID,
		"kind":   kindName(o.Kind),
		"access": accessStr(o.Access),
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
