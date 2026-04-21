// Scenario discovery test — walks every internal/<proto>/scenarios/ tree
// and runs every .json file through the declarative harness. Each scenario
// appears as its own sub-test in `go test -v` output.
//
// Add a new scenario = drop a .json in internal/<proto>/scenarios/. No
// test-code changes required.
package scenario_test

import (
	"path/filepath"
	"testing"

	"acp/internal/scenario"
)

// TestScenarios discovers every committed scenario under
// internal/*/scenarios/ and runs it. Failures are per-scenario — one green
// line per passing scenario, one red line per failing one.
func TestScenarios(t *testing.T) {
	roots, err := filepath.Glob(filepath.Join("..", "..", "..", "internal", "*", "scenarios"))
	if err != nil {
		t.Fatalf("glob scenarios: %v", err)
	}

	var paths []string
	for _, root := range roots {
		found, err := scenario.Discover(root)
		if err != nil {
			t.Fatalf("discover %s: %v", root, err)
		}
		paths = append(paths, found...)
	}
	if len(paths) == 0 {
		t.Fatalf("no scenarios found under internal/*/scenarios/ — check the layout docs")
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
