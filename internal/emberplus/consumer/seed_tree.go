package emberplus

import (
	"strconv"
	"strings"
	"time"

	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/emberplus/codec/matrix"
	"dhs/internal/protocol"
)

// SeedTreeFromCachedObjects rehydrates the in-RAM tree from a slice of
// canonical protocol.Object loaded from .cache/dm/emberplus/<identity>.json
// (per ADR-0022). Each Object carries an Ember+-specific Meta map written
// by the walker (nodeMeta / parameterMeta / matrixMeta / functionMeta);
// this method reads those keys back into minimal glow.Node / glow.Parameter
// / glow.Matrix / glow.Function structs so the existing verbs (matrix,
// get, set, stream, watch) keep working without a fresh wire walk.
//
// Slot is always 0 for Ember+ — the plugin flattens the Glow tree.
// Non-zero slots are silently ignored to match the ACP2 contract.
//
// Freshness: every seeded entry starts at FreshnessStale. The first live
// confirmation (announce, get reply, walk) flips it to FreshnessLive.
// Callers therefore know which values are disk-shadow vs wire-fresh.
//
// What gets rehydrated:
//
//   - Path resolution maps (numIndex, pathIndex, labelIndex, numPath) so
//     all subsequent ValueRequest path/label/id lookups work.
//   - glowNode.Path / Identifier — enough for parent-path traversal.
//   - glowParam.Path / Identifier / Type / StreamIdentifier +
//     HasStreamIdentifier — enough for get/set/subscribe/stream.
//   - glowMatrix.Path / Identifier + matrixState — enough for
//     matrix verb (SendMatrixConnect takes the numeric Path).
//   - streamIndex (stream parameters indexed by streamIdentifier) so
//     `dhs stream` finds subscribers without a walk.
//
// What does NOT get rehydrated (post-cache callers fall back gracefully):
//
//   - Glow ParameterContents fields not currently required by any verb
//     (Description, Minimum, Maximum, etc.) — read from Meta on demand.
//   - Matrix.Targets / Sources / Labels / Connections — populated on first
//     live walk or matrix GetDirectory, not from the cache.
//
// The contract mirrors ACP2's SeedTreeFromCachedObjects (#363): seed
// enough structure for the watch/get/set/matrix paths to function;
// per-value enrichment continues live.
func (p *Plugin) SeedTreeFromCachedObjects(slot int, objs []protocol.Object) {
	if slot != 0 {
		return
	}

	p.treeMu.Lock()
	defer p.treeMu.Unlock()

	if p.numIndex == nil {
		p.numIndex = make(map[string]*treeEntry)
	}
	if p.pathIndex == nil {
		p.pathIndex = make(map[string]*treeEntry)
	}
	if p.labelIndex == nil {
		p.labelIndex = make(map[string][]*treeEntry)
	}
	if p.numPath == nil {
		p.numPath = make(map[string]string)
	}

	p.subsMu.Lock()
	if p.streamIndex == nil {
		p.streamIndex = make(map[int64][]string)
	}
	p.subsMu.Unlock()

	now := time.Now()
	for _, o := range objs {
		entry := p.seedOneEntry(o, now)
		if entry == nil {
			continue
		}
		numKey := numericKey(entry.numericPath)
		strKey := strings.Join(o.Path, ".")
		if numKey != "" {
			p.numIndex[numKey] = entry
			if o.Label != "" {
				p.numPath[numKey] = o.Label
			}
		}
		if strKey != "" && strKey != numKey {
			p.pathIndex[strKey] = entry
		}
		if o.Label != "" {
			p.labelIndex[o.Label] = append(p.labelIndex[o.Label], entry)
		}
		if entry.glowParam != nil && entry.glowParam.StreamIdentifier != 0 {
			p.subsMu.Lock()
			id := entry.glowParam.StreamIdentifier
			p.streamIndex[id] = append(p.streamIndex[id], numKey)
			p.subsMu.Unlock()
		}
	}
}

