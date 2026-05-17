package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseStreamIDList(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []int64
		wantErr string
	}{
		{name: "empty subscribes to all", in: "", want: nil},
		{name: "whitespace only subscribes to all", in: "   ", want: nil},
		{name: "single id", in: "0", want: []int64{0}},
		{name: "single positive id", in: "1001", want: []int64{1001}},
		{name: "csv two", in: "0,1001", want: []int64{0, 1001}},
		{name: "csv three", in: "0,1001,1002", want: []int64{0, 1001, 1002}},
		{name: "csv with spaces", in: " 0 , 1001 ,1002 ", want: []int64{0, 1001, 1002}},
		{name: "negative id allowed", in: "-7", want: []int64{-7}},
		{name: "dedup preserves first-seen order", in: "0,0,1001,0,1001", want: []int64{0, 1001}},

		{name: "bad token alpha", in: "abc", wantErr: `bad token "abc"`},
		{name: "bad token mid-csv", in: "0,abc", wantErr: `bad token "abc"`},
		{name: "bad token trailing", in: "0,1001,xyz", wantErr: `bad token "xyz"`},
		{name: "empty token mid-csv", in: "0,,1001", wantErr: "empty token"},
		{name: "trailing comma", in: "0,1001,", wantErr: "empty token"},
		{name: "leading comma", in: ",0,1001", wantErr: "empty token"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseStreamIDList(tc.in)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("parseStreamIDList(%q) = %v, want error containing %q", tc.in, got, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("parseStreamIDList(%q) error = %q, want substring %q", tc.in, err.Error(), tc.wantErr)
				}
				if !strings.HasPrefix(err.Error(), "validation:") {
					t.Errorf("parseStreamIDList(%q) error = %q, want validation: prefix", tc.in, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("parseStreamIDList(%q) unexpected error: %v", tc.in, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseStreamIDList(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
