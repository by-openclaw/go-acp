package codec

import "fmt"

// MemberChange is one element of TMemberChangeList (§6.4): add a port
// to a group, or remove it.
type MemberChange struct {
	// AddToGroup is true to add the port and false to remove it.
	AddToGroup bool
	// Port is the port added or removed, a TPortAddress.
	Port PortAddress
	// UseSecondChannel tells whether the first or the second channel of
	// the port is used.
	UseSecondChannel bool
}

// Value renders the change as its wire struct.
func (c MemberChange) Value() Value {
	return Struct(
		Member{Name: "AddToGroup", Value: Bool(c.AddToGroup)},
		Member{Name: "PortAddress", Value: c.Port.Value()},
		Member{Name: "UseSecondChannel", Value: Bool(c.UseSecondChannel)},
	)
}

// MemberChangeListValue renders TMemberChangeList: an array of changes.
func MemberChangeListValue(changes []MemberChange) Value {
	items := make([]Value, 0, len(changes))
	for _, c := range changes {
		items = append(items, c.Value())
	}
	return Array(items...)
}

// ParseMemberChangeList reads a TMemberChangeList array.
func ParseMemberChangeList(v Value) ([]MemberChange, error) {
	if v.Kind != KindArray {
		return nil, fmt.Errorf("%w: member change list: want array, got %s", ErrType, v.Kind)
	}
	out := make([]MemberChange, 0, len(v.Items))
	for i, it := range v.Items {
		var c MemberChange
		var err error
		if c.AddToGroup, err = fieldBool(it, "AddToGroup"); err != nil {
			return nil, fmt.Errorf("change %d: %w", i, err)
		}
		addr, ok := it.Field("PortAddress")
		if !ok {
			return nil, fmt.Errorf("change %d: %w: struct has no member %q", i, ErrMalformed, "PortAddress")
		}
		if c.Port, err = ParsePortAddress(addr); err != nil {
			return nil, fmt.Errorf("change %d: %w", i, err)
		}
		if c.UseSecondChannel, err = fieldBool(it, "UseSecondChannel"); err != nil {
			return nil, fmt.Errorf("change %d: %w", i, err)
		}
		out = append(out, c)
	}
	return out, nil
}
