package codec

import (
	"errors"
	"reflect"
	"testing"
)

// §6.1 TPortAddress: IsInput, Node, Port, in that order.
func TestPortAddress(t *testing.T) {
	a := PortAddress{IsInput: true, Node: 2, Port: 12}
	doc, err := EncodeResponse(a.Value())
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}
	want := `<?xml version="1.0"?><methodResponse><params><param><value><struct>` +
		`<member><name>IsInput</name><value><boolean>1</boolean></value></member>` +
		`<member><name>Node</name><value><int>2</int></value></member>` +
		`<member><name>Port</name><value><int>12</int></value></member>` +
		`</struct></value></param></params></methodResponse>`
	if string(doc) != want {
		t.Errorf("got  %s\nwant %s", doc, want)
	}
	back, err := ParsePortAddress(a.Value())
	if err != nil || back != a {
		t.Errorf("round trip: %+v, %v", back, err)
	}
	if a.IsNull() {
		t.Error("node 2 port 12 reported as the null port")
	}
	// §6.1: the null port has Node and Port set to 0.
	if !(PortAddress{}).IsNull() {
		t.Error("node 0 port 0 is the null port")
	}
}

func TestPortAddressErrors(t *testing.T) {
	tests := map[string]Value{
		"not a struct":  Int(1),
		"no IsInput":    Struct(Member{"Node", Int(1)}, Member{"Port", Int(1)}),
		"no Node":       Struct(Member{"IsInput", Bool(true)}, Member{"Port", Int(1)}),
		"no Port":       Struct(Member{"IsInput", Bool(true)}, Member{"Node", Int(1)}),
		"IsInput wrong": Struct(Member{"IsInput", Int(1)}, Member{"Node", Int(1)}, Member{"Port", Int(1)}),
		"Node wrong":    Struct(Member{"IsInput", Bool(true)}, Member{"Node", String("1")}, Member{"Port", Int(1)}),
	}
	for name, v := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ParsePortAddress(v)
			if !errors.Is(err, ErrMalformed) && !errors.Is(err, ErrType) {
				t.Errorf("got %v, want ErrMalformed or ErrType", err)
			}
		})
	}
}

// §6.2 TGroupPortAddress. IsInputOnly is added only for a pure matrix
// input.
func TestGroupPortAddress(t *testing.T) {
	plain := GroupPortAddress{Node: 2, Port: 5, SecondChannel: true}
	if _, has := plain.Value().Field("IsInputOnly"); has {
		t.Error("IsInputOnly emitted for a member that is not input-only")
	}
	if got := len(plain.Value().Members); got != 3 {
		t.Errorf("%d members, want 3", got)
	}
	input := GroupPortAddress{IsInputOnly: true, Node: 2, Port: 5}
	if got := input.Value().Members[0].Name; got != "IsInputOnly" {
		t.Errorf("first member is %q, want IsInputOnly as §6.2 prints it", got)
	}
	for _, a := range []GroupPortAddress{plain, input} {
		back, err := ParseGroupPortAddress(a.Value())
		if err != nil || back != a {
			t.Errorf("round trip of %+v: %+v, %v", a, back, err)
		}
	}
	bad := map[string]Value{
		"IsInputOnly wrong": Struct(Member{"IsInputOnly", Int(1)}, Member{"Node", Int(1)}, Member{"Port", Int(1)}, Member{"SecondChannel", Bool(false)}),
		"no Node":           Struct(Member{"Port", Int(1)}, Member{"SecondChannel", Bool(false)}),
		"no Port":           Struct(Member{"Node", Int(1)}, Member{"SecondChannel", Bool(false)}),
		"no SecondChannel":  Struct(Member{"Node", Int(1)}, Member{"Port", Int(1)}),
	}
	for name, v := range bad {
		if _, err := ParseGroupPortAddress(v); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// §6.3 TConferencePortAddress, decoded from the specification's own
// text — including the member name printed with a trailing space.
func TestConferencePortAddressFromSpec(t *testing.T) {
	v, err := specValue(specConferencePortAddress)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	got, err := ParseConferencePortAddress(v)
	if err != nil {
		t.Fatalf("ParseConferencePortAddress: %v", err)
	}
	want := ConferencePortAddress{Node: 2, Port: 12, UseSecondChannel: true, Talk: true, Listen: false}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	back, err := ParseConferencePortAddress(want.Value())
	if err != nil || back != want {
		t.Errorf("round trip: %+v, %v", back, err)
	}
}

func TestConferencePortAddressErrors(t *testing.T) {
	full := ConferencePortAddress{Node: 1, Port: 2}.Value()
	for i := range full.Members {
		missing := Struct(append(append([]Member{}, full.Members[:i]...), full.Members[i+1:]...)...)
		if _, err := ParseConferencePortAddress(missing); !errors.Is(err, ErrMalformed) {
			t.Errorf("without %s: got %v, want ErrMalformed", full.Members[i].Name, err)
		}
	}
}

// §6.4 TMemberChangeList, decoded from the specification's own text.
func TestMemberChangeListFromSpec(t *testing.T) {
	v, err := specValue(specMemberChangeList)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	got, err := ParseMemberChangeList(v)
	if err != nil {
		t.Fatalf("ParseMemberChangeList: %v", err)
	}
	want := []MemberChange{{AddToGroup: false, Port: PortAddress{IsInput: false, Node: 3, Port: 7}, UseSecondChannel: true}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	back, err := ParseMemberChangeList(MemberChangeListValue(want))
	if err != nil || !reflect.DeepEqual(back, want) {
		t.Errorf("round trip: %+v, %v", back, err)
	}
	if !valuesEqual(MemberChangeListValue(want), v) {
		t.Errorf("our rendering differs from the specification's:\n got %+v\nwant %+v", MemberChangeListValue(want), v)
	}
}

func TestMemberChangeListErrors(t *testing.T) {
	good := MemberChange{AddToGroup: true, Port: PortAddress{Node: 1, Port: 1}}.Value()
	without := func(name string) Value {
		var m []Member
		for _, x := range good.Members {
			if x.Name != name {
				m = append(m, x)
			}
		}
		return Array(Struct(m...))
	}
	if _, err := ParseMemberChangeList(Int(1)); !errors.Is(err, ErrType) {
		t.Errorf("not an array: %v", err)
	}
	for _, name := range []string{"AddToGroup", "PortAddress", "UseSecondChannel"} {
		if _, err := ParseMemberChangeList(without(name)); !errors.Is(err, ErrMalformed) {
			t.Errorf("without %s: got %v, want ErrMalformed", name, err)
		}
	}
	badAddr := Array(Struct(Member{"AddToGroup", Bool(true)}, Member{"PortAddress", Int(1)}, Member{"UseSecondChannel", Bool(false)}))
	if _, err := ParseMemberChangeList(badAddr); err == nil {
		t.Error("a port address that is not a struct was accepted")
	}
	if got, err := ParseMemberChangeList(Array()); err != nil || len(got) != 0 {
		t.Errorf("empty list: %+v, %v", got, err)
	}
}
