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
	"os/exec"
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

// R23 #488 / R12 #473 validation codes raised by the validate verb.
var (
	errReportInvalidFormat    = errcode.New(errcode.LayerValidation, "invalid-report-format", errcode.ClassUsage)
	errReportTargetUnwritable = errcode.New(errcode.LayerTransport, "report-target-unwritable", errcode.ClassRuntime)
	errInputNotFound          = errcode.New(errcode.LayerTransport, "input-not-found", errcode.ClassRuntime)
	errTsharkNotFound         = errcode.New(errcode.LayerValidation, "tshark-not-found", errcode.ClassUsage)
	errLuaPcapRequired        = errcode.New(errcode.LayerValidation, "lua-pcap-required", errcode.ClassUsage)
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
	lua := fs.Bool("lua", false, "R12 #473: replay through the project's Wireshark dissector via `tshark -V -X lua_script:<dhs_<proto>.lua>` instead of the Go codec. When --pcap is unset the jsonl fixture is synthesised into a temporary pcap on the fly so committed jsonl traces stay replayable without separate pcap files.")
	pcapPath := fs.String("pcap", "", "R12 #473: pcap/pcapng input for --lua mode (real wire capture). When set, --lua dispatches to this file instead of synthesising one from the jsonl positional.")

	tramesPath, rest, err := popHost(args)
	if err != nil {
		return errors.New("usage: dhs consumer <proto> validate <frames.jsonl> [flags]")
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}

	// R12 #473: --lua short-circuits the Go-codec path. When --pcap is
	// supplied, dispatch to the real wire capture. Otherwise synthesise
	// a pcap from the positional jsonl so the dissector check runs
	// against committed fixtures with no extra files.
	if *lua {
		pcap := *pcapPath
		if pcap == "" {
			synth, serr := synthesisePcapFromJSONL(tramesPath, cf.protocol)
			if serr != nil {
				return serr
			}
			pcap = synth
			defer func() { _ = os.Remove(synth) }()
		}
		return runValidateLua(ctx, cf.protocol, pcap)
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

// synthesisePcapFromJSONL reads the named jsonl fixture and writes a
// synthesised libpcap into a host-OS temp file so `tshark -r` can
// consume it. Returns the temp file path; caller is responsible for
// deleting it once tshark exits. Per R12 #473 strict-spec — committed
// jsonl fixtures stay replayable without operator-side pcap captures.
//
// providerPort defaults are per-protocol; the map mirrors the listener
// ports each protocol's producer binds to. Unknown protocols fall back
// to 9000 (the Ember+ default) — safe because the dissector identifies
// frames by wire bytes, not port.
func synthesisePcapFromJSONL(jsonlPath, protoName string) (string, error) {
	f, err := os.Open(jsonlPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: %s", errInputNotFound, jsonlPath)
		}
		return "", fmt.Errorf("open %s: %w", jsonlPath, err)
	}
	defer func() { _ = f.Close() }()

	tmp, err := os.CreateTemp("", "dhs-validate-*.pcap")
	if err != nil {
		return "", fmt.Errorf("temp pcap: %w", err)
	}
	defer func() { _ = tmp.Close() }()

	port := defaultProviderPort(protoName)
	if err := wiretrace.SynthesisePcap(f, tmp, port); err != nil {
		// Clean up the partial temp file so we don't leak.
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("synthesise pcap from %s: %w", jsonlPath, err)
	}
	return tmp.Name(), nil
}

// defaultProviderPort returns the wire port the named protocol's
// producer listens on by default. Used by the pcap synthesiser to
// route synthesised TCP segments to a port the dissector recognises.
func defaultProviderPort(protoName string) uint16 {
	switch protoName {
	case "emberplus":
		return 9000
	case "probel-sw08p":
		return 2008
	case "probel-sw02p":
		return 2008
	default:
		return 9000
	}
}

// runValidateLua replays a pcap through tshark with the project's
// per-protocol Lua dissector loaded. Pure shell-out — we don't post-
// process tshark's output, so the operator gets exactly the
// Wireshark-V text they would from running tshark themselves with
// the same -X lua_script: invocation.
//
// Per R12 #473 spec: jsonl→pcap synthesis is the v2 enhancement;
// today --lua requires a real --pcap input.
func runValidateLua(ctx context.Context, protoName, pcapPath string) error {
	tsharkBin, err := exec.LookPath("tshark")
	if err != nil {
		return fmt.Errorf("%w: install Wireshark (https://www.wireshark.org)", errTsharkNotFound)
	}
	dissectorPath := filepath.Join("internal", protoName, "wireshark", "dhs_"+protoName+".lua")
	if _, err := os.Stat(dissectorPath); err != nil {
		// Try emberplus naming variant for the ACP1 v1 dissector and
		// equivalents — kept best-effort so unknown layouts surface a
		// clear error rather than a tshark argv miss.
		return fmt.Errorf("dissector not found at %s: %w", dissectorPath, err)
	}
	cmd := exec.CommandContext(ctx, tsharkBin,
		"-r", pcapPath,
		"-V",
		"-X", "lua_script:"+dissectorPath,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tshark: %w", err)
	}
	return nil
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
