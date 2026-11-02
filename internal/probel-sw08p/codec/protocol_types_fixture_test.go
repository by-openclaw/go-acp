package codec

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Every frame of the replay set — real traffic with an EVS Neuron, one
// folder per command under ../testdata/protocol_types — goes through the
// framer: a link ACK is a link ACK, anything else unpacks whole (framing,
// byte count, checksum) and packs back to the bytes the device or the
// consumer put on the wire. Expected bytes from a real matrix, not from
// this code.
func TestReplaySetFramesUnpackAndPackBack(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "testdata", "protocol_types", "*", "frames.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 15 {
		t.Fatalf("the replay set holds %d trace(s), want the 17 command folders", len(files))
	}
	frames := 0
	for _, file := range files {
		fh, err := os.Open(file)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
		line := 0
		for sc.Scan() {
			line++
			var rec struct {
				Dir string `json:"dir"`
				Hex string `json:"hex"`
			}
			if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
				t.Fatalf("%s:%d: %v", file, line, err)
			}
			raw, err := hex.DecodeString(rec.Hex)
			if err != nil {
				t.Fatalf("%s:%d: %v", file, line, err)
			}
			frames++
			if bytes.Equal(raw, PackACK()) {
				continue
			}
			f, n, err := Unpack(raw)
			if err != nil {
				t.Errorf("%s:%d (%s): %v", file, line, rec.Dir, err)
				continue
			}
			if n != len(raw) {
				t.Errorf("%s:%d (%s): %d of %d bytes are a frame", file, line, rec.Dir, n, len(raw))
			}
			if back := Pack(f); !bytes.Equal(back, raw) {
				t.Errorf("%s:%d (%s): packs back as %x, the wire carried %x", file, line, rec.Dir, back, raw)
			}
		}
		_ = fh.Close()
		if line < 2 {
			t.Errorf("%s holds %d frame(s): not an exchange", file, line)
		}
	}
	if frames < 50 {
		t.Errorf("%d frames in the whole set", frames)
	}
}
