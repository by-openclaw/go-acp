package main

import (
	"context"
	"strings"
	"testing"

	"dhs/internal/probel-sw02p/codec"
	probelsw02proto "dhs/internal/probel-sw02p/consumer"
)

func TestSW02RouterConfigDsts(t *testing.T) {
	// Levels 1 and 3 present (bits 1+3); entries in bit order: level 1
	// has 64 dsts, level 3 has 128.
	r1 := probelsw02proto.RouterConfigResponse{Response1: &codec.RouterConfigResponse1Params{
		LevelMap: 0b1010,
		Levels: []codec.RouterConfigResponse1LevelEntry{
			{NumDestinations: 64, NumSources: 32},
			{NumDestinations: 128, NumSources: 96},
		},
	}}
	if got := sw02RouterConfigDsts(r1, 1); got != 64 {
		t.Fatalf("level 1 = %d, want 64", got)
	}
	if got := sw02RouterConfigDsts(r1, 3); got != 128 {
		t.Fatalf("level 3 = %d, want 128", got)
	}
	if got := sw02RouterConfigDsts(r1, 0); got != 0 {
		t.Fatalf("absent level = %d, want 0", got)
	}
	if got := sw02RouterConfigDsts(r1, 28); got != 0 {
		t.Fatalf("out-of-range level = %d, want 0", got)
	}

	// RESPONSE-2 variant, same level-map convention.
	r2 := probelsw02proto.RouterConfigResponse{Response2: &codec.RouterConfigResponse2Params{
		LevelMap: 0b1,
		Levels:   []codec.RouterConfigResponse2LevelEntry{{NumDestinations: 16, NumSources: 16}},
	}}
	if got := sw02RouterConfigDsts(r2, 0); got != 16 {
		t.Fatalf("r2 level 0 = %d, want 16", got)
	}

	// Malformed: level bit set but entry list too short — and the
	// empty union.
	short := probelsw02proto.RouterConfigResponse{Response1: &codec.RouterConfigResponse1Params{LevelMap: 0b1}}
	if got := sw02RouterConfigDsts(short, 0); got != 0 {
		t.Fatalf("short entries = %d, want 0", got)
	}
	if got := sw02RouterConfigDsts(probelsw02proto.RouterConfigResponse{}, 0); got != 0 {
		t.Fatalf("empty union = %d, want 0", got)
	}
}

func TestSortProbelUsage(t *testing.T) {
	rows := sortProbelUsage([]probelUsageRow{
		{Src: 7, Dst: 2}, {Src: 3, Dst: 9}, {Src: 3, Dst: 1},
	})
	want := []probelUsageRow{{Src: 3, Dst: 1}, {Src: 3, Dst: 9}, {Src: 7, Dst: 2}}
	for i, w := range want {
		if rows[i] != w {
			t.Fatalf("row %d = %+v, want %+v", i, rows[i], w)
		}
	}
}

func TestRunProbelSW02Usage_Validation(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"missing addr", []string{"--srce=1"}, "missing <host:port>"},
		{"bad format", []string{"127.0.0.1:2002", "--format", "xml"}, "--format"},
		{"srce+dest", []string{"127.0.0.1:2002", "--srce", "1", "--dest", "2"}, "mutually exclusive"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := runProbelSW02Usage(ctx, c.args)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

func TestRunProbelSW02Replace_Validation(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"missing addr", []string{"--srce=1", "--with=2"}, "missing <host:port>"},
		{"missing pair", []string{"127.0.0.1:2002"}, "--srce and --with are required"},
		{"same source", []string{"127.0.0.1:2002", "--srce", "3", "--with", "3"}, "same source"},
		{"bad output", []string{"127.0.0.1:2002", "--srce", "1", "--with", "2", "--output", "xml"}, "expected text | json"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := runProbelSW02Replace(ctx, c.args)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}
