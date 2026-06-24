package emberplus

import (
	"encoding/hex"
	"os"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/emberplus/codec/s101"
	"dhs/internal/wiretrace"
)

// replayCapture feeds a committed raw.s101.jsonl through the full S101
// reassembly + tree-builder (exactly as session.readLoop does) and returns
// the post-walk snapshot. Proves end-to-end decode → tree → enrich on real
// device bytes, independent of the live walk's settle timing.
func replayCapture(t *testing.T, path string) []consumer.Object {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	trames, err := wiretrace.ReadTrames(f)
	if err != nil {
		t.Fatalf("ReadTrames: %v", err)
	}

	p := freshPlugin()
	var buf []byte
	for _, tr := range trames {
		if tr.Direction != wiretrace.DirectionRx {
			continue
		}
		raw, derr := hex.DecodeString(tr.Hex)
		if derr != nil {
			continue
		}
		frame, ferr := s101.Decode(raw)
		if ferr != nil || !frame.IsEmBER() || len(frame.Payload) == 0 {
			continue
		}
		var payload []byte
		switch {
		case frame.Flags == s101.FlagSingle:
			payload = frame.Payload
		case frame.Flags&s101.FlagFirst != 0:
			buf = append([]byte{}, frame.Payload...)
			continue
		case frame.Flags&s101.FlagLast != 0:
			buf = append(buf, frame.Payload...)
			payload = buf
			buf = nil
		default:
			buf = append(buf, frame.Payload...)
			continue
		}
		if els, eerr := glow.DecodeRoot(payload); eerr == nil {
			p.handleElements(els)
		}
	}
	return p.snapshot()
}

func findMatrix(objs []consumer.Object, oid string) *consumer.Object {
	for i := range objs {
		if objs[i].OID != oid {
			continue
		}
		if elem, _ := objs[i].Meta["element"].(string); elem == "matrix" {
			return &objs[i]
		}
	}
	return nil
}

func metaInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int32:
		return int(x)
	case int64:
		return int(x)
	case float64:
		return int(x)
	}
	return -1
}

func countTargetLabels(m *consumer.Object) int {
	tl, ok := m.Meta["targetLabels"].(map[string]map[string]string)
	if !ok {
		return 0
	}
	n := 0
	for _, lvl := range tl {
		n += len(lvl)
	}
	return n
}

// TestMatrixResolvesFromRealCaptures proves the matrix grid (targetCount +
// resolved targetLabels) is recovered from real device captures — the DHD
// audio console and the Lawo PowerCore. Both deliver matrix contents in the
// directory stream; the consumer merges contents onto the connection-bearing
// matrix entry and enrichMatrixLabels joins the label subtree. Regression
// guard: a clobbered/dropped contents merge would drop these to 0.
func TestMatrixResolvesFromRealCaptures(t *testing.T) {
	cases := []struct {
		name        string
		path        string
		oid         string
		wantTargets int
	}{
		{"dhd", "../testdata/fixtures/dhd/raw.s101.jsonl", "0.5.2", 147},
		{"powercore", "../testdata/fixtures/powercore/raw.s101.jsonl", "1.3.1", 1024},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objs := replayCapture(t, c.path)
			m := findMatrix(objs, c.oid)
			if m == nil {
				t.Fatalf("matrix %s not found in snapshot", c.oid)
			}
			if got := metaInt(m.Meta["targetCount"]); got != c.wantTargets {
				t.Errorf("targetCount = %d, want %d", got, c.wantTargets)
			}
			if got := countTargetLabels(m); got != c.wantTargets {
				t.Errorf("resolved targetLabels = %d, want %d", got, c.wantTargets)
			}
		})
	}
}
