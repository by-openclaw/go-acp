package manifest

import (
	"strconv"

	"dhs/internal/export/canonical"
)

// elementKind returns the Ember+ element discriminator stored in
// Meta["element"] by the walker — "node", "parameter", "matrix",
// "function", "template" — or "" when the key is missing. Lets
// buildSlotNode dispatch on Glow element type without re-decoding wire
// bytes (refs #438, ADR-0022).
func elementKind(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	s, _ := meta["element"].(string)
	return s
}

// buildCanonicalMatrix converts a cached Ember+ matrix Object (whose
// Meta carries the full MatrixContents schema per p.88 — see
// internal/emberplus/consumer/plugin.go matrixMeta) into a
// canonical.Matrix that the Ember+ provider's tree builder will encode
// back onto the wire identically.
//
// Reads Meta["type" / "mode" / "targetCount" / "sourceCount" /
// "maximumTotalConnects" / "maximumConnectsPerTarget" /
// "parametersLocation" / "gainParameterNumber" / "labels[]" /
// "targets[]" / "sources[]" / "connections" / "targetLabels" /
// "sourceLabels"]. Missing optional keys map to nil/zero per spec.
func buildCanonicalMatrix(o dmObject, oid, pathDot string) *canonical.Matrix {
	m := &canonical.Matrix{
		Header: canonical.Header{
			Number:     o.ID,
			Identifier: o.Label,
			Path:       pathDot,
			OID:        oid,
			IsOnline:   true,
			Access:     accessString(o.Access),
		},
		Type: metaStringOr(o.Meta, "type", canonical.MatrixOneToN),
		Mode: metaStringOr(o.Meta, "mode", canonical.ModeLinear),
	}
	if n, ok := metaInt64(o.Meta["targetCount"]); ok {
		m.TargetCount = n
	}
	if n, ok := metaInt64(o.Meta["sourceCount"]); ok {
		m.SourceCount = n
	}
	if n, ok := metaInt64(o.Meta["maximumTotalConnects"]); ok {
		v := n
		m.MaximumTotalConnects = &v
	}
	if n, ok := metaInt64(o.Meta["maximumConnectsPerTarget"]); ok {
		v := n
		m.MaximumConnectsPerTarget = &v
	}
	if s, ok := o.Meta["parametersLocation"].(string); ok && s != "" {
		v := s
		m.ParametersLocation = &v
	}
	if n, ok := metaInt64(o.Meta["gainParameterNumber"]); ok {
		v := n
		m.GainParameterNumber = &v
	}
	m.Labels = metaToMatrixLabels(o.Meta["labels"])
	m.Targets = metaToMatrixTargets(o.Meta["targets"])
	m.Sources = metaToMatrixSources(o.Meta["sources"])
	m.Connections = metaToMatrixConnections(o.Meta["connections"])
	m.TargetLabels = metaToLevelMap(o.Meta["targetLabels"])
	m.SourceLabels = metaToLevelMap(o.Meta["sourceLabels"])
	return m
}

// buildCanonicalFunction converts a cached Ember+ function Object
// (Meta["arguments"] / ["result"] tuples per spec p.91) into a
// canonical.Function. TupleItem.Type is preserved as the canonical
// type-name string.
func buildCanonicalFunction(o dmObject, oid, pathDot string) *canonical.Function {
	f := &canonical.Function{
		Header: canonical.Header{
			Number:     o.ID,
			Identifier: o.Label,
			Path:       pathDot,
			OID:        oid,
			IsOnline:   true,
			Access:     accessString(o.Access),
		},
		Arguments: metaToTuple(o.Meta["arguments"]),
		Result:    metaToTuple(o.Meta["result"]),
	}
	return f
}

// metaStringOr returns the string at key or fallback when missing.
func metaStringOr(m map[string]any, key, fallback string) string {
	if s, ok := m[key].(string); ok && s != "" {
		return s
	}
	return fallback
}

// metaInt64 coerces a JSON-decoded numeric back to int64. Handles
// float64 (the encoding/json default for numbers) plus the typed
// in-memory shapes the live walker stashes.
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

func metaToMatrixLabels(v any) []canonical.MatrixLabel {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]canonical.MatrixLabel, 0, len(arr))
	for _, it := range arr {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		bp, _ := m["basePath"].(string)
		desc, _ := m["description"].(string)
		lbl := canonical.MatrixLabel{BasePath: bp}
		if desc != "" {
			d := desc
			lbl.Description = &d
		}
		out = append(out, lbl)
	}
	return out
}

func metaToMatrixTargets(v any) []canonical.MatrixTarget {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]canonical.MatrixTarget, 0, len(arr))
	for _, it := range arr {
		if n, ok := metaInt64(it); ok {
			out = append(out, canonical.MatrixTarget{Number: n})
		}
	}
	return out
}

func metaToMatrixSources(v any) []canonical.MatrixSource {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]canonical.MatrixSource, 0, len(arr))
	for _, it := range arr {
		if n, ok := metaInt64(it); ok {
			out = append(out, canonical.MatrixSource{Number: n})
		}
	}
	return out
}

func metaToMatrixConnections(v any) []canonical.MatrixConnection {
	mp, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make([]canonical.MatrixConnection, 0, len(mp))
	for _, raw := range mp {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		var srcs []int64
		if arr, ok := entry["sources"].([]any); ok {
			for _, s := range arr {
				if n, ok := metaInt64(s); ok {
					srcs = append(srcs, n)
				}
			}
		}
		t, _ := metaInt64(entry["target"])
		op, _ := entry["operation"].(string)
		disp, _ := entry["disposition"].(string)
		if op == "" {
			op = canonical.ConnOpAbsolute
		}
		if disp == "" {
			disp = canonical.ConnDispTally
		}
		out = append(out, canonical.MatrixConnection{
			Target:      t,
			Sources:     srcs,
			Operation:   op,
			Disposition: disp,
			Locked:      disp == canonical.ConnDispLocked,
		})
	}
	return out
}

// metaToLevelMap reads the inline-resolved label maps written by
// internal/emberplus/consumer/walk_enrich.enrichMatrixLabels —
// {levelDesc: {indexAsString: labelString}}.
func metaToLevelMap(v any) map[string]map[string]string {
	mp, ok := v.(map[string]any)
	if !ok {
		// Live walker shape (already typed):
		if direct, ok := v.(map[string]map[string]string); ok {
			return direct
		}
		return nil
	}
	out := make(map[string]map[string]string, len(mp))
	for level, raw := range mp {
		inner, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		row := make(map[string]string, len(inner))
		for idx, val := range inner {
			if s, ok := val.(string); ok {
				row[idx] = s
			}
		}
		if len(row) > 0 {
			out[level] = row
		}
	}
	return out
}

func metaToTuple(v any) []canonical.TupleItem {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]canonical.TupleItem, 0, len(arr))
	for _, it := range arr {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		typ, _ := m["type"].(string)
		out = append(out, canonical.TupleItem{
			Name: name,
			Type: typ,
		})
	}
	return out
}

// _ keeps strconv referenced if a future field needs it; declarations
// above don't use it directly today.
var _ = strconv.Itoa
