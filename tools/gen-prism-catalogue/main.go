// gen-prism-catalogue compiles the page a PRISM serves at /api/help into
// the catalogue the connector embeds: one row per call with its name,
// mode, datatype, scope family and option enum. Descriptions are not
// copied.
//
//	go run ./tools/gen-prism-catalogue \
//	    -page "internal/prism/assets/PRISM API Documentation.html" \
//	    -out internal/prism/codec/catalogue.tsv
package main

import (
	"bytes"
	"flag"
	"fmt"
	"html"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var osExit = os.Exit

func main() { osExit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(argv []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gen-prism-catalogue", flag.ContinueOnError)
	fs.SetOutput(stderr)
	page := fs.String("page", "", "the saved PRISM API page (required)")
	out := fs.String("out", "", "output path (default stdout)")
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	if *page == "" {
		_, _ = fmt.Fprintln(stderr, "gen-prism-catalogue: -page is required")
		return 1
	}
	src, err := os.ReadFile(*page)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "gen-prism-catalogue:", err)
		return 1
	}
	rows := parsePage(string(src))
	var b bytes.Buffer
	writeTSV(&b, rows)
	if *out == "" {
		_, _ = stdout.Write(b.Bytes())
	} else if err := os.WriteFile(*out, b.Bytes(), 0o644); err != nil {
		_, _ = fmt.Fprintln(stderr, "gen-prism-catalogue:", err)
		return 1
	}
	_, _ = fmt.Fprintf(stderr, "%d calls\n", len(rows))
	return 0
}

type row struct {
	name, mode, datatype, scope string
	flags                       []string
	options                     []string
}

var (
	reRow    = regexp.MustCompile(`(?s)<tr[^>]*>(.*?)</tr>`)
	reCell   = regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`)
	reTag    = regexp.MustCompile(`<[^>]+>`)
	reHref   = regexp.MustCompile(`href="[^"]*/api/[^"/?]+[/?]([^"]+)"`)
	reSymbol = regexp.MustCompile(`\b[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+(?:_[A-Za-z0-9]+)?\b`)
	reBrack  = regexp.MustCompile(`\b([A-Z0-9_]+_)\[([A-Z, ]+)\]`)
	reNumber = regexp.MustCompile(`(?:^|[\s,:;])(-?\d+)\s+(?:for\s|to\s+[A-Za-z])`)
)

// Three string calls list their values in prose, not as option tokens.
var stringOptions = map[string][]string{
	"audiotest_chan":  {"left", "right", "both", "l", "r", "b"},
	"stop_mode":       {"Y", "RGB", "YRGB"},
	"timecode_select": {"OFF", "LTC", "VITC"},
}

// A token in prose that is no option of this call.
var notOption = map[string]bool{"FALSE_COLOR_MODE": true}

// noRead: the page does not say a GET of these is harmless (the name or
// the description is an action), or the answer is not a value. Never
// read by walk, get or watch.
var noRead = map[string]bool{
	"cable_calibration_save":   true,
	"data_logger_db_diag_init": true,
	"downloadScreenshot":       true,
	"eye_calibration_save":     true,
	"fan_diag":                 true,
	"help":                     true,
	"system_poweroff":          true,
	"system_reboot":            true,
}

// guarded: a POST that stops the instrument, destroys data or rewrites
// its identity. set refuses it unless the operator opens the guard.
var guarded = map[string]bool{
	"cable_calibration_reset": true,
	"cable_calibration_save":  true,
	"data_logger_db_new":      true,
	"eye_calibration_reset":   true,
	"eye_calibration_save":    true,
	"fan_diag":                true,
	"ip_mac_address":          true,
	"nomenclature":            true,
	"serial_number":           true,
	"system_poweroff":         true,
	"system_reboot":           true,
}

// The two calls the page's script appends by hand take the JSON body as
// it stands; every other POST carries its value under "object".
var bareBody = map[string]bool{"activeInput": true, "inputConfigure": true}

