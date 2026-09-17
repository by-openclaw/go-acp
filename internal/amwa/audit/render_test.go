package audit

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// TestRenderTextCapsGroupTable: a plant with hundreds of groups gets
// forty rows and a pointer to the JSON pivot, not a scrolling wall.
func TestRenderTextCapsGroupTable(t *testing.T) {
	res := Result{Counts: map[string]int{}}
	for i := 0; i < 41; i++ {
		res.Groups = append(res.Groups, GroupRow{Name: fmt.Sprintf("GROUP-%02d", i), Roles: []string{"video"}})
	}
	var b bytes.Buffer
	if err := RenderText(&b, res); err != nil {
		t.Fatal(err)
	}
	s := b.String()
	if !strings.Contains(s, "GROUP-39") {
		t.Error("the fortieth group should be listed")
	}
	if strings.Contains(s, "GROUP-40") {
		t.Error("the forty-first group should be elided")
	}
	if !strings.Contains(s, "1 more group(s)") {
		t.Errorf("the elision should be counted:\n%s", s)
	}
}

// TestRenderTextNamesTargetWithoutLabel: a finding on a device that
// published no label is placed by its target, never left unplaced.
func TestRenderTextNamesTargetWithoutLabel(t *testing.T) {
	res := Result{
		Counts:   map[string]int{"ERROR": 1},
		Findings: []Finding{{Code: "X-1", Severity: SevError, Target: "198.51.100.5:3212", Detail: "d"}},
	}
	var b bytes.Buffer
	if err := RenderText(&b, res); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "on 198.51.100.5:3212") {
		t.Errorf("an unlabelled device should be named by target:\n%s", b.String())
	}
}
