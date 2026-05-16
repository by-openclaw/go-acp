package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"dhs/internal/consumer"
)

// sectionHeader returns the container-path string under which an
// object should be grouped. For single-segment paths (ACP1's flat
// groups) that's the group name; for deep paths (Ember+) it's the
// parent path joined with `.`. Nodes with no path fall back to Group.
func sectionHeader(o consumer.Object) string {
	if len(o.Path) > 1 {
		return strings.Join(o.Path[:len(o.Path)-1], ".")
	}
	if len(o.Path) == 1 {
		return o.Path[0]
	}
	return o.Group
}

// printSlotTree is the shared render helper used by `walk --slot N` and
// `walk --all`. ACP1's flat groups stay flat; Ember+'s deep trees are
// rendered as a readable tree with siblings grouped under their parent
// path and depth-based indentation.
func printSlotTree(slot int, objs []consumer.Object, filter string) {
	fmt.Printf("\nslot %d — %d objects\n\n", slot, len(objs))
	filterLower := strings.ToLower(filter)

	sorted := make([]consumer.Object, len(objs))
	copy(sorted, objs)
	sort.SliceStable(sorted, func(i, j int) bool {
		return strings.Join(sorted[i].Path, ".") < strings.Join(sorted[j].Path, ".")
	})

	var currentSection string
	for _, o := range sorted {
		if o.SubGroupMarker {
			if filter == "" {
				fmt.Printf("\n  ── %s ──\n", strings.TrimSpace(o.Label))
			}
			continue
		}
		valStr := walkValueColumn(o)
		rngStr := walkRangeColumn(o)
		depth := len(o.Path)
		if depth < 1 {
			depth = 1
		}
		indent := strings.Repeat("  ", depth-1)
		line := fmt.Sprintf("%s  %3d  %-20s  %-6s  %-3s  %-18s  %s",
			indent,
			o.ID,
			truncate(o.Label, 20),
			kindName(o.Kind),
			accessStr(o.Access),
			truncate(valStr, 18),
			rngStr)
		if filter != "" && !strings.Contains(strings.ToLower(line), filterLower) {
			continue
		}
		section := sectionHeader(o)
		if section != currentSection {
			if filter == "" {
				fmt.Printf("\n[%s]\n", section)
			}
			currentSection = section
		}
		fmt.Println(line)
	}
}

// walkValueColumn renders the per-object value column for `acp walk`.
// Uses the formatValue path (which respects step-based float precision
// and applies the object's unit) when an object has usable metadata;
// falls back to the compact inline formatter otherwise.
func walkValueColumn(o consumer.Object) string {
	switch o.Value.Kind {
	case consumer.KindInt:
		return appendUnit(fmt.Sprintf("%d", o.Value.Int), &o)
	case consumer.KindUint:
		return appendUnit(fmt.Sprintf("%d", o.Value.Uint), &o)
	case consumer.KindFloat:
		return appendUnit(fmt.Sprintf("%.*f", decimalsFromStep(&o), o.Value.Float), &o)
	case consumer.KindEnum:
		if o.Value.Str != "" {
			return fmt.Sprintf("%q", o.Value.Str)
		}
		return fmt.Sprintf("idx %d", o.Value.Enum)
	case consumer.KindString:
		return fmt.Sprintf("%q", o.Value.Str)
	case consumer.KindIPAddr:
		return fmt.Sprintf("%d.%d.%d.%d",
			o.Value.IPAddr[0], o.Value.IPAddr[1], o.Value.IPAddr[2], o.Value.IPAddr[3])
	case consumer.KindFrame:
		return formatFrameStatus(o.Value.SlotStatus)
	}
	return ""
}

