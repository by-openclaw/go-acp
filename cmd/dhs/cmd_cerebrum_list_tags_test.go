package main

import (
	"reflect"
	"testing"
)

// TestFormatCerebrumTagRow pins the terminal row of list-tags: tags joined
// by ';', "-" for a level without tags.
func TestFormatCerebrumTagRow(t *testing.T) {
	cases := []struct {
		row  cerebrumTagRow
		want string
	}{
		{cerebrumTagRow{ID: "16", Level: "1", Available: true, Tags: []string{"ZZ-PROBE"}}, "      16  level 1     available=true   tags=ZZ-PROBE"},
		{cerebrumTagRow{ID: "16", Level: "2", Available: true, Tags: []string{"a", "b"}}, "      16  level 2     available=true   tags=a;b"},
		{cerebrumTagRow{ID: "3", Level: "*", Available: false}, "       3  level *     available=false  tags=-"},
	}
	for _, c := range cases {
		if got := formatCerebrumTagRow(c.row); got != c.want {
			t.Errorf("row %+v:\n got %q\nwant %q", c.row, got, c.want)
		}
	}
}

// TestSortCerebrumTagRows pins numeric ordering by ID then level, with a
// wildcard echo after the numbers.
func TestSortCerebrumTagRows(t *testing.T) {
	rows := []cerebrumTagRow{
		{ID: "16", Level: "2"}, {ID: "3", Level: "*"}, {ID: "3", Level: "10"}, {ID: "3", Level: "2"}, {ID: "16", Level: "1"},
	}
	sortCerebrumTagRows(rows)
	var got []string
	for _, r := range rows {
		got = append(got, r.ID+"/"+r.Level)
	}
	want := []string{"3/2", "3/10", "3/*", "16/1", "16/2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}
