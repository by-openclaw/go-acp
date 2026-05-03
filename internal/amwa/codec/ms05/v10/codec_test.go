package v10

import (
	"testing"

	"dhs/internal/amwa/codec/ms05"
)

func TestRegistration(t *testing.T) {
	c, ok := ms05.Get("v1.0")
	if !ok {
		t.Fatalf("ms05.Get(v1.0) not found")
	}
	if c.SpecID() != ms05.SpecID {
		t.Fatalf("spec id %q", c.SpecID())
	}
	if c.APIVer() != "v1.0" {
		t.Fatalf("api ver %q", c.APIVer())
	}
	if c.SpecPatch() != SpecPatch {
		t.Fatalf("patch %q", c.SpecPatch())
	}
}

func TestSelectHighest(t *testing.T) {
	got, err := ms05.SelectHighest([]string{"v0.9", "v1.0"})
	if err != nil {
		t.Fatalf("SelectHighest: %v", err)
	}
	if got.APIVer() != "v1.0" {
		t.Fatalf("got %s", got.APIVer())
	}
}
