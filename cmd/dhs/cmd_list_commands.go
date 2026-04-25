package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	acp1 "acp/internal/acp1/consumer"
	acp2 "acp/internal/acp2/consumer"
	emberplus "acp/internal/emberplus/consumer"
	codecsw02 "acp/internal/probel-sw02p/codec"
	codecsw08 "acp/internal/probel-sw08p/codec"
)

// catalogueRow is the rendered shape used by `dhs list-commands` —
// flat columns common across all protocols. Per-protocol catalogues
// (which differ in their native shape) are projected into this row
// type by the per-protocol adapter functions below.
type catalogueRow struct {
	Address     string `json:"address"`
	Name        string `json:"name"`
	Direction   string `json:"direction,omitempty"`
	SpecRef     string `json:"spec_ref,omitempty"`
	PayloadHint string `json:"payload_hint,omitempty"`
	Notes       string `json:"notes,omitempty"`
	Supported   bool   `json:"supported"`
}

// runListCommands implements `dhs list-commands <proto> [--format=table|json|md]`.
// Sourced from each protocol's static catalogue exporter (no live device).
func runListCommands(args []string) error {
	if len(args) == 0 || hasHelpFlag(args) {
		printListCommandsHelp(os.Stdout)
		return nil
	}
	proto := args[0]
	rest := args[1:]
	format := "table"
	for i := 0; i < len(rest); i++ {
		switch {
		case rest[i] == "--format" || rest[i] == "-format":
			if i+1 >= len(rest) {
				return fmt.Errorf("--format requires a value (table|json|md)")
			}
			format = rest[i+1]
			i++
		case strings.HasPrefix(rest[i], "--format=") || strings.HasPrefix(rest[i], "-format="):
			format = strings.SplitN(rest[i], "=", 2)[1]
		}
	}
	rows, err := catalogueRowsForProto(proto)
	if err != nil {
		return err
	}
	switch format {
	case "table", "":
		return renderCatalogueTable(os.Stdout, proto, rows)
	case "json":
		return renderCatalogueJSON(os.Stdout, proto, rows)
	case "md", "markdown":
		return renderCatalogueMarkdown(os.Stdout, proto, rows)
	}
	return fmt.Errorf("--format: unknown value %q (want table|json|md)", format)
}

// runHelpCmd implements `dhs help-cmd <proto> <address>` printing one
// catalogue entry's full detail.
func runHelpCmd(args []string) error {
	if len(args) < 2 || hasHelpFlag(args) {
		printHelpCmdHelp(os.Stdout)
		return nil
	}
	proto := args[0]
	addr := args[1]
	row, ok, err := lookupCatalogueForProto(proto, addr)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("help-cmd %s: address %q not in catalogue (try `dhs list-commands %s`)", proto, addr, proto)
	}
	return renderCatalogueRowDetail(os.Stdout, proto, addr, row)
}

func catalogueRowsForProto(proto string) ([]catalogueRow, error) {
	switch proto {
	case "probel-sw02p":
		return sw02pRows(), nil
	case "probel-sw08p":
		return sw08pRows(), nil
	case "acp1":
		return acp1Rows(), nil
	case "acp2":
		return acp2Rows(), nil
	case "emberplus":
		return emberplusRows(), nil
	}
	return nil, fmt.Errorf("list-commands: unknown protocol %q (acp1 | acp2 | emberplus | probel-sw02p | probel-sw08p)", proto)
}

func lookupCatalogueForProto(proto, addr string) (catalogueRow, bool, error) {
	switch proto {
	case "probel-sw02p":
		id, ok := parseProbelByte(addr)
		if !ok {
			return catalogueRow{}, false, fmt.Errorf("probel-sw02p: %q is not a byte (try 0x.. or decimal)", addr)
		}
		spec, ok := codecsw02.CommandByID(codecsw02.CommandID(id))
		if !ok {
			return catalogueRow{}, false, nil
		}
		return sw02pRowFromSpec(spec), true, nil
	case "probel-sw08p":
		id, ok := parseProbelByte(addr)
		if !ok {
			return catalogueRow{}, false, fmt.Errorf("probel-sw08p: %q is not a byte (try 0x.. or decimal)", addr)
		}
		spec, ok := codecsw08.CommandByID(codecsw08.CommandID(id))
		if !ok {
			return catalogueRow{}, false, nil
		}
		return sw08pRowFromSpec(spec), true, nil
	case "acp1":
		entry, ok := acp1.LookupCatalogue(addr)
		if !ok {
			return catalogueRow{}, false, nil
		}
		return acp1RowFromEntry(entry), true, nil
	case "acp2":
		entry, ok := acp2.LookupCatalogue(addr)
		if !ok {
			return catalogueRow{}, false, nil
		}
		return acp2RowFromEntry(entry), true, nil
	case "emberplus":
		entry, ok := emberplus.LookupCatalogue(addr)
		if !ok {
			return catalogueRow{}, false, nil
		}
		return emberplusRowFromEntry(entry), true, nil
	}
	return catalogueRow{}, false, fmt.Errorf("help-cmd: unknown protocol %q", proto)
}

