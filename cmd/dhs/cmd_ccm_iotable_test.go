package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ccmc "dhs/internal/ccm/consumer"
	"dhs/internal/consumer"
	"dhs/internal/export"
)

// The label file every matrix export writes: the rows of the asked
// matrix and level, keyed by the id column, every label width kept.
func TestParseLabelCSV(t *testing.T) {
	file := "Feffmatrix_id,level_id,src_id,default_label,label_4,label_8,label_12,label_16\n" +
		"0,0,0,IP I0001-01,,IP000101,IP I0001-01,\n" +
		"0,1,0,OTHER LEVEL,,,,\n" +
		"1,0,0,OTHER MATRIX,,,,\n" +
		"0,0,x,not a number,,,,\n" +
		"0,0,17664,AG000101,,AG000101,,\n"
	got, err := parseLabelCSV(strings.NewReader(file), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Default != "IP I0001-01" || got[0].Labels[1] != "IP000101" || got[17664].Labels[1] != "AG000101" {
		t.Errorf("%+v", got)
	}
	if _, err := parseLabelCSV(strings.NewReader("matrix_id,level_id,default_label\n"), 0, 0); err == nil {
		t.Error("a file with no id column was read")
	}
	if _, err := parseLabelCSV(strings.NewReader(""), 0, 0); err == nil {
		t.Error("an empty file was read")
	}
	if l, err := readLabelFile("", 0, 0); l != nil || err != nil {
		t.Error("no path should read nothing")
	}
	if _, err := readLabelFile(filepath.Join(t.TempDir(), "missing.csv"), 0, 0); err == nil {
		t.Error("a missing file gave no error")
	}
}

// From a params.csv to the four files, with the label files joined.
func TestRunCCMIOTableFromAnExport(t *testing.T) {
	dir := t.TempDir()
	str := func(p, v string) consumer.Object {
		return consumer.Object{Path: strings.Split(p, "."), Label: p, Kind: consumer.KindString, Access: 1,
			Value: consumer.Value{Kind: consumer.KindString, Str: v}}
	}
	snap := &export.Snapshot{Device: export.DeviceInfo{IP: "10.6.255.103", Protocol: "ccm", NumSlots: 1},
		Slots: []export.SlotDump{{Slot: 0, Status: "present", Objects: []consumer.Object{
			str("io.ip.receivers.audio.r.name", "Input Audio Stream 1"),
			str("io.ip.receivers.audio.r.channels.c.matrixIndex", "0"),
			str("io.ip.receivers.audio.r.channels.c.name", "IP001-01"),
			str("io.ip.senders.audio.s.name", "Output Audio Stream 1"),
			str("io.ip.senders.audio.s.channels.d.matrixIndex", "0"),
			str("io.ip.senders.audio.s.channels.d.name", "IP001-01"),
			str("processing.audio.generator.channels.g.name", "AG001-01"),
		}}}}
	var buf bytes.Buffer
	if err := export.WriteCSV(&buf, snap); err != nil {
		t.Fatal(err)
	}
	params := filepath.Join(dir, "params.csv")
	names := filepath.Join(dir, "sw08p-src.csv")
	mustNil(t, os.WriteFile(params, buf.Bytes(), 0o644))
	mustNil(t, os.WriteFile(names, []byte("matrix_id,level_id,src_id,default_label,label_8\n0,0,0,IP000101,IP000101\n0,0,17664,AG000101,AG000101\n"), 0o644))
	out := filepath.Join(dir, "out")
	if err := runCCMIOTable(context.Background(), []string{params, "--names-src", names, "--out", out}); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(out, "matrix-src.csv"))
	mustNil(t, err)
	if !strings.Contains(string(src), "17664,generator,,AG001-01,g,1,AG001-01,g,labels,AG000101,AG000101,yes") {
		t.Errorf("matrix-src.csv:\n%s", src)
	}
	for _, f := range []string{"inputs.csv", "outputs.csv", "matrix-dst.csv"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Error(err)
		}
	}
	if err := runCCMIOTable(context.Background(), nil); err == nil {
		t.Error("no target gave no error")
	}
	if err := runCCMIOTable(context.Background(), []string{params, "--names-dst", filepath.Join(dir, "none.csv"), "--out", out}); err == nil {
		t.Error("a missing label file gave no error")
	}
}

func TestReportIndexGaps(t *testing.T) {
	var b bytes.Buffer
	reportIndexGaps(&b, &ccmc.IOTables{})
	if b.Len() != 0 {
		t.Errorf("nothing to report printed %q", b.String())
	}
	reportIndexGaps(&b, &ccmc.IOTables{
		NoIndex:      []string{"generator: AG001-01", "analyser: AA001-01"},
		Sources:      []ccmc.MatrixRow{{Index: 9, IndexFrom: "labels"}},
		Destinations: []ccmc.MatrixRow{{Index: -1}},
	})
	if got := b.String(); !strings.Contains(got, "2 channel(s) without matrixIndex (generator 1, analyser 1); 1 placed from the label file, 1 without an index") {
		t.Errorf("%q", got)
	}
}

func mustNil(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
