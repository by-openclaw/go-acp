package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	dhsc "dhs/internal/consumer"
)

// Writing to a REST device that has no PATCH.
//
// Every write here is a read-modify-write: GET the resource, change the
// field, PUT the whole document back. That is not a shortcut, it is
// what the API offers — this device declares PUT on a resource and
// nothing finer — and it has one consequence worth stating plainly:
// the PUT carries every OTHER field as it was read a moment earlier.
// Two writers changing two different fields of one resource at the
// same time is a lost update, on this device, by design of its API.
//
// Which is why SetValues exists. Converging a matrix means changing
// many crosspoints in one map, and doing that one SetValue at a time
// would be one GET+PUT per crosspoint — 17 728 round trips, each one
// shipping the whole 17 728-entry map back, and each one a chance to
// overwrite the previous. Grouped by resource it is one GET and one
// PUT, and the map the device ends up with is the map that was asked
// for.

// SetValue writes one value.
func (p *Plugin) SetValue(ctx context.Context, req dhsc.ValueRequest, v dhsc.Value) (dhsc.Value, error) {
	out, err := p.SetValues(ctx, []dhsc.ValueRequest{req}, []dhsc.Value{v})
	if err != nil {
		return dhsc.Value{}, err
	}
	return out[0], nil
}

// SetValues writes several values, grouping them by the resource that
// carries them so each resource is read once and written once.
//
// It is all-or-nothing per resource: a field this device does not have
// fails before anything is sent, because a half-applied matrix is
// worse than a refused one.
func (p *Plugin) SetValues(ctx context.Context, reqs []dhsc.ValueRequest, vals []dhsc.Value) ([]dhsc.Value, error) {
	if len(reqs) != len(vals) {
		return nil, fmt.Errorf("ccm: %d paths but %d values", len(reqs), len(vals))
	}
	if len(reqs) == 0 {
		return nil, nil
	}
	client, spec, err := p.session()
	if err != nil {
		return nil, err
	}

	// Group by resource, keeping each change's place in the answer.
	type change struct {
		field []string
		val   dhsc.Value
		at    int
	}
	byResource := map[string][]change{}
	var order []string
	for i, req := range reqs {
		resource, field, serr := split(spec, req.Path)
		if serr != nil {
			return nil, serr
		}
		if len(field) == 0 {
			return nil, fmt.Errorf("ccm: %s is a resource, not a value — name a field inside it", resource)
		}
		tpl, ok := spec.TemplateFor(resource)
		if !ok || !spec.Writable(tpl) {
			return nil, fmt.Errorf(
				"ccm: %s is read-only — this device's api.yml declares no PUT on %s", req.Path, resource)
		}
		if _, seen := byResource[resource]; !seen {
			order = append(order, resource)
		}
		byResource[resource] = append(byResource[resource], change{field: field, val: vals[i], at: i})
	}
	sort.Strings(order)

	out := make([]dhsc.Value, len(reqs))
	for _, resource := range order {
		body, gerr := client.get(ctx, resource)
		if gerr != nil {
			return nil, fmt.Errorf("ccm: get %s before writing it: %w", resource, gerr)
		}
		p.RecordRx()
		var doc any
		if uerr := json.Unmarshal(body, &doc); uerr != nil {
			return nil, fmt.Errorf("ccm: %s is not JSON — refusing to write it back: %w", resource, uerr)
		}
		for _, c := range byResource[resource] {
			if serr := setField(doc, c.field, c.val); serr != nil {
				return nil, fmt.Errorf("ccm: %s: %w", resource, serr)
			}
			out[c.at] = c.val
		}
		if perr := client.put(ctx, resource, doc); perr != nil {
			return nil, perr
		}
		p.RecordTx()
	}
	return out, nil
}

// setField changes one field inside a decoded document, in place.
//
// The field has to be there already. Inventing one would send the
// device a document it never described, and the first thing a device
// does with an unknown field is either ignore it or refuse the whole
// write — neither of which an operator would see as "the value did not
// take".
func setField(doc any, field []string, v dhsc.Value) error {
	if len(field) == 0 {
		return fmt.Errorf("no field named")
	}
	parent := doc
	for i, seg := range field[:len(field)-1] {
		next, err := child(parent, seg)
		if err != nil {
			return fmt.Errorf("%s: %w", strings.Join(field[:i+1], "."), err)
		}
		parent = next
	}
	last := field[len(field)-1]
	switch t := parent.(type) {
	case map[string]any:
		if _, has := t[last]; !has {
			return fmt.Errorf("has no field %q", strings.Join(field, "."))
		}
		t[last] = nativeValue(v)
		return nil
	case []any:
		i, err := strconv.Atoi(last)
		if err != nil || i < 0 || i >= len(t) {
			return fmt.Errorf("has no element %q", strings.Join(field, "."))
		}
		t[i] = nativeValue(v)
		return nil
	default:
		return fmt.Errorf("%q is not something with fields", strings.Join(field, "."))
	}
}

// child descends one segment, by key or by array index.
func child(parent any, seg string) (any, error) {
	switch t := parent.(type) {
	case map[string]any:
		v, has := t[seg]
		if !has {
			return nil, fmt.Errorf("no field %q", seg)
		}
		return v, nil
	case []any:
		// An array of objects is addressed by uuid in the model, so it
		// is addressed by uuid here too — and by index when it has none.
		for _, e := range t {
			if m, ok := e.(map[string]any); ok {
				if id, ok := m["uuid"].(string); ok && id == seg {
					return e, nil
				}
			}
		}
		i, err := strconv.Atoi(seg)
		if err != nil || i < 0 || i >= len(t) {
			return nil, fmt.Errorf("no element %q", seg)
		}
		return t[i], nil
	default:
		return nil, fmt.Errorf("%q is not something with fields", seg)
	}
}

// nativeValue turns a neutral Value into what JSON should carry.
func nativeValue(v dhsc.Value) any {
	switch v.Kind {
	case dhsc.KindBool:
		return v.Bool
	case dhsc.KindInt:
		return v.Int
	case dhsc.KindFloat:
		return v.Float
	default:
		return v.Str
	}
}