func sw02pRows() []catalogueRow {
	cmds := codecsw02.Commands()
	out := make([]catalogueRow, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, sw02pRowFromSpec(c))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	return out
}

func sw02pRowFromSpec(c codecsw02.CommandSpec) catalogueRow {
	return catalogueRow{
		Address:     fmt.Sprintf("0x%02x (%d)", uint8(c.ID), uint8(c.ID)),
		Name:        c.Name,
		Direction:   string(c.Direction),
		SpecRef:     "SW-P-02 " + c.SpecRef,
		PayloadHint: c.Payload,
		Notes:       c.Notes,
		Supported:   c.Supported,
	}
}

func sw08pRows() []catalogueRow {
	cmds := codecsw08.Commands()
	out := make([]catalogueRow, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, sw08pRowFromSpec(c))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	return out
}

func sw08pRowFromSpec(c codecsw08.CommandSpec) catalogueRow {
	return catalogueRow{
		Address:     fmt.Sprintf("0x%02x (%d)", uint8(c.ID), uint8(c.ID)),
		Name:        c.Name,
		Direction:   string(c.Direction),
		SpecRef:     "SW-P-08 " + c.SpecRef,
		PayloadHint: c.Payload,
		Notes:       c.Notes,
		Supported:   c.Supported,
	}
}

func acp1Rows() []catalogueRow {
	entries := acp1.Catalogue()
	out := make([]catalogueRow, 0, len(entries))
	for _, e := range entries {
		out = append(out, acp1RowFromEntry(e))
	}
	return out
}

func acp1RowFromEntry(e acp1.CatalogueEntry) catalogueRow {
	return catalogueRow{
		Address:   e.Address(),
		Name:      e.Name,
		SpecRef:   "ACP1 " + e.SpecRef,
		Notes:     e.Notes,
		Supported: true,
	}
}

func acp2Rows() []catalogueRow {
	entries := acp2.Catalogue()
	out := make([]catalogueRow, 0, len(entries))
	for _, e := range entries {
		out = append(out, acp2RowFromEntry(e))
	}
	return out
}

func acp2RowFromEntry(e acp2.CatalogueEntry) catalogueRow {
	return catalogueRow{
		Address:   e.Address(),
		Name:      e.Name,
		SpecRef:   e.SpecRef,
		Notes:     e.Notes,
		Supported: true,
	}
}

func emberplusRows() []catalogueRow {
	entries := emberplus.Catalogue()
	out := make([]catalogueRow, 0, len(entries))
	for _, e := range entries {
		out = append(out, emberplusRowFromEntry(e))
	}
	return out
}

func emberplusRowFromEntry(e emberplus.CatalogueEntry) catalogueRow {
	return catalogueRow{
		Address:   e.Address(),
		Name:      e.Name,
		SpecRef:   e.SpecRef,
		Notes:     e.Notes,
		Supported: true,
	}
}

// parseProbelByte accepts hex (0x..) or decimal, returns the uint8.
func parseProbelByte(s string) (uint8, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		var n uint16
		for _, c := range s[2:] {
			var d uint16
			switch {
			case c >= '0' && c <= '9':
				d = uint16(c - '0')
			case c >= 'a' && c <= 'f':
				d = uint16(c-'a') + 10
			case c >= 'A' && c <= 'F':
				d = uint16(c-'A') + 10
			default:
				return 0, false
			}
			n = n*16 + d
			if n > 255 {
				return 0, false
			}
		}
		return uint8(n), true
	}
	var n uint16
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + uint16(c-'0')
		if n > 255 {
			return 0, false
		}
	}
	return uint8(n), true
}

