//go:build integration

package tsl_integration

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"dhs/internal/tsl/codec"
)

// goldenFramesFixture is the committed golden wire-byte fixture for the
// TSL UMD codec. Unlike the matrix connectors (which export a canonical
// tree), TSL is a tally/UMD PUSH protocol with no walkable tree — the
// natural replay fixture is the exact on-wire frame for each version.
//
// Every line is one JSON object {version, transport, description, hex}
// where hex is the lower-case hex of bytes produced by the REAL codec
// encode path (V31Frame.Encode / V40Frame.Encode / V50Packet.Encode,
// the last optionally DLE/STX-wrapped via EncodeDLEFrame). The file is
// NEVER hand-edited — TestGoldenFrameFixtures regenerates it from the
// codec and the drift-guard asserts the committed bytes still match.
const goldenFramesFixture = "../testdata/fixtures/golden_frames.jsonl"

// goldenFrame is one committed wire sample.
type goldenFrame struct {
	Version     string `json:"version"`     // "v3.1" | "v4.0" | "v5.0"
	Transport   string `json:"transport"`   // "udp" | "tcp-dle-stx"
	Description string `json:"description"` // human scenario label
	Hex         string `json:"hex"`         // lower-case hex of the wire bytes
}

// buildGoldenFrames constructs the canonical sample set and encodes each
// through the real codec. The struct literals are the source of truth for
// the *semantics*; the bytes are derived, never fabricated.
func buildGoldenFrames(t *testing.T) []goldenFrame {
	t.Helper()

	var out []goldenFrame

	enc := func(b []byte, err error, version, transport, desc string) {
		t.Helper()
		if err != nil {
			t.Fatalf("encode %s/%s (%s): %v", version, transport, desc, err)
		}
		out = append(out, goldenFrame{
			Version:     version,
			Transport:   transport,
			Description: desc,
			Hex:         hex.EncodeToString(b),
		})
	}

	// --- v3.1 (18-byte UDP frame, §3.0) -------------------------------
	v31 := codec.V31Frame{
		Address:    7,
		Tally1:     true,
		Tally4:     true,
		Brightness: codec.BrightnessFull,
		Text:       "PGM LIVE",
	}
	b, err := v31.Encode()
	enc(b, err, "v3.1", "udp", "addr=7 tally1+tally4 brightness=full text=\"PGM LIVE\"")

	// --- v4.0 (v3.1 + CHKSUM + VBC + 2-byte XDATA, §4.0) --------------
	v40 := codec.V40Frame{
		V31: codec.V31Frame{
			Address:    11,
			Tally1:     true,
			Tally3:     true,
			Brightness: codec.BrightnessFull,
			Text:       "CAM 1 ISO",
		},
		DisplayLeft:  codec.XByte{LH: codec.TallyRed, Text: codec.TallyGreen, RH: codec.TallyAmber},
		DisplayRight: codec.XByte{LH: codec.TallyGreen, Text: codec.TallyRed, RH: codec.TallyOff},
	}
	b, err = v40.Encode()
	enc(b, err, "v4.0", "udp", "addr=11 displayL=red/green/amber displayR=green/red/off text=\"CAM 1 ISO\"")

	// --- v5.0 single-DMSG ASCII over UDP (§5.0) -----------------------
	v50ascii := codec.V50Packet{
		Screen: 1,
		DMSGs: []codec.DMSG{
			{
				Index:      11,
				LH:         codec.TallyRed,
				TextTally:  codec.TallyGreen,
				RH:         codec.TallyAmber,
				Brightness: codec.BrightnessFull,
				Text:       "CAM 1",
			},
		},
	}
	b, err = v50ascii.Encode()
	enc(b, err, "v5.0", "udp", "screen=1 single-DMSG index=11 lh=red/text=green/rh=amber ascii text=\"CAM 1\"")

	// --- v5.0 multi-DMSG UTF-16LE over UDP ----------------------------
	v50utf16 := codec.V50Packet{
		UTF16LE: true,
		DMSGs: []codec.DMSG{
			{Index: 0, LH: codec.TallyRed, Text: "CAMÉRA"},
			{Index: 1, LH: codec.TallyGreen, Text: "日本語"},
			{Index: 2, LH: codec.TallyAmber, Text: "HOLA"},
		},
	}
	b, err = v50utf16.Encode()
	enc(b, err, "v5.0", "udp", "screen=0 multi-DMSG utf-16le text=[CAMÉRA,日本語,HOLA]")

	// --- v5.0 over TCP with DLE/STX wrapper + 0xFE byte-stuffing ------
	v50tcp := codec.V50Packet{
		Screen: 0xFEFE, // forces 0xFE → 0xFE 0xFE stuffing in the wrapper
		DMSGs: []codec.DMSG{
			{Index: 0xFE01, LH: codec.TallyRed, Brightness: codec.BrightnessFull, Text: "STUFF"},
		},
	}
	raw, err := v50tcp.Encode()
	if err != nil {
		t.Fatalf("encode v5.0 tcp body: %v", err)
	}
	wrapped := codec.EncodeDLEFrame(raw)
	enc(wrapped, nil, "v5.0", "tcp-dle-stx", "screen=0xFEFE dmsg index=0xFE01 lh=red — exercises DLE/STX wrap + 0xFE stuffing")

	return out
}

