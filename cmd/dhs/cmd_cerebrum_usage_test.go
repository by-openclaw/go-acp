package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// The staging-shaped fixture: Src 2 feeds virtual Proc 1; Proc 1 feeds
// virtual Proc 2 and Dest 3; Proc 2 feeds Dest 1 and Dest 4; Src 2
// also feeds Dest 2 on level 1 only. IDs: sources 1,2 physical; 3,4
// virtual (same ID+name on both faces).
func usageFixture() ([]routeSpec, map[string]bool) {
	routes := []routeSpec{
		{Dest: "2", Srce: "2", Level: "1"},
		{Dest: "3", Srce: "2", Level: "1"}, {Dest: "3", Srce: "2", Level: "2"}, {Dest: "3", Srce: "2", Level: "3"},
		{Dest: "4", Srce: "3", Level: "1"}, {Dest: "4", Srce: "3", Level: "2"}, {Dest: "4", Srce: "3", Level: "3"},
		{Dest: "5", Srce: "3", Level: "1"}, {Dest: "5", Srce: "3", Level: "2"}, {Dest: "5", Srce: "3", Level: "3"},
		{Dest: "1", Srce: "4", Level: "1"}, {Dest: "1", Srce: "4", Level: "2"}, {Dest: "1", Srce: "4", Level: "3"},
	}
	return routes, map[string]bool{"3": true, "4": true}
}

func TestCerebrumDetectVirtuals(t *testing.T) {
	src := []cerebrumMneRow{{ID: "1", Mnemonic: "Src 1"}, {ID: "3", Mnemonic: "Proc 1"}}
	dst := []cerebrumMneRow{{ID: "1", Mnemonic: "Dest 1"}, {ID: "3", Mnemonic: "Proc 1"}}
	v := cerebrumDetectVirtuals(src, dst)
	if !v["3"] {
		t.Fatal("Proc 1 (same ID+name both faces) must be virtual")
	}
	if v["1"] {
		t.Fatal("ID collision with different names must NOT be virtual")
	}
}

func TestCerebrumResolveSrc(t *testing.T) {
	routes, virtuals := usageFixture()
	feed := map[string]string{}
	for _, r := range routes {
		feed[r.Dest+"\x00"+r.Level] = r.Srce
	}
	// Two-virtual chain: 4 -> (fed by 3) -> (fed by 2).
	eff, via, mark := cerebrumResolveSrc("4", "2", feed, virtuals)
	if eff != "2" || mark != "" || strings.Join(via, ";") != "4;3" {
		t.Fatalf("chain = eff %q via %v mark %q", eff, via, mark)
	}
	// Physical source resolves to itself, no hops.
	if eff, via, _ := cerebrumResolveSrc("2", "1", feed, virtuals); eff != "2" || len(via) != 0 {
		t.Fatalf("physical = %q via %v", eff, via)
	}
	// Unfed virtual (no route into its dest face on that level).
	unfedFeed := map[string]string{}
	if eff, _, mark := cerebrumResolveSrc("3", "1", unfedFeed, virtuals); eff != "" || mark != "unfed" {
		t.Fatalf("unfed = %q mark %q", eff, mark)
	}
	// Loop: 3 fed by 4, 4 fed by 3.
	loopFeed := map[string]string{"3\x001": "4", "4\x001": "3"}
	if eff, _, mark := cerebrumResolveSrc("3", "1", loopFeed, virtuals); eff != "" || mark != "loop" {
		t.Fatalf("loop = %q mark %q", eff, mark)
	}
}

func TestCerebrumBuildUsage_ResolveAndCollapse(t *testing.T) {
	routes, virtuals := usageFixture()
	rows := cerebrumBuildUsage(routes, virtuals, true)
	byKey := map[string]cerebrumUsageRow{}
	for _, r := range rows {
		byKey[r.Srce+">"+r.Dest] = r
	}
	// Dest 1 fed by virtual 4, effective 2 via 4;3, all levels collapsed.
	r := byKey["4>1"]
	if r.Effective != "2" || strings.Join(r.Via, ";") != "4;3" || strings.Join(r.Levels, ";") != "1;2;3" {
		t.Fatalf("dest1 row = %+v", r)
	}
	// Physical direct row keeps empty via.
	if r := byKey["2>2"]; r.Effective != "2" || len(r.Via) != 0 || strings.Join(r.Levels, ";") != "1" {
		t.Fatalf("dest2 row = %+v", r)
	}

	csv := formatCerebrumUsageCSV(rows, true)
	if !strings.Contains(csv, "srce,dest,levels,effective_srce,via\n") ||
		!strings.Contains(csv, "4,1,1;2;3,2,4;3\n") {
		t.Fatalf("resolved csv:\n%s", csv)
	}
	plain := formatCerebrumUsageCSV(cerebrumBuildUsage(routes, virtuals, false), false)
	if strings.Contains(plain, "effective") || !strings.Contains(plain, "2,3,1;2;3\n") {
		t.Fatalf("literal csv:\n%s", plain)
	}
}

func TestCerebrumUsageAsciiRenders(t *testing.T) {
	routes, virtuals := usageFixture()
	rows := cerebrumBuildUsage(routes, virtuals, true)
	srcNames := map[string]string{"2": "Src 2", "3": "Proc 1", "4": "Proc 2"}
	dstNames := map[string]string{"1": "Dest 1", "2": "Dest 2", "3": "Proc 1", "4": "Proc 2", "5": "Dest 3"}

	var fan bytes.Buffer
	renderCerebrumUsageFanout(&fan, "2", rows, virtuals, srcNames, dstNames, "", map[string]bool{})
	s := fan.String()
	// Nested virtuals with their subscribers, physical leaves plain.
	if !strings.Contains(s, "[virtual]") || !strings.Contains(s, "Dest 1 (1)") || !strings.Contains(s, "Dest 2 (2)") {
		t.Fatalf("fanout:\n%s", s)
	}

	var chain bytes.Buffer
	renderCerebrumUsageChain(&chain, "1", rows, srcNames, dstNames)
	c := chain.String()
	if !strings.Contains(c, "effective Src 2 (2)") || !strings.Contains(c, "[virtual]") {
		t.Fatalf("chain:\n%s", c)
	}

	// A dest fed by a PHYSICAL source (empty Via) must render without
	// panicking (live-found 2026-08-19: Via[1:] on an empty slice).
	var direct bytes.Buffer
	renderCerebrumUsageChain(&direct, "2", rows, srcNames, dstNames)
	if !strings.Contains(direct.String(), "fed by Src 2 (2)") {
		t.Fatalf("direct chain:\n%s", direct.String())
	}
}

func TestCerebrumUsageReplaceValidateFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"usage-bad-format", []string{"usage", "h", "--format", "xml"}, "--format must be csv or ascii"},
		{"usage-srce-and-dest", []string{"usage", "h", "--srce", "1", "--dest", "1"}, "mutually exclusive"},
		{"usage-no-host", []string{"usage", "--srce", "1"}, "missing host"},
		{"replace-no-args", []string{"replace", "h"}, "--srce and --with are required"},
		{"replace-same", []string{"replace", "h", "--srce", "1", "--with", "1"}, "the same source"},
		{"replace-no-host", []string{"replace", "--srce", "1", "--with", "2"}, "missing host"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := runCerebrum(context.Background(), c.args)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("args %v: got %v, want %q", c.args, err, c.want)
			}
		})
	}
}
