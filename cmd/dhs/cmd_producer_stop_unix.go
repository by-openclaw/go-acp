//go:build !windows

package main

import (
	"os"
	"syscall"
)

// stopProcess sends a graceful SIGTERM so the target serve's
// signal.NotifyContext(os.Interrupt, SIGTERM) teardown runs and it shuts down
// cleanly (drains sessions, removes its pidfile).
func stopProcess(p *os.Process) error {
	return p.Signal(syscall.SIGTERM)
}
