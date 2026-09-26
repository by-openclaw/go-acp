package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/consumer"
)

// A matrix file-set is what an operator reads instead of the device.
// These tests pin the grammar (dest,srce,levels — the same shape as a
// Probel tally dump) and the two refusals that matter: never invent a
// matrix, never silently pick one of several.

func xp(path []string, value string, meta map[string]any) consumer.Object {
	return consumer.Object{
		Path:  path,
		Kind:  consumer.KindString,
		Value: consumer.Value{Kind: consumer.KindString, Str: value},
		Meta:  meta,
	}
}

func linkedTree() []consumer.Object {
	return []consumer.Object{
		xp([]string{"matrix", "video", "path", "main", "CH01"}, "IP00", map[string]any{
			"target": "/api/v1/processing/video/channels/b", "target_type": "Channel",
			"source": "/api/v1/io/ip/receivers/video/rx-0", "source_type": "IP",
		}),
		xp([]string{"matrix", "video", "path", "main", "CH00"}, "IP01", map[string]any{
			"target": "/api/v1/processing/video/channels/a", "target_type": "Channel",
			"source": "/api/v1/io/ip/receivers/video/rx-1", "source_type": "IP",
		}),
		// A second map, and a channel-level one: the channel is part of
		// the address, and it is a number.
		xp([]string{"matrix", "audio", "main", "DB000-05"}, "IP000-05", map[string]any{
			"target": "/api/v1/processing/audio/banks/0", "target_channel": 5, "target_type": "Bank",
			"source": "/api/v1/io/ip/senders/audio/tx-0", "source_channel": 5, "source_type": "IP",
		}),
		// Not a crosspoint: no resolved side, so not routing.
		{Path: []string{"self", "name"}, Kind: consumer.KindString,
			Value: consumer.Value{Kind: consumer.KindString, Str: "BRIDGE"}},
		// Meta, but nothing about routing.
		xp([]string{"misc", "thing"}, "x", map[string]any{"value_name": "X"}),
	}
}

func TestLinkedMatricesGroupsByTheMapNotTheDevice(t *testing.T) {
	got := linkedMatrices(linkedTree())
	if len(got) != 2 {
		t.Fatalf("matrices = %d: %+v", len(got), got)
	}
	if got[0].path != "matrix.audio.main" || got[1].path != "matrix.video.path.main" {
		t.Errorf("paths = %s, %s", got[0].path, got[1].path)
	}
	// Sorted by destination, so two captures of one device diff.
	v := got[1]
	if v.rows[0].dest != "CH00" || v.rows[1].dest != "CH01" {
		t.Errorf("order = %s %s", v.rows[0].dest, v.rows[1].dest)
	}
	if v.rows[0].srce != "IP01" {
		t.Errorf("CH00 source = %q — the device's own answer", v.rows[0].srce)
	}
	// An object with no resolved side is not a crosspoint.
	for _, m := range got {
		for _, r := range m.rows {
			if r.dest == "name" || r.dest == "thing" {
				t.Errorf("a plain object became a crosspoint: %+v", r)
			}
		}
	}
}

func TestMatrixFileSetIsWrittenInTheTallyDumpGrammar(t *testing.T) {
	dir := t.TempDir()
	err := runLinkedMatrixSetExport(linkedTree(), "matrix.audio.main", dir, "dev", "ccm", "10.0.0.1")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	read := func(name string) string {
		b, rerr := os.ReadFile(filepath.Join(dir, name))
		if rerr != nil {
			t.Fatalf("read %s: %v", name, rerr)
		}
		return string(b)
	}

	if got, want := read("dev-matrix.csv"),
		"matrix,behavior,targets,sources,max_connects_per_target,max_total_connects,label\n"+
			"matrix.audio.main,1toN,1,1,1,0,main\n"; got != want {
		t.Errorf("-matrix.csv =\n%s\nwant\n%s", got, want)
	}
	if got, want := read("dev-xpoint.csv"), "dest,srce,levels\nDB000-05,IP000-05,0\n"; got != want {
		t.Errorf("-xpoint.csv =\n%s\nwant\n%s", got, want)
	}
	// The mapping: what the key IS. The channel is a number, not "5.0"
	// and not "<nil>" where there is none.
	if got, want := read("dev-dst.csv"),
		"dest,resource,channel,type\nDB000-05,/api/v1/processing/audio/banks/0,5,Bank\n"; got != want {
		t.Errorf("-dst.csv =\n%s\nwant\n%s", got, want)
	}
	if got, want := read("dev-src.csv"),
		"srce,resource,channel,type\nIP000-05,/api/v1/io/ip/senders/audio/tx-0,5,IP\n"; got != want {
		t.Errorf("-src.csv =\n%s\nwant\n%s", got, want)
	}
	// The pack is self-describing, like every other artefact set.
	if _, serr := os.Stat(filepath.Join(dir, "meta.json")); serr != nil {
		t.Errorf("meta.json: %v", serr)
	}
}

