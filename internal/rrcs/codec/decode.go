package codec

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// parseValue turns a <value> element into a Value.
func parseValue(n *node) (Value, error) {
	switch len(n.children) {
	case 0:
		// "If no type is indicated, the type is string" (§5.6.2).
		return String(n.text), nil
	case 1:
	default:
		return Value{}, fmt.Errorf("%w: <value> holds %d elements, want one", ErrMalformed, len(n.children))
	}

	c := n.children[0]
	switch c.name {
	case "string":
		return String(c.text), nil
	case "i4", "int":
		i, err := strconv.ParseInt(strings.TrimSpace(c.text), 10, 32)
		if err != nil {
			return Value{}, fmt.Errorf("%w: <%s> %q is not a four-byte integer", ErrMalformed, c.name, c.text)
		}
		return Int(int32(i)), nil
	case "boolean":
		switch strings.TrimSpace(c.text) {
		case "0":
			return Bool(false), nil
		case "1":
			return Bool(true), nil
		}
		return Value{}, fmt.Errorf("%w: <boolean> %q is neither 0 nor 1", ErrMalformed, c.text)
	case "double":
		f, err := strconv.ParseFloat(strings.TrimSpace(c.text), 64)
		if err != nil {
			return Value{}, fmt.Errorf("%w: <double> %q is not a number", ErrMalformed, c.text)
		}
		return Double(f), nil
	case "dateTime.iso8601":
		return DateTime(strings.TrimSpace(c.text)), nil
	case "base64":
		raw, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(c.text), ""))
		if err != nil {
			return Value{}, fmt.Errorf("%w: <base64> does not decode", ErrMalformed)
		}
		return Base64(raw), nil
	case "struct":
		return parseStruct(c)
	case "array":
		return parseArray(c)
	}
	return Value{}, fmt.Errorf("%w: unknown value type <%s>", ErrMalformed, c.name)
}

// parseStruct reads the <member> elements of a <struct>, in order.
func parseStruct(n *node) (Value, error) {
	members := make([]Member, 0, len(n.children))
	for _, m := range n.children {
		if m.name != "member" {
			return Value{}, fmt.Errorf("%w: <%s> inside <struct>", ErrMalformed, m.name)
		}
		name, val := m.child("name"), m.child("value")
		if name == nil || val == nil {
			return Value{}, fmt.Errorf("%w: <member> without <name> or <value>", ErrMalformed)
		}
		v, err := parseValue(val)
		if err != nil {
			return Value{}, err
		}
		// The specification prints some member names with a stray space
		// ("UseSecondChannel ", §6.3); a name never legitimately has one.
		members = append(members, Member{Name: strings.TrimSpace(name.text), Value: v})
	}
	return Struct(members...), nil
}

// parseArray reads the values of an <array>, which sit inside one <data>.
func parseArray(n *node) (Value, error) {
	data := n.child("data")
	if data == nil || len(n.children) != 1 {
		return Value{}, fmt.Errorf("%w: <array> without a single <data>", ErrMalformed)
	}
	items := make([]Value, 0, len(data.children))
	for _, it := range data.children {
		if it.name != "value" {
			return Value{}, fmt.Errorf("%w: <%s> inside <data>", ErrMalformed, it.name)
		}
		v, err := parseValue(it)
		if err != nil {
			return Value{}, err
		}
		items = append(items, v)
	}
	return Array(items...), nil
}

// paramValue returns the value of a <param>, which holds exactly one.
func paramValue(p *node) (Value, error) {
	val := p.child("value")
	if val == nil || len(p.children) != 1 {
		return Value{}, fmt.Errorf("%w: <param> without a single <value>", ErrMalformed)
	}
	return parseValue(val)
}
