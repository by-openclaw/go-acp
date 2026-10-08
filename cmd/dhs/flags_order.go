package main

import (
	"flag"
	"strings"
)

// flagsBeforePositionals puts the flags of args ahead of its positional
// arguments, so that a flag written after the target is honoured. The
// standard parser stops at the first positional: `snmp walk <host> --oid X`
// walked the default subtree and said nothing about the flag it had
// dropped, while every usage line of this CLI reads `<verb> <target>
// [flags]`.
//
// Which tokens are flags, and which of them take the next token as their
// value, is asked of the verb's own FlagSet. A token that is not one of its
// flags stays where it is — a negative number given as a value, or a
// misspelt flag, which then reaches the parser first and is reported.
// Everything after `--` is positional, as written.
func flagsBeforePositionals(fs *flag.FlagSet, args []string) []string {
	var flags, positionals []string
	unknown := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		name, inline, isFlag := flagToken(a)
		if !isFlag {
			positionals = append(positionals, a)
			continue
		}
		f := fs.Lookup(name)
		if f == nil {
			if name == "h" || name == "help" {
				flags = append(flags, a)
				continue
			}
			if !looksNumeric(a) {
				// Not a flag of this verb: the parser must see it and say so.
				unknown = true
				flags = append(flags, a)
				continue
			}
			positionals = append(positionals, a)
			continue
		}
		flags = append(flags, a)
		if inline {
			continue
		}
		if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
			continue
		}
		if i+1 >= len(args) {
			// A flag that takes a value and has none: it goes to the
			// parser last and alone, which reports it. Followed by the
			// terminator it would swallow that as its value.
			return flags
		}
		i++
		flags = append(flags, args[i])
	}
	if len(positionals) == 0 {
		return flags
	}
	if unknown {
		// Keep the unknown flag reachable: no terminator in front of it.
		return append(flags, positionals...)
	}
	return append(append(flags, "--"), positionals...)
}

// flagToken splits "-name", "--name" or "--name=value" into the flag's
// name and whether its value is in the token.
func flagToken(a string) (name string, inline, isFlag bool) {
	if len(a) < 2 || a[0] != '-' {
		return "", false, false
	}
	name = strings.TrimPrefix(strings.TrimPrefix(a, "-"), "-")
	if name == "" || name[0] == '-' || name[0] == '=' {
		return "", false, false
	}
	if eq := strings.IndexByte(name, '='); eq >= 0 {
		return name[:eq], true, true
	}
	return name, false, true
}

// looksNumeric says a dash-led token is a negative number, not a flag.
func looksNumeric(a string) bool {
	rest := strings.TrimPrefix(a, "-")
	return rest != "" && (rest[0] == '.' || (rest[0] >= '0' && rest[0] <= '9'))
}
