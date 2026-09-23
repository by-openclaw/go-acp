package alarm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

// A template is per model, like the poll profile and the DM
// (ADR-0022): the same card in two frames alarms the same way. It is
// data an operator edits — one row per object pattern — and it is the
// only place a threshold is written down.

// Duration spells a hold as "10s" in JSON, like the monitor's profile.
type Duration time.Duration

// MarshalJSON renders the duration as a Go duration string.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// UnmarshalJSON accepts a duration string ("10s") or nanoseconds.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch x := v.(type) {
	case string:
		p, err := time.ParseDuration(x)
		if err != nil {
			return fmt.Errorf("alarm: bad duration %q: %w", x, err)
		}
		*d = Duration(p)
	case float64:
		*d = Duration(time.Duration(x))
	default:
		return fmt.Errorf(`alarm: hold must be a string like "10s"`)
	}
	return nil
}

// Kind is how a row reads its values.
type Kind string

const (
	// KindNumber compares against the low/high bands.
	KindNumber Kind = "number"
	// KindCounter expects the value to keep rising; it alarms when it
	// stops. A decrease is a wrap or a reset, not a stall (the FusioN6
	// packet counters are 32-bit and do wrap).
	KindCounter Kind = "counter"
	// KindEnum maps listed values to severities. A row may instead (or
	// also) name the good value in Normal, and then every other value
	// takes the row's Severity — "locked, or not" needs no table.
	KindEnum Kind = "enum"
	// KindText compares the value with Normal, literally or as a
	// regular expression when Normal starts with "~". This is the
	// drift check: the value the plant's source of truth expects.
	KindText Kind = "text"
)

// Band is one rung on one side of normal.
type Band struct {
	// Severity is minor, major or critical.
	Severity string `json:"severity"`

	// Raise is the value at which the band is entered: at or above it
	// on the high side, at or below it on the low side.
	Raise float64 `json:"raise"`

	// Clear is the hysteresis point — the value must come back past it
	// before the band is left. Absent means no hysteresis (Clear =
	// Raise), which is honest but flaps on a value that hovers; Hold
	// is then the only damper.
	Clear *float64 `json:"clear,omitempty"`
}

// Row is one rule: which objects it covers and what their values mean.
type Row struct {
	// Match is a dotted path pattern: "*" is one segment, a leading
	// "**" any number of leading segments, a trailing "**" everything
	// under a prefix. The FIRST matching row wins, so write the
	// specific ones first.
	Match string `json:"match"`

	// Kind is how the value is read; empty means number.
	Kind Kind `json:"kind,omitempty"`

	// Low and High are the numeric bands, worst last is not required —
	// the evaluator picks the worst band the value satisfies.
	Low  []Band `json:"low,omitempty"`
	High []Band `json:"high,omitempty"`

	// Normal is the expected value for text (literal, or "~regex"),
	// or the value that means normal for an enum.
	Normal string `json:"normal,omitempty"`

	// Values maps an enum/bool value to a severity name.
	Values map[string]string `json:"values,omitempty"`

	// Severity is the verdict for a text mismatch or a stalled
	// counter; empty means major.
	Severity string `json:"severity,omitempty"`

	// StalledFor is how long a counter may stand still before it
	// alarms. Empty means the row's Hold, then 30s.
	StalledFor Duration `json:"stalled_for,omitempty"`

	// Hold is how long a candidate severity must persist before it is
	// adopted, raising or clearing. It is the anti-flap of first
	// resort; zero means adopt immediately.
	Hold Duration `json:"hold,omitempty"`

	// ClearHold overrides Hold when coming back towards normal.
	ClearHold Duration `json:"clear_hold,omitempty"`

	// FlapCap is how many transitions a minute this object may make
	// before it is reported as flapping and silenced until it settles.
	// Zero disables the cap.
	FlapCap int `json:"flap_cap,omitempty"`

	// Text is what an operator reads in the notification.
	Text string `json:"text,omitempty"`

	// Source records where the numbers come from (a device threshold,
	// a manual, a site rule). A row without one is a guess, and this
	// repo does not ship guesses.
	Source string `json:"source,omitempty"`

	// compiled regex for a "~" Normal, filled by Validate.
	re *regexp.Regexp
}

// Template is one model's alarm plan.
type Template struct {
	// Model is the DM identity the template belongs to
	// ("FusioN6@0x68cd783f"), or a bare model when it spans firmwares.
	Model string `json:"model"`

	// Protocol is optional and informational: a template is matched by
	// model, not by protocol name.
	Protocol string `json:"protocol,omitempty"`

	Rows []Row `json:"rows"`
}

// Load parses and validates a template.
func Load(data []byte) (*Template, error) {
	var t Template
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return nil, fmt.Errorf("alarm: decode template: %w", err)
	}
	if err := t.Validate(); err != nil {
		return nil, err
	}
	return &t, nil
}

// Encode renders the template as the operator's file: stable field
// order, one row per entry, trailing newline — so export, edit,
// import, export again is a no-op.
func (t *Template) Encode(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", " ")
	return enc.Encode(t)
}

