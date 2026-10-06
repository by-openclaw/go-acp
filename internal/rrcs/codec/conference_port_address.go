package codec

// ConferencePortAddress is TConferencePortAddress (§6.3): one member of
// an Artist conference, with its talk and listen rights.
type ConferencePortAddress struct {
	Node             int32
	Port             int32
	UseSecondChannel bool
	Talk             bool
	Listen           bool
}

// Value renders the member as its wire struct.
func (a ConferencePortAddress) Value() Value {
	return Struct(
		Member{Name: "Node", Value: Int(a.Node)},
		Member{Name: "Port", Value: Int(a.Port)},
		Member{Name: "UseSecondChannel", Value: Bool(a.UseSecondChannel)},
		Member{Name: "Talk", Value: Bool(a.Talk)},
		Member{Name: "Listen", Value: Bool(a.Listen)},
	)
}

// ParseConferencePortAddress reads a TConferencePortAddress struct.
func ParseConferencePortAddress(v Value) (ConferencePortAddress, error) {
	var a ConferencePortAddress
	var err error
	if a.Node, err = fieldInt(v, "Node"); err != nil {
		return ConferencePortAddress{}, err
	}
	if a.Port, err = fieldInt(v, "Port"); err != nil {
		return ConferencePortAddress{}, err
	}
	if a.UseSecondChannel, err = fieldBool(v, "UseSecondChannel"); err != nil {
		return ConferencePortAddress{}, err
	}
	if a.Talk, err = fieldBool(v, "Talk"); err != nil {
		return ConferencePortAddress{}, err
	}
	if a.Listen, err = fieldBool(v, "Listen"); err != nil {
		return ConferencePortAddress{}, err
	}
	return a, nil
}
