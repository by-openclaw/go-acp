//go:build windows

package main

import "os"

// stopProcess terminates the target process. Windows has no cross-process
// SIGTERM equivalent in the Go stdlib (os.Process.Signal only supports Kill for
// other processes), so this is a hard kill. The serve process's own PID file is
// cleaned up here by runProducerStop rather than by the killed process's defer.
func stopProcess(p *os.Process) error {
	return p.Kill()
}
