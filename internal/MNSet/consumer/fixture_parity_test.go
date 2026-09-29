package mnset

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ADR-0025 deliverable 6: every resource kind the module serves has a
// replay set — the raw capture as the wire carried it (capture.pcapng,
// one TCP conversation cut from a live walk of the FusioN6), the frozen
// tshark tree of that capture through wireshark/dhs_mnset.lua, the
// ADR-0028 exchange (wire.jsonl) and the body the decoder reads. The
// tree is the dissector's contract: if dhs_mnset.lua stops naming the
// resource or the status, this fails before anyone opens Wireshark.
//
// Regenerate (control node, dissector installed, module reachable):
//
//	dumpcap -i eth0 -f "host <module> and tcp port 80" -w walk.pcapng &
//	dhs consumer mnset walk <module> --slot 0
//	scripts/fixturize.sh walk.pcapng internal/MNSet/testdata/protocol_types/<kind> <frames of one tcp.stream>
func TestEveryProtocolTypeHasAFrozenDissectorTree(t *testing.T) {
	root := filepath.Join(replayRoot, "protocol_types")
	kinds, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, k := range kinds {
		if !k.IsDir() {
			continue
		}
		n++
		dir := filepath.Join(root, k.Name())
		for _, f := range []string{"capture.pcapng", "tshark.tree", "wire.jsonl", "body.txt", "README.md"} {
			if st, err := os.Stat(filepath.Join(dir, f)); err != nil || st.Size() == 0 {
				t.Errorf("%s: %s missing or empty", k.Name(), f)
			}
		}
		tree, err := os.ReadFile(filepath.Join(dir, "tshark.tree"))
		if err != nil {
			continue
		}
		uri := requestURI(t, filepath.Join(dir, "wire.jsonl"))
		for _, want := range []string{
			"Riedel MuoN/FusioN REST (dhs)",
			"Direction: request",
			"Direction: response",
			"Status: 200",
			"URI: " + uri,
			"Arrival Time: [frozen]",
		} {
			if !strings.Contains(string(tree), want) {
				t.Errorf("%s: tshark.tree lacks %q", k.Name(), want)
			}
		}
	}
	if n < 16 {
		t.Errorf("%d protocol types — the FusioN6 walk serves 16 kinds", n)
	}
}

// requestURI is the request target of the first exchange in a
// wire.jsonl — the same exchange the capture was cut for.
func requestURI(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	for sc.Scan() {
		var e struct {
			Dir string `json:"dir"`
			Hex string `json:"hex"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.Dir != "tx" {
			continue
		}
		raw, err := hex.DecodeString(e.Hex)
		if err != nil {
			t.Fatalf("%s: tx hex: %v", path, err)
		}
		// "GET /emsfp/node/v1/... HTTP/1.1"
		if parts := strings.Fields(strings.SplitN(string(raw), "\r\n", 2)[0]); len(parts) >= 2 {
			return parts[1]
		}
	}
	t.Fatalf("%s: no request line", path)
	return ""
}
