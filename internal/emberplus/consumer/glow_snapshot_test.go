package emberplus

import "testing"

// TestLessNumericPath pins every branch directly — including the
// b-shorter-than-a arm, which was only coincidence-covered before
// (map iteration order decided whether a shorter b ever met a longer
// a; coverage floor red, PR #742 run 32536961316).
func TestLessNumericPath(t *testing.T) {
	cases := []struct {
		name string
		a, b []int32
		want bool
	}{
		{"differs mid-path", []int32{1, 2, 3}, []int32{1, 3}, true},
		{"b shorter, equal prefix", []int32{1, 2, 3}, []int32{1, 2}, false},
		{"a shorter, equal prefix", []int32{1, 2}, []int32{1, 2, 3}, true},
		{"equal", []int32{1, 2}, []int32{1, 2}, false},
		{"natural numeric order", []int32{10}, []int32{2}, false},
		{"empty a sorts first", nil, []int32{1}, true},
		{"both empty", nil, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := lessNumericPath(c.a, c.b); got != c.want {
				t.Fatalf("lessNumericPath(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}
