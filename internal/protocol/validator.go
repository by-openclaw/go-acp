package protocol

import (
	"context"

	"dhs/internal/wiretrace"
)

// Validator is the optional capability a connector implements when it
// can decode a captured wire-trace through its codec offline (per
// ADR-0021), without a live peer. The CLI's `validate` verb (per
// ADR-0002) detects this via type assertion on the consumer plugin and
// returns ErrNotImplemented for connectors that have not yet been
// migrated.
//
// `replay` (per ADR-0021's `--as-client` / `--as-server` modes) is a
// separate, deferred capability — Validator is the today scope.
//
// Naming: the input type is `wiretrace.Trame`, deliberately NOT
// `Frame`. See wiretrace/record.go for the rationale.
type Validator interface {
	Validate(ctx context.Context, trames []wiretrace.Trame, opts ValidateOpts) (*ValidateReport, error)
}

// ValidateOpts configures one Validate run. Zero-valued opts means
// "decode every trame, report counts + invariants, write nothing".
type ValidateOpts struct {
	// OutTree is an optional path. When non-empty, the connector
	// canonicalises the decoded sequence and writes a tree.json there
	// (same shape as `dhs export --format json`).
	OutTree string

	// OutParams is an optional path. When non-empty, the connector
	// canonicalises and writes a params dump (CSV / JSON, dispatched
	// by file extension).
	OutParams string

	// StopAt is an optional Trame.Note marker. When non-empty, decoding
	// halts at the first trame whose Note matches and StoppedAt is
	// populated in the report.
	StopAt string
}

// ValidateReport summarises one Validate run.
type ValidateReport struct {
	// TramesProcessed is the total decoded successfully.
	TramesProcessed int

	// PerDirection counts decoded trames keyed by direction.
	PerDirection map[wiretrace.Direction]int

	// Errors are decode failures (one per offending trame).
	Errors []ValidateError

	// Invariants are human-readable invariant violations the connector
	// caught while decoding (e.g. ACP1 "request with MTID=0").
	Invariants []string

	// StoppedAt is the Note marker that triggered an early halt, or
	// empty if decoding ran to completion.
	StoppedAt string
}

// ValidateError describes one trame the connector could not decode.
type ValidateError struct {
	// TrameIndex is the zero-based index in the input trames slice.
	TrameIndex int

	// Direction is the trame's tx/rx tag.
	Direction wiretrace.Direction

	// HexPrefix is the first ≤16 bytes hex-encoded so log readers can
	// orient without dumping the whole trame.
	HexPrefix string

	// Err is the codec's error message.
	Err string
}