// Validate enforces what a row must state to be actionable, and
// compiles the text patterns. It refuses rather than guessing: a band
// with an unknown severity, a clear point on the wrong side of its
// raise, or a row with no rule at all is an authoring mistake that
// would otherwise show up as silence in the plant.
func (t *Template) Validate() error {
	if len(t.Rows) == 0 {
		return fmt.Errorf("alarm: template has no rows")
	}
	seen := map[string]int{}
	for i := range t.Rows {
		r := &t.Rows[i]
		if strings.TrimSpace(r.Match) == "" {
			return fmt.Errorf("alarm: row %d has no match", i)
		}
		if j, dup := seen[r.Match]; dup {
			return fmt.Errorf("alarm: rows %d and %d both match %q (the first would always win)", j, i, r.Match)
		}
		seen[r.Match] = i

		switch r.Kind {
		case "", KindNumber:
			r.Kind = KindNumber
			if len(r.Low) == 0 && len(r.High) == 0 {
				return fmt.Errorf("alarm: row %d (%s) is a number rule with no band", i, r.Match)
			}
		case KindCounter, KindEnum, KindText:
		default:
			return fmt.Errorf("alarm: row %d (%s): unknown kind %q", i, r.Match, r.Kind)
		}
		if err := r.validateBands(i); err != nil {
			return err
		}
		for v, s := range r.Values {
			if _, err := ParseSeverity(s); err != nil {
				return fmt.Errorf("alarm: row %d (%s) value %q: %w", i, r.Match, v, err)
			}
		}
		if r.Severity != "" {
			if _, err := ParseSeverity(r.Severity); err != nil {
				return fmt.Errorf("alarm: row %d (%s): %w", i, r.Match, err)
			}
		}
		// An enum row says what the values mean, either by mapping them
		// (0=major) or by naming the good one (normal=3, anything else
		// takes the row's severity). Neither is a rule.
		if r.Kind == KindEnum && len(r.Values) == 0 && r.Normal == "" {
			return fmt.Errorf("alarm: row %d (%s) is an enum rule with neither values nor a normal value", i, r.Match)
		}
		if r.Kind == KindText {
			if r.Normal == "" {
				return fmt.Errorf("alarm: row %d (%s) is a text rule with no expected value", i, r.Match)
			}
			if strings.HasPrefix(r.Normal, "~") {
				re, err := regexp.Compile(r.Normal[1:])
				if err != nil {
					return fmt.Errorf("alarm: row %d (%s): %w", i, r.Match, err)
				}
				r.re = re
			}
		}
	}
	return nil
}

// validateBands checks one row's bands: known severities, and a clear
// point that lies on the normal side of its raise point.
func (r *Row) validateBands(i int) error {
	for side, bands := range map[string][]Band{"low": r.Low, "high": r.High} {
		for j := range bands {
			b := bands[j]
			if _, err := ParseSeverity(b.Severity); err != nil {
				return fmt.Errorf("alarm: row %d (%s) %s band %d: %w", i, r.Match, side, j, err)
			}
			if b.Clear == nil {
				continue
			}
			if side == "high" && *b.Clear > b.Raise {
				return fmt.Errorf("alarm: row %d (%s) high band %s: clear %v is above raise %v", i, r.Match, b.Severity, *b.Clear, b.Raise)
			}
			if side == "low" && *b.Clear < b.Raise {
				return fmt.Errorf("alarm: row %d (%s) low band %s: clear %v is below raise %v", i, r.Match, b.Severity, *b.Clear, b.Raise)
			}
		}
	}
	return nil
}

// RowFor returns the first row whose pattern matches the dotted path,
// or nil when no rule covers it (the value is then Info).
func (t *Template) RowFor(path string) *Row {
	segs := strings.Split(path, ".")
	for i := range t.Rows {
		if MatchPath(t.Rows[i].Match, segs) {
			return &t.Rows[i]
		}
	}
	return nil
}

// MatchPath reports whether a dotted pattern matches a dotted path:
// "*" matches exactly one segment, a leading "**" any number of
// leading segments, a trailing "**" everything under the prefix.
func MatchPath(pattern string, path []string) bool {
	pat := strings.Split(pattern, ".")
	if len(pat) > 0 && pat[0] == "**" {
		pat = pat[1:]
		if len(path) < len(pat) {
			return false
		}
		path = path[len(path)-len(pat):]
	}
	if n := len(pat); n > 0 && pat[n-1] == "**" {
		pat = pat[:n-1]
		if len(path) < len(pat) {
			return false
		}
		path = path[:len(pat)]
	}
	if len(pat) != len(path) {
		return false
	}
	for i, p := range pat {
		if p != "*" && p != path[i] {
			return false
		}
	}
	return true
}

// hold returns the row's adopt delay in the given direction.
func (r *Row) hold(towardsNormal bool) time.Duration {
	if towardsNormal && r.ClearHold > 0 {
		return time.Duration(r.ClearHold)
	}
	return time.Duration(r.Hold)
}

// verdict returns the row's severity for a mismatch or a stall.
func (r *Row) verdict() Severity {
	if r.Severity == "" {
		return Major
	}
	s, err := ParseSeverity(r.Severity)
	if err != nil {
		return Major
	}
	return s
}

// stalledFor is how long a counter may stand still.
func (r *Row) stalledFor() time.Duration {
	if r.StalledFor > 0 {
		return time.Duration(r.StalledFor)
	}
	if r.Hold > 0 {
		return time.Duration(r.Hold)
	}
	return 30 * time.Second
}
