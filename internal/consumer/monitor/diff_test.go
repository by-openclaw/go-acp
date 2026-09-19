package monitor

import (
	"testing"

	"dhs/internal/consumer"
)

func TestValueEqual(t *testing.T) {
	cases := []struct {
		name string
		a, b consumer.Value
		want bool
	}{
		{"int same", intVal(5), intVal(5), true},
		{"int diff", intVal(5), intVal(6), false},
		{"string same", strVal("x"), strVal("x"), true},
		{"string diff", strVal("x"), strVal("y"), false},
		{"kind diff", intVal(5), strVal("5"), false},
		{"uint same", consumer.Value{Kind: consumer.KindUint, Uint: 9}, consumer.Value{Kind: consumer.KindUint, Uint: 9}, true},
		{"bool diff", consumer.Value{Kind: consumer.KindBool, Bool: true}, consumer.Value{Kind: consumer.KindBool, Bool: false}, false},
		{"enum same", consumer.Value{Kind: consumer.KindEnum, Enum: 5}, consumer.Value{Kind: consumer.KindEnum, Enum: 5}, true},
		{"ip diff", consumer.Value{Kind: consumer.KindIPAddr, IPAddr: [4]byte{10, 6, 250, 104}}, consumer.Value{Kind: consumer.KindIPAddr, IPAddr: [4]byte{10, 6, 250, 105}}, false},
		{"raw same", consumer.Value{Raw: []byte{1, 2}}, consumer.Value{Raw: []byte{1, 2}}, true},
		{"raw diff", consumer.Value{Raw: []byte{1, 2}}, consumer.Value{Raw: []byte{1, 3}}, false},
	}
	for _, c := range cases {
		if got := valueEqual(c.a, c.b); got != c.want {
			t.Errorf("%s: valueEqual = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestValueString(t *testing.T) {
	cases := []struct {
		v    consumer.Value
		want string
	}{
		{intVal(42), "42"},
		{strVal("hello"), "hello"},
		{consumer.Value{Kind: consumer.KindUint, Uint: 7}, "7"},
		{consumer.Value{Kind: consumer.KindBool, Bool: true}, "true"},
		{consumer.Value{Kind: consumer.KindEnum, Enum: 5}, "5"},
		{consumer.Value{Kind: consumer.KindIPAddr, IPAddr: [4]byte{10, 6, 250, 104}}, "10.6.250.104"},
	}
	for _, c := range cases {
		if got := valueString(c.v); got != c.want {
			t.Errorf("valueString(%v) = %q, want %q", c.v, got, c.want)
		}
	}
}
