package main

// Per-verb -h/--help composition (#751 G5, refs #462). Every generic
// verb already has a rich catalogue help function (usage line,
// description, EXAMPLES) — but `dhs consumer <proto> <verb> -h` only
// printed the bare flag defaults. verbUsageFn composes BOTH: the
// verb's own help text followed by the complete, auto-generated flag
// list, so -h always shows every argument AND a runnable example,
// and the two can never drift apart (the flag list comes from the
// FlagSet itself).

import (
	"flag"
	"fmt"
)

// verbUsageFn returns a flag.Usage that prints the verb's rich help
// followed by all defined flags.
func verbUsageFn(fs *flag.FlagSet, help func()) func() {
	return func() {
		help()
		_, _ = fmt.Fprintf(fs.Output(), "\nFLAGS (all arguments)\n")
		fs.PrintDefaults()
	}
}
