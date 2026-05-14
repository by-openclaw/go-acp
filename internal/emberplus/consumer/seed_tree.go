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
		m := &glow.Matrix{
			Path:        numericPath,
			Identifier:  identifier,
			Description: metaString(o.Meta, "description"),
		}
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
