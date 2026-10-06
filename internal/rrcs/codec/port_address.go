package codec

import "fmt"

// PortAddress is TPortAddress (§6.1): the address of one Artist port,
// and which of its two sides is meant.
type PortAddress struct {
	IsInput bool
	Node    int32
	Port    int32
}

// IsNull reports the null port: "the Node and Port member must be set
// to 0", used for instance to clear the mix-minus of an IFB (§6.1).
func (a PortAddress) IsNull() bool { return a.Node == 0 && a.Port == 0 }

// Value renders the address as its wire struct, members in the order
// the specification prints them.
func (a PortAddress) Value() Value {
	return Struct(
		Member{Name: "IsInput", Value: Bool(a.IsInput)},
		Member{Name: "Node", Value: Int(a.Node)},
		Member{Name: "Port", Value: Int(a.Port)},
	)
}

// ParsePortAddress reads a TPortAddress struct.
func ParsePortAddress(v Value) (PortAddress, error) {
	var a PortAddress
	var err error
	if a.IsInput, err = fieldBool(v, "IsInput"); err != nil {
		return PortAddress{}, err
	}
	if a.Node, err = fieldInt(v, "Node"); err != nil {
		return PortAddress{}, err
	}
	if a.Port, err = fieldInt(v, "Port"); err != nil {
		return PortAddress{}, err
	}
	return a, nil
}

// fieldInt returns a mandatory integer member of a struct.
func fieldInt(v Value, name string) (int32, error) {
	m, ok := v.Field(name)
	if !ok {
		return 0, fmt.Errorf("%w: struct has no member %q", ErrMalformed, name)
	}
	i, err := m.AsInt()
	if err != nil {
		return 0, fmt.Errorf("member %q: %w", name, err)
	}
	return i, nil
}

// fieldBool returns a mandatory boolean member of a struct.
func fieldBool(v Value, name string) (bool, error) {
	m, ok := v.Field(name)
	if !ok {
		return false, fmt.Errorf("%w: struct has no member %q", ErrMalformed, name)
	}
	b, err := m.AsBool()
	if err != nil {
		return false, fmt.Errorf("member %q: %w", name, err)
	}
	return b, nil
}
