package emberplus

import (
	"reflect"
	"sort"
	"testing"
)

// TestStreamParameterPaths_Filter pins the R10 multi-subscribe contract:
//   - nil / empty filter → return every indexed stream path
//   - single-id filter → only paths registered under that id
//   - multi-id filter → paths registered under any matching id (set union)
//   - unknown id in filter → ignored, not an error
func TestStreamParameterPaths_Filter(t *testing.T) {
	p := &Plugin{
		streamIndex: map[int64][]string{
			0:    {"1.4.1"},
			1001: {"1.4.2"},
			1002: {"1.4.3"},
		},
	}

	tests := []struct {
		name   string
		filter []int64
		want   []string
	}{
		{name: "nil filter returns all", filter: nil, want: []string{"1.4.1", "1.4.2", "1.4.3"}},
		{name: "empty slice returns all", filter: []int64{}, want: []string{"1.4.1", "1.4.2", "1.4.3"}},
		{name: "single id 0", filter: []int64{0}, want: []string{"1.4.1"}},
		{name: "single id 1001", filter: []int64{1001}, want: []string{"1.4.2"}},
		{name: "two ids", filter: []int64{0, 1001}, want: []string{"1.4.1", "1.4.2"}},
		{name: "all ids explicit", filter: []int64{0, 1001, 1002}, want: []string{"1.4.1", "1.4.2", "1.4.3"}},
		{name: "unknown id ignored", filter: []int64{42}, want: nil},
		{name: "known + unknown id", filter: []int64{0, 42}, want: []string{"1.4.1"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := p.StreamParameterPaths(tc.filter)
			sort.Strings(got)
			want := append([]string(nil), tc.want...)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("StreamParameterPaths(%v) = %v, want %v", tc.filter, got, want)
			}
		})
	}
}

// TestStreamParameterPaths_MultiPathPerID covers the case where one
// streamIdentifier is shared by multiple parameters (legal per spec p.93).
func TestStreamParameterPaths_MultiPathPerID(t *testing.T) {
	p := &Plugin{
		streamIndex: map[int64][]string{
			7: {"1.4.1", "1.4.2", "1.4.3"},
			9: {"1.5.1"},
		},
	}
	got := p.StreamParameterPaths([]int64{7})
	sort.Strings(got)
	want := []string{"1.4.1", "1.4.2", "1.4.3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("StreamParameterPaths([7]) = %v, want %v", got, want)
	}
}
