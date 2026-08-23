// Package audit analyses a captured NMOS plant and reports where it
// deviates from the AMWA specifications.
//
// The input is a harvest directory produced by an operator-side
// exporter: a tree of per-device folders each holding device.json,
// tree.json, report.txt, raw/ and sdp/. Nothing here talks to a
// network — an audit runs at a desk, days later, from a customer's
// export, and must reach the same verdict every time it is re-run over
// the same bytes.
//
// The audit never repairs and never normalises. Per the repo-wide
// spec-strict posture, a deviation is surfaced as a Finding with the
// clause it violates; the tool keeps going and audits the rest.
package audit

import (
	"fmt"
	"sort"
	"strings"
)

// Severity ranks a finding by what it costs the operator.
type Severity int

const (
	// SevInfo records an observation that is legal but worth seeing —
	// an inventory line, a version spread, an inactive sender.
	SevInfo Severity = iota
	// SevWarn marks a deviation that a tolerant controller absorbs but
	// that will bite on a stricter one, or an interoperability risk.
	SevWarn
	// SevError marks a clause violation: a conformant controller is
	// entitled to reject the resource outright.
	SevError
	// SevCritical marks a deviation that already breaks the plant —
	// the device cannot be routed, or two devices collide on the wire.
	SevCritical
)

// String renders the severity as the fixed-width token used in reports.
func (s Severity) String() string {
	switch s {
	case SevInfo:
		return "INFO"
	case SevWarn:
		return "WARN"
	case SevError:
		return "ERROR"
	case SevCritical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}

// MarshalText renders the severity for JSON/JSONL output.
func (s Severity) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// UnmarshalText reads back what MarshalText wrote. A report has to
// round-trip: comparing this month's audit against last month's means
// loading both, and a rank that only serialises one way makes that
// impossible.
func (s *Severity) UnmarshalText(b []byte) error {
	got, err := ParseSeverity(string(b))
	if err != nil {
		return err
	}
	*s = got
	return nil
}

// ParseSeverity maps a report token back to a Severity. It is
// case-insensitive so `--min-severity warn` works.
func ParseSeverity(s string) (Severity, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "INFO":
		return SevInfo, nil
	case "WARN", "WARNING":
		return SevWarn, nil
	case "ERROR", "ERR":
		return SevError, nil
	case "CRITICAL", "CRIT":
		return SevCritical, nil
	default:
		return SevInfo, fmt.Errorf("audit: unknown severity %q (want info|warn|error|critical)", s)
	}
}

// Finding is one audited deviation, keyed by a stable Code so a report
// can be diffed between two harvests of the same plant.
//
// Every field is populated from the capture — the audit never invents a
// value it did not read. Spec cites the clause the finding rests on so
// the operator can take the report to the vendor.
type Finding struct {
	Code     string   `json:"code"`
	Severity Severity `json:"severity"`

	// Target is the harvested device the finding belongs to, as the
	// exporter recorded it (`host:port`).
	Target string `json:"target"`
	// Device is the human label of that device, when it published one.
	Device string `json:"device,omitempty"`
	// Resource is the NMOS resource the finding is about, in
	// `<type>/<id>` form — e.g. `sender/2c47bf5e-…`.
	Resource string `json:"resource,omitempty"`

	// Detail states the deviation in one line, with the observed value.
	Detail string `json:"detail"`
	// Spec cites the clause: `IS-04 v1.3 sender.json manifest_href`.
	Spec string `json:"spec,omitempty"`
	// Hint says what to change, when there is an unambiguous fix.
	Hint string `json:"hint,omitempty"`
}

// String renders a finding as one report line.
func (f Finding) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-8s %-22s %s", f.Severity, f.Code, f.Detail)
	if f.Resource != "" {
		fmt.Fprintf(&b, " [%s]", f.Resource)
	}
	return b.String()
}

// sortFindings orders findings worst-first, then by code, then by
// resource — a stable order so two runs over the same capture produce
// byte-identical reports and `diff` shows only real change.
func sortFindings(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		switch {
		case a.Severity != b.Severity:
			return a.Severity > b.Severity
		case a.Code != b.Code:
			return a.Code < b.Code
		case a.Target != b.Target:
			return a.Target < b.Target
		default:
			return a.Resource < b.Resource
		}
	})
}
