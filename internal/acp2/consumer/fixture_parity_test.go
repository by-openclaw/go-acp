package acp2_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestACP2PerTypeFixtures asserts fixture integrity for the ACP2 per-type
// library: every expected fixture directory exists, each ships
// capture.pcapng + tshark.tree + README.md, and the frozen tshark.tree
// contains the marker strings that define the element type.
//
// Full coverage of the ACP2 spec elements: 6 obj types, 4 functions,
// announce, 6 error codes.
func TestACP2PerTypeFixtures(t *testing.T) {
	base := "../testdata/protocol_types"

	cases := []struct {
		dir     string
		markers []string
	}{
		// Object types — acp2_protocol.pdf §2
		{"node", []string{"Object Type: node (0)"}},
		{"preset", []string{"Object Type: preset (1)"}},
		{"enum", []string{"Object Type: enum (2)"}},
		{"number", []string{"Object Type: number (3)", "Number Type: s32 (2)"}},
		{"ipv4", []string{"Object Type: ipv4 (4)"}},
		{"string", []string{"Object Type: string (5)"}},

		// Functions — acp2_protocol.pdf §3
		{"get_version", []string{"Function: GetVersion (0)"}},
		{"get_object", []string{"Function: GetObject (1)"}},
		{"get_property", []string{"Function: GetProperty (2)"}},
		{"set_property", []string{"Function: SetProperty (3)"}},

		// Announces — acp2_protocol.pdf §4
		{"announce", []string{"Type: Announce (2)"}},

		// Error codes — acp2_protocol.pdf §4
		{"error_protocol", []string{"Type: Error (3)", "Status: Protocol error (0)"}},
		{"error_invalid_obj_id", []string{"Type: Error (3)", "Status: Invalid obj-id (1)"}},
		{"error_invalid_idx", []string{"Type: Error (3)", "Status: Invalid idx (2)"}},
		{"error_invalid_pid", []string{"Type: Error (3)", "Status: Invalid pid (3)"}},
		{"error_no_access", []string{"Type: Error (3)", "Status: No access (4)"}},
		{"error_invalid_value", []string{"Type: Error (3)", "Status: Invalid value (5)"}},
	}

	for _, tc := range cases {
		t.Run(tc.dir, func(t *testing.T) {
			dir := filepath.Join(base, tc.dir)

			for _, f := range []string{"capture.pcapng", "tshark.tree", "README.md"} {
				path := filepath.Join(dir, f)
				info, err := os.Stat(path)
				if err != nil {
					t.Fatalf("missing %s: %v", path, err)
				}
				if info.Size() == 0 {
					t.Fatalf("empty %s", path)
				}
			}

			treeBytes, err := os.ReadFile(filepath.Join(dir, "tshark.tree"))
			if err != nil {
				t.Fatalf("read tshark.tree: %v", err)
			}
			tree := string(treeBytes)

			for _, m := range tc.markers {
				if !strings.Contains(tree, m) {
					t.Fatalf("tshark.tree missing marker %q", m)
				}
			}

			readmeBytes, err := os.ReadFile(filepath.Join(dir, "README.md"))
			if err != nil {
				t.Fatalf("read README.md: %v", err)
			}
			if !strings.Contains(string(readmeBytes), "Spec") {
				t.Fatalf("README.md missing Spec heading")
			}
		})
	}
}
