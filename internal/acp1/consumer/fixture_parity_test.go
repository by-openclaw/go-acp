package acp1_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestACP1PerTypeFixtures asserts fixture integrity for the ACP1 per-type
// library: every expected type directory exists, each ships pcap+tree+README,
// and the frozen tshark.tree contains the protocol fields that define the
// type (acpv1.obj.type, acpv1.mtype, acpv1.err_object).
func TestACP1PerTypeFixtures(t *testing.T) {
	base := "../testdata/protocol_types"

	cases := []struct {
		dir     string
		markers []string // substrings that must all appear
	}{
		{"root", []string{"Object Type: root (0)"}},
		{"integer", []string{"Object Type: integer (1)"}},
		{"ip_address", []string{"Object Type: ipaddr (2)"}},
		{"float", []string{"Object Type: float (3)"}},
		{"enumerated", []string{"Object Type: enum (4)"}},
		{"string", []string{"Object Type: string (5)"}},
		{"frame_status", []string{"Object Group: frame (6)"}},
		{"alarm", []string{"Object Type: alarm (7)"}},
		{"long", []string{"Object Type: long (9)"}},
		{"byte", []string{"Object Type: byte (10)"}},
		{"request", []string{"Message Type: Request (1)"}},
		{"reply", []string{"Message Type: Reply (2)"}},
		{"error", []string{"Message Type: Error (3)", "Object Error"}},
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
