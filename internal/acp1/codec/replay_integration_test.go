package codec

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestReplay_RealFrames is the codec INTEGRATION oracle: it replays real ACP1
// wire frames captured from the Axon Synapse Simulator (a vendor emulator, not
// our own provider — so it's an independent oracle per the test-strategy
// rules) and asserts the stdlib codec decodes every one, that non-error frames
// re-encode byte-exact (Encode is the exact inverse of Decode on real bytes),
// and that every getObject reply's value decodes into a typed object.
//
// Device-free and CI-runnable: the captured corpus lives in
// testdata/replay/synapse_walk.jsonl, so this runs on every push without a
// VPN or live device — closing the codec integration tier that the
// ACP1_TEST_HOST-gated smoke test cannot cover in CI.
//
// Regenerate the corpus with:
//
//	dhs consumer acp1 walk <emulator-ip> --slot N --transport udp --capture <dir>
//
// then concatenate the raw.acp1.jsonl frames.
func TestReplay_RealFrames(t *testing.T) {
	f, err := os.Open("testdata/replay/synapse_walk.jsonl")
	if err != nil {
		t.Fatalf("open corpus: %v", err)
	}
	defer func() { _ = f.Close() }()

	type frame struct {
		Dir string `json:"dir"`
		Hex string `json:"hex"`
	}

	var decoded, roundTripped, objects int
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		var fr frame
		if err := json.Unmarshal([]byte(text), &fr); err != nil {
			t.Fatalf("line %d: bad json: %v", line, err)
		}
		raw, err := hex.DecodeString(fr.Hex)
		if err != nil {
			t.Fatalf("line %d: bad hex: %v", line, err)
		}

		msg, err := Decode(raw)
		if err != nil {
			t.Fatalf("line %d: Decode real frame failed: %v (hex=%s)", line, err, fr.Hex)
		}
		decoded++

		// Encode is the exact inverse of Decode for request/reply frames.
		// (Error frames carry only MCODE in MDATA, so they are exempt from
		// the byte-exact check — the wire may echo ObjGroup/ObjID that
		// Encode intentionally drops per spec p.29.)
		if msg.MType != MTypeError {
			out, err := msg.Encode()
			if err != nil {
				t.Fatalf("line %d: re-Encode failed: %v", line, err)
			}
			if !bytes.Equal(out, raw) {
				t.Fatalf("line %d: round-trip mismatch\n have %x\n want %x", line, out, raw)
			}
			roundTripped++
		}

		// Every getObject reply must decode into a typed object.
		if msg.MType == MTypeReply && msg.MCode == byte(MethodGetObject) {
			if _, err := DecodeObject(msg.Value); err != nil {
				t.Fatalf("line %d: DecodeObject(getObject reply) failed: %v (value=%x)", line, err, msg.Value)
			}
			objects++
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}

	// Sanity floor: the committed corpus is a multi-slot walk, so it must
	// carry a substantial number of frames and decoded objects.
	if decoded < 200 {
		t.Fatalf("only %d frames decoded — corpus too small or unreadable", decoded)
	}
	if objects < 50 {
		t.Fatalf("only %d getObject replies decoded — corpus lacks object coverage", objects)
	}
	t.Logf("codec replay: %d frames decoded, %d round-tripped, %d objects decoded", decoded, roundTripped, objects)
}
