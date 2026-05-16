package emberplus

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// unknownCTXKey uniquely identifies one unrecognised context-tagged
// field encountered in a Contents SET (spec p.93). Kind matches the
// element type ("node", "parameter", "matrix", "function"); Tag is the
// raw CTX number reported by the BER decoder.
type unknownCTXKey struct {
	Kind string
	Tag  int32
}

// unknownCTXObservation aggregates how many times one (kind, tag)
// pair was seen across the session and a small sample of OIDs that
// carried it, for vendor-outreach context in the audit report.
type unknownCTXObservation struct {
	Count      int
	SampleOIDs []string
}

const maxUnknownSampleOIDs = 5

// unknownCTXAudit accumulates unrecognised CTX tags during a session.
// One instance lives on Plugin, reset on Connect. WriteAuditReport
// emits a Markdown summary suitable for sharing with the device
// vendor when non-empty; no file is written when nothing was seen.
type unknownCTXAudit struct {
	mu        sync.Mutex
	startedAt time.Time
	entries   map[unknownCTXKey]*unknownCTXObservation
}

func newUnknownCTXAudit() *unknownCTXAudit {
	return &unknownCTXAudit{
		startedAt: time.Now().UTC(),
		entries:   make(map[unknownCTXKey]*unknownCTXObservation),
	}
}

// record adds tags to the audit. oid is the canonical numeric OID of
// the element carrying the unknown CTX, captured as a sample.
func (a *unknownCTXAudit) record(kind string, oid []int32, tags []int32) {
	if len(tags) == 0 {
		return
	}
	oidStr := numericKey(oid)
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range tags {
		k := unknownCTXKey{Kind: kind, Tag: t}
		obs := a.entries[k]
		if obs == nil {
			obs = &unknownCTXObservation{}
			a.entries[k] = obs
		}
		obs.Count++
		if oidStr != "" && len(obs.SampleOIDs) < maxUnknownSampleOIDs {
			// Dedupe — same OID emitting twice in one session does
			// not need to occupy two sample slots.
			already := false
			for _, s := range obs.SampleOIDs {
				if s == oidStr {
					already = true
					break
				}
			}
			if !already {
				obs.SampleOIDs = append(obs.SampleOIDs, oidStr)
			}
		}
	}
}

// snapshot returns the accumulated data sorted by (kind, tag) for
// deterministic Markdown output. Caller holds nothing.
func (a *unknownCTXAudit) snapshot() []unknownCTXRow {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]unknownCTXRow, 0, len(a.entries))
	for k, v := range a.entries {
		row := unknownCTXRow{Key: k, Count: v.Count}
		row.SampleOIDs = append(row.SampleOIDs, v.SampleOIDs...)
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Key.Kind != out[j].Key.Kind {
			return out[i].Key.Kind < out[j].Key.Kind
		}
		return out[i].Key.Tag < out[j].Key.Tag
	})
	return out
}

type unknownCTXRow struct {
	Key        unknownCTXKey
	Count      int
	SampleOIDs []string
}

// renderUnknownCTXMarkdown formats the audit data as a Markdown
// document. Empty input returns "" (caller skips the file write).
//
// header carries device identity + endpoint context for the report
// preamble.
type unknownCTXHeader struct {
	Identity     string
	Host         string
	Port         int
	DTDDeclared  string // e.g. "2.60"
	FramesTotal  int    // 0 when unknown — omitted from output
	StartedAtUTC time.Time
}

