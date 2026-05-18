package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"dhs/internal/errcode"
)

// R14 #475 validation codes for the --ensure flag.
var (
	errEnsureInvalidMode    = errcode.New(errcode.LayerValidation, "ensure-invalid-mode", errcode.ClassUsage)
	errEnsureNoDefault      = errcode.New(errcode.LayerValidation, "no-default-declared", errcode.ClassUsage)
	errEnsureModePending    = errcode.New(errcode.LayerValidation, "ensure-mode-pending", errcode.ClassUsage)
)

// ensureMode is the parsed --ensure flag value. Empty = flag not set
// (verb runs in legacy non-ensure mode).
type ensureMode string

const (
	ensureUnset   ensureMode = ""
	ensurePresent ensureMode = "present"
	ensureAbsent  ensureMode = "absent"
	ensureDryrun  ensureMode = "dryrun"
)

// addEnsureFlag wires the `--ensure` flag onto an existing FlagSet.
// Returns a pointer the caller reads after fs.Parse.
//
// R14 #475: Ansible-friendly idempotent verbs. `--ensure present`
// makes set / matrix / invoke verbs check current state before
// sending the write; `--ensure absent` reverses; `--ensure dryrun`
// shows what would change without sending. Output is JSON with the
// `changed` boolean Ansible playbooks read directly.
func addEnsureFlag(fs *flag.FlagSet) *string {
	return fs.String("ensure", "",
		"R14 #475: Ansible idempotency. One of present | absent | dryrun. "+
			"Empty = legacy mode (verb always sends). present = read current, "+
			"set only if different. absent = reset to declared default (Parameter "+
			"only; errors if no Default). dryrun = compare without sending.")
}

// parseEnsureMode validates the --ensure value. "" => ensureUnset.
func parseEnsureMode(raw string) (ensureMode, error) {
	switch ensureMode(raw) {
	case ensureUnset:
		return ensureUnset, nil
	case ensurePresent, ensureAbsent, ensureDryrun:
		return ensureMode(raw), nil
	}
	return ensureUnset, fmt.Errorf("%w: --ensure=%q must be present|absent|dryrun", errEnsureInvalidMode, raw)
}

// ensureReport is the structured stdout shape every --ensure call
// emits (Ansible reads this with the json filter). Fields match the
// R14 #475 spec exactly so a playbook author can rely on the schema
// across protocols and verbs.
type ensureReport struct {
	Verb    string `json:"verb"`
	Ensure  string `json:"ensure"`
	Changed bool   `json:"changed"`
	Before  any    `json:"before,omitempty"`
	After   any    `json:"after,omitempty"`
	Diff    string `json:"diff,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// emitEnsureReport writes the JSON-encoded ensure report to stdout
// in a deterministic key order (json.Encoder preserves struct order).
func emitEnsureReport(r ensureReport) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// ensureFmtDiff builds the `value: <before> -> <after>` string the
// spec uses for human-readable diff. Returns empty when before == after.
func ensureFmtDiff(field string, before, after any) string {
	if fmt.Sprintf("%v", before) == fmt.Sprintf("%v", after) {
		return ""
	}
	return fmt.Sprintf("%s: %v -> %v", field, before, after)
}
