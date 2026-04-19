// Scenario discovery test — walks tests/scenarios/** and runs every
// .json file through the declarative harness. Each scenario appears
// as its own sub-test in `go test -v` output.
//
// Add a new scenario = drop a .json in tests/scenarios/<proto>/. No
// test-code changes required.
package scenario_test

import (
	"path/filepath"
	"testing"

	"acp/internal/scenario"
)

// TestScenarios discovers every committed scenario under
// tests/scenarios/ and runs it. Failures are per-scenario — one green
// line per passing scenario, one red line per failing one.
func TestScenarios(t *testing.T) {
	root := filepath.Join("..", "..", "scenarios")
	paths, err := scenario.Discover(root)
	if err != nil {
		t.Fatalf("discover %s: %v", root, err)
	}
	if len(paths) == 0 {
		t.Fatalf("no scenarios found under %s — check the layout docs", root)
	}

	for _, p := range paths {
		p := p
		s, err := scenario.Load(p)
		if err != nil {
			t.Run(filepath.Base(p), func(t *testing.T) {
				t.Fatalf("load %s: %v", p, err)
			})
			continue
		}
		t.Run(s.Name, func(t *testing.T) {
			scenario.Run(t, s)
		})
	}
}
