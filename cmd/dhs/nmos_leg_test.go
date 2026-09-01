package main

import "testing"

func TestExpandLeg(t *testing.T) {
	cases := []struct {
		name, leg, dest, portRaw string
		ports                    []int
		wantIPs                  []string
		wantPorts                []int
		wantErr                  bool
	}{
		{"red dest+port", "red", "239.60.1.1", "5010", []int{5010},
			[]string{"239.60.1.1", ""}, []int{5010, 0}, false},
		{"blue dest only", "blue", "239.62.1.1", "", nil,
			[]string{"", "239.62.1.1"}, nil, false},
		{"both port only", "both", "", "5004", []int{5004},
			nil, []int{5004, 5004}, false},
		{"both refuses one destination", "both", "239.60.1.1", "", nil, nil, nil, true},
		{"comma list refused", "red", "a,b", "", nil, nil, nil, true},
		{"unknown leg", "green", "239.60.1.1", "", nil, nil, nil, true},
	}
	for _, tc := range cases {
		ips, ports, err := expandLeg(tc.leg, tc.dest, tc.portRaw, tc.ports)
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: err = %v", tc.name, err)
			continue
		}
		if tc.wantErr {
			continue
		}
		if !eqStrings(ips, tc.wantIPs) || !eqInts(ports, tc.wantPorts) {
			t.Errorf("%s: got %v/%v, want %v/%v", tc.name, ips, ports, tc.wantIPs, tc.wantPorts)
		}
	}
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func eqInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
