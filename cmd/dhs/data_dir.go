package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dhs/internal/datastore"
)

// stripDataDirFlag removes the global --data-dir[=]DIR from args and
// exports it as DHS_DATA_DIR, the root every ADR-0028 bucket (.cache/,
// snapshots/, captures/) then lives under. Without it the root is the
// binary's install dir (<X> for <X>/bin/dhs), or DHS_DATA_DIR when the
// environment already names one.
func stripDataDirFlag(args []string) ([]string, error) {
	dir := ""
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--data-dir" || a == "-data-dir":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--data-dir needs a directory")
			}
			dir = args[i+1]
			i++
		case strings.HasPrefix(a, "--data-dir="):
			dir = strings.TrimPrefix(a, "--data-dir=")
		case strings.HasPrefix(a, "-data-dir="):
			dir = strings.TrimPrefix(a, "-data-dir=")
		default:
			out = append(out, a)
		}
	}
	if dir == "" {
		return out, nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("--data-dir %s: %w", dir, err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("--data-dir: %w", err)
	}
	return out, os.Setenv(datastore.DataDirEnv, abs)
}
