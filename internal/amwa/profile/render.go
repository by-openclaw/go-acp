package profile

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// RenderText writes the human report: the assertions grouped by
// outcome, then the timing.
//
// Failures come first. A probe is usually run because something is
// suspected, and the answer should not need scrolling to.
func RenderText(w io.Writer, r *Report) error {
	bw := &errWriter{w: w}

	bw.printf("NMOS LIVE PROBE — %s\n", r.Target)
	bw.printf("%s\n\n", strings.Repeat("=", 78))

	for _, st := range []Status{StatusFail, StatusWarn, StatusSkip, StatusPass} {
		group := filterByStatus(r.Results, st)
		if len(group) == 0 {
			continue
		}
		bw.printf("%s — %d\n%s\n", st, len(group), strings.Repeat("-", 40))
		for _, res := range group {
			bw.printf("  %-18s %s\n", res.ID, res.Name)
			bw.printf("  %-18s   %s\n", "", res.Detail)
			if res.Spec != "" {
				bw.printf("  %-18s   spec %s\n", "", res.Spec)
			}
		}
		bw.printf("\n")
	}

	bw.printf("LATENCY (ms)\n\n")
	bw.printf("%-46s %5s %8s %8s %8s %6s\n", "ENDPOINT", "REQS", "P50", "P95", "P99", "ERRS")
	bw.printf("%s\n", strings.Repeat("-", 78))
	for _, s := range r.Latency {
		bw.printf("%-46s %5d %8.1f %8.1f %8.1f %6d\n",
			truncLeft(s.Endpoint, 46), s.Requests, s.P50MS, s.P95MS, s.P99MS, s.Errors)
	}

	bw.printf("\nSUMMARY\n\n")
	for _, st := range []Status{StatusFail, StatusWarn, StatusSkip, StatusPass} {
		bw.printf("  %-6s %d\n", st, r.Counts[string(st)])
	}
	return bw.err
}

// RenderJSON writes the whole report as one object.
func RenderJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// RenderJSONL writes one result per line.
//
// This is the form that appends across runs and diffs as plain lines,
// so "did this device get better or worse since the firmware update" is
// a diff rather than a reading exercise. The latency rows follow the
// results, tagged so a reader can tell them apart.
func RenderJSONL(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	for _, res := range r.Results {
		if err := enc.Encode(res); err != nil {
			return err
		}
	}
	for _, s := range r.Latency {
		row := struct {
			Kind   string `json:"kind"`
			Target string `json:"target"`
			EndpointStat
		}{Kind: "latency", Target: r.Target, EndpointStat: s}
		if err := enc.Encode(row); err != nil {
			return err
		}
	}
	return nil
}

func filterByStatus(rs []Result, st Status) []Result {
	var out []Result
	for _, r := range rs {
		if r.Status == st {
			out = append(out, r)
		}
	}
	return out
}

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

// truncLeft clips from the left, keeping the tail of a path — the part
// that distinguishes two endpoints is at the end, not the start.
func truncLeft(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n+1:]
}
