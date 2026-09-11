package is11

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/spec"
)

// Default on an empty registry is the "forgot the blank import" bug; a
// panic that names the missing import is the only useful answer, and
// it is only reachable by swapping the package registry for an empty
// one (every real build registers v1.0 at init).
func TestDefaultPanicsWhenNothingRegistered(t *testing.T) {
	saved := versions
	versions = spec.NewRegistry[Codec]()
	t.Cleanup(func() { versions = saved })

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Default must panic on an empty registry")
		}
		if msg, _ := r.(string); !strings.Contains(msg, "is11/v10") {
			t.Fatalf("panic = %v, want it to name the missing import", r)
		}
	}()
	Default()
}