func TestOneEndpointIsDescribedOnceHoweverOftenItIsRouted(t *testing.T) {
	// A source feeding four destinations is one row in -src.csv. On a
	// 17 728-crosspoint matrix that is the difference between a file a
	// person reads and a file they scroll.
	objs := []consumer.Object{
		xp([]string{"m", "main", "D1"}, "S1", map[string]any{"source": "/s/1", "target": "/d/1"}),
		xp([]string{"m", "main", "D2"}, "S1", map[string]any{"source": "/s/1", "target": "/d/2"}),
		xp([]string{"m", "main", "D3"}, "S1", map[string]any{"source": "/s/1", "target": "/d/3"}),
	}
	dir := t.TempDir()
	if err := runLinkedMatrixSetExport(objs, "m.main", dir, "p", "ccm", "h"); err != nil {
		t.Fatalf("export: %v", err)
	}
	src, _ := os.ReadFile(filepath.Join(dir, "p-src.csv"))
	if n := strings.Count(string(src), "\n"); n != 2 {
		t.Errorf("-src.csv has %d lines, want header + 1:\n%s", n, src)
	}
	dst, _ := os.ReadFile(filepath.Join(dir, "p-dst.csv"))
	if n := strings.Count(string(dst), "\n"); n != 4 {
		t.Errorf("-dst.csv has %d lines, want header + 3:\n%s", n, dst)
	}
	// A destination with no source is still a destination — an unrouted
	// output is a fact, not a gap.
	objs = append(objs, xp([]string{"m", "main", "D4"}, "", map[string]any{"target": "/d/4"}))
	dir2 := t.TempDir()
	if err := runLinkedMatrixSetExport(objs, "m.main", dir2, "p", "ccm", "h"); err != nil {
		t.Fatalf("export: %v", err)
	}
	xpt, _ := os.ReadFile(filepath.Join(dir2, "p-xpoint.csv"))
	if !strings.Contains(string(xpt), "D4,,0") {
		t.Errorf("an unrouted destination must still appear:\n%s", xpt)
	}
}

func TestAMatrixIsNeverInventedOrGuessed(t *testing.T) {
	dir := t.TempDir()

	// Nothing resolved: say so, rather than write four empty files that
	// look like a device with no routing.
	err := runLinkedMatrixSetExport([]consumer.Object{
		{Path: []string{"self", "name"}, Value: consumer.Value{Str: "x"}},
	}, "", dir, "p", "ccm", "h")
	if !errors.Is(err, consumer.ErrValidationFailed) {
		t.Errorf("empty tree error = %v", err)
	}

	// A name that is not there lists what is, rather than writing an
	// empty set for a matrix this device does not have.
	err = runLinkedMatrixSetExport(linkedTree(), "matrix.nope", dir, "p", "ccm", "h")
	if !errors.Is(err, consumer.ErrValidationFailed) ||
		!strings.Contains(err.Error(), "matrix.audio.main") {
		t.Errorf("unknown matrix error = %v", err)
	}
}

