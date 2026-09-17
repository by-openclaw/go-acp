package audit

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// RenderText writes the human report: an inventory of what was found,
// then the findings worst-first.
//
// The inventory comes first on purpose. "What does this plant actually
// expose" is the question an operator opens the report to answer, and
// the deviations only mean something once that is on the page.
func RenderText(w io.Writer, r Result) error {
	bw := &errWriter{w: w}

	bw.printf("NMOS PLANT AUDIT\n")
	bw.printf("%s\n\n", strings.Repeat("=", 78))

	bw.printf("INVENTORY — %d device(s)\n\n", len(r.Inventory))
	bw.printf("%-24s %-28s %-9s %5s %5s %5s\n", "TARGET", "LABEL", "ROLE", "SND", "RCV", "SDP")
	bw.printf("%s\n", strings.Repeat("-", 78))
	for _, inv := range r.Inventory {
		bw.printf("%-24s %-28s %-9s %5d %5d %5d\n",
			trunc(inv.Target, 24), trunc(inv.Label, 28), inv.Role,
			inv.Senders, inv.Receivers, inv.SDPFiles)
	}

	bw.printf("\nAPI SURFACE\n\n")
	bw.printf("%-24s %s\n", "TARGET", "APIS")
	bw.printf("%s\n", strings.Repeat("-", 78))
	for _, inv := range r.Inventory {
		names := make([]string, 0, len(inv.APIs))
		for n := range inv.APIs {
			names = append(names, n)
		}
		sort.Strings(names)
		parts := make([]string, 0, len(names))
		for _, n := range names {
			parts = append(parts, n+"("+strings.Join(inv.APIs[n], ",")+")")
		}
		bw.printf("%-24s %s\n", trunc(inv.Target, 24), strings.Join(parts, " "))
	}

	// Grouping comes before the findings because it changes how they
	// read. "13 hints absent" means one thing on a plant with 40 healthy
	// groups and another on a plant with none.
	bw.printf("\nGROUPING (BCP-002-01) — which devices say what belongs together\n\n")
	bw.printf("%-24s %-28s %6s %6s %7s\n", "TARGET", "LABEL", "SND+", "RCV+", "GROUPS")
	bw.printf("%s\n", strings.Repeat("-", 78))
	for _, inv := range r.Inventory {
		if inv.Senders == 0 && inv.Receivers == 0 {
			continue
		}
		bw.printf("%-24s %-28s %6s %6s %7d\n",
			trunc(inv.Target, 24), trunc(inv.Label, 28),
			ofTotal(inv.HintedSenders, inv.Senders),
			ofTotal(inv.HintedReceivers, inv.Receivers),
			inv.Groups)
	}
	if len(r.Groups) == 0 {
		bw.printf("\n  no group hints anywhere in this plant — a controller sees a flat\n")
		bw.printf("  list of essences and cannot offer a grouped route or a breakaway\n")
	} else {
		bw.printf("\n%-28s %-34s %5s %5s %s\n", "GROUP", "ROLES (levels)", "SND", "RCV", "DEVICES")
		bw.printf("%s\n", strings.Repeat("-", 78))
		shown := r.Groups
		const maxGroups = 40
		if len(shown) > maxGroups {
			shown = shown[:maxGroups]
		}
		for _, g := range shown {
			bw.printf("%-28s %-34s %5d %5d %s\n",
				trunc(g.Name, 28), trunc(strings.Join(g.Roles, ", "), 34),
				g.Senders, g.Receivers, trunc(strings.Join(g.Devices, ", "), 20))
		}
		if len(r.Groups) > maxGroups {
			bw.printf("  … %d more group(s); use --format json for the full pivot\n", len(r.Groups)-maxGroups)
		}
	}

	bw.printf("\nFINDINGS — %d\n\n", len(r.Findings))
	if len(r.Findings) == 0 {
		bw.printf("  none at or above the requested severity\n")
	}
	lastSev := Severity(-1)
	for _, f := range r.Findings {
		if f.Severity != lastSev {
			bw.printf("\n  %s\n  %s\n", f.Severity, strings.Repeat("-", 40))
			lastSev = f.Severity
		}
		bw.printf("  %-30s %s\n", f.Code, f.Detail)
		where := f.Device
		if where == "" {
			where = f.Target
		}
		bw.printf("  %-30s   on %s", "", where)
		if f.Resource != "" {
			bw.printf("  %s", f.Resource)
		}
		bw.printf("\n")
		if f.Spec != "" {
			bw.printf("  %-30s   spec %s\n", "", f.Spec)
		}
		if f.Hint != "" {
			bw.printf("  %-30s   → %s\n", "", f.Hint)
		}
	}

	bw.printf("\nSUMMARY\n\n")
	for _, s := range []Severity{SevCritical, SevError, SevWarn, SevInfo} {
		bw.printf("  %-10s %d\n", s, r.Counts[s.String()])
	}
	return bw.err
}

// RenderJSON writes the whole result as one indented object — the shape
// a report generator or a diff between two exports consumes.
func RenderJSON(w io.Writer, r Result) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// RenderJSONL writes one finding per line.
//
// This is the form that survives being appended to across runs and read
// back by anything line-oriented — the same record shape whether it
// came from auditing an export or from a live test run, so a
// before/after comparison is a plain line diff.
func RenderJSONL(w io.Writer, r Result) error {
	enc := json.NewEncoder(w)
	for _, f := range r.Findings {
		if err := enc.Encode(f); err != nil {
			return err
		}
	}
	return nil
}

// errWriter collapses a run of Fprintf calls to a single error check,
// keeping the report code readable.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) printf(format string, args ...any) {
	if e.err != nil {
		return
	}
	_, e.err = fmt.Fprintf(e.w, format, args...)
}

// ofTotal renders "3/176", or a bare dash when the device publishes
// nothing of that kind — an empty cell reads as zero coverage, which is
// a different statement from having nothing to cover.
func ofTotal(n, total int) string {
	if total == 0 {
		return "-"
	}
	return fmt.Sprintf("%d/%d", n, total)
}

// trunc clips a column value, marking that it was clipped.
func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
