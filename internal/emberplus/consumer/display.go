package emberplus

import (
	"strings"

	"dhs/internal/protocol"
)

// extractFormatUnit returns the unit suffix from an Ember+ printf-style
// format string per spec p.85 Contents [6] — anything after the first
// '°' marker, trimmed of surrounding whitespace. Empty when the format
// has no '°' (no unit declared) or is empty.
//
// Examples:
//
//	"%0.1f°dB"      → "dB"
//	"%d°%"          → "%"
//	"%d°\nunits"    → "units" (smh emulator convention)
//	"%d"            → ""    (no marker)
//	""              → ""
func extractFormatUnit(format string) string {
	i := strings.Index(format, "°")
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(format[i+len("°"):])
}

// applyFactor returns Value scaled by 1 / factor when factor > 1 and
// the value is numeric, preserving the original Value otherwise. The
// scaled result is always KindFloat — Ember+ factor convention
// (spec p.85 Contents [8]) is "raw integer × (1 / factor) = engineering
// value", which is intrinsically a real-number transform.
//
// factor <= 1 (or non-numeric Value) → Value returned unchanged.
func applyFactor(v protocol.Value, factor int64) protocol.Value {
	if factor <= 1 {
		return v
	}
	switch v.Kind {
	case protocol.KindInt:
		return protocol.Value{
			Kind:  protocol.KindFloat,
			Float: float64(v.Int) / float64(factor),
		}
	case protocol.KindFloat:
		return protocol.Value{
			Kind:  protocol.KindFloat,
			Float: v.Float / float64(factor),
		}
	default:
		return v
	}
}

// displayValueAndUnit produces the (Value, Unit) pair the watch event
// should carry for a Parameter entry, applying:
//
//   - factor scaling (raw integer ÷ factor) when factor > 1, per
//     spec p.85 Contents [8].
//   - unit extraction from the printf-style format suffix, per
//     spec p.85 Contents [6].
//
// Falls back to the entry's stored obj.Value / obj.Unit when no
// glowParam is attached (e.g. before processParameter has merged
// the first announce).
func displayValueAndUnit(entry *treeEntry) (protocol.Value, string) {
	if entry == nil {
		return protocol.Value{}, ""
	}
	val := entry.obj.Value
	unit := entry.obj.Unit
	if p := entry.glowParam; p != nil {
		val = applyFactor(val, p.Factor)
		if u := extractFormatUnit(p.Format); u != "" {
			unit = u
		}
	}
	return val, unit
}

