//go:build integration

// The per-type fixtures, re-rendered and compared.
//
// Each folder under testdata/protocol_types holds a slice of real traffic and
// the dissector's rendering of it. Committing the rendering is only worth
// anything if something checks it: this re-renders each capture with the
// current dissector and fails when the result differs from what was committed.
//
// So a change to the dissector that alters how a real device's bytes are drawn
// shows up as a diff in review, in the words a reader will actually see, rather
// than as a silent change to a tool nobody runs until they are debugging a
// plant at three in the morning.
//
// Skips when tshark is not installed. Run with:
//
//	go test -tags integration ./internal/snell-rollcall/integration/... -run ProtocolTypes
//
// To accept a deliberate change, regenerate the tree the folder's README
// documents and commit it alongside the reason.

package rollcall_integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const protocolTypesRel = "../testdata/protocol_types"

// renderTree draws one capture the way the committed tree was drawn: the
// RollCall layer only, so a Wireshark that decodes TCP differently does not
// fail a test about RollCall.
func renderTree(t *testing.T, pcap string) string {
	t.Helper()

	cmd := exec.Command(findTshark(t),
		"-r", pcap,
		"-X", "lua_script:"+mustAbs(t, dissectorRel),
		"-V", "-O", "dhs_snell_rollcall")

	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("tshark %s: %v\nstderr: %s", pcap, err, stderr.String())
	}
	if strings.Contains(stdout.String(), "Lua Error") {
		t.Fatalf("the dissector raised a Lua error on %s:\n%s", pcap, stdout.String())
	}
	return stdout.String()
}

func TestProtocolTypesStillRenderAsCommitted(t *testing.T) {
	root := mustAbs(t, protocolTypesRel)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read %s: %v", root, err)
	}

	var checked int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(root, name)
			pcap := filepath.Join(dir, "capture.pcapng")
			if _, err := os.Stat(pcap); err != nil {
				t.Fatalf("%s has no capture.pcapng", name)
			}

			// A fixture nobody documented is a fixture nobody can review.
			if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
				t.Errorf("%s has no README.md saying what the bytes are", name)
			}

			wantBytes, err := os.ReadFile(filepath.Join(dir, "tshark.tree"))
			if err != nil {
				t.Fatalf("%s has no committed tree: %v", name, err)
			}

			// Line endings belong to whoever checked the file out.
			want := strings.ReplaceAll(string(wantBytes), "\r\n", "\n")
			got := strings.ReplaceAll(renderTree(t, pcap), "\r\n", "\n")
			if got == want {
				return
			}

			// Say WHICH line moved. A whole-tree diff of a hundred lines is
			// something a reader skips.
			wantLines := strings.Split(want, "\n")
			gotLines := strings.Split(got, "\n")
			for i := 0; i < len(wantLines) || i < len(gotLines); i++ {
				var w, g string
				if i < len(wantLines) {
					w = wantLines[i]
				}
				if i < len(gotLines) {
					g = gotLines[i]
				}
				if w != g {
					t.Fatalf("%s renders differently at line %d:\n  committed: %q\n  now:       %q\n"+
						"If the change is deliberate, regenerate the tree (see "+
						"testdata/protocol_types/README.md) and commit it with the reason.",
						name, i+1, w, g)
				}
			}
			t.Fatalf("%s renders differently but line by line it does not; "+
				"the trailing bytes differ", name)
		})
		checked++
	}

	// A directory that has quietly emptied would otherwise pass in silence.
	if checked < 10 {
		t.Errorf("%d type fixtures were checked; the set was committed with 11", checked)
	}
}
