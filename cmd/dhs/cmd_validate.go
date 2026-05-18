package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/errcode"
	"dhs/internal/wiretrace"
)

// _ keeps errcode imported even when callers below don't reference it
// after a refactor — it documents the contract that ValidateError.Err
// strings follow the `<layer>:<name>: <msg>` shape from R1 #468.
var _ = errcode.LayerValidation

// R23 #488 validation codes raised by the validate verb.
var (
	errReportInvalidFormat    = errcode.New(errcode.LayerValidation, "invalid-report-format", errcode.ClassUsage)
	errReportTargetUnwritable = errcode.New(errcode.LayerTransport, "report-target-unwritable", errcode.ClassRuntime)
	errInputNotFound          = errcode.New(errcode.LayerTransport, "input-not-found", errcode.ClassRuntime)
)

// validateReport is the machine-readable shape rendered by --report
// (R23 #488 spec, markdown derives the same fields).
type validateReport struct {
	File     string                       `json:"file"`
	Frames   int                          `json:"frames"`
	Pass     int                          `json:"pass"`
	Fail     int                          `json:"fail"`
	Started  time.Time                    `json:"started"`
	Ended    time.Time                    `json:"ended"`
	ByLayer  map[string]layerCounts       `json:"byLayer"`
	Failures []validateFailureRecord      `json:"failures"`
}

type layerCounts struct {
	Pass int `json:"pass"`
	Fail int `json:"fail"`
}

