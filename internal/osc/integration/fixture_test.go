//go:build integration

package osc_integration

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"dhs/internal/osc/codec"
)

// Golden OSC wire fixtures, produced by the real osc codec encode path
// (codec.Message.Encode / codec.Bundle.Encode) — NEVER hand-fabricated
// wire bytes. The committed *.bin files under testdata/fixtures/ are the
// byte-exact output of the specs below; this test re-encodes each spec
// and asserts the committed bytes still match, so a codec change can
// never silently drift the fixtures.
//
// Run with DHS_REGEN_FIXTURES=1 to (re)write the fixtures after an
// intentional codec change:
//
//	DHS_REGEN_FIXTURES=1 go test -tags integration \
//	  ./internal/osc/integration/ -run TestOSCFixtures
//
// Then run WITHOUT the env var to confirm the drift-guard passes:
//
//	go test -tags integration ./internal/osc/integration/ -run TestOSCFixtures

const fixtureDir = "../testdata/fixtures"

// oscFixture pairs a committed golden file with the codec packet that
// produces it. encode() returns the real codec wire bytes.
type oscFixture struct {
	file   string
	encode func() ([]byte, error)
}

// oscFixtures is the committed set. Each entry is REAL — its bytes come
// straight from the codec encoder for the documented spec.
//
//   - message_all_tags.bin: a single OSC 1.0 Message exercising the
//     core (i f s b) + extended (h d t S c r m) type tags in one
//     address/type-tag/args packet.
//   - message_v11_payloadless.bin: an OSC 1.1 Message exercising the
//     payload-less tags T F N I alongside an int32, so the fixture pins
//     the 1.1 tag-string additions.
//   - bundle_timetag.bin: an OSC Bundle carrying a non-immediate NTP
//     timetag and two element Messages (the natural "bundle with a time
//     tag" fixture for a push protocol).
var oscFixtures = []oscFixture{
	{
		file: "message_all_tags.bin",
		encode: func() ([]byte, error) {
			return codec.Message{
				Address: "/mixer/fader1",
				Args: []codec.Arg{
					codec.Int32(42),
					codec.Float32(0.75),
					codec.String("PGM"),
					codec.Blob([]byte{0xDE, 0xAD, 0xBE, 0xEF}),
					codec.Int64(0x0102030405060708),
					codec.Float64(3.14159265358979),
					codec.Timetag(0x0000000100000000), // 1 second past the OSC epoch
					codec.Symbol("sym"),
					codec.Char('Q'),
					codec.RGBA([]byte{0x11, 0x22, 0x33, 0xFF}),
					codec.MIDI([]byte{0x00, 0x90, 0x40, 0x7F}),
				},
			}.Encode()
		},
	},
	{
		file: "message_v11_payloadless.bin",
		encode: func() ([]byte, error) {
			return codec.Message{
				Address: "/q/go",
				Args: []codec.Arg{
					codec.True(),
					codec.False(),
					codec.Nil(),
					codec.Infinitum(),
					codec.Int32(7),
				},
			}.Encode()
		},
	},
	{
		file: "bundle_timetag.bin",
		encode: func() ([]byte, error) {
			return codec.Bundle{
				Timetag: 0x0000000100000000, // 1s past the OSC epoch (not "immediately")
				Elements: []codec.Packet{
					codec.Message{Address: "/ch/1/mute", Args: []codec.Arg{codec.True()}},
					codec.Message{Address: "/ch/2/label", Args: []codec.Arg{codec.String("Host Mic")}},
				},
			}.Encode()
		},
	},
}

func TestOSCFixtures(t *testing.T) {
	regen := os.Getenv("DHS_REGEN_FIXTURES") == "1"
	if regen {
		if err := os.MkdirAll(fixtureDir, 0o755); err != nil {
			t.Fatalf("mkdir fixtures: %v", err)
		}
	}

	for _, fx := range oscFixtures {
		fx := fx
		t.Run(fx.file, func(t *testing.T) {
			want, err := fx.encode()
			if err != nil {
				t.Fatalf("codec encode: %v", err)
			}
			path := filepath.Join(fixtureDir, fx.file)

			if regen {
				if err := os.WriteFile(path, want, 0o644); err != nil {
					t.Fatalf("write fixture: %v", err)
				}
				t.Logf("regenerated %s (%d bytes)", path, len(want))
				return
			}

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read committed fixture %s: %v "+
					"(regenerate with DHS_REGEN_FIXTURES=1)", path, err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("committed %s is stale — does not match the codec "+
					"encode output. Regenerate with DHS_REGEN_FIXTURES=1.\n"+
					"  committed: %x\n  codec:     %x", fx.file, got, want)
			}
		})
	}
}

// TestOSCFixtures_RoundTrip proves the committed fixtures are REAL OSC
// wire bytes: each decodes cleanly through the codec back to a packet
// with no compliance notes. A hand-fabricated or corrupt fixture would
// fail to decode or carry deviation notes here.
func TestOSCFixtures_RoundTrip(t *testing.T) {
	cases := []struct {
		file    string
		isMsg   bool
		address string
		nArgs   int
	}{
		{"message_all_tags.bin", true, "/mixer/fader1", 11},
		{"message_v11_payloadless.bin", true, "/q/go", 5},
	}
	for _, c := range cases {
		c := c
		t.Run(c.file, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join(fixtureDir, c.file))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			m, err := codec.DecodeMessage(b)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(m.Notes) != 0 {
				t.Errorf("unexpected compliance notes: %+v", m.Notes)
			}
			if m.Address != c.address {
				t.Errorf("address = %q, want %q", m.Address, c.address)
			}
			if len(m.Args) != c.nArgs {
				t.Errorf("args = %d, want %d", len(m.Args), c.nArgs)
			}
		})
	}

	t.Run("bundle_timetag.bin", func(t *testing.T) {
		b, err := os.ReadFile(filepath.Join(fixtureDir, "bundle_timetag.bin"))
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}
		bn, err := codec.DecodeBundle(b)
		if err != nil {
			t.Fatalf("decode bundle: %v", err)
		}
		if len(bn.Notes) != 0 {
			t.Errorf("unexpected compliance notes: %+v", bn.Notes)
		}
		if bn.Timetag != 0x0000000100000000 {
			t.Errorf("timetag = %#x, want %#x", bn.Timetag, uint64(0x0000000100000000))
		}
		if len(bn.Elements) != 2 {
			t.Fatalf("elements = %d, want 2", len(bn.Elements))
		}
	})
}
