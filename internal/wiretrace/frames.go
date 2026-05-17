// Package wiretrace reads and writes the wire-trace JSONL contract
// defined in docs/adr/0021-wire-trace-jsonl-contract.md. One JSON
// object per line, one Trame per line, UTF-8 + LF.
//
// Three callers consume this package: the `--capture` flag (live writer),
// the `validate` verb (offline reader; see ADR-0002), and codec test
// harnesses (replay-style decoder smoke tests). All three share this
// one library so the line schema cannot drift between uses.
//
// Stdlib only — this package is lift-to-own-repo ready per ADR-0006.
// In particular, wiretrace does NOT depend on internal/consumer so the
// dependency arrow runs protocol → wiretrace and not the other way.
package wiretrace

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// SchemaVersion is the wire-trace contract version this package reads
// and writes. Bumps on a breaking change to required fields per
// ADR-0021. Readers MUST reject lines whose schema_version is HIGHER
// than this constant.
const SchemaVersion = 1

// ReadTrames decodes a frames.jsonl stream into a slice of Trame.
// Empty lines are skipped. Any line whose schema_version exceeds
// SchemaVersion or whose required fields (dir, hex) are missing or
// invalid yields a parse error that includes the line number for
// debuggability.
//
// Captured traces are local-only per ADR-0021 (no LFS, no committed
// blobs); callers handle a missing file with os.IsNotExist before
// reaching this function.
func ReadTrames(r io.Reader) ([]Trame, error) {
	sc := bufio.NewScanner(r)
	// Some captured trames are large; allow up to 1 MB per line.
	sc.Buffer(make([]byte, 64*1024), 1024*1024)

	var trames []Trame
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var t Trame
		if err := json.Unmarshal(line, &t); err != nil {
			return nil, fmt.Errorf("wiretrace: line %d: %w", lineNo, err)
		}
		if t.SchemaVersion > SchemaVersion {
			return nil, fmt.Errorf("wiretrace: line %d: schema_version %d exceeds reader's %d", lineNo, t.SchemaVersion, SchemaVersion)
		}
		if t.Hex == "" {
			return nil, fmt.Errorf("wiretrace: line %d: required field `hex` missing", lineNo)
		}
		if t.Direction != DirectionTx && t.Direction != DirectionRx {
			return nil, fmt.Errorf("wiretrace: line %d: required field `dir` must be %q or %q, got %q",
				lineNo, DirectionTx, DirectionRx, t.Direction)
		}
		trames = append(trames, t)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("wiretrace: scan: %w", err)
	}
	return trames, nil
}

// WriteTrames serialises trames as JSONL per ADR-0021. SchemaVersion
// is auto-set to the package constant when zero on input so writers
// can populate it incrementally. One JSON object per line, no
// pretty-printing, UTF-8 + LF.
func WriteTrames(w io.Writer, trames []Trame) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for i, t := range trames {
		if t.SchemaVersion == 0 {
			t.SchemaVersion = SchemaVersion
		}
		if err := enc.Encode(&t); err != nil {
			return fmt.Errorf("wiretrace: write trame %d: %w", i, err)
		}
	}
	return nil
}
