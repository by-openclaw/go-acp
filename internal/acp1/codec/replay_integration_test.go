package codec

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// replayCorpusGlob discovers every committed capture under the replay corpus.
// Layout: testdata/replay/<device>/*.jsonl — one directory per device or
// firmware (e.g. synapse-sim, axon-rrs18@1601). Adding a new device is just
// dropping a file in; no code change. See testdata/replay/INDEX.md.
const replayCorpusGlob = "testdata/replay/*/*.jsonl"

type replayFrame struct {
	Dir string `json:"dir"`
	Hex string `json:"hex"`
}

// readReplayCorpus parses one device capture file into frames, skipping blank
// lines and # comments.
func readReplayCorpus(t *testing.T, path string) []replayFrame {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open corpus %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()

	var frames []replayFrame
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		var fr replayFrame
		if err := json.Unmarshal([]byte(text), &fr); err != nil {
			t.Fatalf("%s:%d: bad json: %v", path, line, err)
		}
		frames = append(frames, fr)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return frames
}

// TestReplay_RealFrames is the codec INTEGRATION oracle. It replays real ACP1
// wire frames captured from actual devices/emulators (an independent oracle —
// vendor hardware, not our own provider) and asserts the stdlib codec decodes
// every one, that non-error frames re-encode byte-exact (Encode is the exact
// inverse of Decode on real bytes), and that every getObject reply decodes
// into a typed object.
//
// Device-free and CI-runnable: the corpus lives in testdata/replay/<device>/,
// auto-discovered here. Each device file is a subtest, so a regression is
// attributed to a specific device. Grow the corpus by capturing more devices —
// see testdata/replay/CAPTURING.md.
func TestReplay_RealFrames(t *testing.T) {
	files, err := filepath.Glob(replayCorpusGlob)
	if err != nil {
		t.Fatalf("glob corpus: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no replay corpus found under %s", replayCorpusGlob)
	}

	var grandFrames, grandObjects int
	for _, path := range files {
		// device name = parent dir (testdata/replay/<device>/file.jsonl).
		device := filepath.Base(filepath.Dir(path))
		t.Run(device, func(t *testing.T) {
			frames := readReplayCorpus(t, path)
			var decoded, roundTripped, objects int
			for i, fr := range frames {
				raw, err := hex.DecodeString(fr.Hex)
				if err != nil {
					t.Fatalf("frame %d: bad hex: %v", i, err)
				}
				msg, err := Decode(raw)
				if err != nil {
					t.Fatalf("frame %d: Decode real frame failed: %v (hex=%s)", i, err, fr.Hex)
				}
				decoded++

				// Encode is the exact inverse of Decode for request/reply
				// frames. Error frames carry only MCODE in MDATA, so they are
				// exempt from the byte-exact check (the wire may echo
				// ObjGroup/ObjID that Encode intentionally drops, spec p.29).
				if msg.MType != MTypeError {
					out, err := msg.Encode()
					if err != nil {
						t.Fatalf("frame %d: re-Encode failed: %v", i, err)
					}
					if !bytes.Equal(out, raw) {
						t.Fatalf("frame %d: round-trip mismatch\n have %x\n want %x", i, out, raw)
					}
					roundTripped++
				}

				if msg.MType == MTypeReply && msg.MCode == byte(MethodGetObject) {
					if _, err := DecodeObject(msg.Value); err != nil {
						t.Fatalf("frame %d: DecodeObject(getObject reply) failed: %v (value=%x)", i, err, msg.Value)
					}
					objects++
				}
			}
			if decoded == 0 {
				t.Fatalf("%s: corpus empty or unreadable", device)
			}
			t.Logf("%s: %d frames decoded, %d round-tripped, %d objects", device, decoded, roundTripped, objects)
			grandFrames += decoded
			grandObjects += objects
		})
	}

	// Sanity floor across the whole corpus.
	if grandFrames < 200 {
		t.Fatalf("only %d frames across corpus — too small", grandFrames)
	}
	if grandObjects < 50 {
		t.Fatalf("only %d getObject replies across corpus — lacks object coverage", grandObjects)
	}
}
