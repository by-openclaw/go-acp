package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"dhs/internal/export/canonical"
)

// dmFile mirrors internal/storage.DM (kept duplicated to avoid an
// import cycle between manifest and storage).
//
// Two shapes are supported on disk:
//   - flat / legacy : Objects populated (ACP1, ACP2)
//   - canonical     : Root populated (Ember+ — per ADR-0022 chunk
//                     e0d585a); Templates may also be present
type dmFile struct {
	Model     string                     `json:"model"`
	SwRev     string                     `json:"sw_rev"`
	Protocol  string                     `json:"protocol"`
	Root      json.RawMessage            `json:"root,omitempty"`
	Templates []*canonical.TemplateEntry `json:"templates,omitempty"`
	Objects   []dmObject                 `json:"objects,omitempty"`
}

// dmObject mirrors protocol.Object — only the fields we use.
type dmObject struct {
	Slot   int            `json:"slot"`
	Group  string         `json:"group,omitempty"`
	Path   []string       `json:"path,omitempty"`
	ID     int            `json:"id"`
	OID    string         `json:"oid,omitempty"`
	Meta   map[string]any `json:"meta,omitempty"`
	Label  string         `json:"label"`
	Unit   string         `json:"unit,omitempty"`
	Kind   string         `json:"kind"`
	Access uint8          `json:"access"`
	Min    any            `json:"min,omitempty"`
	Max    any            `json:"max,omitempty"`
	Step   any            `json:"step,omitempty"`
	Def       any      `json:"default,omitempty"`
	EnumItems []string `json:"enum_items,omitempty"`
	MaxLen    int      `json:"max_len,omitempty"`
	Value  json.RawMessage `json:"value,omitempty"`
}

// loadDM reads `.cache/dm/<proto>/<Model@SwRev>.json`.
func loadDM(path string) (*dmFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("dm read %s: %w", path, err)
	}
	var d dmFile
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("dm parse %s: %w", path, err)
	}
	return &d, nil
}

// BuildExport reads the manifest, resolves every DM under cacheDir,
// and assembles a canonical.Export ready for `factory.New(logger,
// tree)`. Each manifest slot becomes a child node under the root,
// containing the DM's object tree.
//
// Slot order is the manifest's `frames[].slots[]` order. Each slot's
// `addr` is preserved as a description annotation but does not affect
// tree shape.
//
// The conversion is lossy by design — we surface what the ACP1/ACP2
// providers need to answer walks (obj-id, label, kind, value, access)
// and drop the rest. Plugin-internal metadata (Meta) is captured in
// the node Description as JSON so it remains visible for debug.
func BuildExport(m *Manifest, cacheDir string) (*canonical.Export, error) {
	root := &canonical.Node{
		Header: canonical.Header{
			Number:     1,
			Identifier: m.Device.Name,
			Path:       m.Device.Name,
			OID:        "1",
			IsOnline:   true,
			Access:     "read",
			Children:   nil,
		},
	}

	var collectedTemplates []*canonical.TemplateEntry

	for _, fr := range m.Frames {
		for _, sl := range fr.Slots {
			dmPath := DMPath(cacheDir, m.Device.Protocol, sl.DM)
			dm, err := loadDM(dmPath)
			if err != nil {
				return nil, fmt.Errorf("manifest frame=%q slot dm=%q: %w", fr.Name, sl.DM, err)
			}
			// Canonical-shape DM (Ember+, refs #438 chunk e0d585a):
			// the slot DM IS a canonical sub-tree; graft Root directly
			// as a child of our synthetic device root. No need for
			// buildSlotNode's flat-object translation.
			if len(dm.Root) > 0 {
				slotEl, derr := canonical.UnmarshalElement(dm.Root)
				if derr != nil {
					return nil, fmt.Errorf("manifest frame=%q slot dm=%q: parse canonical root: %w", fr.Name, sl.DM, derr)
				}
				root.Children = append(root.Children, slotEl)
				if len(dm.Templates) > 0 {
					collectedTemplates = append(collectedTemplates, dm.Templates...)
				}
				continue
			}
			// Flat / legacy shape (ACP1, ACP2): assemble a synthetic
			// slot Node from the flat Objects slice.
			slotNum, slotOK := slotIndex(sl.Addr)
			if !slotOK {
				slotNum = len(root.Children)
			}
			slotNode, err := buildSlotNode(slotNum, sl, dm, m.Device.Name)
			if err != nil {
				return nil, fmt.Errorf("manifest frame=%q slot=%d: %w", fr.Name, slotNum, err)
			}
			root.Children = append(root.Children, slotNode)
		}
	}

	return &canonical.Export{Root: root, Templates: collectedTemplates}, nil
}

