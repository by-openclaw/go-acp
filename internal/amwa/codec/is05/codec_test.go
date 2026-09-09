package is05

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/spec"
)

// stubCodec is the smallest thing that satisfies [Codec]. The real
// minors live in vXX/ packages that import this one, so the registry
// wrappers can only be exercised here with a stand-in whose only job
// is to carry an identity.
type stubCodec struct{ specID, apiVer string }

func (s stubCodec) SpecID() string  { return s.specID }
func (s stubCodec) APIVer() string  { return s.apiVer }
func (stubCodec) SpecPatch() string { return "v0.0.0" }

func (stubCodec) EncodeStagedSender(StagedSender) ([]byte, error)     { return nil, nil }
func (stubCodec) DecodeStagedSender([]byte) (StagedSender, error)     { return StagedSender{}, nil }
func (stubCodec) ValidateStagedSender(StagedSender) error             { return nil }
func (stubCodec) EncodeStagedReceiver(StagedReceiver) ([]byte, error) { return nil, nil }
func (stubCodec) DecodeStagedReceiver([]byte) (StagedReceiver, error) { return StagedReceiver{}, nil }
func (stubCodec) ValidateStagedReceiver(StagedReceiver) error         { return nil }

// isolatedRegistry points the package at an empty registry for one
// test, so nothing a test registers leaks into another and the
// "nothing registered" state is reachable at all.
func isolatedRegistry(t *testing.T) {
	t.Helper()
	old := versions
	versions = spec.NewRegistry[Codec]()
	t.Cleanup(func() { versions = old })
}

func mustPanic(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected a panic mentioning %q", want)
		}
		if s, _ := r.(string); !strings.Contains(s, want) {
			t.Fatalf("panic = %v, want it to mention %q", r, want)
		}
	}()
	f()
}

// TestDefaultPanicsWhenNothingIsRegistered: a binary that forgot the
// blank-imports must fail at start, loudly, rather than serve no
// Connection API tree at all.
func TestDefaultPanicsWhenNothingIsRegistered(t *testing.T) {
	isolatedRegistry(t)
	mustPanic(t, "no codec registered", func() { Default() })
}

// TestRegisterRefusesAnotherSpec: the registry is per spec, and a
// codec claiming a different catalogue slug is a wiring bug.
func TestRegisterRefusesAnotherSpec(t *testing.T) {
	isolatedRegistry(t)
	mustPanic(t, "SpecID must be is-05", func() {
		Register(stubCodec{specID: "is-04", apiVer: "v1.0"})
	})
}

// TestRegistryWrappersRouteByAPIVer pins the contract the plugin
// layer builds on: ascending version order, exact-minor lookup, and
// highest-mutual selection that refuses rather than downgrades.
func TestRegistryWrappersRouteByAPIVer(t *testing.T) {
	isolatedRegistry(t)
	Register(stubCodec{specID: SpecID, apiVer: "v1.2"})
	Register(stubCodec{specID: SpecID, apiVer: "v1.0"})

	if got, want := SupportedVersions(), []string{"v1.0", "v1.2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SupportedVersions = %v, want %v", got, want)
	}
	all := AllCodecs()
	if len(all) != 2 || all[0].APIVer() != "v1.0" || all[1].APIVer() != "v1.2" {
		t.Fatalf("AllCodecs must be ascending, got %v", all)
	}
	if c, ok := Get("v1.2"); !ok || c.APIVer() != "v1.2" {
		t.Fatalf("Get(v1.2) = %v, %v", c, ok)
	}
	if _, ok := Get("v1.1"); ok {
		t.Fatal("Get(v1.1) must miss: nothing serves that minor")
	}
	c, err := SelectHighest([]string{"v1.0", "v1.2", "v2.0"})
	if err != nil || c.APIVer() != "v1.2" {
		t.Fatalf("SelectHighest = %v, %v; want v1.2", c, err)
	}
	var none spec.ErrNoCommonVersion
	if _, err := SelectHighest([]string{"v2.0"}); !errors.As(err, &none) {
		t.Fatalf("a peer with no common minor must yield ErrNoCommonVersion, got %v", err)
	}
	if got := Default().APIVer(); got != "v1.2" {
		t.Fatalf("Default = %q, want the highest registered minor", got)
	}
}
