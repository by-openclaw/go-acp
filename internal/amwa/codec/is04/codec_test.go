package is04

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/spec"
)

// stubCodec is the smallest thing that satisfies [Codec]. The real
// minors live in vXX/ packages that import this one, so the registry
// wrappers can only be exercised here with a stand-in. Its resource
// methods are inert; only the identity triple and what EncodeXxx hands
// back matter. Every field is comparable on purpose: the registry
// compares codecs by == to detect a duplicate registration.
type stubCodec struct {
	specID, apiVer string
	// encoded and encodeErr are what every EncodeXxx returns, so a
	// registration-envelope test can make the codec misbehave.
	encoded   string
	encodeErr error
}

func (s stubCodec) SpecID() string          { return s.specID }
func (s stubCodec) APIVer() string          { return s.apiVer }
func (stubCodec) SpecPatch() string         { return "v0.0.0" }
func (s stubCodec) encode() ([]byte, error) { return []byte(s.encoded), s.encodeErr }

func (s stubCodec) EncodeNode(Node) ([]byte, error)         { return s.encode() }
func (stubCodec) DecodeNode([]byte) (Node, error)           { return Node{}, nil }
func (stubCodec) ValidateNode(Node) error                   { return nil }
func (s stubCodec) EncodeDevice(Device) ([]byte, error)     { return s.encode() }
func (stubCodec) DecodeDevice([]byte) (Device, error)       { return Device{}, nil }
func (stubCodec) ValidateDevice(Device) error               { return nil }
func (s stubCodec) EncodeSource(Source) ([]byte, error)     { return s.encode() }
func (stubCodec) DecodeSource([]byte) (Source, error)       { return Source{}, nil }
func (stubCodec) ValidateSource(Source) error               { return nil }
func (s stubCodec) EncodeFlow(Flow) ([]byte, error)         { return s.encode() }
func (stubCodec) DecodeFlow([]byte) (Flow, error)           { return Flow{}, nil }
func (stubCodec) ValidateFlow(Flow) error                   { return nil }
func (s stubCodec) EncodeSender(Sender) ([]byte, error)     { return s.encode() }
func (stubCodec) DecodeSender([]byte) (Sender, error)       { return Sender{}, nil }
func (stubCodec) ValidateSender(Sender) error               { return nil }
func (s stubCodec) EncodeReceiver(Receiver) ([]byte, error) { return s.encode() }
func (stubCodec) DecodeReceiver([]byte) (Receiver, error)   { return Receiver{}, nil }
func (stubCodec) ValidateReceiver(Receiver) error           { return nil }

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
// version tree at all.
func TestDefaultPanicsWhenNothingIsRegistered(t *testing.T) {
	isolatedRegistry(t)
	mustPanic(t, "no codec registered", func() { Default() })
}

// TestRegisterRefusesAnotherSpec: the registry is per spec, and a
// codec claiming a different catalogue slug is a wiring bug.
func TestRegisterRefusesAnotherSpec(t *testing.T) {
	isolatedRegistry(t)
	mustPanic(t, "SpecID must be is-04", func() {
		Register(stubCodec{specID: "is-05", apiVer: "v1.0"})
	})
}

// TestRegistryWrappersRouteByAPIVer pins the contract the plugin
// layer builds on: ascending version order, exact-minor lookup, and
// highest-mutual selection that refuses rather than downgrades.
func TestRegistryWrappersRouteByAPIVer(t *testing.T) {
	isolatedRegistry(t)
	Register(stubCodec{specID: SpecID, apiVer: "v1.3"})
	Register(stubCodec{specID: SpecID, apiVer: "v1.0"})

	if got, want := SupportedVersions(), []string{"v1.0", "v1.3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SupportedVersions = %v, want %v", got, want)
	}
	all := AllCodecs()
	if len(all) != 2 || all[0].APIVer() != "v1.0" || all[1].APIVer() != "v1.3" {
		t.Fatalf("AllCodecs must be ascending, got %v", all)
	}
	if c, ok := Get("v1.3"); !ok || c.APIVer() != "v1.3" {
		t.Fatalf("Get(v1.3) = %v, %v", c, ok)
	}
	if _, ok := Get("v1.1"); ok {
		t.Fatal("Get(v1.1) must miss: nothing serves that minor")
	}
	c, err := SelectHighest([]string{"v1.0", "v1.3", "v2.0"})
	if err != nil || c.APIVer() != "v1.3" {
		t.Fatalf("SelectHighest = %v, %v; want v1.3", c, err)
	}
	var none spec.ErrNoCommonVersion
	if _, err := SelectHighest([]string{"v2.0"}); !errors.As(err, &none) {
		t.Fatalf("a peer with no common minor must yield ErrNoCommonVersion, got %v", err)
	}
	if got := Default().APIVer(); got != "v1.3" {
		t.Fatalf("Default = %q, want the highest registered minor", got)
	}
}
