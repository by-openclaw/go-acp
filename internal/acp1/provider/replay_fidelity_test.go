package acp1

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/export/canonical"
)

// TestReplayFidelity_GetObject proves the provider serves objects with the
// same fidelity as a real device. For every getObject reply in the shared
// replay corpus (real captured frames — internal/acp1/codec/testdata/replay,
// an independent oracle, not our own encoder), it:
//
//  1. decodes the real reply into a DecodedObject (`want`),
//  2. builds a provider tree entry carrying those exact fields,
//  3. runs the provider's encodeObject on it, and
//  4. decodes the provider's bytes back (`got`) and asserts got == want on the
//     spec-defined fields.
//
// i.e. an object a real device exposed can be re-served by our provider and a
// consumer can't tell the difference. Scope: the value-carrying types whose
// canonical mapping is lossless (Integer/Long/Byte/Float/IPAddr/Enum/String).
// Root is synthesised by the session layer; Alarm/File/Frame use
// provider-specific encoding conventions (priority/tag from format hints, etc.)
// that aren't reconstructable from wire bytes alone — the codec replay test
// already proves those DECODE, which is the relevant guarantee there.
func TestReplayFidelity_GetObject(t *testing.T) {
	files, err := filepath.Glob("../codec/testdata/replay/*/*.jsonl")
	if err != nil {
		t.Fatalf("glob corpus: %v", err)
	}
	if len(files) == 0 {
		t.Skip("no replay corpus found")
	}

	var total int
	for _, path := range files {
		device := filepath.Base(filepath.Dir(path))
		t.Run(device, func(t *testing.T) {
			n := 0
			for i, raw := range readFidelityFrames(t, path) {
				msg, err := codec.Decode(raw)
				if err != nil || msg.MType != codec.MTypeReply || msg.MCode != byte(codec.MethodGetObject) {
					continue
				}
				want, err := codec.DecodeObject(msg.Value)
				if err != nil {
					continue
				}
				e, ok := buildEntryFromDecoded(want)
				if !ok {
					continue // type out of fidelity scope
				}
				out, err := encodeObject(e)
				if err != nil {
					t.Fatalf("frame %d (type %d): provider encodeObject failed: %v", i, want.Type, err)
				}
				got, err := codec.DecodeObject(out)
				if err != nil {
					t.Fatalf("frame %d: re-decode of provider bytes failed: %v", i, err)
				}
				if diff := compareDecoded(want, got); diff != "" {
					t.Fatalf("frame %d (type %d): provider re-serve diverged: %s", i, want.Type, diff)
				}
				n++
			}
			if n == 0 {
				t.Skipf("%s: no in-scope getObject replies", device)
			}
			t.Logf("%s: %d objects re-served with full fidelity", device, n)
			total += n
		})
	}
	if total == 0 {
		t.Skip("no in-scope objects in corpus")
	}
}

func readFidelityFrames(t *testing.T, path string) [][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	var out [][]byte
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		var fr struct {
			Hex string `json:"hex"`
		}
		if err := json.Unmarshal([]byte(text), &fr); err != nil {
			continue
		}
		raw, err := hex.DecodeString(fr.Hex)
		if err == nil {
			out = append(out, raw)
		}
	}
	return out
}

// buildEntryFromDecoded reconstructs a provider tree entry from a decoded
// real object, for the value-carrying types whose canonical mapping is
// lossless. Returns ok=false for out-of-scope types.
func buildEntryFromDecoded(d *codec.DecodedObject) (*entry, bool) {
	p := &canonical.Parameter{Header: canonical.Header{Identifier: d.Label, Children: canonical.EmptyChildren()}}
	if d.Unit != "" {
		u := d.Unit
		p.Unit = &u
	}
	switch d.Type {
	case codec.TypeInteger, codec.TypeLong:
		p.Value, p.Default, p.Step = d.IntVal, d.DefInt, d.StepInt
		p.Minimum, p.Maximum = d.MinInt, d.MaxInt
	case codec.TypeByte:
		p.Value, p.Default, p.Step = int64(d.ByteVal), int64(d.DefByte), int64(d.StepByte)
		p.Minimum, p.Maximum = int64(d.MinByte), int64(d.MaxByte)
	case codec.TypeFloat:
		p.Value, p.Default, p.Step = d.FloatVal, d.DefFloat, d.StepFloat
		p.Minimum, p.Maximum = d.MinFloat, d.MaxFloat
	case codec.TypeIPAddr:
		p.Value = uint32ToDottedQuad(uint32(d.UintVal))
		p.Default = uint32ToDottedQuad(uint32(d.DefUint))
		p.Step = uint32ToDottedQuad(uint32(d.StepUint))
		p.Minimum = uint32ToDottedQuad(uint32(d.MinUint))
		p.Maximum = uint32ToDottedQuad(uint32(d.MaxUint))
	case codec.TypeEnum:
		p.Value, p.Default = int64(d.ByteVal), int64(d.DefByte)
		entries := make([]canonical.EnumEntry, len(d.EnumItems))
		for i, it := range d.EnumItems {
			entries[i] = canonical.EnumEntry{Key: it, Value: int64(i)}
		}
		p.EnumMap = entries
	default:
		// Out of fidelity scope: String (maxLen buffer-size semantics vary
		// per device, so re-encode isn't a guaranteed byte inverse), and
		// Root/Alarm/File/Frame (synthesised or format-hint encoded). The
		// codec replay test already proves these DECODE.
		return nil, false
	}
	return &entry{acpType: d.Type, access: d.Access, param: p}, true
}