// seedOneEntry rebuilds the appropriate glow.* struct for one Object
// based on its Meta["element"] discriminator. Unknown / missing element
// returns nil — caller skips it.
func (p *Plugin) seedOneEntry(o protocol.Object, now time.Time) *treeEntry {
	numericPath := parseNumericOID(o.OID)
	if len(numericPath) == 0 {
		return nil
	}
	identifier := lastPathSegment(o.Path)

	entry := &treeEntry{
		obj:         o,
		numericPath: numericPath,
		freshness:   FreshnessStale,
		updatedAt:   now,
	}

	element := metaString(o.Meta, "element")
	switch element {
	case "node":
		entry.glowNode = &glow.Node{
			Path:        numericPath,
			Identifier:  identifier,
			Description: metaString(o.Meta, "description"),
		}
	case "parameter":
		param := &glow.Parameter{
			Path:        numericPath,
			Identifier:  identifier,
			Description: metaString(o.Meta, "description"),
		}
		if t := metaString(o.Meta, "type"); t != "" {
			param.Type = paramTypeFromName(t)
		}
		// Stream identifier. NOTE: until #436 (PR #437) merges and adds
		// HasStreamIdentifier, id=0 is indistinguishable from "absent".
		// After that PR merges, this branch rebases and a follow-up
		// preserves the presence bit through the cache round-trip.
		if v, ok := o.Meta["streamIdentifier"]; ok {
			if id, ok := metaInt64(v); ok {
				param.StreamIdentifier = id
			}
		}
		entry.glowParam = param
	case "matrix":
		m := buildMatrixFromMeta(numericPath, identifier, o.Meta)
		entry.glowMatrix = m
		entry.matrixState = matrix.NewStateFromGlow(m)
	case "function":
		entry.glowFunc = &glow.Function{
			Path:        numericPath,
			Identifier:  identifier,
			Description: metaString(o.Meta, "description"),
		}
	default:
		// Pre-#438 cache files may carry objects without an element
		// key. Keep them in the index for label resolution only —
		// no glow struct, get/set/matrix on them will fail clean.
	}
	return entry
}

// parseNumericOID is the reverse of numericKey: "1.2.3" → []int32{1,2,3}.
// Tolerates empty strings and ignores blank segments.
func parseNumericOID(s string) []int32 {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ".")
	out := make([]int32, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		n, err := strconv.ParseInt(p, 10, 32)
		if err != nil {
			return nil
		}
		out = append(out, int32(n))
	}
	return out
}

// lastPathSegment returns the last element of a string-path slice. Used
// to recover the Glow identifier from the cached Object.Path.
func lastPathSegment(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

// metaString reads a string-valued key from the Meta map. Returns ""
// when the key is missing or the value isn't a string. JSON round-trip
// preserves strings as strings, so no type-coercion gymnastics needed.
func metaString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// metaInt64 coerces a Meta value into int64. Tolerates the JSON-decoded
// float64 arriving when a Snapshot round-trips through encoding/json
// plus the live walker's typed values. Mirrors ACP2.metaToUint8.
func metaInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case int:
		return int64(x), true
	case int32:
		return int64(x), true
	case float64:
		return int64(x), true
	}
	return 0, false
}

// paramTypeFromName is the reverse of paramTypeName (declared in
// plugin.go). Defaults to ParamTypeNull on unknown / empty inputs —
// the value-decode paths fall back to the wire CHOICE branch in that
// case (spec p.85 allows inferred typing).
func paramTypeFromName(name string) int64 {
	switch name {
	case "integer":
		return glow.ParamTypeInteger
	case "real":
		return glow.ParamTypeReal
	case "string":
		return glow.ParamTypeString
	case "boolean":
		return glow.ParamTypeBoolean
	case "trigger":
		return glow.ParamTypeTrigger
	case "enum":
		return glow.ParamTypeEnum
	case "octets":
		return glow.ParamTypeOctets
	}
	return glow.ParamTypeNull
}

// buildMatrixFromMeta rehydrates a glow.Matrix from cached Meta. Reads
// every MatrixContents field the walker's matrixMeta() writes — type,
// mode, targetCount, sourceCount, maximumTotalConnects /
// maximumConnectsPerTarget (nToN only), parametersLocation,
// gainParameterNumber, labels (basePath + description), targets,
// sources, and last-known connections (FreshnessStale until a live
// announce / matrix GetDirectory refreshes).
//
// Spec reference: Ember+ §MatrixContents p.88 + §Label p.89.
func buildMatrixFromMeta(numericPath []int32, identifier string, meta map[string]any) *glow.Matrix {
	m := &glow.Matrix{
		Path:        numericPath,
		Identifier:  identifier,
		Description: metaString(meta, "description"),
	}
	if t := metaString(meta, "type"); t != "" {
		m.MatrixType = matrixTypeFromName(t)
	}
	if a := metaString(meta, "mode"); a != "" {
		m.AddressingMode = matrixAddrFromName(a)
	}
	if v, ok := meta["targetCount"]; ok {
		if n, ok := metaInt64(v); ok {
			m.TargetCount = int32(n)
		}
	}
	if v, ok := meta["sourceCount"]; ok {
		if n, ok := metaInt64(v); ok {
			m.SourceCount = int32(n)
		}
	}
	if v, ok := meta["maximumTotalConnects"]; ok {
		if n, ok := metaInt64(v); ok {
			m.MaxTotalConnects = int32(n)
		}
	}
	if v, ok := meta["maximumConnectsPerTarget"]; ok {
		if n, ok := metaInt64(v); ok {
			m.MaxConnectsPerTarget = int32(n)
		}
	}
	if v, ok := meta["parametersLocation"]; ok {
		switch x := v.(type) {
		case string:
			if parts := parseNumericOID(x); len(parts) > 0 {
				m.ParametersLocation = parts
			}
		case float64:
			m.ParametersLocation = int32(x)
		case int32:
			m.ParametersLocation = x
		case int64:
			m.ParametersLocation = int32(x)
		}
	}
	if v, ok := meta["gainParameterNumber"]; ok {
		if n, ok := metaInt64(v); ok {
			m.GainParameterNumber = int32(n)
		}
	}
	if v, ok := meta["schemaIdentifiers"].(string); ok {
		m.SchemaIdentifiers = v
	}
	if v, ok := meta["labels"]; ok {
		m.Labels = decodeLabels(v)
	}
	if v, ok := meta["targets"]; ok {
		m.Targets = decodeInt32Slice(v)
	}
	if v, ok := meta["sources"]; ok {
		m.Sources = decodeInt32Slice(v)
	}
	if v, ok := meta["connections"]; ok {
		m.Connections = decodeConnections(v)
	}
	return m
}

