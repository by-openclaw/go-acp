package main

import (
	"reflect"
	"testing"
)

// TestSubtractSorted pins the R14 #475 matrix-ensure helper for the
// ensurePresent path (compute the "to add" set).
func TestSubtractSorted(t *testing.T) {
	cases := []struct {
		name string
		a, b []int32
		want []int32
	}{
		{name: "empty a", a: []int32{}, b: []int32{1, 2}, want: []int32{}},
		{name: "empty b", a: []int32{1, 2}, b: []int32{}, want: []int32{1, 2}},
		{name: "no overlap", a: []int32{1, 2, 3}, b: []int32{4, 5}, want: []int32{1, 2, 3}},
		{name: "full overlap", a: []int32{1, 2}, b: []int32{1, 2}, want: []int32{}},
		{name: "partial overlap", a: []int32{1, 2, 3, 4}, b: []int32{2, 4}, want: []int32{1, 3}},
		{name: "preserves order", a: []int32{1, 3, 5, 7}, b: []int32{3, 7}, want: []int32{1, 5}},
		{name: "duplicates in a kept", a: []int32{1, 1, 2}, b: []int32{2}, want: []int32{1, 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := subtractSorted(tc.a, tc.b)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("subtractSorted(%v, %v) = %v; want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// TestIntersectSorted pins the R14 #475 matrix-ensure helper for the
// ensureAbsent path (compute the "to remove" set).
func TestIntersectSorted(t *testing.T) {
	cases := []struct {
		name string
		a, b []int32
		want []int32
	}{
		{name: "empty a", a: []int32{}, b: []int32{1, 2}, want: []int32{}},
		{name: "empty b", a: []int32{1, 2}, b: []int32{}, want: []int32{}},
		{name: "no overlap", a: []int32{1, 2}, b: []int32{3, 4}, want: []int32{}},
		{name: "full overlap", a: []int32{1, 2}, b: []int32{1, 2}, want: []int32{1, 2}},
		{name: "partial overlap", a: []int32{1, 2, 3, 4}, b: []int32{2, 4, 5}, want: []int32{2, 4}},
		{name: "preserves a's order", a: []int32{1, 3, 5}, b: []int32{5, 3, 1}, want: []int32{1, 3, 5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := intersectSorted(tc.a, tc.b)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("intersectSorted(%v, %v) = %v; want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
