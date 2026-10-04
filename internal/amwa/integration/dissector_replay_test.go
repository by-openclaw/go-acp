//go:build integration

package amwa_integration

// Replay regression for wireshark/dhs_nmos.lua: every committed capture
// of the replay set, read again through the dissector, gives the
// committed tree. The dissector is a second reading of the same wire as
// the Go code; this is what keeps a change to it honest.
//
// Needs a tshark that loads Lua (the control node's does; the tooling
// host's does not, and this test then says so rather than passing):
//
//	go test -tags integration ./internal/amwa/integration/ -run DissectorReplay

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDissectorReplayGivesTheCommittedTrees(t *testing.T) {
	tshark, err := exec.LookPath("tshark")
	if err != nil {
		t.Skip("tshark is not installed on this host")
	}
	root := repoRoot(t)
	lua := filepath.Join(root, "internal", "amwa", "wireshark", "dhs_nmos.lua")
	captures, _ := filepath.Glob(filepath.Join(root, "internal", "amwa", "testdata", "protocol_types", "*", "capture.pcapng"))
	if len(captures) < 10 {
		t.Fatalf("FAIL-real: the replay set holds %d captures", len(captures))
	}
	// A tshark built without Lua, or one that refuses scripts, does not
	// know our fields and says so.
	if out, err := exec.Command(tshark, "-X", "lua_script:"+lua, "-r", captures[0], "-T", "fields", "-e", "dhs_nmos_http.api").CombinedOutput(); err != nil || bytes.Contains(out, []byte("aren't valid")) {
		t.Skip("this tshark does not load Lua dissectors")
	}

	captures = append(captures, filepath.Join(root, "internal", "amwa", "testdata", "fixtures", "node-registers.pcapng"))
	for _, capture := range captures {
		treePath := filepath.Join(filepath.Dir(capture), "tshark.tree")
		if strings.HasSuffix(capture, "node-registers.pcapng") {
			treePath = strings.TrimSuffix(capture, ".pcapng") + ".tree"
		}
		want, err := os.ReadFile(treePath)
		if err != nil {
			t.Errorf("FAIL-real: %v", err)
			continue
		}
		got, err := exec.Command(tshark, "-X", "lua_script:"+lua, "-r", capture, "-O", "dhs_nmos,dhs_nmos_http").Output()
		if err != nil {
			t.Errorf("FAIL-real: tshark on %s: %v", capture, err)
			continue
		}
		// The NMOS lines are ours; the rest of a tree is the tshark build's.
		if a, b := nmosLines(got), nmosLines(want); a != b {
			t.Errorf("FAIL-real: %s — the dissector no longer says what the committed tree says\n--- now\n%s\n--- committed\n%s",
				filepath.Base(filepath.Dir(capture)), tail(a), tail(b))
		}
	}
	t.Logf("PASS: %d captures replayed through dhs_nmos.lua, each as its committed tree", len(captures))
}

// nmosLines keeps what our dissector wrote: its layers and their fields.
func nmosLines(tree []byte) string {
	var out []string
	keep := false
	for _, line := range strings.Split(strings.ReplaceAll(string(tree), "\r\n", "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "AMWA NMOS"):
			keep = true
		case !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t"):
			keep = false
		}
		if keep {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
