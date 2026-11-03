package codec

// GroupPortAddress is TGroupPortAddress (§6.2): one member of an Artist
// group.
type GroupPortAddress struct {
	// IsInputOnly marks a pure matrix input. Group members can be
	// matrix inputs since Artist 6.20; "to be backward compatible, RRCS
	// adds the IsInputOnly member for pure matrix inputs", so the member
	// is present only when true.
	IsInputOnly   bool
	Node          int32
	Port          int32
	SecondChannel bool
}

// Value renders the member as its wire struct.
func (a GroupPortAddress) Value() Value {
	members := make([]Member, 0, 4)
	if a.IsInputOnly {
		members = append(members, Member{Name: "IsInputOnly", Value: Bool(true)})
	}
	members = append(members,
		Member{Name: "Node", Value: Int(a.Node)},
		Member{Name: "Port", Value: Int(a.Port)},
		Member{Name: "SecondChannel", Value: Bool(a.SecondChannel)},
	)
	return Struct(members...)
}

// ParseGroupPortAddress reads a TGroupPortAddress struct. A missing
// IsInputOnly means false.
func ParseGroupPortAddress(v Value) (GroupPortAddress, error) {
	var a GroupPortAddress
	var err error
	if _, ok := v.Field("IsInputOnly"); ok {
		if a.IsInputOnly, err = fieldBool(v, "IsInputOnly"); err != nil {
			return GroupPortAddress{}, err
		}
	}
	if a.Node, err = fieldInt(v, "Node"); err != nil {
		return GroupPortAddress{}, err
	}
	if a.Port, err = fieldInt(v, "Port"); err != nil {
		return GroupPortAddress{}, err
	}
	if a.SecondChannel, err = fieldBool(v, "SecondChannel"); err != nil {
		return GroupPortAddress{}, err
	}
	return a, nil
}
