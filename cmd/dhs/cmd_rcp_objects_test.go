package main

import (
	"errors"
	"testing"

	rcpc "dhs/internal/rcp/consumer"
)

func TestParseRCPObjectRef(t *testing.T) {
	ok := []struct {
		in   string
		want rcpObjectRef
	}{
		{"0 Nodes", rcpObjectRef{0, "Nodes"}},
		{"3 Devices.[lab-view-01].Senders", rcpObjectRef{3, "Devices.[lab-view-01].Senders"}},
		// A device label holds spaces: everything after the first one is the path.
		{"4 Devices.[emsfp-a2-10-0c Device CH1].Receivers", rcpObjectRef{4, "Devices.[emsfp-a2-10-0c Device CH1].Receivers"}},
	}
	for _, tc := range ok {
		got, err := parseRCPObjectRef(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("parse(%q) = %+v, %v; want %+v", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []string{"", "Nodes", "x Nodes", "-1 Nodes", "2 ", "2"} {
		if _, err := parseRCPObjectRef(bad); err == nil {
			t.Errorf("parse(%q) was accepted", bad)
		}
	}
}

func TestRCPObjectLine(t *testing.T) {
	ref := rcpObjectRef{2, "Devices.[lab-shuffle-01].Senders"}
	cases := []struct {
		name string
		a    rcpc.ObjectAnswer
		err  error
		want string
	}{
		{"a table gives its row count and its indices", rcpc.ObjectAnswer{IsTable: true, Indices: []string{"a", "b"}}, nil,
			"2\tDevices.[lab-shuffle-01].Senders\ttable\t2\ta,b"},
		{"an empty table is a table of 0 rows", rcpc.ObjectAnswer{IsTable: true, Indices: []string{}}, nil,
			"2\tDevices.[lab-shuffle-01].Senders\ttable\t0\t"},
		{"a value", rcpc.ObjectAnswer{Value: &rcpc.Object{Current: float64(3)}}, nil,
			"2\tDevices.[lab-shuffle-01].Senders\tvalue\t3"},
		{"no value at this path", rcpc.ObjectAnswer{}, nil,
			"2\tDevices.[lab-shuffle-01].Senders\tnone\t"},
		{"a failed read is its own line, on one line", rcpc.ObjectAnswer{}, errors.New("rcp: GET: 404\nno slot"),
			"2\tDevices.[lab-shuffle-01].Senders\terror\trcp: GET: 404 no slot"},
	}
	for _, tc := range cases {
		if got := rcpObjectLine(ref, tc.a, tc.err); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
	}
}
