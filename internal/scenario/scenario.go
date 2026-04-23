// Package scenario defines a declarative format for error-path
// regression tests (#51) and the harness that runs them. A scenario
// is a JSON file describing:
//
//   - a committed wire capture to replay,
//   - the compliance-event labels that must appear while replaying,
//   - optionally, an error class + numeric status the decoder must
//     emit for the first error reply in the capture.
//
// The harness is consumed by tests/unit/scenario/scenario_test.go,
// which walks every internal/<proto>/scenarios/ directory and runs
// every file it finds. New scenarios are added by dropping a JSON
// file in place — no Go code changes required.
package scenario

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Scenario is the parsed JSON shape. ExpectErrorStatus is a pointer
// so the scenario author can omit it when no error status is expected
// (vs. the separate meaning of "expect status 0").
type Scenario struct {
	Name              string   `json:"name"`
	Protocol          string   `json:"protocol"`
	WireFile          string   `json:"wire_file"`
	ExpectEvents      []string `json:"expect_events,omitempty"`
	ExpectErrorClass  string   `json:"expect_error_class,omitempty"`
	ExpectErrorStatus *int     `json:"expect_error_status,omitempty"`

	// SourcePath is set by Load() to the absolute path of the
	// scenario file; used to resolve WireFile relative to the
	// scenario's own directory when needed.
	SourcePath string `json:"-"`
}

// Load reads and parses a scenario file. The parsed Scenario's
// SourcePath is set to the absolute path so the runner can resolve
// relative WireFile references correctly regardless of cwd.
func Load(path string) (*Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var s Scenario
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("abs %s: %w", path, err)
	}
	s.SourcePath = abs
	return &s, nil
}

// Discover walks a directory recursively and returns every *.json
// file found, sorted for deterministic ordering. Used by the test
// harness to enumerate every committed scenario.
func Discover(dir string) ([]string, error) {
	var out []string
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(p) == ".json" && filepath.Base(p) != "README.json" {
			out = append(out, p)
		}
		return nil
	})
	return out, err
}

// ResolveWirePath returns the absolute path to the scenario's
// referenced wire.jsonl. The JSON file's WireFile may be:
//   - absolute — used as-is
//   - relative to the scenario file's dir — resolved against SourcePath
//   - relative to the repo root (starts with "tests/") — resolved
//     against the repo root (walks up from SourcePath to find the
//     nearest go.mod)
func (s *Scenario) ResolveWirePath() (string, error) {
	if filepath.IsAbs(s.WireFile) {
		return s.WireFile, nil
	}
	// Try: relative to scenario directory
	dir := filepath.Dir(s.SourcePath)
	candidate := filepath.Join(dir, s.WireFile)
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	// Try: walk up to find go.mod (repo root) and resolve from there
	root := dir
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return filepath.Join(root, s.WireFile), nil
		}
		parent := filepath.Dir(root)
		if parent == root {
			break
		}
		root = parent
	}
	return "", fmt.Errorf("cannot resolve wire_file %q (tried scenario-dir and repo root)", s.WireFile)
}
