package emberplus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUnknownCTXAudit_DedupesByKindAndTag(t *testing.T) {
	a := newUnknownCTXAudit()
	a.record("node", []int32{1, 2}, []int32{6})
	a.record("node", []int32{1, 5}, []int32{6})       // same key, different OID → sample
	a.record("parameter", []int32{1, 2, 3}, []int32{19, 20})
	a.record("parameter", []int32{1, 2, 4}, []int32{19}) // dup tag

	rows := a.snapshot()
	if got, want := len(rows), 3; got != want {
		t.Fatalf("rows = %d, want %d", got, want)
	}
	// Sorted by (kind, tag): node 6 ; parameter 19 ; parameter 20
	if rows[0].Key.Kind != "node" || rows[0].Key.Tag != 6 {
		t.Errorf("rows[0] = %+v, want node CTX 6", rows[0].Key)
	}
	if rows[0].Count != 2 {
		t.Errorf("rows[0].Count = %d, want 2", rows[0].Count)
	}
	if got, want := len(rows[0].SampleOIDs), 2; got != want {
		t.Errorf("rows[0].SampleOIDs len = %d, want %d", got, want)
	}
	if rows[1].Key.Kind != "parameter" || rows[1].Key.Tag != 19 {
		t.Errorf("rows[1] = %+v, want parameter CTX 19", rows[1].Key)
	}
	if rows[1].Count != 2 {
		t.Errorf("rows[1].Count = %d, want 2", rows[1].Count)
	}
}

func TestUnknownCTXAudit_SampleOIDsCap(t *testing.T) {
	a := newUnknownCTXAudit()
	for i := int32(0); i < 20; i++ {
		a.record("node", []int32{1, i}, []int32{99})
	}
	rows := a.snapshot()
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if got := len(rows[0].SampleOIDs); got != maxUnknownSampleOIDs {
		t.Errorf("SampleOIDs cap len = %d, want %d", got, maxUnknownSampleOIDs)
	}
	if rows[0].Count != 20 {
		t.Errorf("Count = %d, want 20", rows[0].Count)
	}
}

func TestRenderUnknownCTXMarkdown_Empty(t *testing.T) {
	got := renderUnknownCTXMarkdown(unknownCTXHeader{}, nil)
	if got != "" {
		t.Errorf("empty rows must render empty string, got %d bytes", len(got))
	}
}

func TestRenderUnknownCTXMarkdown_GroupsByKind(t *testing.T) {
	rows := []unknownCTXRow{
		{Key: unknownCTXKey{Kind: "node", Tag: 6}, Count: 1240, SampleOIDs: []string{"1.2", "1.5.3"}},
		{Key: unknownCTXKey{Kind: "parameter", Tag: 19}, Count: 88, SampleOIDs: []string{"2.1", "2.3"}},
	}
	h := unknownCTXHeader{
		Identity:     "mc2_56_mk3@12-2-0-0 build 45",
		Host:         "10.41.40.139",
		Port:         9000,
		DTDDeclared:  "2.60",
		StartedAtUTC: time.Date(2026, 5, 16, 2, 35, 0, 0, time.UTC),
	}
	md := renderUnknownCTXMarkdown(h, rows)
	wantSubstr := []string{
		"# Unknown CTX Tags Report",
		"`mc2_56_mk3@12-2-0-0 build 45`",
		"10.41.40.139:9000",
		"2026-05-16T02:35:00Z",
		"**DTD declared**: 2.60",
		"## NodeContents",
		"| 6 | 1240 | 1.2, 1.5.3 |",
		"## ParameterContents",
		"| 19 | 88 | 2.1, 2.3 |",
		"How to use this report",
	}
	for _, s := range wantSubstr {
		if !strings.Contains(md, s) {
			t.Errorf("missing substring: %q\n---\n%s", s, md)
		}
	}
}

func TestWriteUnknownCTXReport_SkipsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x", "y", "out.md")
	if err := writeUnknownCTXReport(path, unknownCTXHeader{}, nil); err != nil {
		t.Fatalf("writeUnknownCTXReport: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("empty rows should NOT write a file, err = %v", err)
	}
}

func TestWriteUnknownCTXReport_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit", "emberplus", "X@1.0-unknown-ctx.md")
	rows := []unknownCTXRow{{Key: unknownCTXKey{Kind: "matrix", Tag: 13}, Count: 4, SampleOIDs: []string{"5.1.1.1"}}}
	if err := writeUnknownCTXReport(path, unknownCTXHeader{Identity: "X@1.0"}, rows); err != nil {
		t.Fatalf("writeUnknownCTXReport: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "## MatrixContents") {
		t.Errorf("missing section header in:\n%s", data)
	}
	if !strings.Contains(string(data), "| 13 | 4 | 5.1.1.1 |") {
		t.Errorf("missing row in:\n%s", data)
	}
}
