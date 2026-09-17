package datastore

import (
	"errors"
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

// swapGOOS and swapExecutable pin the two resolution inputs DataDir
// / ProjectRoot cannot vary on a single test host; both restore on
// cleanup so the remaining tests see the real values.
func swapGOOS(t *testing.T, v string) {
	t.Helper()
	orig := goos
	goos = v
	t.Cleanup(func() { goos = orig })
}

func swapExecutable(t *testing.T, fn func() (string, error)) {
	t.Helper()
	orig := osExecutable
	osExecutable = fn
	t.Cleanup(func() { osExecutable = orig })
}

// TestDataDir_Override_CreatesDir pins rule 1 of DataDir: the flag
// value is used verbatim AND mkdir -p'd, so a fresh --data-dir works
// on first run without the operator pre-creating it.
func TestDataDir_Override_CreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fresh", "data")
	if got := DataDir(dir); got != dir {
		t.Fatalf("override: got %q want %q", got, dir)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("override dir not created: err=%v", err)
	}
}

// TestDataDir_PerOS walks the portable-first rule on every OS from
// one host: Windows prefers the binary's own directory and only falls
// to %APPDATA% when the binary cannot be located; Linux honours
// XDG_DATA_HOME before $HOME/.local/share; macOS is fixed; an unknown
// OS with nothing to go on lands in the working directory.
func TestDataDir_PerOS(t *testing.T) {
	exeDir := t.TempDir()
	exe := filepath.Join(exeDir, "dhs.exe")
	noExe := func() (string, error) { return "", errors.New("executable: not permitted") }
	withExe := func() (string, error) { return exe, nil }

	cases := []struct {
		name string
		goos string
		exe  func() (string, error)
		env  map[string]string
		want string
	}{
		{
			name: "windows portable layout uses the binary's directory",
			goos: "windows", exe: withExe,
			env:  map[string]string{"APPDATA": `C:\Users\op\AppData\Roaming`},
			want: exeDir,
		},
		{
			name: "windows without a locatable binary falls back to APPDATA",
			goos: "windows", exe: noExe,
			env:  map[string]string{"APPDATA": filepath.Join("C:", "roaming")},
			want: filepath.Join("C:", "roaming", "dhs"),
		},
		{
			name: "windows without binary nor APPDATA lands in cwd",
			goos: "windows", exe: noExe,
			env:  map[string]string{"APPDATA": ""},
			want: ".",
		},
		{
			name: "linux honours XDG_DATA_HOME",
			goos: "linux", exe: withExe,
			env:  map[string]string{"XDG_DATA_HOME": "/xdg", "HOME": "/home/op"},
			want: filepath.Join("/xdg", "dhs"),
		},
		{
			name: "linux defaults to HOME/.local/share",
			goos: "linux", exe: withExe,
			env:  map[string]string{"XDG_DATA_HOME": "", "HOME": "/home/op"},
			want: filepath.Join("/home/op", ".local", "share", "dhs"),
		},
		{
			name: "darwin uses Application Support",
			goos: "darwin", exe: withExe,
			env:  map[string]string{"HOME": "/Users/op"},
			want: filepath.Join("/Users/op", "Library", "Application Support", "dhs"),
		},
		{
			name: "unknown OS lands in cwd",
			goos: "plan9", exe: withExe,
			env:  map[string]string{},
			want: ".",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			swapGOOS(t, tc.goos)
			swapExecutable(t, tc.exe)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if got := DataDir(""); got != tc.want {
				t.Fatalf("DataDir: got %q want %q", got, tc.want)
			}
		})
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
