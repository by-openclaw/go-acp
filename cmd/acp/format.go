package main

import (
	"context"
	"fmt"
	"math"
	"strings"

	"acp/internal/protocol"
)

// printSlotTree is the shared render helper used by `walk --slot N` and
// `walk --all`. Moving it out of the runWalk body keeps the --all loop
// readable.
func printSlotTree(slot int, objs []protocol.Object, filter string) {
	fmt.Printf("\nslot %d — %d objects\n\n", slot, len(objs))
	filterLower := strings.ToLower(filter)
	// Group for a readable tree view. We rely on the walker returning
	// objects in (identity, control, status, alarm) order.
	var currentGroup string
	for _, o := range objs {
		if o.Group != currentGroup && filter == "" {
			fmt.Printf("\n[%s]\n", o.Group)
			currentGroup = o.Group
		}
		if o.SubGroupMarker {
			if filter == "" {
				// Render section headers with visual separation and strip the
				// leading-whitespace convention from the label.
				fmt.Printf("\n  ── %s ──\n", strings.TrimSpace(o.Label))
			}
			continue
		}
		// Format the current value captured during walk. For numeric
		// kinds we apply step-based precision so "50.8%" doesn't show
		// up as "50%". For strings/enums/ipaddr the inline formatter
		// is already type-aware.
		valStr := walkValueColumn(o)
		rngStr := walkRangeColumn(o)
		line := fmt.Sprintf("  %3d  %-20s  %-6s  %-3s  %-18s  %s",
			o.ID,
			truncate(o.Label, 20),
			kindName(o.Kind),
			accessStr(o.Access),
			truncate(valStr, 18),
			rngStr)
		if filter != "" && !strings.Contains(strings.ToLower(line), filterLower) {
			continue
		}
		if o.Group != currentGroup {
			fmt.Printf("\n[%s]\n", o.Group)
			currentGroup = o.Group
		}
		fmt.Println(line)
	}
}

// walkValueColumn renders the per-object value column for `acp walk`.
// Uses the formatValue path (which respects step-based float precision
// and applies the object's unit) when an object has usable metadata;
// falls back to the compact inline formatter otherwise.
func walkValueColumn(o protocol.Object) string {
	switch o.Value.Kind {
	case protocol.KindInt:
		return appendUnit(fmt.Sprintf("%d", o.Value.Int), &o)
	case protocol.KindUint:
		return appendUnit(fmt.Sprintf("%d", o.Value.Uint), &o)
	case protocol.KindFloat:
		return appendUnit(fmt.Sprintf("%.*f", decimalsFromStep(&o), o.Value.Float), &o)
	case protocol.KindEnum:
		if o.Value.Str != "" {
			return fmt.Sprintf("%q", o.Value.Str)
		}
		return fmt.Sprintf("idx %d", o.Value.Enum)
	case protocol.KindString:
		return fmt.Sprintf("%q", o.Value.Str)
	case protocol.KindIPAddr:
		return fmt.Sprintf("%d.%d.%d.%d",
			o.Value.IPAddr[0], o.Value.IPAddr[1], o.Value.IPAddr[2], o.Value.IPAddr[3])
	case protocol.KindFrame:
		return formatFrameStatus(o.Value.SlotStatus)
	}
	return ""
}

// walkRangeColumn renders the per-object constraint column for walk.
// For numeric kinds it shows "min..max step unit". For enums it shows
// the item list. For strings it shows the max length. Empty for kinds
// without meaningful constraints (ipaddr, alarm, frame).
func walkRangeColumn(o protocol.Object) string {
	switch o.Kind {
	case protocol.KindInt:
		return fmt.Sprintf("%s..%s step %s%s",
			fmtNumPlain(o.Min), fmtNumPlain(o.Max), fmtNumPlain(o.Step),
			unitSuffix(o.Unit))
	case protocol.KindUint:
		return fmt.Sprintf("%s..%s step %s%s",
			fmtNumPlain(o.Min), fmtNumPlain(o.Max), fmtNumPlain(o.Step),
			unitSuffix(o.Unit))
	case protocol.KindFloat:
		d := decimalsFromStep(&o)
		minf, _ := o.Min.(float64)
		maxf, _ := o.Max.(float64)
		stepf, _ := o.Step.(float64)
		return fmt.Sprintf("%.*f..%.*f step %.*f%s",
			d, minf, d, maxf, d, stepf, unitSuffix(o.Unit))
	case protocol.KindEnum:
		return "[" + strings.Join(o.EnumItems, ", ") + "]"
	case protocol.KindString:
		if o.MaxLen > 0 {
			return fmt.Sprintf("max %d chars", o.MaxLen)
		}
		return ""
	case protocol.KindAlarm:
		return fmt.Sprintf("tag 0x%02X", o.AlarmTag)
	}
	return ""
}

