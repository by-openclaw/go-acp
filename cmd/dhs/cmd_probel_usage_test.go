package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"dhs/internal/probel-sw08p/codec"
	probelproto "dhs/internal/probel-sw08p/consumer"
)

// The 6-dst fixture on level 2, starting at dst 4: src 12 feeds dsts
// 4 and 7, src 3 feeds dst 5, src 0 feeds dst 6 (0 is a valid source),
// src 12 also feeds dst 8, src 9 feeds dst 9.
func probelUsageFixture() (int, []int) {
	return 4, []int{12, 3, 0, 12, 12, 9}
}

func TestProbelTallyTable_WordAndByte(t *testing.T) {
	word := probelproto.TallyDumpResult{IsWord: true, Word: codec.CrosspointTallyDumpWordParams{
		FirstDestinationID: 300, SourceIDs: []uint16{40000, 5},
	}}
	first, srcs := probelTallyTable(word)
	if first != 300 || len(srcs) != 2 || srcs[0] != 40000 || srcs[1] != 5 {
		t.Fatalf("word table = %d %v", first, srcs)
	}
	byteForm := probelproto.TallyDumpResult{Byte: codec.CrosspointTallyDumpByteParams{
		FirstDestinationID: 4, SourceIDs: []uint8{12, 3},
	}}
	first, srcs = probelTallyTable(byteForm)
	if first != 4 || len(srcs) != 2 || srcs[0] != 12 || srcs[1] != 3 {
		t.Fatalf("byte table = %d %v", first, srcs)
	}
}

func TestProbelBuildUsage_SortedBySrcThenDst(t *testing.T) {
	first, srcs := probelUsageFixture()
	rows := probelBuildUsage(first, srcs, 2, nil)
	if len(rows) != 6 {
		t.Fatalf("rows = %d", len(rows))
	}
	// Expected order: src 0 (dst 6), src 3 (dst 5), src 9 (dst 9),
	// src 12 (dsts 4, 7, 8).
	want := []probelUsageRow{
		{Src: 0, Dst: 6, Levels: "2"}, {Src: 3, Dst: 5, Levels: "2"}, {Src: 9, Dst: 9, Levels: "2"},
		{Src: 12, Dst: 4, Levels: "2"}, {Src: 12, Dst: 7, Levels: "2"}, {Src: 12, Dst: 8, Levels: "2"},
	}
	for i, w := range want {
		if rows[i] != w {
			t.Fatalf("row %d = %+v, want %+v", i, rows[i], w)
		}
	}
}

func TestProbelBuildUsage_ProtectJoin(t *testing.T) {
	first, srcs := probelUsageFixture()
	rows := probelBuildUsage(first, srcs, 2, map[int]string{7: "state=1 device=9"})
	var hit bool
	for _, r := range rows {
		if r.Dst == 7 && r.Protect == "state=1 device=9" {
			hit = true
		}
		if r.Dst != 7 && r.Protect != "" {
			t.Fatalf("dst %d unexpectedly protected: %+v", r.Dst, r)
		}
	}
	if !hit {
		t.Fatal("dst 7 protect state not joined")
	}
}

func TestProbelFilterUsage(t *testing.T) {
	first, srcs := probelUsageFixture()
	rows := probelBuildUsage(first, srcs, 2, nil)
	if got := probelFilterUsage(rows, -1, -1); len(got) != len(rows) {
		t.Fatalf("no-filter changed row count: %d", len(got))
	}
	bySrc := probelFilterUsage(rows, 12, -1)
	if len(bySrc) != 3 {
		t.Fatalf("src filter rows = %d", len(bySrc))
	}
	for _, r := range bySrc {
		if r.Src != 12 {
			t.Fatalf("src filter leaked %+v", r)
		}
	}
	byDst := probelFilterUsage(rows, -1, 5)
	if len(byDst) != 1 || byDst[0].Src != 3 {
		t.Fatalf("dst filter = %+v", byDst)
	}
}

