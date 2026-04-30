package storage

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDataDir_Override(t *testing.T) {
	dir := t.TempDir()
	got := DataDir(dir)
	if got != dir {
		t.Errorf("override: got %q want %q", got, dir)
	}
}

func TestDataDir_DefaultIsAbsolute(t *testing.T) {
	got := DataDir("")
	if got == "" || got == "." {
		// Path may be "." in unprivileged sandboxes — acceptable.
		return
	}
	if !filepath.IsAbs(got) {
		t.Errorf("default dir not absolute: %q", got)
	}
}

func TestDataDir_PortableOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("portable layout is Windows-only")
	}
	got := DataDir("")
	// Portable rule: the data dir must be the directory containing
	// the running test binary, not %APPDATA%\dhs.
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("can't resolve os.Executable: %v", err)
	}
	want := filepath.Dir(exe)
	if got != want {
		t.Errorf("portable: got %q want %q (exe-dir)", got, want)
	}
	if strings.HasSuffix(strings.ToLower(got), `\appdata\roaming\dhs`) {
		t.Errorf("portable layout fell back to %%APPDATA%%: %q", got)
	}
}

func TestSubdirHelpers(t *testing.T) {
	root := t.TempDir()
	if got := LogsDir(root); !strings.HasSuffix(got, "logs") {
		t.Errorf("LogsDir: got %q", got)
	}
	if got := CapturesDir(root); !strings.HasSuffix(got, "captures") {
		t.Errorf("CapturesDir: got %q", got)
	}
	if got := ConfigPath(root); !strings.HasSuffix(got, "config.yaml") {
		t.Errorf("ConfigPath: got %q", got)
	}
}
