package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"dhs/internal/errcode"
	"dhs/internal/logging"
)

// Validation-layer codes raised at flag-parse time for R15 #476 logging
// flags. ClassUsage so the CLI returns exit 2 ("caller's input fault").
var (
	errLogLevelConflict  = errcode.New(errcode.LayerValidation, "log-level-conflict", errcode.ClassUsage)
	errLogFormatInvalid  = errcode.New(errcode.LayerValidation, "log-format-invalid", errcode.ClassUsage)
)

// logFlagSet groups every R15 logging flag a verb may add. Lives on the
// `flag.FlagSet` long enough to be parsed alongside the verb's own
// flags; resolve() turns the parsed state into a concrete *slog.Logger
// and applies side effects (--log-only redirects os.Stdout to discard).
//
// Acceptance for R15 #476:
//
//   - `-v` ladder: -v=info, -vv=debug, -vvv=trace, -vvvv=raw (trace +
//     hex-dump territory; today maps to trace because raw frame logs
//     are still routed through --capture).
//   - `--log-level` and `-v…` are mutually exclusive → resolve()
//     returns `validation:log-level-conflict` when both are set.
//   - `--log-format text|json|loki` picks the handler shape; loki uses
//     `ts` / lowercase level / `component` field names.
//   - `--log-only` suppresses operator stdout result lines so the
//     stderr log feed is the only output the operator parses.
type logFlagSet struct {
	level   string
	v1      bool
	v2      bool
	v3      bool
	v4      bool
	format  string
	logOnly bool
}

// addLogFlags wires the R15 flag set onto fs. Callers continue parsing
// their own flags on the same fs; resolve() must run after fs.Parse to
// inspect default-vs-explicit state.
//
// defaultLevel is the level the verb would use if neither -v… nor
// --log-level were set. Common.go (consumer) keeps "info"; producer
// also "info"; future verbs may differ.
func addLogFlags(fs *flag.FlagSet, defaultLevel string) *logFlagSet {
	lf := &logFlagSet{level: defaultLevel}
	fs.StringVar(&lf.level, "log-level", defaultLevel,
		"log level: trace, debug, info, warn, error, critical. Mutually exclusive with -v / -vv / -vvv / -vvvv.")
	fs.BoolVar(&lf.v1, "v", false, "verbose: --log-level info")
	fs.BoolVar(&lf.v2, "vv", false, "very verbose: --log-level debug")
	fs.BoolVar(&lf.v3, "vvv", false, "trace verbose: --log-level trace (per-frame decoded events)")
	fs.BoolVar(&lf.v4, "vvvv", false, "raw verbose: trace + raw S101/AN2 hex (extra-noisy)")
	fs.StringVar(&lf.format, "log-format", "text",
		"log format: text (default human-readable) | json (stdlib slog) | loki (R15 Loki/Promtail shape)")
	fs.BoolVar(&lf.logOnly, "log-only", false,
		"suppress stdout operator result lines so stderr log feed is the only output (tail -f friendly)")
	return lf
}

// effectiveLevel resolves the level name from the ladder + --log-level
// pair, returning an error when both are set.
func (lf *logFlagSet) effectiveLevel(defaultLevel string) (string, error) {
	ladder := lf.ladderLevel()
	custom := lf.level != defaultLevel
	if ladder != "" && custom {
		return "", fmt.Errorf("%w: --log-level=%q and -v… cannot both be set", errLogLevelConflict, lf.level)
	}
	if ladder != "" {
		return ladder, nil
	}
	return lf.level, nil
}

// ladderLevel returns the level name corresponding to the highest -v
// flag set; "" when none are set.
func (lf *logFlagSet) ladderLevel() string {
	switch {
	case lf.v4:
		return "trace"
	case lf.v3:
		return "trace"
	case lf.v2:
		return "debug"
	case lf.v1:
		return "info"
	default:
		return ""
	}
}

// resolve constructs the logger per --log-format and applies the
// --log-only stdout-suppression side effect. Must run after fs.Parse.
//
// defaultLevel is the verb's fallback when neither --log-level nor -v…
// is set.
func (lf *logFlagSet) resolve(defaultLevel string) (*slog.Logger, error) {
	levelName, err := lf.effectiveLevel(defaultLevel)
	if err != nil {
		return nil, err
	}
	level := logging.ParseLevel(levelName)
	var logger *slog.Logger
	switch lf.format {
	case "text", "":
		logger = logging.NewTextLogger(level)
	case "json":
		logger = logging.NewJSONLogger(os.Stderr, level)
	case "loki":
		logger = logging.NewLokiLogger(os.Stderr, level)
	default:
		return nil, fmt.Errorf("%w: --log-format=%q must be one of text|json|loki", errLogFormatInvalid, lf.format)
	}
	if lf.logOnly {
		// Suppress operator stdout. Verbs print results via fmt.Println /
		// fmt.Fprintln(os.Stdout, ...) — redirecting os.Stdout to discard
		// turns those into no-ops without touching any verb's render code.
		// Stderr stays attached for the log feed.
		os.Stdout = devNullStdout()
	}
	return logger, nil
}

// devNullStdout replaces os.Stdout with a write-only sink. Best-effort:
// on Windows the existing handle is dup'd over to NUL when openable; on
// failure we fall back to a pipe to discard so the file descriptor type
// stays a *os.File for downstream code that assumes it.
func devNullStdout() *os.File {
	if f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0); err == nil {
		return f
	}
	// Fallback for environments where os.DevNull isn't openable: a pipe
	// whose read end we leak intentionally (writes go nowhere meaningful
	// but the type remains *os.File).
	pr, pw, perr := os.Pipe()
	if perr == nil {
		go io.Copy(io.Discard, pr) //nolint:errcheck // best-effort discard
		return pw
	}
	return os.Stdout // last resort: keep the original
}