// marshalGoldenFrames renders the sample set to the committed JSONL
// representation (one object per line, trailing newline).
func marshalGoldenFrames(frames []goldenFrame) ([]byte, error) {
	var buf bytes.Buffer
	for _, f := range frames {
		line, err := json.Marshal(f)
		if err != nil {
			return nil, err
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

// TestGoldenFrameFixtures is the TSL sibling of the matrix connectors'
// TestMatrixTreeExportFixture drift-guard. It asserts the committed
// golden_frames.jsonl is byte-identical to what the real codec emits for
// the canonical sample set, so the fixture can never drift from the
// codec. Regenerate after an intentional wire change with:
//
//	DHS_REGEN_FIXTURES=1 go test -tags integration \
//	  ./internal/tsl/integration/ -run TestGoldenFrameFixtures
func TestGoldenFrameFixtures(t *testing.T) {
	frames := buildGoldenFrames(t)
	want, err := marshalGoldenFrames(frames)
	if err != nil {
		t.Fatalf("marshal golden frames: %v", err)
	}

	if os.Getenv("DHS_REGEN_FIXTURES") == "1" {
		if err := os.MkdirAll(filepath.Dir(goldenFramesFixture), 0o755); err != nil {
			t.Fatalf("mkdir fixtures: %v", err)
		}
		if err := os.WriteFile(goldenFramesFixture, want, 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		t.Logf("regenerated %s (%d frames, %d bytes)", goldenFramesFixture, len(frames), len(want))
		return
	}

	got, err := os.ReadFile(goldenFramesFixture)
	if err != nil {
		t.Fatalf("read committed fixture %s: %v "+
			"(regenerate with DHS_REGEN_FIXTURES=1)", goldenFramesFixture, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("committed %s is stale — does not match codec-encoded golden "+
			"frames. Regenerate with DHS_REGEN_FIXTURES=1.", goldenFramesFixture)
	}
}

// TestGoldenFramesRoundTrip proves every committed golden frame decodes
// back through the real codec to a structurally valid frame with NO
// compliance notes — i.e. the fixtures are clean, spec-conformant wire
// samples, not hand-fabricated bytes. This is the "fixtures must be REAL
// (round-trip through the codec)" guarantee.
func TestGoldenFramesRoundTrip(t *testing.T) {
	data, err := os.ReadFile(goldenFramesFixture)
	if err != nil {
		t.Fatalf("read fixture: %v (regenerate with DHS_REGEN_FIXTURES=1)", err)
	}
	for i, line := range bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var gf goldenFrame
		if err := json.Unmarshal(line, &gf); err != nil {
			t.Fatalf("line %d: unmarshal: %v", i, err)
		}
		raw, err := hex.DecodeString(gf.Hex)
		if err != nil {
			t.Fatalf("line %d: hex decode: %v", i, err)
		}
		switch {
		case gf.Version == "v3.1":
			f, err := codec.DecodeV31(raw)
			if err != nil {
				t.Errorf("line %d (%s): DecodeV31: %v", i, gf.Description, err)
				continue
			}
			if len(f.Notes) != 0 {
				t.Errorf("line %d (%s): clean frame has notes: %+v", i, gf.Description, f.Notes)
			}
		case gf.Version == "v4.0":
			f, err := codec.DecodeV40(raw)
			if err != nil {
				t.Errorf("line %d (%s): DecodeV40: %v", i, gf.Description, err)
				continue
			}
			if len(f.Notes) != 0 || len(f.V31.Notes) != 0 {
				t.Errorf("line %d (%s): clean frame has notes: v31=%+v v40=%+v",
					i, gf.Description, f.V31.Notes, f.Notes)
			}
		case gf.Version == "v5.0" && gf.Transport == "tcp-dle-stx":
			dec := codec.NewDLEStreamDecoder(bytes.NewReader(raw), 0)
			body, err := dec.ReadFrame()
			if err != nil {
				t.Errorf("line %d (%s): DLE ReadFrame: %v", i, gf.Description, err)
				continue
			}
			if _, err := codec.DecodeV50(body); err != nil {
				t.Errorf("line %d (%s): DecodeV50: %v", i, gf.Description, err)
			}
		case gf.Version == "v5.0":
			p, err := codec.DecodeV50(raw)
			if err != nil {
				t.Errorf("line %d (%s): DecodeV50: %v", i, gf.Description, err)
				continue
			}
			// The only note a clean v5.0 UTF-16LE frame legitimately
			// carries is tsl_charset_transcode (informational, fired per
			// transcoded DMSG — not a spec deviation). Any other note on a
			// golden sample means the bytes aren't clean.
			for _, n := range p.Notes {
				if n.Kind != "tsl_charset_transcode" {
					t.Errorf("line %d (%s): clean frame has deviation note: %+v",
						i, gf.Description, n)
				}
			}
		default:
			t.Errorf("line %d: unknown version/transport %q/%q", i, gf.Version, gf.Transport)
		}
	}
}
