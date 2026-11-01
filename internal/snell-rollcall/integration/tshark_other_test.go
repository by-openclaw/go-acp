//go:build integration && !unix

package rollcall_integration

import (
	"os/exec"
	"testing"
)

// dropRoot has nothing to do: only Unix has a root Wireshark refuses Lua to.
func dropRoot(t *testing.T, pcap, dissector string) (string, string, func(*exec.Cmd)) {
	t.Helper()
	return pcap, dissector, func(*exec.Cmd) {}
}
