//go:build integration

// The replay set, re-rendered and compared.
//
// Each folder under testdata/protocol_types holds a slice of real traffic
// with an EVS Neuron and the dissector's rendering of it. This re-renders
// each capture with the current dhs_probel_sw08p.lua and fails when the
// result differs from the committed tree — so a change to the dissector
// that alters how a real matrix's bytes are drawn shows up in review.
//
// Skips when tshark is not installed (the control node's loads Lua):
//
//	go test -tags integration ./internal/probel-sw08p/integration/ -run ProtocolTypes
//
// PROBEL_SW08P_TEST_REPO names the repository when the test binary runs
// outside it.
package probelsw08p_integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProtocolTypesStillRenderAsCommitted(t *testing.T) {
	tshark, err := exec.LookPath("tshark")
	if err != nil {
		t.Skip("tshark is not installed on this host")
	}
	root := strings.TrimSpace(os.Getenv("PROBEL_SW08P_TEST_REPO"))
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		root = filepath.Join(wd, "..", "..", "..")
	}
	lua := filepath.Join(root, "internal", "probel-sw08p", "wireshark", "dhs_probel_sw08p.lua")
	captures, _ := filepath.Glob(filepath.Join(root, "internal", "probel-sw08p", "testdata", "protocol_types", "*", "capture.pcapng"))
	if len(captures) < 15 {
		t.Fatalf("FAIL-real: the replay set holds %d capture(s) under %s", len(captures), root)
	}
	for _, pcap := range captures {
		name := filepath.Base(filepath.Dir(pcap))
		cmd := exec.Command(tshark, "-r", pcap, "-X", "lua_script:"+lua, "-V", "-O", "dhs_probel_sw08p")
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("%s: tshark: %v\n%s", name, err, stderr.String())
		}
		got := stdout.String()
		if strings.Contains(got, "Lua Error") {
			t.Fatalf("%s: the dissector raised a Lua error:\n%s", name, got)
		}
		if !strings.Contains(got, "Probel") {
			t.Skipf("%s: this tshark did not load the Lua dissector (built without Lua?)", name)
		}
		want, err := os.ReadFile(filepath.Join(filepath.Dir(pcap), "tshark.tree"))
		if err != nil {
			t.Fatal(err)
		}
		if normaliseTree(got) != normaliseTree(string(want)) {
			t.Errorf("%s: the dissector no longer renders this capture as committed.\n--- committed\n%s\n--- now\n%s", name, want, got)
		}
	}
	t.Logf("PASS: %d captures replayed through dhs_probel_sw08p.lua, each as its committed tree", len(captures))
}

// normaliseTree drops what differs between hosts without being the
// dissector's doing: line endings and trailing blanks.
func normaliseTree(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
