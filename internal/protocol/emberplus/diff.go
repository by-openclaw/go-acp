package emberplus

import (
	"fmt"
	"strings"

	"acp/internal/protocol"
	"acp/internal/protocol/emberplus/glow"
)

// diffParameters returns the ordered list of FieldChange entries that
// represent how `next` differs from `prev`. `prev` may be nil, which
// means "this is the first sighting"; we return an empty slice in
// that case (no diff to show).
//
// The canonical field set matches the one documented on
// protocol.FieldChange. Stable order for deterministic rendering.
func diffParameters(prev, next *glow.Parameter) []protocol.FieldChange {
	if prev == nil || next == nil {
		return nil
	}
	var out []protocol.FieldChange

	add := func(name, old, newVal string) {
		if old == newVal {
			return
		}
		out = append(out, protocol.FieldChange{Name: name, Old: old, New: newVal})
	}

	// Value — stringify generically; real-number tolerance is left
	// to the caller's rendering layer.
	if !valueEqual(prev.Value, next.Value) {
		add("value", formatAny(prev.Value), formatAny(next.Value))
	}
	add("description", prev.Description, next.Description)
	add("access", accessLabel(prev.Access), accessLabel(next.Access))
	add("min", formatAny(prev.Minimum), formatAny(next.Minimum))
	add("max", formatAny(prev.Maximum), formatAny(next.Maximum))
	add("step", formatAny(prev.Step), formatAny(next.Step))
	add("default", formatAny(prev.Default), formatAny(next.Default))
	add("format", prev.Format, next.Format)
	if prev.Factor != next.Factor {
		add("factor", fmt.Sprintf("%d", prev.Factor), fmt.Sprintf("%d", next.Factor))
	}
	add("formula", prev.Formula, next.Formula)
	add("enumeration",
		compactEnumeration(prev.Enumeration),
		compactEnumeration(next.Enumeration))
	if prev.StreamIdentifier != next.StreamIdentifier {
		add("streamIdentifier",
			fmt.Sprintf("%d", prev.StreamIdentifier),
			fmt.Sprintf("%d", next.StreamIdentifier))
	}
	if prev.IsOnline != next.IsOnline {
		add("isOnline", boolLabel(prev.IsOnline), boolLabel(next.IsOnline))
	}

	return out
}

// valueEqual compares two Parameter.Value CHOICE values for equality.
// Matches types first; interface values with different types are
// unequal (provider shouldn't switch the type of a live parameter).
func valueEqual(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

// formatAny stringifies a Parameter.Value CHOICE for human-readable
// diff output. Strings get quoted; nil renders as "nil".
func formatAny(v any) string {
	if v == nil {
		return "nil"
	}
	switch x := v.(type) {
	case string:
		return fmt.Sprintf("%q", x)
	case []byte:
		return fmt.Sprintf("%d bytes", len(x))
	default:
		return fmt.Sprintf("%v", x)
	}
}

// accessLabel renders a Glow ParameterAccess integer compactly for
// diff output. Zero is treated as the spec default "read".
func accessLabel(a int64) string {
	switch a {
	case 0, glow.AccessRead:
		return "R"
	case glow.AccessWrite:
		return "W"
	case glow.AccessReadWrite:
		return "RW"
	}
	return fmt.Sprintf("access(%d)", a)
}

func boolLabel(b bool) string {
	if b {
		return "y"
	}
	return "n"
}

// compactEnumeration turns a long LF-joined enum list into a summary
// (count + first few labels) so the diff stays readable when a
// provider rewrites a 200-option enum.
func compactEnumeration(s string) string {
	if s == "" {
		return ""
	}
	items := strings.Split(s, "\n")
	if len(items) <= 3 {
		return s
	}
	return fmt.Sprintf("%s,%s,%s,...(%d total)", items[0], items[1], items[2], len(items))
}
