package fixture_parity_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEmberPlusPerTypeFixtures asserts the integrity of per-type fixtures:
//   - every expected type directory exists under internal/emberplus/testdata/protocol_types/
//   - each directory ships capture.pcapng + tshark.tree + README.md
//   - the frozen tshark.tree contains the APPLICATION tag(s) that define the type
//
// Byte-exact parity against a live tshark run is verified manually via
// `scripts/fixturize.sh` when the dissector changes — doing it in CI would
// require a matching Wireshark install and the Lua dissector loaded, which
// is brittle across runners.
func TestEmberPlusPerTypeFixtures(t *testing.T) {
	base := "../../../internal/emberplus/testdata/protocol_types"

	cases := []struct {
		dir        string
		appTags    []string // APPLICATION tags (any one must appear)
		extraCheck string   // optional substring that must appear
	}{
		{"root_node", []string{"APPLICATION 0] Root", "APPLICATION 3] Node"}, ""},
		{"qualified_node", []string{"APPLICATION 10] QualifiedNode"}, ""},
		{"parameter", []string{"APPLICATION 1] Parameter"}, ""},
		{"qualified_parameter", []string{"APPLICATION 9] QualifiedParameter"}, ""},
		{"matrix", []string{"APPLICATION 13] Matrix"}, ""},
		{"qualified_matrix", []string{"APPLICATION 17] QualifiedMatrix"}, ""},
		{"matrix_connection", []string{"APPLICATION 16] Connection"}, ""},
		{"label", []string{"APPLICATION 18] Label"}, ""},
		{"stream_collection", []string{"APPLICATION 6] StreamCollection", "APPLICATION 5] StreamEntry"}, ""},
		{"command_get_directory", []string{"APPLICATION 2] Command"}, "Value (int): 32"},
		{"command_subscribe", []string{"APPLICATION 2] Command"}, "Value (int): 30"},
		{"command_unsubscribe", []string{"APPLICATION 2] Command"}, "Value (int): 31"},
		{"function_invoke", []string{"APPLICATION 19] Function", "APPLICATION 22] Invocation"}, "Value (int): 33"},
		{"invocation_result", []string{"APPLICATION 23] InvocationResult"}, ""},
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

			for _, tag := range tc.appTags {
				if strings.Contains(tree, tag) {
					goto tagOK
				}
			}
			t.Fatalf("tshark.tree missing any of %v", tc.appTags)
		tagOK:

			if tc.extraCheck != "" && !strings.Contains(tree, tc.extraCheck) {
				t.Fatalf("tshark.tree missing extra check %q", tc.extraCheck)
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