// walkRangeColumn renders the per-object constraint column for walk.
// For numeric kinds it shows "min..max step unit". For enums it shows
// the item list. For strings it shows the max length. Empty for kinds
// without meaningful constraints (ipaddr, alarm, frame).
func walkRangeColumn(o consumer.Object) string {
	switch o.Kind {
	case consumer.KindInt:
		return fmt.Sprintf("%s..%s step %s%s",
			fmtNumPlain(o.Min), fmtNumPlain(o.Max), fmtNumPlain(o.Step),
			unitSuffix(o.Unit))
	case consumer.KindUint:
		return fmt.Sprintf("%s..%s step %s%s",
			fmtNumPlain(o.Min), fmtNumPlain(o.Max), fmtNumPlain(o.Step),
			unitSuffix(o.Unit))
	case consumer.KindFloat:
		d := decimalsFromStep(&o)
		minf, _ := o.Min.(float64)
		maxf, _ := o.Max.(float64)
		stepf, _ := o.Step.(float64)
		return fmt.Sprintf("%.*f..%.*f step %.*f%s",
			d, minf, d, maxf, d, stepf, unitSuffix(o.Unit))
	case consumer.KindEnum:
		return "[" + strings.Join(o.EnumItems, ", ") + "]"
	case consumer.KindString:
		if o.MaxLen > 0 {
			return fmt.Sprintf("max %d chars", o.MaxLen)
		}
		return ""
	case consumer.KindAlarm:
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
func kindName(k consumer.ValueKind) string {
	switch k {
	case consumer.KindBool:
		return "bool"
	case consumer.KindInt:
		return "int"
	case consumer.KindUint:
		return "uint"
	case consumer.KindFloat:
		return "float"
	case consumer.KindEnum:
		return "enum"
	case consumer.KindString:
		return "string"
	case consumer.KindIPAddr:
		return "ipaddr"
	case consumer.KindAlarm:
		return "alarm"
	case consumer.KindFrame:
		return "frame"
	case consumer.KindRaw:
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

// formatValue renders a typed consumer.Value for human consumption.
// When obj is non-nil it uses the object's Unit and (for floats) its
// Step to pick a sensible decimal precision. When obj is nil it falls
// back to compact %g formatting with no unit suffix.
func formatValue(v consumer.Value, obj *consumer.Object) string {
	switch v.Kind {
	case consumer.KindBool:
		// Previously fell through to the default "value = ?  (kind 1)"
		// branch — broke `get` / `set` confirm output on every bool
		// Parameter (mute, vBoolean, Ember+ Parameter.type=boolean).
		// formatValueInline already had the bool case; this one was
		// just missing.
		if v.Bool {
			return "value = true"
		}
		return "value = false"
	case consumer.KindInt:
		return "value = " + appendUnit(fmt.Sprintf("%d", v.Int), obj)
	case consumer.KindUint:
		return "value = " + appendUnit(fmt.Sprintf("%d", v.Uint), obj)
	case consumer.KindFloat:
		dec := decimalsFromStep(obj)
		return "value = " + appendUnit(fmt.Sprintf("%.*f", dec, v.Float), obj)
	case consumer.KindEnum:
		if v.Str != "" {
			return fmt.Sprintf("value = %q  (enum idx %d)", v.Str, v.Enum)
		}
		return fmt.Sprintf("value = idx %d  (enum)", v.Enum)
	case consumer.KindString:
		return fmt.Sprintf("value = %q", v.Str)
	case consumer.KindIPAddr:
		return fmt.Sprintf("value = %d.%d.%d.%d",
			v.IPAddr[0], v.IPAddr[1], v.IPAddr[2], v.IPAddr[3])
	case consumer.KindFrame:
		return "value = " + formatFrameStatus(v.SlotStatus)
	case consumer.KindRaw:
		return fmt.Sprintf("value = (raw, %d bytes)", len(v.Raw))
	default:
		return fmt.Sprintf("value = ?  (kind %d)", v.Kind)
	}
}

// formatValueInline is a compact value renderer for the watch output.
// Loses the unit (we don't have the Object here) but still typed.
// formatChanges renders a compact "name old→new" list for the watch
// output's changed: tag. Comma-separated, stable order (preserving
// the order the plugin emitted).
func formatChanges(cs []consumer.FieldChange) string {
	if len(cs) == 0 {
		return ""
	}
	parts := make([]string, len(cs))
	for i, c := range cs {
		parts[i] = fmt.Sprintf("%s %s→%s", c.Name, c.Old, c.New)
	}
	return strings.Join(parts, ", ")
}

func formatValueInline(v consumer.Value) string {
	switch v.Kind {
	case consumer.KindBool:
		if v.Bool {
			return "true"
		}
		return "false"
	case consumer.KindInt:
		return fmt.Sprintf("%d", v.Int)
	case consumer.KindUint:
		return fmt.Sprintf("%d", v.Uint)
	case consumer.KindFloat:
		return fmt.Sprintf("%.2f", v.Float)
	case consumer.KindEnum:
		if v.Str != "" {
			return fmt.Sprintf("%q (idx %d)", v.Str, v.Enum)
		}
		return fmt.Sprintf("idx %d", v.Enum)
	case consumer.KindString:
		return fmt.Sprintf("%q", v.Str)
	case consumer.KindIPAddr:
		return fmt.Sprintf("%d.%d.%d.%d", v.IPAddr[0], v.IPAddr[1], v.IPAddr[2], v.IPAddr[3])
	case consumer.KindFrame:
		return formatFrameStatus(v.SlotStatus)
	case consumer.KindRaw:
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
func formatFrameStatus(statuses []consumer.SlotStatus) string {
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
		if s == consumer.SlotNoCard {
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

func slotStatusChar(s consumer.SlotStatus) byte {
	switch s {
	case consumer.SlotNoCard:
		return '.'
	case consumer.SlotPowerUp:
		return 'U'
	case consumer.SlotPresent:
		return 'P'
	case consumer.SlotError:
		return 'E'
	case consumer.SlotRemoved:
		return 'R'
	case consumer.SlotBootMode:
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
func appendUnit(num string, obj *consumer.Object) string {
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
func fmtNum(v any, obj *consumer.Object, withUnit bool) string {
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
func printObjectMeta(o consumer.Object) {
	fmt.Printf("kind = %s  access = %s\n", kindName(o.Kind), accessStr(o.Access))

	switch o.Kind {
	case consumer.KindInt:
		fmt.Printf("range = %s .. %s  step = %s  default = %s\n",
			fmtNum(o.Min, &o, false),
			fmtNum(o.Max, &o, false),
			fmtNum(o.Step, &o, false),
			fmtNum(o.Def, &o, true),
		)

	case consumer.KindUint:
		fmt.Printf("range = %s .. %s  step = %s  default = %s\n",
			fmtNum(o.Min, &o, false),
			fmtNum(o.Max, &o, false),
			fmtNum(o.Step, &o, false),
			fmtNum(o.Def, &o, true),
		)

	case consumer.KindFloat:
		fmt.Printf("range = %s .. %s  step = %s  default = %s\n",
			fmtNum(o.Min, &o, false),
			fmtNum(o.Max, &o, false),
			fmtNum(o.Step, &o, false),
			fmtNum(o.Def, &o, true),
		)

	case consumer.KindEnum:
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

	case consumer.KindString:
		fmt.Printf("max length = %d chars\n", o.MaxLen)

	case consumer.KindIPAddr:
		if d, ok := o.Def.(uint64); ok {
			fmt.Printf("default = %d.%d.%d.%d\n",
				byte(d>>24), byte(d>>16), byte(d>>8), byte(d))
		}

	case consumer.KindAlarm:
		fmt.Printf("priority = %d  tag = 0x%02X\n", o.AlarmPriority, o.AlarmTag)
		if o.AlarmOnMsg != "" {
			fmt.Printf("event on  = %q\n", o.AlarmOnMsg)
		}
		if o.AlarmOffMsg != "" {
			fmt.Printf("event off = %q\n", o.AlarmOffMsg)
		}

	case consumer.KindFrame:
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
func decimalsFromStep(obj *consumer.Object) int {
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
func findObjectByLabel(plug consumer.Protocol, slot int, group, label string) *consumer.Object {
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
func filterByPath(objs []consumer.Object, prefix []string) []consumer.Object {
	if len(prefix) == 0 {
		return objs
	}
	var out []consumer.Object
	for _, o := range objs {
		if matchPathPrefix(o.Path, prefix) {
			out = append(out, o)
		}
	}
	return out
}
