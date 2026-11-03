package main

import (
	"dhs/internal/consumer"
)

// writeToHelp is the help of --write-to on every writing verb.
const writeToHelp = "confirm the target of a write: repeat the host here, exactly as given. every connector that can write (consumer.WriteGuard) refuses a write without it and stays with the dry run (ADR-0037 P5)"

// dryRunHelp is the help of --dry-run on every writing verb.
const dryRunHelp = "read, report what would be written, send nothing (the same switch as --check, ADR-0007)"

// writeGuard decides whether a write may be sent (ADR-0037 P5).
//
// A --write-to that does not name the host is always refused: the
// operator confirmed another target. A plugin that declares
// consumer.WriteGuard is refused without --write-to, with its reason;
// every plugin that can write declares it (TestEveryWritingPluginGuards).
// Both refusals are validation errors (exit 2): the device was never
// asked.
func writeGuard(verb, host, writeTo string, plug consumer.Protocol) error {
	if writeTo != "" && writeTo != host {
		return &consumer.ValidationError{Field: verb, Reason: "--write-to " + writeTo + " does not match the host " + host}
	}
	g, guarded := plug.(consumer.WriteGuard)
	if !guarded || writeTo != "" {
		return nil
	}
	reason := g.WriteGuardReason()
	if reason != "" {
		reason = " (" + reason + ")"
	}
	return &consumer.ValidationError{Field: verb,
		Reason: "this would WRITE to " + host + ": add --write-to " + host + " to confirm the target, or stay with --dry-run" + reason}
}