func TestWithoutAPathEveryMatrixIsExported(t *testing.T) {
	// A device is not one matrix. Asking an operator to name each of
	// them — and to know which exist before they can look — is how a
	// video-only command fails on an audio-only box. No --path means
	// all of them, each in its own directory.
	dir := t.TempDir()
	if err := runLinkedMatrixSetExport(linkedTree(), "", dir, "dev", "ccm", "10.0.0.1"); err != nil {
		t.Fatalf("export: %v", err)
	}
	for _, want := range []string{"matrix-audio-main", "matrix-video-path-main"} {
		for _, f := range []string{"dev-matrix.csv", "dev-xpoint.csv", "dev-dst.csv", "dev-src.csv"} {
			p := filepath.Join(dir, want, f)
			if _, err := os.Stat(p); err != nil {
				t.Errorf("%s: %v", p, err)
			}
		}
	}
	// Each directory holds ITS matrix, not a copy of the first.
	b, err := os.ReadFile(filepath.Join(dir, "matrix-video-path-main", "dev-xpoint.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "CH00,IP01,0") || strings.Contains(string(b), "DB000-05") {
		t.Errorf("the video directory holds:\n%s", b)
	}
	// One meta.json for the pack, at the top.
	if _, err := os.Stat(filepath.Join(dir, "meta.json")); err != nil {
		t.Errorf("meta.json: %v", err)
	}
}

func TestMetaCellRendersWhatAConnectorPutThere(t *testing.T) {
	m := map[string]any{
		"s": "text", "i": 7, "i64": int64(8),
		"whole": 5.0, "frac": 1.5, "other": []string{"a"},
	}
	cases := []struct{ key, want string }{
		{"s", "text"}, {"i", "7"}, {"i64", "8"},
		{"whole", "5"}, // a channel number read from JSON is a float64
		{"frac", "1.5"},
		{"other", "[a]"},
		{"missing", ""},
	}
	for _, c := range cases {
		if got := metaCell(m, c.key); got != c.want {
			t.Errorf("metaCell(%q) = %q, want %q", c.key, got, c.want)
		}
	}
}

func TestUnwritableOutputDirIsReported(t *testing.T) {
	// A file where the directory should be: the error is the operator's
	// to see, not something to swallow and report success for.
	f := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	one := []consumer.Object{xp([]string{"m", "main", "D1"}, "S1", map[string]any{"target": "/d/1"})}
	if err := runLinkedMatrixSetExport(one, "", f, "p", "ccm", "h"); err == nil {
		t.Error("writing into a file must fail")
	}
}

// writingPlug is a connector whose crosspoints are ordinary writable
// objects: what a REST device looks like to the converge leg.
type writingPlug struct {
	fakePlugin
	batched  [][2]string // path, value — one entry per value in a batch
	batches  int
	singles  [][2]string
	failWith error
}

func (w *writingPlug) SetValues(ctx context.Context, reqs []consumer.ValueRequest, vals []consumer.Value) ([]consumer.Value, error) {
	if w.failWith != nil {
		return nil, w.failWith
	}
	w.batches++
	for i := range reqs {
		w.batched = append(w.batched, [2]string{reqs[i].Path, vals[i].Str})
	}
	return vals, nil
}

func (w *writingPlug) SetValue(ctx context.Context, req consumer.ValueRequest, v consumer.Value) (consumer.Value, error) {
	if w.failWith != nil {
		return consumer.Value{}, w.failWith
	}
	w.singles = append(w.singles, [2]string{req.Path, v.Str})
	return v, nil
}

// singleWriter can only write one value at a time — a connector with
// no batch, which the converge leg has to cope with.
type singleWriter struct {
	fakePlugin
	writes [][2]string
}

func (s *singleWriter) SetValue(ctx context.Context, req consumer.ValueRequest, v consumer.Value) (consumer.Value, error) {
	s.writes = append(s.writes, [2]string{req.Path, v.Str})
	return v, nil
}

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// oneMatrix is a device with a single two-crosspoint matrix.
func oneMatrix() []consumer.Object {
	return []consumer.Object{
		xp([]string{"m", "main", "D1"}, "S1", map[string]any{"target": "/d/1", "source": "/s/1"}),
		xp([]string{"m", "main", "D2"}, "S2", map[string]any{"target": "/d/2", "source": "/s/2"}),
	}
}

func TestAnExportedMatrixGoesBackIn(t *testing.T) {
	// The round trip is the point: what export wrote, import applies.
	// D1 is moved, D2 is already right and must not be written.
	dir := t.TempDir()
	xpoint := writeFile(t, dir, "p-xpoint.csv", "dest,srce,levels\nD1,S9,0\nD2,S2,0\n")

	p := &writingPlug{}
	if err := runLinkedXpointImport(context.Background(), p, oneMatrix(), xpoint, "", false, false); err != nil {
		t.Fatalf("import: %v", err)
	}
	if p.batches != 1 {
		t.Errorf("batches = %d — one matrix is one device operation", p.batches)
	}
	if len(p.batched) != 1 || p.batched[0] != [2]string{"m.main.D1", "S9"} {
		t.Errorf("wrote %v — only what differs", p.batched)
	}

	// Run it again against a device that now matches: nothing is sent.
	converged := []consumer.Object{
		xp([]string{"m", "main", "D1"}, "S9", map[string]any{"target": "/d/1"}),
		xp([]string{"m", "main", "D2"}, "S2", map[string]any{"target": "/d/2"}),
	}
	q := &writingPlug{}
	if err := runLinkedXpointImport(context.Background(), q, converged, xpoint, "", false, false); err != nil {
		t.Fatalf("second import: %v", err)
	}
	if q.batches != 0 || len(q.batched) != 0 {
		t.Errorf("a converged matrix must send nothing: %v", q.batched)
	}
}

func TestCheckReportsWithoutSending(t *testing.T) {
	dir := t.TempDir()
	xpoint := writeFile(t, dir, "p-xpoint.csv", "dest,srce,levels\nD1,S9,0\nD2,S8,0\n")
	p := &writingPlug{}
	if err := runLinkedXpointImport(context.Background(), p, oneMatrix(), xpoint, "", true, false); err != nil {
		t.Fatalf("check: %v", err)
	}
	if p.batches != 0 || len(p.singles) != 0 {
		t.Error("--check must send nothing")
	}
}

func TestConvergeFallsBackWhenAConnectorCannotBatch(t *testing.T) {
	// A connector with no batch still converges — one write at a time,
	// which is correct if slower, and is what a connector whose device
	// takes crosspoints individually does anyway.
	dir := t.TempDir()
	xpoint := writeFile(t, dir, "p-xpoint.csv", "dest,srce,levels\nD1,S9,0\nD2,S8,0\n")
	p := &singleWriter{}
	if err := runLinkedXpointImport(context.Background(), p, oneMatrix(), xpoint, "", false, false); err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(p.writes) != 2 {
		t.Fatalf("wrote %v, want both crosspoints", p.writes)
	}
	if p.writes[0] != [2]string{"m.main.D1", "S9"} || p.writes[1] != [2]string{"m.main.D2", "S8"} {
		t.Errorf("wrote %v", p.writes)
	}
}

func TestConvergeRefusesWhatItCannotPlace(t *testing.T) {
	dir := t.TempDir()
	p := &writingPlug{}

	// A destination this matrix does not have: refused before anything
	// is written, because half a matrix is worse than none.
	bad := writeFile(t, dir, "bad-xpoint.csv", "dest,srce,levels\nNOPE,S1,0\n")
	err := runLinkedXpointImport(context.Background(), p, oneMatrix(), bad, "", false, false)
	if !errors.Is(err, consumer.ErrValidationFailed) || !strings.Contains(err.Error(), "NOPE") {
		t.Errorf("unknown destination = %v", err)
	}
	if len(p.batched) != 0 {
		t.Errorf("nothing may be written: %v", p.batched)
	}

	// Several matrices and no descriptor: say which, do not guess.
	good := writeFile(t, dir, "p-xpoint.csv", "dest,srce,levels\nD1,S9,0\n")
	err = runLinkedXpointImport(context.Background(), p, linkedTree(), good, "", false, false)
	if !errors.Is(err, consumer.ErrValidationFailed) || !strings.Contains(err.Error(), "--matrix") {
		t.Errorf("ambiguous device = %v", err)
	}

	// A descriptor naming a matrix this device does not have.
	desc := writeFile(t, dir, "p-matrix.csv",
		"matrix,behavior,targets,sources,max_connects_per_target,max_total_connects,label\n"+
			"not.here,1toN,1,1,1,0,x\n")
	err = runLinkedXpointImport(context.Background(), p, linkedTree(), good, desc, false, false)
	if !errors.Is(err, consumer.ErrValidationFailed) || !strings.Contains(err.Error(), "not.here") {
		t.Errorf("wrong matrix = %v", err)
	}

	// A device with no resolved crosspoints at all.
	err = runLinkedXpointImport(context.Background(), p,
		[]consumer.Object{{Path: []string{"self", "name"}}}, good, "", false, false)
	if !errors.Is(err, consumer.ErrValidationFailed) {
		t.Errorf("no matrices = %v", err)
	}

	// Files that are not there, or not a crosspoint CSV.
	if err := runLinkedXpointImport(context.Background(), p, oneMatrix(),
		filepath.Join(dir, "missing.csv"), "", false, false); err == nil {
		t.Error("a missing xpoint file must fail")
	}
	if err := runLinkedXpointImport(context.Background(), p, oneMatrix(), good,
		filepath.Join(dir, "missing.csv"), false, false); err == nil {
		t.Error("a missing matrix file must fail")
	}
}

func TestADeviceThatRefusesTheWriteIsReported(t *testing.T) {
	dir := t.TempDir()
	xpoint := writeFile(t, dir, "p-xpoint.csv", "dest,srce,levels\nD1,S9,0\n")
	p := &writingPlug{failWith: errors.New("device said no")}
	err := runLinkedXpointImport(context.Background(), p, oneMatrix(), xpoint, "", false, false)
	if err == nil || !strings.Contains(err.Error(), "device said no") {
		t.Errorf("error = %v", err)
	}
}