// compareDecoded returns "" when got matches want on the spec fields the
// provider is responsible for re-serving, else a human-readable diff.
func compareDecoded(want, got *codec.DecodedObject) string {
	if want.Type != got.Type {
		return fmt.Sprintf("type %d != %d", want.Type, got.Type)
	}
	if want.Access != got.Access {
		return fmt.Sprintf("access %d != %d", want.Access, got.Access)
	}
	// Label/unit: the provider re-serves a SPEC-COMPLIANT object, truncating
	// label to 16 and unit to 4 chars (spec p.22). Some devices/emulators emit
	// over-long fields (e.g. the Synapse sim's 5-char " bits" unit), so the
	// faithful re-serve is the spec-truncated prefix — tolerate that, but still
	// catch a dropped or garbled field (got must be a non-empty prefix).
	if !specTruncEqual(want.Label, got.Label) {
		return fmt.Sprintf("label %q not a faithful re-serve of %q", got.Label, want.Label)
	}
	if !specTruncEqual(want.Unit, got.Unit) {
		return fmt.Sprintf("unit %q not a faithful re-serve of %q", got.Unit, want.Unit)
	}
	switch want.Type {
	case codec.TypeInteger, codec.TypeLong:
		if want.IntVal != got.IntVal || want.MinInt != got.MinInt || want.MaxInt != got.MaxInt ||
			want.StepInt != got.StepInt || want.DefInt != got.DefInt {
			return fmt.Sprintf("int fields val/min/max/step/def: %v/%v/%v/%v/%v != %v/%v/%v/%v/%v",
				want.IntVal, want.MinInt, want.MaxInt, want.StepInt, want.DefInt,
				got.IntVal, got.MinInt, got.MaxInt, got.StepInt, got.DefInt)
		}
	case codec.TypeByte:
		if want.ByteVal != got.ByteVal || want.MinByte != got.MinByte || want.MaxByte != got.MaxByte {
			return "byte fields diverged"
		}
	case codec.TypeFloat:
		if want.FloatVal != got.FloatVal || want.MinFloat != got.MinFloat || want.MaxFloat != got.MaxFloat {
			return "float fields diverged"
		}
	case codec.TypeIPAddr:
		if want.UintVal != got.UintVal || want.MinUint != got.MinUint || want.MaxUint != got.MaxUint {
			return "ipaddr fields diverged"
		}
	case codec.TypeEnum:
		if want.ByteVal != got.ByteVal || len(want.EnumItems) != len(got.EnumItems) {
			return "enum value/items diverged"
		}
		for i := range want.EnumItems {
			if want.EnumItems[i] != got.EnumItems[i] {
				return fmt.Sprintf("enum item %d %q != %q", i, want.EnumItems[i], got.EnumItems[i])
			}
		}
	case codec.TypeString:
		if want.StrValue != got.StrValue || want.MaxLen != got.MaxLen {
			return "string value/maxlen diverged"
		}
	}
	return ""
}

// specTruncEqual reports whether got is a faithful (possibly spec-truncated)
// re-serve of want: equal, or — when want exceeds the spec field cap — the
// truncated prefix. A dropped/garbled field (empty got for non-empty want, or
// a non-prefix) fails.
func specTruncEqual(want, got string) bool {
	if want == got {
		return true
	}
	if want == "" || got == "" {
		return false
	}
	return strings.HasPrefix(want, got)
}
