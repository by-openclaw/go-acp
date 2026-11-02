package codec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// ScalarForm is how a one-element numeric value travels under "object".
// The page shows an array for quantity even with one value and for the
// array[2] ints, and names ints, floats and doubles in the plural; it
// gives no scalar example. Array is the reading taken; Bare is the other
// one, a switch away at the customer site.
type ScalarForm uint8

// The two forms.
const (
	ScalarArray ScalarForm = iota
	ScalarBare
)

// ParseScalarForm reads the configuration word; empty is the default.
func ParseScalarForm(s string) (ScalarForm, error) {
	switch s {
	case "", "array":
		return ScalarArray, nil
	case "bare":
		return ScalarBare, nil
	}
	return ScalarArray, fmt.Errorf("prism: scalar form %q (array or bare)", s)
}

// EncodeBody builds the POST body of one value, and validates it against
// the datatype and the option enum.
//
//	{"object":<value>}  every call but the two below
//	<value>             activeInput, inputConfigure ("consumes a JSON object in the POST body")
//
// An ints call with symbolic options takes the option name or, since the
// datatype is ints and a read may answer a number, an integer.
func (e Entry) EncodeBody(value string, form ScalarForm) ([]byte, error) {
	switch e.Type {
	case TypeObject:
		return e.objectBody(value)
	case TypeString:
		if len(e.Options) > 0 && !slices.Contains(e.Options, value) {
			return nil, e.badOption(value)
		}
		return wrap(jsonString(value)), nil
	case TypeQuantity:
		var items [][]byte
		for _, v := range splitList(value) {
			items = append(items, jsonString(v))
		}
		return wrap(array(items)), nil
	case TypeInts, TypeSysio:
		var items [][]byte
		for _, v := range splitList(value) {
			item, err := e.intItem(v)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return wrap(numbers(items, form)), nil
	case TypeFloats, TypeDoubles:
		var items [][]byte
		for _, v := range splitList(value) {
			if _, err := strconv.ParseFloat(v, 64); err != nil || !json.Valid([]byte(v)) {
				return nil, &Invalid{e.Name, fmt.Sprintf("%q is not a number", v)}
			}
			items = append(items, []byte(v))
		}
		return wrap(numbers(items, form)), nil
	}
	return nil, &Invalid{e.Name, "datatype " + string(e.Type) + " takes no value"}
}

func (e Entry) objectBody(value string) ([]byte, error) {
	if !json.Valid([]byte(value)) {
		return nil, &Invalid{e.Name, "the value is not JSON"}
	}
	if e.BareBody {
		return []byte(value), nil
	}
	// The page writes some examples with the wrapper already on.
	var top map[string]json.RawMessage
	if json.Unmarshal([]byte(value), &top) == nil && len(top) == 1 && top["object"] != nil {
		return []byte(value), nil
	}
	return wrap([]byte(value)), nil
}

func (e Entry) intItem(v string) ([]byte, error) {
	_, err := strconv.ParseInt(v, 10, 64)
	switch {
	case err == nil && (!e.Numeric() || slices.Contains(e.Options, v)):
		return []byte(v), nil
	case err != nil && slices.Contains(e.Options, v):
		return jsonString(v), nil
	case len(e.Options) > 0:
		return nil, e.badOption(v)
	}
	return nil, &Invalid{e.Name, fmt.Sprintf("%q is not an integer", v)}
}

func (e Entry) badOption(v string) error {
	return &Invalid{e.Name, fmt.Sprintf("%q is not an option: %s", v, strings.Join(e.Options, ", "))}
}

func splitList(s string) []string {
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func numbers(items [][]byte, form ScalarForm) []byte {
	if len(items) == 1 && form == ScalarBare {
		return items[0]
	}
	return array(items)
}

func array(items [][]byte) []byte {
	return append(append([]byte{'['}, bytes.Join(items, []byte{','})...), ']')
}

func wrap(v []byte) []byte {
	return append(append([]byte(`{"object":`), v...), '}')
}

func jsonString(s string) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a string always encodes
	return bytes.TrimRight(b.Bytes(), "\n")
}
