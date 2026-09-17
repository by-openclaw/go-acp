package bcp

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/spec"
)

// fake is the minimal Validator the registry conflict rules need: the
// real BCP packages all register one (SpecID, "v1.0") each, so the
// same-SpecID/different-APIVer sort tie-break and the two panic
// paths can only be reached with a stand-in.
type fake struct {
	id, ver, patch string
	kind           Kind
}

func (f fake) SpecID() string                         { return f.id }
func (f fake) APIVer() string                         { return f.ver }
func (f fake) SpecPatch() string                      { return f.patch }
func (f fake) HostKind() Kind                         { return f.kind }
func (f fake) Validate([]byte) []spec.ComplianceEvent { return nil }

// isolate swaps in an empty store for one test and restores the real
// one afterwards so init()-registered validators are never disturbed.
func isolate(t *testing.T) {
	t.Helper()
	storeMu.Lock()
	saved := store
	store = nil
	storeMu.Unlock()
	t.Cleanup(func() {
		storeMu.Lock()
		store = saved
		storeMu.Unlock()
	})
}

func mustPanic(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic containing %q", want)
		}
		if msg, _ := r.(string); !strings.Contains(msg, want) {
			t.Fatalf("panic = %v, want it to contain %q", r, want)
		}
	}()
	fn()
}

func TestRegisterRejectsEmptyIdentity(t *testing.T) {
	isolate(t)
	cases := []struct {
		name string
		v    fake
	}{
		{"empty SpecID", fake{"", "v1.0", "v1.0.0", KindFlow}},
		{"empty APIVer", fake{"bcp-x", "", "v1.0.0", KindFlow}},
		{"empty SpecPatch", fake{"bcp-x", "v1.0", "", KindFlow}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mustPanic(t, "must be non-empty", func() { Register(tc.v) })
		})
	}
}

func TestRegisterIdempotentForSameInstance(t *testing.T) {
	isolate(t)
	v := fake{"bcp-x", "v1.0", "v1.0.0", KindFlow}
	Register(v)
	Register(v)
	if got := len(All()); got != 1 {
		t.Fatalf("All() has %d entries after re-registering the same instance, want 1", got)
	}
}

func TestRegisterPanicsOnDifferentInstanceSameKey(t *testing.T) {
	isolate(t)
	Register(fake{"bcp-x", "v1.0", "v1.0.0", KindFlow})
	mustPanic(t, "duplicate (SpecID, APIVer): bcp-x/v1.0", func() {
		Register(fake{"bcp-x", "v1.0", "v1.0.1", KindFlow})
	})
}

// The registry order is (SpecID, APIVer) ascending regardless of the
// init() order the Go linker happens to pick: All() must be stable
// for the compliance report to be diffable across builds.
func TestAllSortedBySpecIDThenAPIVer(t *testing.T) {
	isolate(t)
	Register(fake{"bcp-b", "v1.1", "v1.1.0", KindFlow})
	Register(fake{"bcp-b", "v1.0", "v1.0.0", KindFlow})
	Register(fake{"bcp-a", "v2.0", "v2.0.0", KindSender})

	var got []string
	for _, v := range All() {
		got = append(got, v.SpecID()+"/"+v.APIVer())
	}
	want := "bcp-a/v2.0 bcp-b/v1.0 bcp-b/v1.1"
	if strings.Join(got, " ") != want {
		t.Fatalf("All() order = %v, want %s", got, want)
	}
	if _, ok := Get("bcp-b", "v1.1"); !ok {
		t.Fatal("Get(bcp-b, v1.1) missed a registered validator")
	}
	if _, ok := Get("bcp-b", "v9.9"); ok {
		t.Fatal("Get(bcp-b, v9.9) hit an unregistered version")
	}
}
