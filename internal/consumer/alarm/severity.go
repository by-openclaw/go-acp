// Package alarm turns values into severities.
//
// Every connector already produces the same neutral leaf (a
// consumer.Object with value, unit and range) and the same neutral
// change (a consumer.Event, pushed by the device or polled by the
// monitor). This package is the one place that decides what a value
// MEANS: a per-model template maps paths to severity bands, and an
// evaluator turns each sample into a transition — raised, cleared,
// worsened — with hysteresis and hold so a value hovering on a
// threshold does not flood the plant.
//
// It is protocol-neutral by construction: it names no wire format,
// reads no device, and sends nothing. The caller feeds it events and
// decides what to do with the transitions (print them, log them as
// RFC 5424 with the severity code, export them as metrics).
package alarm

import "fmt"

// Severity is the X.733 ladder the broadcast plant already speaks,
// plus the two ends an operator needs: info for a value no rule
// covers, error for a value that cannot be judged at all.
//
// The order is meaningful: a higher value is worse, so a transition
// is a worsening when Severity > Prior.
type Severity uint8

const (
	// Info is a value no template row names. It is reported as a
	// change, never as an alarm.
	Info Severity = iota

	// Normal is a value a rule covers and finds where it should be.
	// Reaching Normal from anything worse is a CLEAR.
	Normal

	// Minor, Major and Critical are the three alarm rungs, on either
	// side of normal (a value can be too low or too high).
	Minor
	Major
	Critical

	// Error is "I cannot judge this": the device stopped answering for
	// this object, or it answered with something the rule cannot read
	// (a string where a number was declared). It is not a worse
	// Critical — it is the absence of a verdict, and it is louder than
	// Major because silence hides faults.
	Error
)

// names are the wire/JSON spelling of each severity.
var names = [...]string{"info", "normal", "minor", "major", "critical", "error"}

// String returns the lower-case name used in templates, logs and the CLI.
func (s Severity) String() string {
	if int(s) >= len(names) {
		return fmt.Sprintf("severity(%d)", uint8(s))
	}
	return names[s]
}

// ParseSeverity reads a severity name. It accepts only the names a
// template may use — "info" and "error" are produced by the evaluator,
// never authored, so they are refused here.
func ParseSeverity(s string) (Severity, error) {
	switch s {
	case "normal":
		return Normal, nil
	case "minor":
		return Minor, nil
	case "major":
		return Major, nil
	case "critical":
		return Critical, nil
	}
	return Info, fmt.Errorf("%q is not a severity (normal, minor, major, critical)", s)
}

// Syslog maps the ladder onto RFC 5424 severity codes, so one line
// carries a severity every collector already understands:
//
//	info     -> 6 informational
//	normal   -> 5 notice        (a clear is worth noticing)
//	minor    -> 4 warning
//	major    -> 3 error
//	critical -> 2 critical
//	error    -> 3 error
func (s Severity) Syslog() int {
	switch s {
	case Normal:
		return 5
	case Minor:
		return 4
	case Major, Error:
		return 3
	case Critical:
		return 2
	}
	return 6
}

// IsAlarm reports whether the severity is one an operator must act on.
func (s Severity) IsAlarm() bool { return s >= Minor }
