package main

import (
	"fmt"
	"net"
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
	// The tree store (.cache: DMs, manifests) was rooted at start-up,
	// before this flag was read: re-root it here, or only snapshots/
	// and captures/ would follow --data-dir.
	treeStore = datastore.NewTreeStore(filepath.Join(abs, ".cache"))
	return out, os.Setenv(datastore.DataDirEnv, abs)
}

// primaryEnv carries the global --primary for this run only: main
// clears any inherited value first, so a stale shell export can never
// re-key a device.
const primaryEnv = "DHS_PRIMARY_RUN"

// stripPrimaryFlag removes the global --primary[=]IP from args: the
// target of this run is a controller of the device whose primary IP is
// given — its folder, manifest, logs and captures are that device's
// (ADR-0028 §2). For a connector that reads no serial number, where a
// second controller cannot be recognised on its own.
func stripPrimaryFlag(args []string) ([]string, error) {
	_ = os.Unsetenv(primaryEnv)
	ip := ""
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--primary" || a == "-primary":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--primary needs the device's primary IP")
			}
			ip = args[i+1]
			i++
		case strings.HasPrefix(a, "--primary="):
			ip = strings.TrimPrefix(a, "--primary=")
		case strings.HasPrefix(a, "-primary="):
			ip = strings.TrimPrefix(a, "-primary=")
		default:
			out = append(out, a)
		}
	}
	if ip == "" {
		return out, nil
	}
	if net.ParseIP(hostOnly(ip)) == nil {
		return nil, fmt.Errorf("--primary %q is not an IP address", ip)
	}
	return out, os.Setenv(primaryEnv, hostOnly(ip))
}