type validateFailureRecord struct {
	Frame   int    `json:"frame"`
	Offset  int    `json:"offset,omitempty"`
	Layer   string `json:"layer"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
	RawHex  string `json:"raw_hex,omitempty"`
}

// runValidate drives `dhs consumer <proto> validate <frames.jsonl> [flags]`.
//
// Decodes every Trame in the JSONL through the connector's codec
// (per ADR-0021) and reports per-direction counts plus invariant
// violations. Optional flags drive offline canonical-tree / params
// emission in the same pass.
//
// R23 #488 adds --report <path>: writes a Markdown or JSON report
// (format inferred from extension; `-` means stdout). Per-frame stdout
// output stays unchanged unless --report is `-`.
//
// `replay` (peer simulation per ADR-0021) is a separate, deferred verb.
// See ADR-0002 canonical-verb table for the full role split.
func runValidate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	cf := addCommonFlags(fs)

	outTree := fs.String("out-tree", "", "optional path: write canonical tree.json (per-protocol support)")
	outParams := fs.String("out-params", "", "optional path: write canonical params dump (csv/json by extension)")
	stopAt := fs.String("stop-at", "", "optional Trame.Note marker to halt decoding at")
	report := fs.String("report", "", "R23 #488: write a structured validation report. Path ending in `.md` → Markdown; `.json` → JSON; `-` → stdout (suppresses per-frame stdout). Any other extension → validation:invalid-report-format.")

	tramesPath, rest, err := popHost(args)
	if err != nil {
		return errors.New("usage: dhs consumer <proto> validate <frames.jsonl> [flags]")
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}

	// Resolve the report format before we touch the wire / disk so we
	// fail fast on a typo.
	reportFmt, err := resolveReportFormat(*report)
	if err != nil {
		return err
	}

	factory, err := consumer.Get(cf.protocol)
	if err != nil {
		return err
	}

	f, err := os.Open(tramesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", errInputNotFound, tramesPath)
		}
		return fmt.Errorf("open %s: %w", tramesPath, err)
	}
	defer func() { _ = f.Close() }()

	trames, err := wiretrace.ReadTrames(f)
	if err != nil {
		return err
	}

	plug := factory.New(slog.Default())

	validator, ok := plug.(consumer.Validator)
	if !ok {
		return fmt.Errorf("validate: protocol %q does not implement consumer.Validator yet (per-protocol migration tracker: issue #212)", cf.protocol)
	}

	started := time.Now().UTC()
	vreport, err := validator.Validate(ctx, trames, consumer.ValidateOpts{
		OutTree:   *outTree,
		OutParams: *outParams,
		StopAt:    *stopAt,
	})
	ended := time.Now().UTC()
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	rendered := renderValidateReport(tramesPath, trames, vreport, started, ended)

	// `--report -` suppresses per-frame stdout (the operator wants only
	// the report). Otherwise keep the legacy per-frame summary so
	// existing runbook captures stay byte-compat.
	suppressDefaultStdout := *report == "-"
	if !suppressDefaultStdout {
		printLegacyValidateSummary(vreport)
	}

	if *report != "" {
		if err := writeValidateReport(rendered, *report, reportFmt); err != nil {
			return err
		}
	}

	if len(vreport.Errors) > 0 || len(vreport.Invariants) > 0 {
		return errors.New("validate: failures detected")
	}
	return nil
}

// printLegacyValidateSummary preserves the byte-compat summary line set
// emitted by the verb before R23 landed.
func printLegacyValidateSummary(r *consumer.ValidateReport) {
	fmt.Printf("validate: %d trames decoded\n", r.TramesProcessed)
	dirs := make([]string, 0, len(r.PerDirection))
	for d := range r.PerDirection {
		dirs = append(dirs, string(d))
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		fmt.Printf("  %s: %d\n", d, r.PerDirection[wiretrace.Direction(d)])
	}
	if r.StoppedAt != "" {
		fmt.Printf("  stopped-at: %s\n", r.StoppedAt)
	}
	if len(r.Errors) > 0 {
		fmt.Fprintf(os.Stderr, "errors: %d\n", len(r.Errors))
		for _, e := range r.Errors {
			fmt.Fprintf(os.Stderr, "  trame %d (%s): %s\n", e.TrameIndex, e.Direction, e.Err)
		}
	}
	if len(r.Invariants) > 0 {
		fmt.Fprintf(os.Stderr, "invariant violations: %d\n", len(r.Invariants))
		for _, v := range r.Invariants {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
	}
}

// resolveReportFormat interprets the --report argument.
//
//	""        → no report (today's behavior)
//	"-"       → stdout (markdown)
//	"x.md"    → markdown to file
//	"x.json"  → JSON to file
//	"x.txt"   → error validation:invalid-report-format
func resolveReportFormat(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if path == "-" {
		return "md", nil
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md":
		return "md", nil
	case ".json":
		return "json", nil
	default:
		return "", fmt.Errorf("%w: --report=%q must end in .md or .json", errReportInvalidFormat, path)
	}
}

// renderValidateReport builds the structured report shape from the
// decoded trames + validator output. Pure-compute (no I/O); the result
// goes through writeValidateReport for output.
func renderValidateReport(path string, trames []wiretrace.Trame, r *consumer.ValidateReport, started, ended time.Time) validateReport {
	out := validateReport{
		File:     path,
		Frames:   r.TramesProcessed,
		Pass:     r.TramesProcessed - len(r.Errors),
		Fail:     len(r.Errors),
		Started:  started.Truncate(time.Millisecond),
		Ended:    ended.Truncate(time.Millisecond),
		ByLayer:  map[string]layerCounts{},
		Failures: make([]validateFailureRecord, 0, len(r.Errors)),
	}
	if out.Pass < 0 {
		out.Pass = 0
	}

	// Layers default to all-pass = total frames; per-layer fail counts
	// derive from the layer of each ValidateError. Layers without
	// observed failures keep their default pass row.
	totalFrames := r.TramesProcessed
	defaultLayers := []string{"s101", "ber", "glow", "stream"}
	for _, l := range defaultLayers {
		out.ByLayer[l] = layerCounts{Pass: totalFrames}
	}

	for _, e := range r.Errors {
		layer, code := classifyFromMessage(e.Err)
		// Each frame failure decrements the layer's pass count and
		// increments the layer's fail count (one per error).
		lc := out.ByLayer[layer]
		if lc.Pass > 0 {
			lc.Pass--
		}
		lc.Fail++
		out.ByLayer[layer] = lc

		rec := validateFailureRecord{
			Frame:   e.TrameIndex,
			Layer:   layer,
			Code:    code,
			Message: e.Err,
		}
		// Best-effort raw_hex from the original Trame. Trames already
		// carry a hex string per ADR-0021; pass it through trimmed.
		if idx := e.TrameIndex; idx >= 0 && idx < len(trames) {
			rec.RawHex = strings.ToLower(strings.TrimSpace(trames[idx].Hex))
		}
		out.Failures = append(out.Failures, rec)
	}
	return out
}

// classifyFromMessage extracts the layer and `<layer>:<name>` code
// prefix from a ValidateError.Err string. Per R1 #468 every codec
// error site renders as `<layer>:<name>: <msg>` so the prefix before
// the second colon is the typed code. Falls back to layer="validate"
// when the message doesn't follow the convention.
func classifyFromMessage(msg string) (layer, code string) {
	first := strings.IndexByte(msg, ':')
	if first <= 0 {
		return "validate", ""
	}
	cand := msg[:first]
	switch cand {
	case "s101", "ber", "glow", "matrix", "transport", "validation", "plugin":
		layer = cand
	default:
		return "validate", ""
	}
	// Code prefix is `<layer>:<name>` — find the next colon (or eos).
	rest := msg[first+1:]
	next := strings.IndexByte(rest, ':')
	if next <= 0 {
		// No name component — just the layer.
		return layer, layer
	}
	code = cand + ":" + rest[:next]
	return layer, code
}

// writeValidateReport serialises the report to the target per format.
// path=="-" goes to stdout; any other path is created (parent dir must
// exist — we report transport:report-target-unwritable on permission /
// missing-dir failure).
func writeValidateReport(report validateReport, path, format string) error {
	var rendered []byte
	switch format {
	case "json":
		buf, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		rendered = append(buf, '\n')
	case "md":
		rendered = []byte(renderMarkdownReport(report))
	default:
		return fmt.Errorf("%w: --report=%q must end in .md or .json", errReportInvalidFormat, path)
	}
	if path == "-" {
		_, err := io.Copy(os.Stdout, strings.NewReader(string(rendered)))
		return err
	}
	if err := os.WriteFile(path, rendered, 0o644); err != nil {
		return fmt.Errorf("%w: %s: %v", errReportTargetUnwritable, path, err)
	}
	return nil
}

// renderMarkdownReport produces the byte-deterministic Markdown shape
// from R23 #488. Same input always produces the same output bytes so
// the report file is git-diffable across runs.
func renderMarkdownReport(r validateReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Validation report — %s\n\n", filepath.Base(r.File))
	fmt.Fprintf(&b, "- File: `%s`\n", r.File)
	fmt.Fprintf(&b, "- Frames: %d\n", r.Frames)
	passPct := 100.0
	if r.Frames > 0 {
		passPct = (float64(r.Pass) / float64(r.Frames)) * 100.0
	}
	fmt.Fprintf(&b, "- Pass:   %d (%.1f%%)\n", r.Pass, passPct)
	fmt.Fprintf(&b, "- Fail:   %d\n", r.Fail)
	fmt.Fprintf(&b, "- Started: %s\n", r.Started.Format(time.RFC3339))
	fmt.Fprintf(&b, "- Ended:   %s\n\n", r.Ended.Format(time.RFC3339))

	fmt.Fprintln(&b, "## Per-layer pass rate")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Layer | Pass | Fail |")
	fmt.Fprintln(&b, "| --- | ---: | ---: |")
	keys := make([]string, 0, len(r.ByLayer))
	for k := range r.ByLayer {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		lc := r.ByLayer[k]
		fmt.Fprintf(&b, "| %s | %d | %d |\n", k, lc.Pass, lc.Fail)
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Failures")
	fmt.Fprintln(&b)
	if len(r.Failures) == 0 {
		fmt.Fprintln(&b, "_None — all frames decoded cleanly._")
		return b.String()
	}
	for _, fr := range r.Failures {
		fmt.Fprintf(&b, "- frame %d / layer `%s`", fr.Frame, fr.Layer)
		if fr.Code != "" {
			fmt.Fprintf(&b, " / code `%s`", fr.Code)
		}
		fmt.Fprintf(&b, ": %s\n", fr.Message)
		if fr.RawHex != "" {
			fmt.Fprintf(&b, "  - raw: `%s`\n", fr.RawHex)
		}
	}
	return b.String()
}

func helpValidate() {
	fmt.Print(`dhs consumer <proto> validate <frames.jsonl> [flags]

Decode every Trame in <frames.jsonl> (per ADR-0021) through the
protocol's codec offline, with no live peer. Reports per-direction
counts and any decode failures or invariant violations. Exits 0 on
a clean capture, 1 on any failure.

Naming: "Trame" is the wire-trace record type — see ADR-0021. The
file naming convention (frames.jsonl) is kept for compatibility
with existing capture tooling.

Flags:
  --out-tree <path>     also write a canonical tree.json
  --out-params <path>   also write a canonical params dump (csv/json by ext)
  --stop-at <note>      halt at the first trame whose .note matches
  --report <path>       R23 #488: write a structured report. Path
                        ending in .md → Markdown; .json → JSON;
                        `+"`-`"+` → stdout (suppresses per-frame stdout
                        output for piping). Any other extension →
                        validation:invalid-report-format (exit 2).

IN   dhs consumer acp1 validate captures/acp1/slot0_walk/frames.jsonl
OUT  validate: 642 trames decoded
       rx: 321
       tx: 321

IN   dhs consumer emberplus validate captures/emberplus/runbook/walk-happy.jsonl \
       --report walk-happy.md
OUT  (legacy stdout summary) + walk-happy.md written

Captures live under captures/<proto>/<scenario>/frames.jsonl by
convention (per ADR-0020 Bucket 4); the captures/ tree is gitignored
in its entirety, no LFS, no committed blobs.

`)
}
