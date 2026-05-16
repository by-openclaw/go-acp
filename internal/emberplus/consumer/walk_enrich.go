package emberplus

import (
	"strings"

	"dhs/internal/consumer"
)

// enrichMatrixLabels resolves each matrix's basePath pointer into an
// inline targetLabels / sourceLabels map and writes the result into the
// matrix's Meta. After this step the DM file is self-contained: a
// consumer / UI / cache reader can render the crosspoint grid without
// hopping out to the matrix's labels-Node descendants.
//
// Spec convention §5.1.1 / §5.1.2: Matrix.Labels[i].basePath points to
// a Node whose two children — number=1 (targets) and number=2 (sources)
// — each hold per-signal Parameter children. Each Parameter's wire
// `number` is the target/source index; its string value is the human
// label.
//
// Output shape mirrors provider-side canonical.Matrix.TargetLabels /
// SourceLabels: map[level]map[indexAsString]labelString. Keyed by
// labels[i].description (e.g. "Primary"). Falls back to the basePath
// string when description is empty.
//
// Idempotent: re-running on already-enriched objects leaves them
// unchanged. Cache files written then loaded then re-walked then
// re-cached keep the same shape.
func enrichMatrixLabels(objs []consumer.Object) []consumer.Object {
	byOID := make(map[string]int, len(objs))
	for i, o := range objs {
		if o.OID != "" {
			byOID[o.OID] = i
		}
	}
	for i := range objs {
		o := &objs[i]
		if o.Meta == nil {
			continue
		}
		if elem, _ := o.Meta["element"].(string); elem != "matrix" {
			continue
		}
		labelsRaw, ok := o.Meta["labels"]
		if !ok {
			continue
		}
		labels := normaliseLabelsField(labelsRaw)
		if len(labels) == 0 {
			continue
		}

		targetLabels := make(map[string]map[string]string)
		sourceLabels := make(map[string]map[string]string)

		for _, lbl := range labels {
			basePath := lbl.basePath
			if basePath == "" {
				continue
			}
			key := lbl.description
			if key == "" {
				key = basePath
			}
			tgts := extractLabelValues(objs, basePath+".1")
			srcs := extractLabelValues(objs, basePath+".2")
			if len(tgts) > 0 {
				targetLabels[key] = tgts
			}
			if len(srcs) > 0 {
				sourceLabels[key] = srcs
			}
		}

		if len(targetLabels) == 0 && len(sourceLabels) == 0 {
			continue
		}
		// Deep-copy Meta to avoid mutating the shared treeEntry map.
		newMeta := make(map[string]any, len(o.Meta)+2)
		for k, v := range o.Meta {
			newMeta[k] = v
		}
		if len(targetLabels) > 0 {
			newMeta["targetLabels"] = targetLabels
		}
		if len(sourceLabels) > 0 {
			newMeta["sourceLabels"] = sourceLabels
		}
		o.Meta = newMeta
	}
	return objs
}

// labelEntry is the normalised view of one matrix label level —
// independent of whether the source map[string]any came from live
// Go ([]map[string]any) or JSON round-trip ([]any of map[string]any).
type labelEntry struct {
	basePath    string
	description string
}

func normaliseLabelsField(v any) []labelEntry {
	switch x := v.(type) {
	case []map[string]any:
		out := make([]labelEntry, 0, len(x))
		for _, m := range x {
			out = append(out, labelEntry{
				basePath:    metaString(m, "basePath"),
				description: metaString(m, "description"),
			})
		}
		return out
	case []any:
		out := make([]labelEntry, 0, len(x))
		for _, item := range x {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			out = append(out, labelEntry{
				basePath:    metaString(m, "basePath"),
				description: metaString(m, "description"),
			})
		}
		return out
	}
	return nil
}

// extractLabelValues returns {indexAsString: labelString} for every
// direct child of the containerOID. Direct child = OID is exactly
// "<containerOID>.<segment>" with no further dots.
func extractLabelValues(objs []consumer.Object, containerOID string) map[string]string {
	prefix := containerOID + "."
	out := make(map[string]string)
	for _, o := range objs {
		if !strings.HasPrefix(o.OID, prefix) {
			continue
		}
		remainder := o.OID[len(prefix):]
		if strings.Contains(remainder, ".") {
			continue
		}
		if o.Value.Kind != consumer.KindString {
			continue
		}
		if o.Value.Str == "" {
			continue
		}
		out[remainder] = o.Value.Str
	}
	return out
}