// fmtNumPlain prints Min/Max/Step/Def values in their native Go type
// without decimals or unit suffix — used in the narrow range column.
func fmtNumPlain(v any) string {
	switch n := v.(type) {
	case int64:
		return fmt.Sprintf("%d", n)
	case uint64:
		return fmt.Sprintf("%d", n)
	case float64:
		return fmt.Sprintf("%g", n)
	case nil:
		return "-"
	default:
		return fmt.Sprintf("%v", n)
	}
}

// unitSuffix returns " unit" (leading space) for non-empty units so we
// can concatenate with a number without worrying about a bare trailing
// space when the unit is missing.
func unitSuffix(u string) string {
	if u == "" {
		return ""
	}
	if u == "%" {
		return "%"
	}
	return " " + u
}

// kindName returns a short, human-readable label for a ValueKind.
func kindName(k protocol.ValueKind) string {
	switch k {
	case protocol.KindBool:
		return "bool"
	case protocol.KindInt:
		return "int"
	case protocol.KindUint:
		return "uint"
	case protocol.KindFloat:
		return "float"
	case protocol.KindEnum:
		return "enum"
	case protocol.KindString:
		return "string"
	case protocol.KindIPAddr:
		return "ipaddr"
	case protocol.KindAlarm:
		return "alarm"
	case protocol.KindFrame:
		return "frame"
	case protocol.KindRaw:
		return "raw"
	default:
		return "?"
	}
}

// accessStr renders the ACP1 access bitmask as the familiar R/W/D triplet.
// Bit 0 = read, bit 1 = write, bit 2 = setDefault. A dash in a slot means
// the capability is absent.
func accessStr(a uint8) string {
	r := "-"
	if a&0x01 != 0 {
		r = "R"
	}
	w := "-"
	if a&0x02 != 0 {
		w = "W"
	}
	d := "-"
	if a&0x04 != 0 {
		d = "D"
	}
	return r + w + d
}

// formatValue renders a typed protocol.Value for human consumption.
// When obj is non-nil it uses the object's Unit and (for floats) its
// Step to pick a sensible decimal precision. When obj is nil it falls
// back to compact %g formatting with no unit suffix.
func formatValue(v protocol.Value, obj *protocol.Object) string {
	switch v.Kind {
	case protocol.KindInt:
		return "value = " + appendUnit(fmt.Sprintf("%d", v.Int), obj)
	case protocol.KindUint:
		return "value = " + appendUnit(fmt.Sprintf("%d", v.Uint), obj)
	case protocol.KindFloat:
		dec := decimalsFromStep(obj)
		return "value = " + appendUnit(fmt.Sprintf("%.*f", dec, v.Float), obj)
	case protocol.KindEnum:
		if v.Str != "" {
			return fmt.Sprintf("value = %q  (enum idx %d)", v.Str, v.Enum)
		}
		return fmt.Sprintf("value = idx %d  (enum)", v.Enum)
	case protocol.KindString:
		return fmt.Sprintf("value = %q", v.Str)
	case protocol.KindIPAddr:
		return fmt.Sprintf("value = %d.%d.%d.%d",
			v.IPAddr[0], v.IPAddr[1], v.IPAddr[2], v.IPAddr[3])
	case protocol.KindFrame:
		return "value = " + formatFrameStatus(v.SlotStatus)
	case protocol.KindRaw:
		return fmt.Sprintf("value = (raw, %d bytes)", len(v.Raw))
	default:
		return fmt.Sprintf("value = ?  (kind %d)", v.Kind)
	}
}

// formatValueInline is a compact value renderer for the watch output.
// Loses the unit (we don't have the Object here) but still typed.
func formatValueInline(v protocol.Value) string {
	switch v.Kind {
	case protocol.KindInt:
		return fmt.Sprintf("%d", v.Int)
	case protocol.KindUint:
		return fmt.Sprintf("%d", v.Uint)
	case protocol.KindFloat:
		return fmt.Sprintf("%.2f", v.Float)
	case protocol.KindEnum:
		if v.Str != "" {
			return fmt.Sprintf("%q (idx %d)", v.Str, v.Enum)
		}
		return fmt.Sprintf("idx %d", v.Enum)
	case protocol.KindString:
		return fmt.Sprintf("%q", v.Str)
	case protocol.KindIPAddr:
		return fmt.Sprintf("%d.%d.%d.%d", v.IPAddr[0], v.IPAddr[1], v.IPAddr[2], v.IPAddr[3])
	case protocol.KindFrame:
		return formatFrameStatus(v.SlotStatus)
	case protocol.KindRaw:
		return fmt.Sprintf("raw(%d)", len(v.Raw))
	default:
		return "?"
	}
}