// slotIndex pulls a numeric slot index out of an addr map. Supports
// `{"slot": N}` (acp1, acp2) where N is int or json.Number. Other
// shapes return ok=false and the caller falls back to positional.
func slotIndex(addr map[string]any) (int, bool) {
	v, ok := addr["slot"]
	if !ok {
		return 0, false
	}
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case json.Number:
		n, err := x.Int64()
		if err != nil {
			return 0, false
		}
		return int(n), true
	case string:
		n, err := strconv.Atoi(x)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// buildSlotNode wraps one DM's object list into a slot-N subtree.
func buildSlotNode(slotNum int, sl Slot, dm *dmFile, devName string) (*canonical.Node, error) {
	addrJSON, _ := json.Marshal(sl.Addr)
	desc := fmt.Sprintf("dm=%s addr=%s", sl.DM, addrJSON)
	slotIdent := fmt.Sprintf("slot-%d", slotNum)
	slotNode := &canonical.Node{
		Header: canonical.Header{
			Number:      slotNum,
			Identifier:  slotIdent,
			Path:        devName + "." + slotIdent,
			OID:         "1." + strconv.Itoa(slotNum),
			Description: &desc,
			IsOnline:    true,
			Access:      "read",
		},
	}

	// Build a map of path-joined-with-dot → node, so we can hang
	// children. Walk objects in path-length order (shortest first) so
	// parents are created before their children.
	objs := append([]dmObject(nil), dm.Objects...)
	sort.SliceStable(objs, func(i, j int) bool {
		return len(objs[i].Path) < len(objs[j].Path)
	})

	nodeAtPath := map[string]*canonical.Node{
		"": slotNode,
	}

	for _, o := range objs {
		// path[] is the parent chain only when its last segment !=
		// o.Label (ACP1 walker convention: leaves carry group as
		// path, label is the leaf's own name). When path[-1] ==
		// o.Label, path includes self (ACP2 walker convention).
		parentSegs := o.Path
		if len(parentSegs) > 0 && parentSegs[len(parentSegs)-1] == o.Label {
			parentSegs = parentSegs[:len(parentSegs)-1]
		}
		parentJoined := strings.Join(parentSegs, ".")
		parent, ok := nodeAtPath[parentJoined]
		if !ok {
			parent = ensureAncestors(slotNode, devName, slotIdent, slotNum, parentSegs, nodeAtPath)
		}

		selfJoined := parentJoined
		if selfJoined == "" {
			selfJoined = o.Label
		} else {
			selfJoined = selfJoined + "." + o.Label
		}

		oid := o.OID
		if oid == "" {
			oid = slotNode.OID + "." + strconv.Itoa(o.ID)
		}

		// Ember+-specific elements live in Meta["element"]. Dispatch
		// before the generic container/leaf branch so matrices and
		// functions emit the correct canonical.* type (refs #438
		// chunk 6, ADR-0022).
		switch elementKind(o.Meta) {
		case "matrix":
			m := buildCanonicalMatrix(o, oid, slotNode.Path+"."+selfJoined)
			parent.Children = append(parent.Children, m)
			continue
		case "function":
			f := buildCanonicalFunction(o, oid, slotNode.Path+"."+selfJoined)
			parent.Children = append(parent.Children, f)
			continue
		case "template":
			// Templates aren't placed in the tree — provider keeps
			// them in a side registry, looked up by reference. The
			// Ember+ producer's tree-builder doesn't consume them via
			// canonical.Export today; skip silently.
			continue
		}

		if isContainerKind(o.Kind) {
			child := &canonical.Node{
				Header: canonical.Header{
					Number:     o.ID,
					Identifier: o.Label,
					Path:       slotNode.Path + "." + selfJoined,
					OID:        oid,
					IsOnline:   true,
					Access:     accessString(o.Access),
				},
			}
			parent.Children = append(parent.Children, child)
			nodeAtPath[selfJoined] = child
			continue
		}

		// Leaf parameter.
		typeStr, formatHint := paramTypeAndFormat(o)
		param := &canonical.Parameter{
			Header: canonical.Header{
				Number:     o.ID,
				Identifier: o.Label,
				Path:       slotNode.Path + "." + selfJoined,
				OID:        oid,
				IsOnline:   true,
				Access:     accessString(o.Access),
			},
			Type:    typeStr,
			Default: sanitiseScalar(typeStr, formatHint, o.Def),
			Minimum: sanitiseScalar(typeStr, formatHint, o.Min),
			Maximum: sanitiseScalar(typeStr, formatHint, o.Max),
			Step:    sanitiseScalar(typeStr, formatHint, o.Step),
		}
		if o.Unit != "" {
			u := o.Unit
			param.Unit = &u
		}
		if formatHint != "" {
			f := formatHint
			param.Format = &f
		}
		if typeStr == "enum" && len(o.EnumItems) > 0 {
			entries := make([]canonical.EnumEntry, len(o.EnumItems))
			for i, name := range o.EnumItems {
				entries[i] = canonical.EnumEntry{Key: name, Value: int64(i)}
			}
			param.EnumMap = entries
		}
		// Unwrap the protocol.Value envelope into a scalar the
		// provider's tree builder accepts. The DM stores values as
		// {kind, str|int|uint|float|bool|enum|ip} (per Value.MarshalJSON).
		// Passing the envelope as-is breaks buildProperties which expects
		// scalars.
		if v, ok := unwrapValue(o.Value); ok {
			param.Value = v
		}
		parent.Children = append(parent.Children, param)
	}

	return slotNode, nil
}

func ensureAncestors(slotNode *canonical.Node, devName, slotIdent string, slotNum int, ancestors []string, idx map[string]*canonical.Node) *canonical.Node {
	parent := slotNode
	cur := ""
	for i, seg := range ancestors {
		if cur == "" {
			cur = seg
		} else {
			cur = cur + "." + seg
		}
		if n, ok := idx[cur]; ok {
			parent = n
			continue
		}
		oid := slotNode.OID
		for j := 0; j <= i; j++ {
			oid = oid + "." + strconv.Itoa(j)
		}
		next := &canonical.Node{
			Header: canonical.Header{
				Number:     i,
				Identifier: seg,
				Path:       slotNode.Path + "." + cur,
				OID:        oid,
				IsOnline:   true,
				Access:     "read",
			},
		}
		parent.Children = append(parent.Children, next)
		idx[cur] = next
		parent = next
	}
	return parent
}

func isContainerKind(k string) bool {
	switch k {
	case "raw", "node", "":
		return true
	}
	return false
}

// paramTypeAndFormat maps the DM object onto a canonical-tree
// (type, format-hint) pair. The format hint disambiguates the ACP1
// wire type (int16 vs int32 vs uint8 vs ipv4 vs file etc.) so the
// provider's encoder can pick the right concrete codec.ObjectType.
//
// Priority:
//
//  1. meta.acp1_type — authoritative for ACP1 walks (carries the
//     wire type byte: 1=int16, 2=ipv4, 3=float, 4=enum, 5=string,
//     6=frame, 7=alarm, 8=file, 9=int32, 10=uint8).
//  2. o.Kind  — falls back to the ValueKind enum (cross-protocol).
func paramTypeAndFormat(o dmObject) (string, string) {
	if v, ok := o.Meta["acp1_type"]; ok {
		switch toUint8(v) {
		case 1:
			return "integer", "int16"
		case 2:
			return "string", "ipv4"
		case 3:
			return "real", ""
		case 4:
			return "enum", ""
		case 5:
			return "string", ""
		case 6:
			return "octets", "frame"
		case 7:
			return "boolean", "alarm"
		case 8:
			return "string", "file"
		case 9:
			return "integer", "int32"
		case 10:
			return "integer", "uint8"
		}
	}
	switch o.Kind {
	case "integer", "number", "int":
		return "integer", ""
	case "uint":
		return "integer", "uint8"
	case "real", "float":
		return "real", ""
	case "enum", "enumerated":
		return "enum", ""
	case "ipv4", "ipaddr", "ip":
		return "string", "ipv4"
	case "string":
		return "string", ""
	case "bool", "boolean":
		return "boolean", ""
	case "alarm":
		return "boolean", "alarm"
	case "frame":
		return "octets", "frame"
	}
	return "string", ""
}

// toUint8 best-effort numeric coerce for map[string]any values
// unmarshalled from JSON. Returns 0 on miss.
func toUint8(v any) uint8 {
	switch x := v.(type) {
	case float64:
		return uint8(x)
	case int:
		return uint8(x)
	case int64:
		return uint8(x)
	case uint8:
		return x
	case json.Number:
		n, _ := x.Int64()
		return uint8(n)
	}
	return 0
}

func accessString(a uint8) string {
	switch {
	case a&0x02 != 0 && a&0x01 != 0:
		return "readWrite"
	case a&0x02 != 0:
		return "write"
	case a&0x01 != 0:
		return "read"
	}
	return "read"
}

// (paramType deprecated — use paramTypeAndFormat)

// sanitiseScalar drops a Min/Max/Step/Default value if it can't be
// represented in the parameter's declared type. The DM cache stores
// enum bounds as label strings ("Off", "Auto") for human readability;
// the provider's buildProperties wants the index. Mismatches surface
// as ERROR log spam on every consumer walk — better to drop than to
// fight the type system.
func sanitiseScalar(typeStr, formatHint string, v any) any {
	if v == nil {
		return nil
	}
	// IPv4 / file / frame parameters: bounds carry no useful wire
	// meaning (they encode the address-space, not a value range).
	// Drop them so the encoder doesn't try to format a uint32 as a
	// dotted-quad.
	switch formatHint {
	case "ipv4", "file", "frame":
		return nil
	}
	switch typeStr {
	case "integer", "boolean", "enum":
		switch x := v.(type) {
		case float64:
			return int64(x)
		case int:
			return int64(x)
		case uint64:
			return int64(x)
		case string:
			return nil
		}
	case "real":
		switch x := v.(type) {
		case int:
			return float64(x)
		case int64:
			return float64(x)
		case uint64:
			return float64(x)
		case string:
			return nil
		}
	}
	return v
}

// unwrapValue pulls the scalar out of a serialised protocol.Value
// envelope:  {"kind":"int","int":42} → int64(42).
// Returns (nil, false) on null, empty, or unsupported envelopes; the
// caller leaves Parameter.Value zero so the provider's tree builder
// uses the type's natural default.
func unwrapValue(raw []byte) (any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	trim := strings.TrimSpace(string(raw))
	if trim == "" || trim == "null" {
		return nil, false
	}
	var env struct {
		Kind  string  `json:"kind"`
		Bool  *bool   `json:"bool"`
		Int   *int64  `json:"int"`
		Uint  *uint64 `json:"uint"`
		Float *float64 `json:"float"`
		Str   *string `json:"str"`
		IP    string  `json:"ip"`
		Enum  *uint8  `json:"enum"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, false
	}
	switch env.Kind {
	case "string":
		if env.Str != nil {
			return *env.Str, true
		}
	case "int":
		if env.Int != nil {
			return *env.Int, true
		}
	case "uint":
		if env.Uint != nil {
			// Provider encoder accepts int64 for integer-type
			// params. Coerce; uint8/uint16/uint32 always fit.
			return int64(*env.Uint), true
		}
	case "float":
		if env.Float != nil {
			return *env.Float, true
		}
	case "bool":
		if env.Bool != nil {
			return *env.Bool, true
		}
	case "enum":
		if env.Enum != nil {
			return int64(*env.Enum), true
		}
	case "ipaddr":
		if env.IP != "" {
			return env.IP, true
		}
	}
	return nil, false
}
