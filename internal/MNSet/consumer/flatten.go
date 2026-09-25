package mnset

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"dhs/internal/consumer"
)

// A resource document is a JSON tree; the DM is its leaves. flatten walks
// a decoded document in a deterministic order (map keys sorted, arrays
// by index) and emits one consumer.Object per leaf with the full path,
// so two exports of the same module diff line by line.

const (
	accessRead  = 0x01
	accessWrite = 0x02
)

// flatten appends one Object per leaf of doc under the given path prefix.
func flatten(slot int, prefix []string, doc any, access uint8, out *[]consumer.Object) {
	switch v := doc.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			flatten(slot, appendPath(prefix, k), v[k], access, out)
		}
	case []any:
		for i, item := range v {
			flatten(slot, appendPath(prefix, strconv.Itoa(i)), item, access, out)
		}
	default:
		obj := consumer.Object{
			Slot:   slot,
			Path:   prefix,
			Label:  prefix[len(prefix)-1],
			Access: access,
			Value:  leafValue(v),
		}
		obj.Kind = obj.Value.Kind
		*out = append(*out, obj)
	}
}

func appendPath(prefix []string, elem string) []string {
	p := make([]string, len(prefix), len(prefix)+1)
	copy(p, prefix)
	return append(p, elem)
}

// leafValue maps a JSON scalar onto the neutral Value. Numbers arrive as
// json.Number (see Client.Get) so an integer stays an integer.
func leafValue(v any) consumer.Value {
	switch x := v.(type) {
	case bool:
		return consumer.Value{Kind: consumer.KindBool, Bool: x}
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return consumer.Value{Kind: consumer.KindInt, Int: i, Str: x.String()}
		}
		f, _ := x.Float64()
		return consumer.Value{Kind: consumer.KindFloat, Float: f, Str: x.String()}
	case string:
		return consumer.Value{Kind: consumer.KindString, Str: x}
	case nil:
		return consumer.Value{Kind: consumer.KindRaw}
	}
	// Unreachable with a document from decodeDoc (json only yields the
	// four scalar shapes above), kept so a future decoder cannot panic here.
	return consumer.Value{Kind: consumer.KindRaw, Str: fmt.Sprint(v)}
}

// lookup returns the leaf at path inside doc.
func lookup(doc any, path []string) (any, bool) {
	cur := doc
	for _, seg := range path {
		switch v := cur.(type) {
		case map[string]any:
			next, ok := v[seg]
			if !ok {
				return nil, false
			}
			cur = next
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(v) {
				return nil, false
			}
			cur = v[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

// assign replaces the leaf at path inside doc, in place, with val
// coerced to the shape the leaf has today, and returns what was stored.
// The parent and the leaf must exist: a set never creates structure the
// module did not publish.
func assign(doc any, path []string, val consumer.Value) (any, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("empty path")
	}
	parent, ok := lookup(doc, path[:len(path)-1])
	if !ok {
		return nil, fmt.Errorf("path %s: parent: %w", strings.Join(path, "."), consumer.ErrObjectNotFound)
	}
	last := path[len(path)-1]
	switch v := parent.(type) {
	case map[string]any:
		cur, exists := v[last]
		if !exists {
			return nil, fmt.Errorf("path %s: no such field: %w", strings.Join(path, "."), consumer.ErrObjectNotFound)
		}
		next, err := coerce(cur, val)
		if err != nil {
			return nil, fmt.Errorf("path %s: %w", strings.Join(path, "."), err)
		}
		v[last] = next
		return next, nil
	case []any:
		i, err := strconv.Atoi(last)
		if err != nil || i < 0 || i >= len(v) {
			return nil, fmt.Errorf("path %s: no such index: %w", strings.Join(path, "."), consumer.ErrObjectNotFound)
		}
		next, err := coerce(v[i], val)
		if err != nil {
			return nil, fmt.Errorf("path %s: %w", strings.Join(path, "."), err)
		}
		v[i] = next
		return next, nil
	}
	return nil, fmt.Errorf("path %s: parent is a scalar", strings.Join(path, "."))
}

// coerce turns the operator's typed Value into the JSON shape the leaf
// currently has, so a PUT preserves the module's types: a bool field gets
// a bool, a numeric field a number, a string field a string. The CLI
// hands SetValue a Value carrying only Str (cmd_set), which is why the
// string is the input of record.
func coerce(existing any, val consumer.Value) (any, error) {
	s := val.Str
	if val.Kind == consumer.KindBool {
		return val.Bool, nil
	}
	switch cur := existing.(type) {
	case bool:
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "true", "1", "on", "yes":
			return true, nil
		case "false", "0", "off", "no":
			return false, nil
		}
		return nil, fmt.Errorf("%q is not a boolean", s)
	case json.Number:
		if val.Kind == consumer.KindInt {
			return json.Number(strconv.FormatInt(val.Int, 10)), nil
		}
		if val.Kind == consumer.KindFloat {
			return json.Number(strconv.FormatFloat(val.Float, 'f', -1, 64)), nil
		}
		s = strings.TrimSpace(s)
		if _, err := strconv.ParseFloat(s, 64); err != nil {
			return nil, fmt.Errorf("%q is not a number", s)
		}
		return json.Number(s), nil
	case string, nil:
		return s, nil
	case map[string]any:
		// A node takes a JSON object and MERGES it: the keys given replace
		// the module's, the rest stay. That is how a validated tuple is
		// written — the FusioN6 checks the six format_code_* of a flow
		// together on every PUT, so six single-field writes can only pass
		// by accident; one merged write passes or is refused as a whole.
		obj, err := decodeDoc([]byte(s))
		if err != nil {
			return nil, fmt.Errorf("node takes a JSON object: %w", err)
		}
		patch, ok := obj.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("node takes a JSON object, not %T", obj)
		}
		merged := make(map[string]any, len(cur)+len(patch))
		for k, v := range cur {
			merged[k] = v
		}
		for k, v := range patch {
			merged[k] = v
		}
		return merged, nil
	case []any:
		// A list takes a JSON array and is replaced whole.
		arr, err := decodeDoc([]byte(s))
		if err != nil {
			return nil, fmt.Errorf("list takes a JSON array: %w", err)
		}
		if _, ok := arr.([]any); !ok {
			return nil, fmt.Errorf("list takes a JSON array, not %T", arr)
		}
		return arr, nil
	}
	return nil, fmt.Errorf("field is a %T, not a settable scalar", existing)
}
