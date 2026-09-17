package main

// --settings <file>: a YAML file of DEFAULTS for any verb's flags,
// so an operator (or an Ansible template) writes one file instead of
// a wall of arguments.
//
// The contract, fixed by design review:
//
//   - the file is FLAT: `flag-name: value`, names exactly as the flag
//     spells them (1:1 — nothing is invented for the file);
//   - precedence is explicit flags > settings file > built-in
//     defaults. A flag given on the command line always wins;
//   - one file may serve MANY verbs: keys that do not name a flag of
//     the running verb are ignored (that is what makes the file
//     shareable), so a typo is found by reading `-h`, not a crash;
//   - the path comes from `--settings <file>` on any migrated verb,
//     or the DHS_SETTINGS environment variable (flag wins).
//
// The parser is deliberately a MINIMAL flat-YAML reader (stdlib only,
// same posture as internal/export's emitter): scalars, quotes,
// comments. Everything a flag can hold is a string to flag.Set, so no
// type system is needed — and nesting is refused loudly rather than
// half-understood.

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// settingsEnvVar names the fallback path source.
const settingsEnvVar = "DHS_SETTINGS"

// stripSettingsFlag removes --settings[=]<path> from args, returning
// the path (or the DHS_SETTINGS value, or "") and the remaining args.
func stripSettingsFlag(args []string) (string, []string) {
	path := os.Getenv(settingsEnvVar)
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--settings" || a == "-settings":
			if i+1 < len(args) {
				path = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--settings="):
			path = strings.TrimPrefix(a, "--settings=")
		case strings.HasPrefix(a, "-settings="):
			path = strings.TrimPrefix(a, "-settings=")
		default:
			out = append(out, a)
		}
	}
	return path, out
}

// parseFlatYAML reads the flat `key: value` shape. Comments and blank
// lines are skipped; quoted values are unquoted; any indented or
// structured line is an error — the settings contract is flat.
func parseFlatYAML(raw string) (map[string]string, error) {
	out := map[string]string{}
	for n, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimRight(line, " \t\r")
		if trimmed == "" || strings.HasPrefix(strings.TrimSpace(trimmed), "#") || trimmed == "---" {
			continue
		}
		if trimmed[0] == ' ' || trimmed[0] == '\t' || trimmed[0] == '-' {
			return nil, fmt.Errorf("settings: line %d: nested or list YAML is not part of the flat flag contract: %q", n+1, strings.TrimSpace(trimmed))
		}
		key, val, found := strings.Cut(trimmed, ":")
		if !found {
			return nil, fmt.Errorf("settings: line %d: not a `flag-name: value` line: %q", n+1, trimmed)
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if i := findCommentStart(val); i >= 0 {
			val = strings.TrimSpace(val[:i])
		}
		if len(val) >= 2 && (val[0] == '"' && val[len(val)-1] == '"' || val[0] == '\'' && val[len(val)-1] == '\'') {
			val = val[1 : len(val)-1]
		}
		if key == "" {
			return nil, fmt.Errorf("settings: line %d: empty flag name", n+1)
		}
		out[key] = val
	}
	return out, nil
}

// findCommentStart locates a ` #` comment outside quotes.
func findCommentStart(s string) int {
	inQ := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inQ != 0:
			if c == inQ {
				inQ = 0
			}
		case c == '"' || c == '\'':
			inQ = c
		case c == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t'):
			return i
		}
	}
	return -1
}

// applySettings fills every flag the command line did NOT set from
// the settings map. Runs AFTER fs.Parse — flag.Visit is the record of
// what was explicitly given.
func applySettings(fs *flag.FlagSet, settings map[string]string) error {
	if len(settings) == 0 {
		return nil
	}
	setExplicitly := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { setExplicitly[f.Name] = true })
	var applyErr error
	fs.VisitAll(func(f *flag.Flag) {
		if applyErr != nil || setExplicitly[f.Name] {
			return
		}
		v, has := settings[f.Name]
		if !has {
			return
		}
		if err := fs.Set(f.Name, v); err != nil {
			applyErr = fmt.Errorf("settings: %s: %v", f.Name, err)
		}
	})
	return applyErr
}

// parseVerbFlags is the drop-in replacement for fs.Parse on verbs
// that honour --settings: strip the flag, parse the rest, then fill
// unset flags from the file.
func parseVerbFlags(fs *flag.FlagSet, args []string) error {
	path, rest := stripSettingsFlag(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	settings, err := parseFlatYAML(string(raw))
	if err != nil {
		return err
	}
	return applySettings(fs, settings)
}
