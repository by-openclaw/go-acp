package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// writePIDFile atomically writes the current process PID to path (.tmp +
// rename, per the repo's file-write convention). Called by `serve --pidfile`.
func writePIDFile(path string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// runProducerStop implements the canonical producer `stop` verb (ADR-0002):
// read the PID file a `serve --pidfile` instance wrote and signal it. Graceful
// on Unix (SIGTERM → serve's signal.NotifyContext teardown); hard kill on
// Windows (no cross-process SIGTERM in the Go stdlib). See stopProcess in the
// build-tagged cmd_producer_stop_{unix,windows}.go.
func runProducerStop(_ context.Context, protoName string, args []string) error {
	fs := flag.NewFlagSet("producer "+protoName+" stop", flag.ContinueOnError)
	pidfile := fs.String("pidfile", "", "PID file the target `serve --pidfile` wrote (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *pidfile == "" {
		return fmt.Errorf("producer %s stop: --pidfile is required (the path serve was started with)", protoName)
	}
	pid, err := signalPIDFile(*pidfile)
	if err != nil {
		return err
	}
	fmt.Printf("producer %s stop: signaled pid %d (pidfile %s)\n", protoName, pid, *pidfile)
	return nil
}

// signalPIDFile reads a PID file, signals that process (graceful on Unix / kill
// on Windows via stopProcess), removes the file, and returns the PID. Shared by
// `stop` and `ensure --state absent`.
func signalPIDFile(pidfile string) (int, error) {
	data, err := os.ReadFile(pidfile)
	if err != nil {
		return 0, fmt.Errorf("read pidfile %s: %w", pidfile, err)
	}
	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, fmt.Errorf("pidfile %s: invalid PID %q: %w", pidfile, pidStr, err)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return 0, fmt.Errorf("find process %d: %w", pid, err)
	}
	if err := stopProcess(proc); err != nil {
		return 0, fmt.Errorf("signal process %d: %w", pid, err)
	}
	_ = os.Remove(pidfile)
	return pid, nil
}
