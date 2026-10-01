package consumer

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"dhs/internal/ccm/codec"
	dhsc "dhs/internal/consumer"
)

// The DM of a CCM device is its own openapi.yml (operator rule,
// 2026-10-01; #1200): every leaf a walk, a get or a write-back
// flattens carries what the schema says about it — enumeration,
// bounds, length, and the read-only bit of CCM §14.2 — and a write is
// checked against that before anything is sent. Nothing here is
// hand-written per firmware: a device that serves a different document
// describes itself differently.

// typeObjects attaches the spec's field types to the leaves flatten
// produced for one resource. Leaves the spec does not describe keep
// what the JSON said.
func typeObjects(spec *codec.Spec, resource string, objs []dhsc.Object) {
	tpl, ok := spec.TemplateFor(resource)
	if !ok {
		return
	}
	base := len(segments(resource))
	for i := range objs {
		if len(objs[i].Path) <= base {
			continue
		}
		ft, ok := spec.FieldType(tpl, objs[i].Path[base:])
		if !ok {
			continue
		}
		applyFieldType(&objs[i], ft)
	}
}

func applyFieldType(o *dhsc.Object, ft codec.FieldType) {
	if len(ft.Enum) > 0 {
		o.EnumItems = append([]string(nil), ft.Enum...)
	}
	if ft.Min != nil {
		o.Min = bound(ft, *ft.Min)
	}
	if ft.Max != nil {
		o.Max = bound(ft, *ft.Max)
	}
	if ft.MaxLength > 0 {
		o.MaxLen = ft.MaxLength
	}
	if ft.ReadOnly {
		o.Access &^= accessWrite
	}
}

// bound keeps an integer field's bound an integer in the export.
func bound(ft codec.FieldType, v float64) any {
	if ft.Type == "integer" && v == math.Trunc(v) {
		return int64(v)
	}
	return v
}

// checkWrite refuses, before the wire, a value the spec says the field
// cannot take: a read-only field, a word outside the enumeration, a
// number outside the bounds, a string past its length. Exit 2 at the
// CLI (a ValidationError), never a device round trip.
func checkWrite(spec *codec.Spec, resource string, field []string, v dhsc.Value) error {
	tpl, ok := spec.TemplateFor(resource)
	if !ok {
		return nil
	}
	ft, ok := spec.FieldType(tpl, field)
	if !ok {
		return nil
	}
	name := strings.Join(append(segments(resource), field...), ".")
	bad := func(reason string) error {
		return fmt.Errorf("%w: %w", dhsc.ErrValidationFailed, &dhsc.ValidationError{Field: name, Reason: reason})
	}
	if ft.ReadOnly {
		return bad("read-only in the device's openapi.yml (CCM §14.2)")
	}
	s := valueText(v)
	if len(ft.Enum) > 0 {
		found := false
		for _, e := range ft.Enum {
			if e == s {
				found = true
				break
			}
		}
		if !found {
			return bad(fmt.Sprintf("%q is not one of %s", s, strings.Join(ft.Enum, ", ")))
		}
	}
	if ft.MaxLength > 0 && len(s) > ft.MaxLength {
		return bad(fmt.Sprintf("%q is longer than %d", s, ft.MaxLength))
	}
	if ft.Min != nil || ft.Max != nil {
		n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return bad(fmt.Sprintf("%q is not a number", s))
		}
		if ft.Min != nil && n < *ft.Min {
			return fmt.Errorf("%w: %s: %v < %v", dhsc.ErrOutOfRangeLow, name, n, *ft.Min)
		}
		if ft.Max != nil && n > *ft.Max {
			return fmt.Errorf("%w: %s: %v > %v", dhsc.ErrOutOfRangeHigh, name, n, *ft.Max)
		}
	}
	return nil
}

// valueText is the operator's value as the words the spec compares
// against. The CLI hands a Value carrying Str; an importer may hand a
// typed one.
func valueText(v dhsc.Value) string {
	switch v.Kind {
	case dhsc.KindBool:
		return strconv.FormatBool(v.Bool)
	case dhsc.KindInt:
		if v.Str == "" {
			return strconv.FormatInt(v.Int, 10)
		}
	case dhsc.KindUint:
		if v.Str == "" {
			return strconv.FormatUint(v.Uint, 10)
		}
	case dhsc.KindFloat:
		if v.Str == "" {
			return strconv.FormatFloat(v.Float, 'f', -1, 64)
		}
	}
	return v.Str
}