func renderCatalogueTable(w io.Writer, proto string, rows []catalogueRow) error {
	_, _ = fmt.Fprintf(w, "# %s — %d catalogue entries\n\n", proto, len(rows))
	maxAddr := 7
	maxName := 4
	maxDir := 3
	for _, r := range rows {
		if len(r.Address) > maxAddr {
			maxAddr = len(r.Address)
		}
		if len(r.Name) > maxName {
			maxName = len(r.Name)
		}
		if len(r.Direction) > maxDir {
			maxDir = len(r.Direction)
		}
	}
	header := fmt.Sprintf("%-*s  %-*s  %-*s  %s\n", maxAddr, "address", maxName, "name", maxDir, "dir", "spec / notes")
	_, _ = fmt.Fprint(w, header)
	_, _ = fmt.Fprint(w, strings.Repeat("-", len(header)-1)+"\n")
	for _, r := range rows {
		notes := r.SpecRef
		if r.PayloadHint != "" {
			notes = notes + " · " + r.PayloadHint
		}
		if r.Notes != "" {
			notes = notes + " · " + r.Notes
		}
		_, _ = fmt.Fprintf(w, "%-*s  %-*s  %-*s  %s\n", maxAddr, r.Address, maxName, r.Name, maxDir, r.Direction, notes)
	}
	return nil
}

func renderCatalogueJSON(w io.Writer, proto string, rows []catalogueRow) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]interface{}{
		"protocol": proto,
		"count":    len(rows),
		"entries":  rows,
	})
}

func renderCatalogueMarkdown(w io.Writer, proto string, rows []catalogueRow) error {
	_, _ = fmt.Fprintf(w, "# %s — %d catalogue entries\n\n", proto, len(rows))
	_, _ = fmt.Fprintln(w, "| Address | Name | Dir | Spec | Payload | Notes |")
	_, _ = fmt.Fprintln(w, "|---|---|---|---|---|---|")
	for _, r := range rows {
		_, _ = fmt.Fprintf(w, "| `%s` | %s | %s | %s | %s | %s |\n",
			r.Address, r.Name, r.Direction, r.SpecRef, r.PayloadHint, r.Notes)
	}
	return nil
}

func renderCatalogueRowDetail(w io.Writer, proto, addr string, r catalogueRow) error {
	_, _ = fmt.Fprintf(w, "%s · %s\n", proto, addr)
	_, _ = fmt.Fprintln(w, strings.Repeat("=", 60))
	_, _ = fmt.Fprintf(w, "Name:       %s\n", r.Name)
	if r.Direction != "" {
		_, _ = fmt.Fprintf(w, "Direction:  %s\n", r.Direction)
	}
	if r.SpecRef != "" {
		_, _ = fmt.Fprintf(w, "Spec:       %s\n", r.SpecRef)
	}
	if r.PayloadHint != "" {
		_, _ = fmt.Fprintf(w, "Payload:    %s\n", r.PayloadHint)
	}
	if r.Notes != "" {
		_, _ = fmt.Fprintf(w, "Notes:      %s\n", r.Notes)
	}
	_, _ = fmt.Fprintf(w, "Supported:  %t\n", r.Supported)
	return nil
}

func printListCommandsHelp(w io.Writer) {
	_, _ = fmt.Fprintln(w, `dhs list-commands — enumerate the static command catalogue for a protocol

USAGE
  dhs list-commands <proto> [--format=table|json|md]

PROTOCOLS
  acp1           message-types, methods, object groups, object types, error codes
  acp2           message-types, funcs, object types, property IDs, number types, error stat codes
  emberplus      Glow element kinds + Command verbs (Parameter, Node, Function, Matrix, …)
  probel-sw02p   wire byte commands (rx/tx)
  probel-sw08p   wire byte commands (rx/tx)

EXAMPLES
  dhs list-commands probel-sw02p
  dhs list-commands acp2 --format=json
  dhs list-commands emberplus --format=md

PAIRED VERB
  dhs help-cmd <proto> <address>   drill into one entry`)
}

func printHelpCmdHelp(w io.Writer) {
	_, _ = fmt.Fprintln(w, `dhs help-cmd — print full detail for one catalogue entry

USAGE
  dhs help-cmd <proto> <address>

ADDRESS FORMAT (per protocol)
  probel-sw02p / -sw08p   byte: 0x02, 0xFF, or decimal 2 / 255
  acp1                    <kind>:<id>  (kinds: msgtype, method, objgroup, objtype, xport-err, obj-err)
  acp2                    <kind>:<id>  (kinds: an2-type, an2-func, acp2-type, acp2-func, obj-type, pid, number-type, err-stat)
  emberplus               <kind>:<name>  (kind:Parameter, cmd:GetDirectory)
                          OR  numeric OID path  (1.2.4.1.0.2)
                          OR  dotted label path (root.foo.bar)

EXAMPLES
  dhs help-cmd probel-sw02p 0x02
  dhs help-cmd acp1 method:0
  dhs help-cmd acp2 pid:4
  dhs help-cmd emberplus kind:Parameter
  dhs help-cmd emberplus 1.2.4.1.0.2

PAIRED VERB
  dhs list-commands <proto>   show every entry`)
}