// formatFrameStatus renders a slot-status slice compactly: each slot
// becomes one letter so the full 31-slot state of a rack fits on a
// single terminal line. Legend is printed alongside so the symbols are
// self-explanatory.
//
//	.  no card       0
//	U  power-up      1
//	P  present       2
//	E  error         3
//	R  removed       4
//	B  boot mode     5
//	?  unknown       (other)
func formatFrameStatus(statuses []protocol.SlotStatus) string {
	if len(statuses) == 0 {
		return "frame: (empty)"
	}
	var b strings.Builder
	b.WriteString("frame: ")
	for _, s := range statuses {
		b.WriteByte(slotStatusChar(s))
	}
	// Also surface any non-empty slots with their names, so you see
	// "slot 1=boot, slot 10=present" without having to decode the
	// symbol strip by eye.
	first := true
	b.WriteString("  [")
	for i, s := range statuses {
		if s == protocol.SlotNoCard {
			continue
		}
		if !first {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%d=%s", i, s)
		first = false
	}
	if first {
		// no non-empty slots at all
		b.WriteString("empty")
	}
	b.WriteByte(']')
	return b.String()
}

func slotStatusChar(s protocol.SlotStatus) byte {
	switch s {
	case protocol.SlotNoCard:
		return '.'
	case protocol.SlotPowerUp:
		return 'U'
	case protocol.SlotPresent:
		return 'P'
	case protocol.SlotError:
		return 'E'
	case protocol.SlotRemoved:
		return 'R'
	case protocol.SlotBootMode:
		return 'B'
	default:
		return '?'
	}
}

// appendUnit attaches the object's Unit string to a formatted number.
// Convention:
//   - "%"  — no space before the unit ("50%")
//   - other units — single space ("-2.37 dB", "100 ms")
//   - empty — no unit appended
func appendUnit(num string, obj *protocol.Object) string {
	if obj == nil || obj.Unit == "" {
		return num
	}
	if obj.Unit == "%" {
		return num + "%"
	}
	return num + " " + obj.Unit
}

// fmtNum renders any of the numeric constraint fields (Min/Max/Step/Def)
// as a display string with the object's unit appended when `withUnit`
// is true. Falls back to %v on unexpected types.
func fmtNum(v any, obj *protocol.Object, withUnit bool) string {
	var s string
	switch n := v.(type) {
	case int64:
		s = fmt.Sprintf("%d", n)
	case uint64:
		s = fmt.Sprintf("%d", n)
	case float64:
		s = fmt.Sprintf("%.*f", decimalsFromStep(obj), n)
	case nil:
		return "-"
	default:
		s = fmt.Sprintf("%v", n)
	}
	if withUnit {
		return appendUnit(s, obj)
	}
	return s
}

// printObjectMeta prints everything the walker captured about an object:
// kind, access, and whichever constraint fields are relevant to its kind.
// Every numeric type gets range/step/default/unit; enums get their item
// list; strings get max length; alarms get priority/tag/messages; ipaddr
// gets default and optionally the declared range (though most devices
// leave it as 0.0.0.0..255.255.255.255 which we hide to avoid noise).
func printObjectMeta(o protocol.Object) {
	fmt.Printf("kind = %s  access = %s\n", kindName(o.Kind), accessStr(o.Access))

	switch o.Kind {
	case protocol.KindInt:
		fmt.Printf("range = %s .. %s  step = %s  default = %s\n",
			fmtNum(o.Min, &o, false),
			fmtNum(o.Max, &o, false),
			fmtNum(o.Step, &o, false),
			fmtNum(o.Def, &o, true),
		)

	case protocol.KindUint:
		fmt.Printf("range = %s .. %s  step = %s  default = %s\n",
			fmtNum(o.Min, &o, false),
			fmtNum(o.Max, &o, false),
			fmtNum(o.Step, &o, false),
			fmtNum(o.Def, &o, true),
		)

	case protocol.KindFloat:
		fmt.Printf("range = %s .. %s  step = %s  default = %s\n",
			fmtNum(o.Min, &o, false),
			fmtNum(o.Max, &o, false),
			fmtNum(o.Step, &o, false),
			fmtNum(o.Def, &o, true),
		)

	case protocol.KindEnum:
		if o.Def != nil {
			switch d := o.Def.(type) {
			case string:
				fmt.Printf("items = [%s]  (default %q)\n",
					strings.Join(o.EnumItems, ", "), d)
			default:
				fmt.Printf("items = [%s]  (default idx %v)\n",
					strings.Join(o.EnumItems, ", "), o.Def)
			}
		} else {
			fmt.Printf("items = [%s]\n",
				strings.Join(o.EnumItems, ", "))
		}

	case protocol.KindString:
		fmt.Printf("max length = %d chars\n", o.MaxLen)

	case protocol.KindIPAddr:
		if d, ok := o.Def.(uint64); ok {
			fmt.Printf("default = %d.%d.%d.%d\n",
				byte(d>>24), byte(d>>16), byte(d>>8), byte(d))
		}

	case protocol.KindAlarm:
		fmt.Printf("priority = %d  tag = 0x%02X\n", o.AlarmPriority, o.AlarmTag)
		if o.AlarmOnMsg != "" {
			fmt.Printf("event on  = %q\n", o.AlarmOnMsg)
		}
		if o.AlarmOffMsg != "" {
			fmt.Printf("event off = %q\n", o.AlarmOffMsg)
		}

	case protocol.KindFrame:
		fmt.Println("frame status — use `acp info` for slot list")
	}
}

// decimalsFromStep picks a display precision for a float based on the
// object's declared Step. Examples:
//
//	step = 1     → 1 decimal  ("50.8 %")   — minimum 1 for floats
//	step = 0.1   → 1 decimal  ("50.8 %")
//	step = 0.01  → 2 decimals ("-2.37 dB")
//	step = 0.001 → 3 decimals
//
// Minimum is 1 — a "whole" number stored in a float field can still
// carry fractional parts (e.g. the emulator stored 50.8 despite
// declaring step=1). Dropping fractions on display would hide truth.
// Falls back to 2 decimals when no metadata is available.
func decimalsFromStep(obj *protocol.Object) int {
	if obj == nil {
		return 2
	}
	step, ok := obj.Step.(float64)
	if !ok || step <= 0 {
		return 2
	}
	if step >= 1 {
		return 1
	}
	d := -int(math.Floor(math.Log10(step)))
	if d < 1 {
		return 1
	}
	if d > 6 {
		return 6
	}
	return d
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// findObjectByLabel peeks into the plugin's cached walker tree for the
// Object matching (slot, group, label). The plugin interface doesn't
// expose a "get metadata" method yet — we round-trip through a second
// Walk-less addressing pass by reusing the Plugin's internal resolve via
// a small helper. For now, we just walk again here — the walker caches
// per slot so the second call is a no-op lookup, not a re-traversal.
//
// This function is cmd-only glue; it does not belong in the library.
func findObjectByLabel(plug protocol.Protocol, slot int, group, label string) *protocol.Object {
	// Walk is idempotent and cached per slot inside the plugin, so this
	// returns the already-walked list without re-hitting the device.
	objs, err := plug.Walk(context.Background(), slot)
	if err != nil {
		return nil
	}
	for i := range objs {
		if objs[i].Label != label {
			continue
		}
		if group != "" && objs[i].Group != group {
			continue
		}
		return &objs[i]
	}
	return nil
}

// matchPathPrefix returns true if the object's Path contains the given
// prefix segments. The match skips ROOT_NODE_V2 (path[0] for ACP2).
// An empty prefix matches everything.
//
// Examples:
//
//	matchPathPrefix(["ROOT_NODE_V2","BOARD","Card Name"], ["BOARD"])       → true  (--path BOARD)
//	matchPathPrefix(["ROOT_NODE_V2","PSU","1","Present"], ["PSU","1"])     → true  (--path PSU.1)
//	matchPathPrefix(["ROOT_NODE_V2","PSU","1","Present"], ["BOARD"])       → false
//	matchPathPrefix(["identity"], ["identity"])                            → true  (ACP1)
//	matchPathPrefix(["router","oneToN","parameters"], ["router","oneToN"]) → true  (Ember+, --path router.oneToN)
func matchPathPrefix(objPath, prefix []string) bool {
	if len(prefix) == 0 {
		return true
	}
	// For ACP2: skip ROOT_NODE_V2 (path[0]) when matching.
	// For ACP1: path has 1 element (group name), match directly.
	p := objPath
	if len(p) > 1 && strings.EqualFold(p[0], "ROOT_NODE_V2") {
		p = p[1:]
	}
	if len(p) < len(prefix) {
		return false
	}
	for i, seg := range prefix {
		if !strings.EqualFold(p[i], seg) {
			return false
		}
	}
	return true
}

// filterByPath returns only objects whose path matches the given prefix.
func filterByPath(objs []protocol.Object, prefix []string) []protocol.Object {
	if len(prefix) == 0 {
		return objs
	}
	var out []protocol.Object
	for _, o := range objs {
		if matchPathPrefix(o.Path, prefix) {
			out = append(out, o)
		}
	}
	return out
}
