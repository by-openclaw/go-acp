package main

import "testing"

// TestMatchPathSuffix covers the cache path-resolver matcher used by
// resolvePathFromCache (cmd_set.go / cmd_get.go). The matcher must be
// suffix-based so callers can address by full path, partial parents,
// or just leaf label.
func TestMatchPathSuffix(t *testing.T) {
	cases := []struct {
		name     string
		objPath  []string
		wanted   []string
		expected bool
	}{
		{
			name:     "exact full path",
			objPath:  []string{"ROOT_NODE_V2", "IDENTITY", "User Label 1"},
			wanted:   []string{"ROOT_NODE_V2", "IDENTITY", "User Label 1"},
			expected: true,
		},
		{
			name:     "two-segment suffix",
			objPath:  []string{"ROOT_NODE_V2", "IDENTITY", "User Label 1"},
			wanted:   []string{"IDENTITY", "User Label 1"},
			expected: true,
		},
		{
			name:     "leaf only",
			objPath:  []string{"ROOT_NODE_V2", "IDENTITY", "User Label 1"},
			wanted:   []string{"User Label 1"},
			expected: true,
		},
		{
			name:     "wrong parent rejected",
			objPath:  []string{"ROOT_NODE_V2", "IDENTITY", "User Label 1"},
			wanted:   []string{"GENERAL", "User Label 1"},
			expected: false,
		},
		{
			name:     "wanted longer than path",
			objPath:  []string{"User Label 1"},
			wanted:   []string{"IDENTITY", "User Label 1"},
			expected: false,
		},
		{
			name:     "empty wanted matches nothing",
			objPath:  []string{"X"},
			wanted:   []string{},
			expected: false,
		},
		{
			name:     "case-sensitive",
			objPath:  []string{"identity", "user label 1"},
			wanted:   []string{"User Label 1"},
			expected: false,
		},
		{
			name:     "spaces preserved in segments",
			objPath:  []string{"INPUT.SDI", "CHANNEL 01", "Direction"},
			wanted:   []string{"CHANNEL 01", "Direction"},
			expected: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := matchPathSuffix(c.objPath, c.wanted)
			if got != c.expected {
				t.Errorf("matchPathSuffix(%v, %v) = %t; want %t",
					c.objPath, c.wanted, got, c.expected)
			}
		})
	}
}
