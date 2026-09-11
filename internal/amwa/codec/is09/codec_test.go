package is09_test

// The registry helpers the plugin layer selects versions through. This
// lives outside the package so it can blank-import v10 the way a
// binary does; the in-package tests cannot without an import cycle.

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is09"
	v10 "dhs/internal/amwa/codec/is09/v10"
)

func TestRegistryWiring(t *testing.T) {
	c, ok := is09.Get("v1.0")
	if !ok {
		t.Fatal("v1.0 codec not registered")
	}
	if c.SpecID() != is09.SpecID || c.SpecPatch() != v10.SpecPatch {
		t.Errorf("identity: %s %s %s", c.SpecID(), c.APIVer(), c.SpecPatch())
	}
	if got := is09.SupportedVersions(); len(got) != 1 || got[0] != "v1.0" {
		t.Errorf("SupportedVersions = %v, want [v1.0]", got)
	}
	if got := is09.AllCodecs(); len(got) != 1 || got[0].APIVer() != "v1.0" {
		t.Errorf("AllCodecs = %v, want the single v1.0 codec", got)
	}
	if is09.Default().APIVer() != "v1.0" {
		t.Errorf("Default = %s", is09.Default().APIVer())
	}
	if _, ok := is09.Get("v9.9"); ok {
		t.Error("Get(v9.9) hit an unregistered version")
	}
}

func TestVersionSelection(t *testing.T) {
	c, err := is09.SelectHighest([]string{"v0.9", "v1.0"})
	if err != nil || c.APIVer() != "v1.0" {
		t.Fatalf("SelectHighest = %v, %v", c, err)
	}
	if _, err := is09.SelectHighest([]string{"v2.0"}); err == nil {
		t.Error("a peer with no common version must be an error, never a downgrade")
	}
}

// Registering a codec that claims another spec is an init-time bug;
// the panic is the only place to catch it before the wrong codec
// starts answering System API traffic.
func TestRegisterRejectsForeignSpecID(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Register must panic on a foreign SpecID")
		}
		if msg, _ := r.(string); !strings.Contains(msg, "is-04") {
			t.Fatalf("panic = %v, want it to name the foreign SpecID", r)
		}
	}()
	is09.Register(foreignCodec{})
}

type foreignCodec struct{ is09.Codec }

func (foreignCodec) SpecID() string    { return "is-04" }
func (foreignCodec) APIVer() string    { return "v1.0" }
func (foreignCodec) SpecPatch() string { return "v1.0.0" }