func TestFormatProbelUsageCSV(t *testing.T) {
	rows := []probelUsageRow{
		{Src: 3, Dst: 5, Levels: "2"},
		{Src: 12, Dst: 7, Levels: "2", Protect: "state=1 device=9"},
	}
	plain := formatProbelUsageCSV(rows, false, false)
	if !strings.HasPrefix(plain, "srce,dest,levels\n") {
		t.Fatalf("plain header: %q", plain)
	}
	if !strings.Contains(plain, "3,5,2\n") || strings.Contains(plain, "state=1") {
		t.Fatalf("plain body: %q", plain)
	}
	prot := formatProbelUsageCSV(rows, true, false)
	if !strings.HasPrefix(prot, "srce,dest,levels,protect\n") {
		t.Fatalf("protect header: %q", prot)
	}
	if !strings.Contains(prot, "3,5,2,\n") || !strings.Contains(prot, "12,7,2,state=1 device=9\n") {
		t.Fatalf("protect body: %q", prot)
	}
}

func TestRenderProbelUsageASCII(t *testing.T) {
	first, srcs := probelUsageFixture()
	rows := probelBuildUsage(first, srcs, 2, map[int]string{7: "state=1 device=9"})
	var buf bytes.Buffer
	renderProbelUsageASCII(&buf, rows)
	out := buf.String()
	for _, want := range []string{
		"src 0\n└── dst 6  levels 2\n",
		"src 12\n├── dst 4  levels 2\n├── dst 7  levels 2  [state=1 device=9]\n└── dst 8  levels 2\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("ascii missing %q in:\n%s", want, out)
		}
	}
}

func TestProbelReplaceCells(t *testing.T) {
	first, srcs := probelUsageFixture()
	rows := probelBuildUsage(first, srcs, 2, nil)
	cells := probelReplaceCells(rows, 12)
	if len(cells) != 3 || cells[0].Dst != 4 || cells[1].Dst != 7 || cells[2].Dst != 8 {
		t.Fatalf("cells = %+v", cells)
	}
	if got := probelReplaceCells(rows, 77); len(got) != 0 {
		t.Fatalf("absent src produced cells: %+v", got)
	}
}

func TestProbelUsageProtectMap(t *testing.T) {
	res := codec.ProtectTallyDumpParams{
		FirstDestinationID: 10,
		Items: []codec.ProtectTallyItem{
			{State: codec.ProtectNone, DeviceID: 0},
			{State: codec.ProtectProbel, DeviceID: 42},
		},
	}
	m := probelUsageProtectMap(res)
	if len(m) != 1 {
		t.Fatalf("map = %+v (unprotected dst must be skipped)", m)
	}
	if m[11] != protectStateStr(codec.ProtectProbel, 42) {
		t.Fatalf("dst 11 = %q", m[11])
	}
}

// Validation paths that must fail before any dial happens.
func TestRunProbelUsage_Validation(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		args []string
		want string
	}{
		// = form: popPositional would otherwise pop a space-form flag
		// value as the positional address (documented CLI shape).
		{"missing addr", []string{"--srce=1"}, "missing <host:port>"},
		{"bad format", []string{"127.0.0.1:2008", "--format", "xml"}, "--format"},
		{"srce+dest", []string{"127.0.0.1:2008", "--srce", "1", "--dest", "2"}, "mutually exclusive"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := runProbelUsage(ctx, c.args)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

func TestRunProbelReplace_Validation(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"missing addr", []string{"--srce=1", "--with=2"}, "missing <host:port>"},
		{"missing pair", []string{"127.0.0.1:2008"}, "--srce and --with are required"},
		{"same source", []string{"127.0.0.1:2008", "--srce", "3", "--with", "3"}, "same source"},
		{"bad output", []string{"127.0.0.1:2008", "--srce", "1", "--with", "2", "--output", "xml"}, "expected text | json"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := runProbelReplace(ctx, c.args)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}