func renderUnknownCTXMarkdown(h unknownCTXHeader, rows []unknownCTXRow) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintln(&b, "# Unknown CTX Tags Report")
	fmt.Fprintln(&b)
	if h.Identity != "" {
		fmt.Fprintf(&b, "**Device**: `%s`\n", h.Identity)
	}
	if h.Host != "" {
		if h.Port > 0 {
			fmt.Fprintf(&b, "**Endpoint**: %s:%d\n", h.Host, h.Port)
		} else {
			fmt.Fprintf(&b, "**Endpoint**: %s\n", h.Host)
		}
	}
	if !h.StartedAtUTC.IsZero() {
		fmt.Fprintf(&b, "**Captured**: %s\n", h.StartedAtUTC.Format(time.RFC3339))
	}
	if h.DTDDeclared != "" {
		fmt.Fprintf(&b, "**DTD declared**: %s\n", h.DTDDeclared)
	}
	fmt.Fprintln(&b)

	// Group rows by kind, in stable order.
	byKind := make(map[string][]unknownCTXRow)
	kinds := make([]string, 0, 4)
	for _, r := range rows {
		if _, ok := byKind[r.Key.Kind]; !ok {
			kinds = append(kinds, r.Key.Kind)
		}
		byKind[r.Key.Kind] = append(byKind[r.Key.Kind], r)
	}
	sort.Strings(kinds)

	titleOf := map[string]string{
		"node":      "NodeContents",
		"parameter": "ParameterContents",
		"matrix":    "MatrixContents",
		"function":  "FunctionContents",
	}

	for _, kind := range kinds {
		title := titleOf[kind]
		if title == "" {
			title = kind
		}
		fmt.Fprintf(&b, "## %s\n\n", title)
		fmt.Fprintln(&b, "| CTX tag | Occurrences | Sample OIDs |")
		fmt.Fprintln(&b, "|--------:|------------:|-------------|")
		for _, r := range byKind[kind] {
			samples := strings.Join(r.SampleOIDs, ", ")
			if samples == "" {
				samples = "_(no OID captured)_"
			}
			fmt.Fprintf(&b, "| %d | %d | %s |\n", r.Key.Tag, r.Count, samples)
		}
		fmt.Fprintln(&b)
	}

	fmt.Fprintln(&b, "## How to use this report")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Each row is a context-tagged field this device emits that our codec")
	fmt.Fprintln(&b, "does not currently map. Spec p.93 mandates decoders accept unknown")
	fmt.Fprintln(&b, "CTX silently (we skip — no decode error), but those fields may carry")
	fmt.Fprintln(&b, "useful semantics. Send this report to the vendor and ask which DTD")
	fmt.Fprintln(&b, "version or extension defines each CTX number, so the codec can be")
	fmt.Fprintln(&b, "extended accordingly.")

	return b.String()
}

// WriteUnknownCTXAuditTo renders this session's accumulated unknown-CTX
// audit as Markdown and writes it to path. When nothing was observed,
// no file is written (returns nil). Intended for the CLI to call after
// walk completes and identity is resolved, so the output sits at
// `.cache/audit/emberplus/<identity>-unknown-ctx.md`.
//
// Called via type assertion in cmd/dhs — kept off the cross-protocol
// Protocol interface because the artefact is Ember+-specific.
func (p *Plugin) WriteUnknownCTXAuditTo(path string) error {
	if p.unknownCTX == nil {
		return nil
	}
	rows := p.unknownCTX.snapshot()
	if len(rows) == 0 {
		return nil
	}
	h := unknownCTXHeader{
		Host:         p.connIP,
		Port:         p.connPort,
		StartedAtUTC: p.unknownCTX.startedAt,
	}
	return writeUnknownCTXReport(path, h, rows)
}

// writeUnknownCTXReport renders the audit to a Markdown file at path.
// No file is written when there is nothing to report. path's parent
// directory is created lazily.
func writeUnknownCTXReport(path string, h unknownCTXHeader, rows []unknownCTXRow) error {
	md := renderUnknownCTXMarkdown(h, rows)
	if md == "" {
		return nil
	}
	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		return fmt.Errorf("emberplus: audit mkdir: %w", err)
	}
	return os.WriteFile(path, []byte(md), 0o644)
}

// dirOf returns the directory portion of a filesystem path, mirroring
// filepath.Dir without pulling the import in (caller already imports
// it elsewhere; keeping this file's import set minimal).
func dirOf(p string) string {
	i := strings.LastIndexAny(p, `/\`)
	if i < 0 {
		return "."
	}
	return p[:i]
}