// parsePage keeps the rows whose MODE cell is a mode: the JSON field
// documentation nested inside a description has a datatype there.
func parsePage(page string) []row {
	var out []row
	for _, m := range reRow.FindAllStringSubmatch(page, -1) {
		cells := reCell.FindAllStringSubmatch(m[1], -1)
		if len(cells) < 3 {
			continue
		}
		mode := text(cells[1][1])
		if mode != "GET" && mode != "POST" && mode != "GET / POST" {
			continue
		}
		desc := ""
		if len(cells) > 3 {
			desc = text(cells[3][1])
		}
		r := row{name: text(cells[0][1]), mode: mode, datatype: text(cells[2][1])}
		href := ""
		if h := reHref.FindStringSubmatch(m[1]); h != nil {
			href = h[1]
		}
		var optional bool
		r.scope, optional = scopeOf(href, desc)
		r.options = optionsOf(r.name, r.datatype, desc)
		r.flags = flagsOf(r.name, optional)
		out = append(out, r)
	}
	return out
}

func text(cell string) string {
	return strings.TrimSpace(html.UnescapeString(reTag.ReplaceAllString(cell, "")))
}

// scopeOf reads the scope family from the description, then from the
// default scope the page's script writes into the row's link.
func scopeOf(href, desc string) (family string, optional bool) {
	optional = strings.Contains(desc, "an empty scope field") || strings.Contains(desc, "If tile scope is specified")
	lower := strings.ToLower(desc)
	switch {
	case strings.Contains(lower, "tile scope") || strings.Contains(desc, "tile1 through tile8") ||
		strings.Contains(desc, "tile1 to tile8"):
		return "tile", optional
	case strings.Contains(desc, "scope of config0 through config5") || strings.Contains(desc, "Scope: config0 to config5"):
		return "config", optional
	case strings.Contains(desc, "SDI1 - SDI4"):
		return "timing", optional
	case strings.Contains(desc, "IP1 or IP2"):
		return "port", optional
	case strings.Contains(desc, "slot 0 or 1"):
		return "slot", optional
	case strings.Contains(desc, "scope of slot0"):
		return "slot0", optional
	case strings.Contains(desc, "index1 and index2"):
		return "index", optional
	case strings.Contains(desc, "scope of index0"):
		return "index0", optional
	}
	switch href {
	case "tile1":
		return "tile", optional
	case "config0":
		return "config", optional
	case "IP1":
		return "port", optional
	case "slot0":
		return "slot0", optional
	case "input=0":
		return "input", optional
	}
	return "", optional
}

func optionsOf(name, datatype, desc string) []string {
	if o, ok := stringOptions[name]; ok {
		return o
	}
	if datatype != "ints" || strings.Contains(desc, "array") {
		return nil
	}
	desc = reBrack.ReplaceAllStringFunc(desc, func(s string) string {
		m := reBrack.FindStringSubmatch(s)
		var parts []string
		for _, tail := range strings.Split(m[2], ",") {
			parts = append(parts, m[1]+strings.TrimSpace(tail))
		}
		return strings.Join(parts, ", ")
	})
	var out []string
	seen := map[string]bool{}
	for _, tok := range reSymbol.FindAllString(desc, -1) {
		if !seen[tok] && !notOption[tok] {
			seen[tok] = true
			out = append(out, tok)
		}
	}
	if len(out) > 0 {
		return out
	}
	for _, m := range reNumber.FindAllStringSubmatch(desc, -1) {
		if _, err := strconv.Atoi(m[1]); err == nil && !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

func flagsOf(name string, optional bool) []string {
	var f []string
	if optional {
		f = append(f, "scope-optional")
	}
	if noRead[name] {
		f = append(f, "no-read")
	}
	if guarded[name] {
		f = append(f, "guarded")
	}
	if bareBody[name] {
		f = append(f, "bare-body")
	}
	return f
}

func writeTSV(w io.Writer, rows []row) {
	_, _ = fmt.Fprintln(w, "# generated by tools/gen-prism-catalogue from the page a PRISM serves at /api/help; do not edit")
	_, _ = fmt.Fprintln(w, "# name\tmode\tdatatype\tscope\tflags\toptions")
	for _, r := range rows {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", r.name, strings.ReplaceAll(r.mode, " ", ""), r.datatype, r.scope,
			strings.Join(r.flags, ","), strings.Join(r.options, "|"))
	}
}
