package storage

import (
	"os"
	"path/filepath"
	"runtime"
)

// DataDir resolves the dhs writable data directory according to a
// portable-first rule:
//
//  1. If override is non-empty (typically the --data-dir flag), use it
//     verbatim. Mkdir -p the path on first call.
//  2. Else on Windows, when os.Executable resolves, use the directory
//     containing the running binary (portable layout — config.yaml,
//     logs/, captures/ all live next to dhs.exe). This keeps the
//     "drop the .exe on the Cerebrum host" workflow self-contained.
//  3. Else fall back to the user data dir per OS:
//       Linux:    $XDG_DATA_HOME/dhs   (default $HOME/.local/share/dhs)
//       macOS:    $HOME/Library/Application Support/dhs
//       Windows:  %APPDATA%\dhs        (only reached when os.Executable failed)
//
// The returned path is the directory; callers compose subpaths
// (logs, captures, devices) with filepath.Join.
func DataDir(override string) string {
	if override != "" {
		_ = os.MkdirAll(override, 0o755)
		return override
	}
	if runtime.GOOS == "windows" {
		if exe, err := os.Executable(); err == nil {
			return filepath.Dir(exe)
		}
	}
	switch runtime.GOOS {
	case "linux":
		if x := os.Getenv("XDG_DATA_HOME"); x != "" {
			return filepath.Join(x, "dhs")
		}
		return filepath.Join(os.Getenv("HOME"), ".local", "share", "dhs")
	case "darwin":
		return filepath.Join(os.Getenv("HOME"), "Library", "Application Support", "dhs")
	case "windows":
		// Reached only when os.Executable() failed — rare but possible
		// in unprivileged sandboxes.
		if a := os.Getenv("APPDATA"); a != "" {
			return filepath.Join(a, "dhs")
		}
	}
	return "."
}

// LogsDir returns the logs/ subdir of the data dir, ensuring it exists.
func LogsDir(override string) string {
	d := filepath.Join(DataDir(override), "logs")
	_ = os.MkdirAll(d, 0o755)
	return d
}

// CapturesDir returns the captures/ subdir of the data dir, ensuring
// it exists.
func CapturesDir(override string) string {
	d := filepath.Join(DataDir(override), "captures")
	_ = os.MkdirAll(d, 0o755)
	return d
}

// ConfigPath returns the canonical config.yaml path next to the data
// dir. The file may not exist; the caller is expected to handle that.
func ConfigPath(override string) string {
	return filepath.Join(DataDir(override), "config.yaml")
}
