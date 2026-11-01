//go:build integration && unix

package rollcall_integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

// nobody is the account tshark runs as when the tests run as root.
const nobody = 65534

// dropRoot makes a run as root behave like anyone else's: tshark runs as
// nobody, on copies of its input, since root's checkout is not nobody's to
// read. Anyone else's run is left alone.
func dropRoot(t *testing.T, pcap, dissector string) (string, string, func(*exec.Cmd)) {
	t.Helper()
	if os.Geteuid() != 0 {
		return pcap, dissector, func(*exec.Cmd) {}
	}
	dir, err := os.MkdirTemp("", "rollcall-tshark")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return readable(t, dir, pcap), readable(t, dir, dissector), func(cmd *exec.Cmd) {
		cmd.Env = append(os.Environ(), "HOME="+dir)
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: nobody, Gid: nobody}}
	}
}

// readable copies a file into dir, for anyone to read.
func readable(t *testing.T, dir, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, filepath.Base(path))
	if err := os.WriteFile(out, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return out
}
