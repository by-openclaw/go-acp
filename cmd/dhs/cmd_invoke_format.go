package main

import (
	"fmt"
	"strings"
)

// formatGetSalvoHuman pretty-prints a getSalvo wire-result string
// (R11 #482). The provider serializes the salvo as
//
//	"tgt=src;tgt=src,src,src;..."
//
// — semicolon between targets, comma between sources within a target.
// Output lines, one per target:
//
//	tgt 0 <- Src [0]
//	tgt 1 <- Src [1]
//	tgt 5 <- Src [3,4,5]
//	tgt 7 <- Src []         (empty target — no sources)
//
// Returns "(empty salvo)" for an empty input.
//
// The parser is lenient — malformed rows are passed through verbatim
// (prefixed with "row: ") so operators see whatever the provider emitted
// rather than the formatter silently dropping a row.
func formatGetSalvoHuman(serialized string) string {
	serialized = strings.TrimSpace(serialized)
	if serialized == "" {
		return "(empty salvo)"
	}
	rows := strings.Split(serialized, ";")
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		row = strings.TrimSpace(row)
		if row == "" {
			continue
		}
		eq := strings.IndexByte(row, '=')
		if eq < 0 {
			// Malformed — pass through so the operator sees the raw shape.
			out = append(out, fmt.Sprintf("row: %s", row))
			continue
		}
		tgt := strings.TrimSpace(row[:eq])
		srcs := strings.TrimSpace(row[eq+1:])
		if srcs == "" {
			out = append(out, fmt.Sprintf("tgt %s <- Src []", tgt))
			continue
		}
		out = append(out, fmt.Sprintf("tgt %s <- Src [%s]", tgt, srcs))
	}
	if len(out) == 0 {
		return "(empty salvo)"
	}
	return strings.Join(out, "\n")
}