// matrixTypeFromName is the reverse of matrixTypeName (plugin.go).
// Defaults to MatrixTypeOneToN per the same convention.
func matrixTypeFromName(s string) int64 {
	switch s {
	case "oneToOne":
		return glow.MatrixTypeOneToOne
	case "nToN":
		return glow.MatrixTypeNToN
	}
	return glow.MatrixTypeOneToN
}

// matrixAddrFromName is the reverse of matrixAddrName (plugin.go).
// Defaults to linear per spec convention.
func matrixAddrFromName(s string) int64 {
	if s == "nonLinear" {
		return glow.MatrixAddrNonLinear
	}
	return glow.MatrixAddrLinear
}

// connOpFromName is the reverse of connOpName (plugin.go). Defaults
// to absolute per spec convention.
func connOpFromName(s string) int64 {
	switch s {
	case "connect":
		return glow.ConnOpConnect
	case "disconnect":
		return glow.ConnOpDisconnect
	}
	return glow.ConnOpAbsolute
}

// connDispFromName is the reverse of connDispName (plugin.go). Defaults
// to tally per spec convention.
func connDispFromName(s string) int64 {
	switch s {
	case "modified":
		return glow.ConnDispModified
	case "pending":
		return glow.ConnDispPending
	case "locked":
		return glow.ConnDispLocked
	}
	return glow.ConnDispTally
}

// decodeLabels reads the JSON-shaped labels array back into
// []glow.Label. Each label carries a basePath (RelOID as dot-string) and
// a description.
func decodeLabels(v any) []glow.Label {
	arr, ok := v.([]any)
	if !ok {
		// Live walker shape (in-memory): []map[string]any
		if direct, ok := v.([]map[string]any); ok {
			out := make([]glow.Label, 0, len(direct))
			for _, l := range direct {
				out = append(out, glow.Label{
					BasePath:    parseNumericOID(metaString(l, "basePath")),
					Description: metaString(l, "description"),
				})
			}
			return out
		}
		return nil
	}
	out := make([]glow.Label, 0, len(arr))
	for _, it := range arr {
		l, ok := it.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, glow.Label{
			BasePath:    parseNumericOID(metaString(l, "basePath")),
			Description: metaString(l, "description"),
		})
	}
	return out
}

// decodeInt32Slice handles both the live walker shape ([]int32) and
// the JSON round-trip shape ([]any of float64). Used for Targets and
// Sources arrays.
func decodeInt32Slice(v any) []int32 {
	switch x := v.(type) {
	case []int32:
		return x
	case []any:
		out := make([]int32, 0, len(x))
		for _, it := range x {
			if n, ok := metaInt64(it); ok {
				out = append(out, int32(n))
			}
		}
		return out
	}
	return nil
}

// decodeConnections rebuilds the live connection-tally map into a
// []glow.Connection. Connections from cache are last-known state and
// arrive as FreshnessStale entries on the entry; a live matrix
// GetDirectory or announce refreshes them.
func decodeConnections(v any) []glow.Connection {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make([]glow.Connection, 0, len(m))
	for _, raw := range m {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		conn := glow.Connection{}
		if t, ok := metaInt64(entry["target"]); ok {
			conn.Target = int32(t)
		}
		conn.Sources = decodeInt32Slice(entry["sources"])
		conn.Operation = connOpFromName(metaString(entry, "operation"))
		conn.Disposition = connDispFromName(metaString(entry, "disposition"))
		out = append(out, conn)
	}
	return out
}
