package main

import (
	"flag"
	"fmt"

	cerebrum "dhs/internal/cerebrum-nb/consumer"
)

// cerebrumWriteFlags is ADR-0037 P5 on a Northbound writing verb. The
// cerebrum-nb dispatcher owns its verbs, so the guard of the generic set,
// import and ensure never runs for it: every writing verb registers these
// two flags and asks guard before it dials.
type cerebrumWriteFlags struct {
	writeTo *string
	dryRun  *bool
}

// addCerebrumWriteFlags registers --write-to and --dry-run. --dry-run is
// the verb's --check where it has one.
func addCerebrumWriteFlags(fs *flag.FlagSet) cerebrumWriteFlags {
	return cerebrumWriteFlags{
		writeTo: fs.String("write-to", "", writeToHelp),
		dryRun:  fs.Bool("dry-run", false, dryRunHelp),
	}
}

// guard refuses a write whose target is not named twice. A dry run sends
// nothing and is not refused, except for a --write-to naming another host:
// the operator confirmed another target. The refusal comes before the dial,
// so the server is never asked.
func (w cerebrumWriteFlags) guard(verb string, positionals []string, dryRun bool) error {
	if len(positionals) == 0 {
		return nil // no host: the dial reports it
	}
	host := positionals[0]
	if dryRun {
		return writeGuard(verb, host, *w.writeTo, nil)
	}
	return writeGuard(verb, host, *w.writeTo, &cerebrum.Plugin{})
}

// dryRunNote reports, for a verb that reads nothing before it writes, what
// a dry run would have sent.
func (w cerebrumWriteFlags) dryRunNote(verb, what string) {
	fmt.Printf("[%s] would send %s (dry run, nothing sent)\n", verb, what)
}
