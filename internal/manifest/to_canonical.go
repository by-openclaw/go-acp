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
type dmFile struct {
	Model    string `json:"model"`
	SwRev    string `json:"sw_rev"`
	Protocol string `json:"protocol"`
	Objects  []dmObject `json:"objects"`
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
	Def    any            `json:"default,omitempty"`
	MaxLen int            `json:"max_len,omitempty"`
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

	for _, fr := range m.Frames {
		for _, sl := range fr.Slots {
			dmPath := DMPath(cacheDir, m.Device.Protocol, sl.DM)
			dm, err := loadDM(dmPath)
			if err != nil {
				return nil, fmt.Errorf("manifest frame=%q slot dm=%q: %w", fr.Name, sl.DM, err)
			}
			slotNum, slotOK := slotIndex(sl.Addr)
			if !slotOK {
				slotNum = len(root.Children) // fallback: positional
			}
			slotNode, err := buildSlotNode(slotNum, sl, dm, m.Device.Name)
			if err != nil {
				return nil, fmt.Errorf("manifest frame=%q slot=%d: %w", fr.Name, slotNum, err)
			}
			root.Children = append(root.Children, slotNode)
		}
	}

	return &canonical.Export{Root: root}, nil
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
		joinedPath := strings.Join(o.Path, ".")
		parentJoined := strings.Join(o.Path[:max0(len(o.Path)-1)], ".")
		parent, ok := nodeAtPath[parentJoined]
		if !ok {
			// Materialise missing ancestors as bare nodes.
			parent = ensureAncestors(slotNode, devName, slotIdent, slotNum, o.Path[:len(o.Path)-1], nodeAtPath)
		}

		oid := o.OID
		if oid == "" {
			oid = slotNode.OID + "." + strconv.Itoa(o.ID)
		}

		if isContainerKind(o.Kind) {
			child := &canonical.Node{
				Header: canonical.Header{
					Number:     o.ID,
					Identifier: o.Label,
					Path:       slotNode.Path + "." + joinedPath,
					OID:        oid,
					IsOnline:   true,
					Access:     accessString(o.Access),
				},
			}
			parent.Children = append(parent.Children, child)
			nodeAtPath[joinedPath] = child
			continue
		}

		// Leaf parameter.
		typeStr := paramType(o.Kind)
		param := &canonical.Parameter{
			Header: canonical.Header{
				Number:     o.ID,
				Identifier: o.Label,
				Path:       slotNode.Path + "." + joinedPath,
				OID:        oid,
				IsOnline:   true,
				Access:     accessString(o.Access),
			},
			Type:    typeStr,
			Default: sanitiseScalar(typeStr, o.Def),
			Minimum: sanitiseScalar(typeStr, o.Min),
			Maximum: sanitiseScalar(typeStr, o.Max),
			Step:    sanitiseScalar(typeStr, o.Step),
		}
		if o.Unit != "" {
			u := o.Unit
			param.Unit = &u
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

// paramType maps the DM kind enum onto the canonical-tree type
// string. The provider's tree builder accepts these literal values.
func paramType(k string) string {
	switch k {
	case "integer", "number", "int":
		return "integer"
	case "real", "float":
		return "real"
	case "enum", "enumerated":
		return "enum"
	case "ipv4", "ip":
		return "string"
	case "string":
		return "string"
	case "bool", "boolean":
		return "boolean"
	}
	return "string"
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

func max0(x int) int {
	if x < 0 {
		return 0
	}
	return x
}

// sanitiseScalar drops a Min/Max/Step/Default value if it can't be
// represented in the parameter's declared type. The DM cache stores
// enum bounds as label strings ("Off", "Auto") for human readability;
// the provider's buildProperties wants the index. Mismatches surface
// as ERROR log spam on every consumer walk — better to drop than to
// fight the type system.
func sanitiseScalar(typeStr string, v any) any {
	if v == nil {
		return nil
	}
	switch typeStr {
	case "integer", "real", "boolean", "enum":
		switch v.(type) {
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
			return *env.Uint, true
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
